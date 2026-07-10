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

package db

// InsertFile creates a new row in the files table (raw content isn't stored
// here - it lives in S3, keyed by the returned file id, via PutContentFile).
// psize exists for parity with the original recovered-Toybox data (it was
// historically a "packed size" hint shown in the old GM12 client); for
// freshly uploaded content there's no separate compressed representation,
// so callers should just pass the same value as size.
func InsertFile(path string, size int, psize int) (int, error) {
	r, err := handle.Exec("INSERT INTO files (path, size, psize) VALUES (?, ?, ?)", path, size, psize)
	if err != nil {
		return 0, err
	}

	i, err := r.LastInsertId()
	if err != nil {
		return 0, err
	}

	return int(i), nil
}

// InsertContentLink associates an existing file with a package. This
// matches the exact shape FetchPackage already queries against:
//
//	SELECT f.id, f.path, f.size, f.psize FROM files f
//	JOIN content c ON f.id = c.fileid WHERE c.id = ?
//
// i.e. the content table is a simple (id -> package id, fileid -> files.id)
// junction table with no separate revision column - content is not
// re-scoped per revision in the existing schema, so new uploads follow
// the same convention.
func InsertContentLink(packageID int, fileID int) error {
	_, err := handle.Exec("INSERT INTO content (id, fileid) VALUES (?, ?)", packageID, fileID)
	if err != nil {
		return err
	}

	return nil
}

// DeletePackage removes a package and all of its content (files + the
// content junction rows). Deliberately does NOT touch other packages that
// might reference the same file id - content files aren't currently
// deduplicated/shared across packages by the upload path, so this is safe
// for what this endpoint is scoped to (self-uploaded "addon" packages
// only - see api/addons/delete.go).
func DeletePackage(id int) error {
	rows, err := handle.Query("SELECT fileid FROM content WHERE id = ?", id)
	if err != nil {
		return err
	}

	var fileIDs []int
	for rows.Next() {
		var fileID int

		err = rows.Scan(&fileID)
		if err != nil {
			rows.Close()
			return err
		}

		fileIDs = append(fileIDs, fileID)
	}

	rows.Close()

	_, err = handle.Exec("DELETE FROM content WHERE id = ?", id)
	if err != nil {
		return err
	}

	for _, fileID := range fileIDs {
		_, err = handle.Exec("DELETE FROM files WHERE id = ?", fileID)
		if err != nil {
			return err
		}
	}

	_, err = handle.Exec("DELETE FROM packages WHERE id = ?", id)
	if err != nil {
		return err
	}

	return nil
}
