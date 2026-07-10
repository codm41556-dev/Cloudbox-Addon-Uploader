/*
	cloudbox - the toybox server emulator
	Copyright (C) 2024-2025  patapancakes <patapancakes@pagefault.games>

	This program is free software: you can redistribute it and/or modify
	it under the terms of the GNU Affero General Public License as published by
	the Free Software Foundation, either version 3 of the License, or
	(at your option) any later version.

	This program is distributed in the hope that it will be useful,
	but WITHOUT ANY WARRANTY; without even the implied warranty of
	MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
	GNU Affero General Public License for more details.

	You should have received a copy of the GNU Affero General Public License
	along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

// This file adds standard "Sign in through Steam" web login (OpenID 2.0),
// completely separate from the legacy in-game ticket handshake that
// ingame/toyboxapi/auth.go emulates. That flow is driven by the GMod game
// engine itself (there's no Lua-level way to request a fresh ticket on
// demand), so it can't be reused for a browser-based upload form. OpenID
// is the same mechanism most Steam-integrated websites use for "log in
// with Steam" and needs no game client running at all.
//
// On success we mint a session ticket using the exact same scheme/table
// the in-game flow already uses (db.InsertLogin / db.FetchSteamIDFromTicket),
// so api/addons/upload.go can authenticate identically either way.
package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/flatgrassdotnet/cloudbox/db"
	"github.com/flatgrassdotnet/cloudbox/utils"
)

const defaultReturnPath = "/addons/upload"

var claimedIDPattern = regexp.MustCompile(`^https://steamcommunity\.com/openid/id/(\d+)$`)

func requestScheme(r *http.Request) string {
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return "https"
	}

	return "http"
}

// sanitizeReturnPath only allows same-site relative paths, so this can't be
// abused as an open redirect.
func sanitizeReturnPath(path string) string {
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return defaultReturnPath
	}

	return path
}

// SteamLoginStart redirects the browser to Steam's OpenID login page.
// Query param "return" (a same-site path) is where to send the user back
// to afterwards; defaults to the addon upload page.
func SteamLoginStart(w http.ResponseWriter, r *http.Request) {
	returnPath := sanitizeReturnPath(r.URL.Query().Get("return"))

	realm := fmt.Sprintf("%s://%s", requestScheme(r), r.Host)
	callback := fmt.Sprintf("%s/auth/steamlogin/callback?return=%s", realm, url.QueryEscape(returnPath))

	v := url.Values{}
	v.Set("openid.ns", "http://specs.openid.net/auth/2.0")
	v.Set("openid.mode", "checkid_setup")
	v.Set("openid.return_to", callback)
	v.Set("openid.realm", realm)
	v.Set("openid.identity", "http://specs.openid.net/auth/2.0/identifier_select")
	v.Set("openid.claimed_id", "http://specs.openid.net/auth/2.0/identifier_select")

	http.Redirect(w, r, "https://steamcommunity.com/openid/login?"+v.Encode(), http.StatusFound)
}

// SteamLoginCallback verifies Steam's OpenID response ("dumb mode" - a
// stateless server-to-server check, no session needed across requests),
// mints a cloudbox ticket for the authenticated steamid, and redirects
// back to returnPath with the ticket in the URL FRAGMENT so it's visible
// to page JS only and never sent to any server or logged anywhere.
func SteamLoginCallback(w http.ResponseWriter, r *http.Request) {
	returnPath := sanitizeReturnPath(r.URL.Query().Get("return"))

	err := r.ParseForm()
	if err != nil {
		utils.WriteError(w, r, fmt.Sprintf("failed to parse openid response: %s", err))
		return
	}

	verify := make(url.Values)
	for k, vals := range r.Form {
		verify[k] = vals
	}
	verify.Set("openid.mode", "check_authentication")

	resp, err := http.PostForm("https://steamcommunity.com/openid/login", verify)
	if err != nil {
		utils.WriteError(w, r, fmt.Sprintf("failed to verify openid response: %s", err))
		return
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		utils.WriteError(w, r, fmt.Sprintf("failed to read openid verification response: %s", err))
		return
	}

	if !strings.Contains(string(body), "is_valid:true") {
		http.Error(w, "steam login could not be verified", http.StatusUnauthorized)
		return
	}

	matches := claimedIDPattern.FindStringSubmatch(r.Form.Get("openid.claimed_id"))
	if matches == nil {
		http.Error(w, "couldn't parse steamid from openid response", http.StatusUnauthorized)
		return
	}

	steamid := matches[1]

	// warms the profile cache the same way the in-game login does, so the
	// uploader's name/avatar are available immediately. Not fatal if it
	// fails (e.g. no -apikey configured yet, or Steam's API hiccups) -
	// the ticket/login itself doesn't depend on this, only the cosmetic
	// display name does.
	_, err = utils.GetPlayerSummaries(steamid)
	if err != nil {
		log.Printf("steamlogin: failed to warm profile cache for %s (continuing anyway): %s", steamid, err)
	}

	ticket := make([]byte, 24)
	_, err = rand.Read(ticket)
	if err != nil {
		utils.WriteError(w, r, fmt.Sprintf("failed to generate ticket: %s", err))
		return
	}

	err = db.InsertLogin(steamid, "", ticket)
	if err != nil {
		utils.WriteError(w, r, fmt.Sprintf("failed to insert login: %s", err))
		return
	}

	dest := fmt.Sprintf("%s#ticket=%s", returnPath, url.QueryEscape(base64.StdEncoding.EncodeToString(ticket)))

	http.Redirect(w, r, dest, http.StatusFound)
}
