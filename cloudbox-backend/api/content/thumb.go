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

package content

import (
	"io"
	"net/http"
	"strconv"

	"github.com/flatgrassdotnet/cloudbox/db"
	"github.com/flatgrassdotnet/cloudbox/utils"
)

// Thumb serves the stored 128x128 PNG thumbnail for a package id directly from
// the object-storage bucket (flatgrass-toybox-image), so the frontend can show
// real icons for self-uploaded addons without depending on the external
// img.cl0udb0x.com CDN existing - which matters for self-hosted / locally
// tested setups where the S3 bucket (e.g. MinIO) has no CDN in front of it.
//
// Thumbnails are written by api/addons/upload.go (and, for the legacy save
// flow, ingame/publishsave/publish.go). Recovered Toybox content keeps using
// the CDN URL directly (see forecaster's browser.go), so this endpoint only
// needs to exist for addon-type packages - but it works for any id that has a
// stored thumbnail.
func Thumb(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil {
		utils.WriteError(w, r, "failed to parse id value")
		return
	}

	o, err := db.GetThumbnail(r.Context(), id)
	if err != nil {
		// treat any S3 error (incl. NoSuchKey) as a plain 404 so the browser
		// falls back to the CSS default icon rather than showing an error page
		http.NotFound(w, r)
		return
	}
	defer o.Body.Close()

	w.Header().Set("Content-Type", "image/png")
	// thumbnails are immutable per package id (a new upload gets a new id), so
	// let the browser cache them aggressively to keep the spawn menu snappy
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	if o.ContentLength != nil {
		w.Header().Set("Content-Length", strconv.Itoa(int(*o.ContentLength)))
	}

	io.Copy(w, o.Body)
}
