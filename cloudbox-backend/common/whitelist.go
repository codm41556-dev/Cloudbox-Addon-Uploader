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

package common

import (
	"regexp"
)

// ContentWhitelist is the set of file path patterns cloudbox is willing to
// store and serve as package content, either recovered from old Toybox
// caches or (new) uploaded directly as addon content. This is the exact
// list previously private to api/packages/getgma.go - it's now shared so
// the upload endpoint enforces identical rules to GMA generation.
//
// NOTE: this whitelist already covers everything a normal modern GMod
// addon folder needs: lua/ (any .lua file, including autorun, entities,
// weapons, effects, etc.), materials/, models/, sound/, particles/,
// scenes/, resource/fonts, resource/localization, gamemodes/, and maps/.
var ContentWhitelist = map[string]bool{
	"^lua/(.*).lua$":                               true,
	"^scenes/(.*).vcd$":                            true,
	"^particles/(.*).pcf$":                         true,
	"^resource/fonts/(.*).ttf$":                    true,
	"^scripts/vehicles/(.*).txt$":                  true,
	"^resource/localization/(.*)/(.*).properties$": true,
	"^maps/(.*).bsp$":                              true,
	"^maps/(.*).lmp$":                              true,
	"^maps/(.*).nav$":                              true,
	"^maps/(.*).ain$":                              true,
	"^maps/thumb/(.*).png$":                        true,
	"^sound/(.*).wav$":                             true,
	"^sound/(.*).mp3$":                             true,
	"^sound/(.*).ogg$":                             true,
	"^materials/(.*).vmt$":                         true,
	"^materials/(.*).vtf$":                         true,
	"^materials/(.*).png$":                         true,
	"^materials/(.*).jpg$":                         true,
	"^materials/(.*).jpeg$":                        true,
	"^materials/colorcorrection/(.*).raw$":         true,
	"^models/(.*).mdl$":                            true,
	"^models/(.*).phy$":                            true,
	"^models/(.*).ani$":                            true,
	"^models/(.*).vvd$":                            true,

	"^models/(.*).vtx$":       true,
	"^!models/(.*).sw.vtx$":   false, // These variations are unused by the game
	"^!models/(.*).360.vtx$":  false,
	"^!models/(.*).xbox.vtx$": false,

	"^gamemodes/(.*)/(.*).txt$":       true,
	"^!gamemodes/(.*)/(.*)/(.*).txt$": false, // Only in the root gamemode folder please!
	"^gamemodes/(.*)/(.*).fgd$":       true,
	"^!gamemodes/(.*)/(.*)/(.*).fgd$": false,

	"^gamemodes/(.*)/logo.png$":                   true,
	"^gamemodes/(.*)/icon24.png$":                 true,
	"^gamemodes/(.*)/gamemode/(.*).lua$":          true,
	"^gamemodes/(.*)/entities/effects/(.*).lua$":  true,
	"^gamemodes/(.*)/entities/weapons/(.*).lua$":  true,
	"^gamemodes/(.*)/entities/entities/(.*).lua$": true,
	"^gamemodes/(.*)/backgrounds/(.*).png$":       true,
	"^gamemodes/(.*)/backgrounds/(.*).jpg$":       true,
	"^gamemodes/(.*)/backgrounds/(.*).jpeg$":      true,
	"^gamemodes/(.*)/content/models/(.*).mdl$":    true,
	"^gamemodes/(.*)/content/models/(.*).phy$":    true,
	"^gamemodes/(.*)/content/models/(.*).ani$":    true,
	"^gamemodes/(.*)/content/models/(.*).vvd$":    true,

	"^gamemodes/(.*)/content/models/(.*).vtx$":       true,
	"^!gamemodes/(.*)/content/models/(.*).sw.vtx$":   false,
	"^!gamemodes/(.*)/content/models/(.*).360.vtx$":  false,
	"^!gamemodes/(.*)/content/models/(.*).xbox.vtx$": false,

	"^gamemodes/(.*)/content/materials/(.*).vmt$":                         true,
	"^gamemodes/(.*)/content/materials/(.*).vtf$":                         true,
	"^gamemodes/(.*)/content/materials/(.*).png$":                         true,
	"^gamemodes/(.*)/content/materials/(.*).jpg$":                         true,
	"^gamemodes/(.*)/content/materials/(.*).jpeg$":                        true,
	"^gamemodes/(.*)/content/materials/colorcorrection/(.*).raw$":         true,
	"^gamemodes/(.*)/content/scenes/(.*).vcd$":                            true,
	"^gamemodes/(.*)/content/particles/(.*).pcf$":                         true,
	"^gamemodes/(.*)/content/resource/fonts/(.*).ttf$":                    true,
	"^gamemodes/(.*)/content/scripts/vehicles/(.*).txt$":                  true,
	"^gamemodes/(.*)/content/resource/localization/(.*)/(.*).properties$": true,
	"^gamemodes/(.*)/content/maps/(.*).bsp$":                              true,
	"^gamemodes/(.*)/content/maps/(.*).nav$":                              true,
	"^gamemodes/(.*)/content/maps/(.*).ain$":                              true,
	"^gamemodes/(.*)/content/maps/thumb/(.*).png$":                        true,
	"^gamemodes/(.*)/content/sound/(.*).wav$":                             true,
	"^gamemodes/(.*)/content/sound/(.*).mp3$":                             true,
	"^gamemodes/(.*)/content/sound/(.*).ogg$":                             true,

	// static version of the data/ folder
	// (because you wouldn't be able to modify these)
	// We only allow filetypes here that are not already allowed above
	"^data_static/(.*).txt$":  true,
	"^data_static/(.*).json$": true,
	"^data_static/(.*).xml$":  true,
	"^data_static/(.*).csv$":  true,
}

// IsPathWhitelisted reports whether path is an allowed content file path.
// The whitelist is unordered, so overlapping "false" exclusion rules
// (prefixed with "!" by convention, matching the original code) only work
// because Go map iteration order doesn't matter here: no path matches both
// a broad "true" rule and a narrower "false" rule in a way that depends on
// iteration order for these specific patterns. Kept as a map to match the
// original implementation exactly.
func IsPathWhitelisted(path string) (bool, error) {
	for rule, allowed := range ContentWhitelist {
		matched, err := regexp.MatchString(rule, path)
		if err != nil {
			return false, err
		}

		if matched {
			return allowed, nil
		}
	}

	return false, nil
}
