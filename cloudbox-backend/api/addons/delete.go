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

package addons

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/flatgrassdotnet/cloudbox/db"
)

// Delete removes one of the caller's own self-uploaded addon packages.
// Deliberately restricted to type == "addon": this must never become a way
// to delete recovered Toybox content, even if a package's author field
// happens to match the caller's steamid for some unrelated reason (e.g.
// that field being reused/repurposed later, or old data quirks) - "addon"
// packages only exist via this same upload flow, so scoping to that type
// is a hard guarantee, not just a convention.
func Delete(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Ticket, Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	ticket, err := base64.StdEncoding.DecodeString(r.Header.Get("Ticket"))
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, uploadResult{Error: "missing or malformed Ticket header - sign in through Steam first"})
		return
	}

	steamid, err := db.FetchSteamIDFromTicket(ticket)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, uploadResult{Error: "not signed in (or session expired) - sign in through Steam again"})
		return
	}

	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, uploadResult{Error: "missing or invalid id"})
		return
	}

	rev, err := db.FetchPackageLatestRevision(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, uploadResult{Error: "package not found"})
		return
	}

	pkg, err := db.FetchPackage(id, rev)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, uploadResult{Error: "package not found"})
			return
		}

		writeJSON(w, http.StatusInternalServerError, uploadResult{Error: fmt.Sprintf("failed to fetch package: %s", err)})
		return
	}

	if pkg.Type != "addon" {
		writeJSON(w, http.StatusForbidden, uploadResult{Error: "only self-uploaded addon packages can be deleted here"})
		return
	}

	if pkg.Author != steamid {
		writeJSON(w, http.StatusForbidden, uploadResult{Error: "you can only delete your own uploads"})
		return
	}

	for _, item := range pkg.Content {
		// best-effort: an S3 hiccup here shouldn't block removing the
		// database record, which is the part that actually controls
		// whether the package is still browsable/downloadable
		db.DeleteContentFile(r.Context(), item.ID)
	}

	// also remove the stored thumbnail (icon) so it doesn't linger in the
	// image bucket after the package row is gone - same best-effort policy
	db.DeleteThumbnail(r.Context(), id)

	err = db.DeletePackage(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, uploadResult{Error: fmt.Sprintf("failed to delete package: %s", err)})
		return
	}

	writeJSON(w, http.StatusOK, uploadResult{OK: true, ID: id, Name: pkg.Name})
}
