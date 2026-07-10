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

// This is the new, self-serve alternative to the recovered-Toybox-cache
// pipeline: instead of a package's content being manually imported from an
// old GMod install, anyone signed in through Steam can upload a zip of a
// folder-based addon (lua/materials/models/sound/particles/...) and it
// becomes a normal "addon"-type package, reusing the exact same
// content/GMA/mounting pipeline every other package already uses.
package addons

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/flatgrassdotnet/cloudbox/common"
	"github.com/flatgrassdotnet/cloudbox/db"
)

const (
	maxUploadSize        = 512 << 20  // total request body cap
	maxFileCount         = 20000      // zip bomb guard: entry count
	maxSingleFileSize    = 256 << 20  // zip bomb guard: per-file uncompressed size
	maxTotalUncompressed = 1536 << 20 // zip bomb guard: total uncompressed size
	maxThumbSize         = 2 << 20    // thumbnail image cap (PNG/JPG), 2 MiB
)

type uploadResult struct {
	OK          bool     `json:"ok"`
	ID          int      `json:"id,omitempty"`
	Name        string   `json:"name,omitempty"`
	Accepted    []string `json:"accepted,omitempty"`
	Rejected    []string `json:"rejected,omitempty"`
	ThumbStored bool     `json:"thumb,omitempty"`
	Error       string   `json:"error,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v uploadResult) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// Upload handles POST (the actual multipart upload) and OPTIONS (CORS
// preflight, since the upload page may not be served from this same
// origin - see pages/templates/addons/upload.html).
func Upload(w http.ResponseWriter, r *http.Request) {
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

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	err = r.ParseMultipartForm(32 << 20)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, uploadResult{Error: fmt.Sprintf("couldn't read the upload (it may be too large - limit is %d MiB): %s", maxUploadSize>>20, err)})
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = "Untitled Addon"
	}
	if len(name) > 128 {
		name = name[:128]
	}

	description := strings.TrimSpace(r.FormValue("description"))
	if len(description) > 2000 {
		description = description[:2000]
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, uploadResult{Error: "missing \"file\" field - should be a .zip of your addon folder"})
		return
	}
	defer file.Close()

	zr, err := zip.NewReader(file, header.Size)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, uploadResult{Error: fmt.Sprintf("not a valid zip file: %s", err)})
		return
	}

	if len(zr.File) > maxFileCount {
		writeJSON(w, http.StatusBadRequest, uploadResult{Error: fmt.Sprintf("too many files in the zip (%d, limit is %d)", len(zr.File), maxFileCount)})
		return
	}

	// create the package first so content can be linked to it as we go
	pkgID, err := db.InsertPackage(common.Package{
		Type:        "addon",
		Name:        name,
		Author:      steamid,
		Description: description,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, uploadResult{Error: fmt.Sprintf("failed to create package: %s", err)})
		return
	}

	var accepted, rejected []string
	var totalSize int64
	limitHit := false

	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}

		clean := normalizeZipPath(zf.Name)
		if clean == "" {
			rejected = append(rejected, fmt.Sprintf("%s (unsafe path, skipped)", zf.Name))
			continue
		}

		if limitHit {
			rejected = append(rejected, fmt.Sprintf("%s (skipped, upload already at size limit)", clean))
			continue
		}

		if int64(zf.UncompressedSize64) > maxSingleFileSize {
			rejected = append(rejected, fmt.Sprintf("%s (too large: %d bytes)", clean, zf.UncompressedSize64))
			continue
		}

		totalSize += int64(zf.UncompressedSize64)
		if totalSize > maxTotalUncompressed {
			rejected = append(rejected, fmt.Sprintf("%s (upload exceeds total size limit, rest skipped)", clean))
			limitHit = true
			continue
		}

		whitelisted, err := common.IsPathWhitelisted(clean)
		if err != nil || !whitelisted {
			rejected = append(rejected, fmt.Sprintf("%s (file type/location not allowed)", clean))
			continue
		}

		rc, err := zf.Open()
		if err != nil {
			rejected = append(rejected, fmt.Sprintf("%s (failed to read from zip: %s)", clean, err))
			continue
		}

		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			rejected = append(rejected, fmt.Sprintf("%s (failed to read from zip: %s)", clean, err))
			continue
		}

		fileID, err := db.InsertFile(clean, len(data), len(data))
		if err != nil {
			rejected = append(rejected, fmt.Sprintf("%s (failed to save: %s)", clean, err))
			continue
		}

		err = db.PutContentFile(r.Context(), fileID, bytes.NewReader(data))
		if err != nil {
			rejected = append(rejected, fmt.Sprintf("%s (failed to store content: %s)", clean, err))
			continue
		}

		err = db.InsertContentLink(pkgID, fileID)
		if err != nil {
			rejected = append(rejected, fmt.Sprintf("%s (failed to link content: %s)", clean, err))
			continue
		}

		accepted = append(accepted, clean)
	}

	if len(accepted) == 0 {
		writeJSON(w, http.StatusBadRequest, uploadResult{
			Error:    "nothing in the zip matched an allowed addon path (lua/, materials/, models/, sound/, particles/, gamemodes/, ...) - check your folder structure",
			Rejected: rejected,
		})
		return
	}

	// Store a thumbnail for this addon so it shows a real icon in the spawn
	// menu instead of the generic "missing" question-mark placeholder.
	// Priority: an explicit "thumb" image field > an icon file found inside
	// the zip (thumb.png / icon.png / logo.png) > a generated on-brand "cloud"
	// placeholder tinted by the addon name. Best-effort: if storage fails the
	// addon is still fully usable, it just falls back to the CSS default icon.
	thumbStored := storeAddonThumbnail(r.Context(), pkgID, name, r, zr)

	writeJSON(w, http.StatusOK, uploadResult{
		OK:          true,
		ID:          pkgID,
		Name:        name,
		Accepted:    accepted,
		Rejected:    rejected,
		ThumbStored: thumbStored,
	})
}

// normalizeZipPath converts a zip entry name into a clean, forward-slash,
// lowercase, non-traversing relative path - or "" if it's unsafe. GMod's
// content whitelist patterns are all lowercase, and addon folders are
// conventionally lowercase anyway, so lowercasing here is both a "zip
// slip" (path traversal) guard and a forgiving normalization for zips
// built on case-insensitive filesystems (e.g. Windows).
func normalizeZipPath(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimPrefix(name, "/")
	name = strings.ToLower(name)

	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return ""
	}

	return clean
}

// storeAddonThumbnail writes a 128x128 PNG thumbnail for the freshly created
// package id so the spawn menu shows a real icon. It returns true if a
// thumbnail was successfully stored. See the Upload handler for the priority
// order (explicit field > zip-embedded icon > generated placeholder).
func storeAddonThumbnail(ctx context.Context, pkgID int, name string, r *http.Request, zr *zip.Reader) bool {
	// 1. an explicit "thumb" image field uploaded alongside the zip
	if tf, _, err := r.FormFile("thumb"); err == nil {
		data, err := io.ReadAll(io.LimitReader(tf, maxThumbSize))
		tf.Close()
		if err == nil && len(data) > 0 {
			if png, err := toPNG(data); err == nil {
				if err := db.PutThumbnail(ctx, pkgID, bytes.NewReader(png)); err == nil {
					return true
				}
			}
		}
	}

	// 2. a recognizable icon file already inside the zip (thumb.png /
	//    icon.png / logo.png, case-insensitive, anywhere in the tree)
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		base := strings.ToLower(path.Base(zf.Name))
		if base != "thumb.png" && base != "icon.png" && base != "logo.png" {
			continue
		}
		if int64(zf.UncompressedSize64) > maxThumbSize {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			continue
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil || len(data) == 0 {
			continue
		}
		if png, err := toPNG(data); err == nil {
			if err := db.PutThumbnail(ctx, pkgID, bytes.NewReader(png)); err == nil {
				return true
			}
		}
	}

	// 3. a generated on-brand "cloud" placeholder, tinted by the addon name so
	//    different addons get visually distinct tiles (and never the boring
	//    question-mark fallback)
	png, err := generatePlaceholderThumb(name)
	if err != nil {
		return false
	}
	if err := db.PutThumbnail(ctx, pkgID, bytes.NewReader(png)); err == nil {
		return true
	}
	return false
}

// toPNG decodes a PNG or JPEG image and re-encodes it as PNG, since the
// thumbnail bucket key is <id>_thumb_128.png and every consumer expects PNG
// bytes. (The blank import of image/jpeg above registers the JPEG decoder.)
func toPNG(data []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// generatePlaceholderThumb renders a 128x128 PNG: a solid background tinted by
// a hash of the addon name, with a white "cloudbox" cloud shape on top. Pure
// stdlib (image/image/png/image/color/hash/fnv) - no font or external asset
// dependency - so it works in any build environment. Each addon gets a
// distinct-ish colored tile, which is recognizably an icon rather than the
// generic "missing" question mark.
func generatePlaceholderThumb(name string) ([]byte, error) {
	const size = 128
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	// pick a background color from a pleasant palette, seeded by the name so
	// the same addon always gets the same color
	h := fnv.New32a()
	h.Write([]byte(name))
	palette := []color.RGBA{
		{64, 150, 238, 255}, // cloudbox blue
		{231, 76, 60, 255},  // red
		{46, 204, 113, 255}, // green
		{155, 89, 182, 255}, // purple
		{241, 196, 15, 255}, // yellow
		{230, 126, 34, 255}, // orange
		{26, 188, 156, 255}, // teal
		{52, 152, 219, 255}, // light blue
	}
	bg := palette[h.Sum32()%uint32(len(palette))]

	fillRect(img, 0, 0, size, size, bg)

	// white cloud: three overlapping circles + a base bar (on-brand for
	// "Cloud"box, and simple enough to draw without any font/text support)
	white := color.RGBA{255, 255, 255, 255}
	fillCircle(img, 46, 76, 22, white)
	fillCircle(img, 68, 66, 28, white)
	fillCircle(img, 90, 76, 22, white)
	fillRect(img, 44, 76, 92, 92, white)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fillRect(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	b := img.Bounds()
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}

func fillCircle(img *image.RGBA, cx, cy, r int, c color.RGBA) {
	b := img.Bounds()
	r2 := r * r
	for y := cy - r; y <= cy+r; y++ {
		if y < b.Min.Y || y >= b.Max.Y {
			continue
		}
		for x := cx - r; x <= cx+r; x++ {
			if x < b.Min.X || x >= b.Max.X {
				continue
			}
			dx := x - cx
			dy := y - cy
			if dx*dx+dy*dy <= r2 {
				img.SetRGBA(x, y, c)
			}
		}
	}
}
