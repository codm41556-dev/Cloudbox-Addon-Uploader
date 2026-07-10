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
	"fmt"
	"html/template"
	"net/http"

	"github.com/flatgrassdotnet/cloudbox/pages"
	"github.com/flatgrassdotnet/cloudbox/utils"
)

var pageTemplate = template.Must(template.New("upload.html").ParseFS(pages.TemplatesFS, "addons/*.html"))

type pageData struct {
	APIURL string
}

// Page serves the self-contained "upload an addon" web page: Steam login
// button, then a form that POSTs directly to Upload above. Served by the
// backend itself (rather than by forecaster) so the whole flow - Steam
// OpenID login, ticket, and the upload POST - stays same-origin and needs
// no CORS/cookie-sharing gymnastics across two different domains.
func Page(w http.ResponseWriter, r *http.Request) {
	err := pageTemplate.Execute(w, pageData{
		APIURL: fmt.Sprintf("%s://%s", requestScheme(r), r.Host),
	})
	if err != nil {
		utils.WriteError(w, r, fmt.Sprintf("failed to execute template: %s", err))
		return
	}
}

func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}

	if r.Header.Get("X-Forwarded-Proto") == "https" {
		return "https"
	}

	return "http"
}
