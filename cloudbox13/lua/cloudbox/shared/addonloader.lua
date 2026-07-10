/*
	cloudbox13 - cloudbox client for gmod 13
	Copyright (C) 2024 - 2025  patapancakes <patapancakes@pagefault.games>

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

/*
	This is the generic loader for Cloudbox package type "addon": a
	folder-based, modern-style GMod addon (as opposed to the single
	RunString'd script the old "entity"/"weapon" Toybox types use).

	Once MountCloudboxPackage() has mounted the package's GMA into the
	"GAME" search path, this walks it using the same conventions the real
	GMod engine uses to auto-load a normal installed/subscribed addon:

	  lua/autorun/*.lua          -> included on both realms
	  lua/autorun/client/*.lua   -> included on CLIENT only
	  lua/autorun/server/*.lua   -> included on SERVER only
	  lua/entities/<name>/...    -> registered via scripted_ents.Register
	  lua/weapons/<name>/...     -> registered via weapons.Register
	  lua/effects/<name>.lua     -> registered via effects.Register

	This intentionally mirrors publicly documented GMod addon-loading
	conventions (see the GMod wiki's "Addon Structure" / "Automatically
	Executed Lua Files" pages) - it's not reverse engineering anything
	private, just replicating documented behavior so content mounted mid
	session (rather than present at map load, which is the only time the
	engine itself does this scan) still gets loaded.

	Known limitations (v1): matproxies (lua/matproxy), custom derma skins
	registered outside of autorun, and postprocess effect auto-registration
	are not specially handled - in practice almost everything real addons
	do lives in autorun anyway, so this covers the large majority of
	addons. PRs / further Cloudbox-side work could extend this.
*/

CloudboxLoadedAddons = CloudboxLoadedAddons or {}

local function safeInclude(path, context)
	local ok, err = pcall(include, path)
	if !ok then
		print("[Cloudbox] addon " .. context .. ": error including " .. path .. ": " .. tostring(err))
	end
end

local function loadAutorun()
	// shared: runs on both realms
	local files = file.Find("lua/autorun/*.lua", "GAME")
	for _, f in pairs(files) do
		safeInclude("autorun/" .. f, "autorun")
	end

	if CLIENT then
		local clientFiles = file.Find("lua/autorun/client/*.lua", "GAME")
		for _, f in pairs(clientFiles) do
			safeInclude("autorun/client/" .. f, "autorun/client")
		end
	end

	if SERVER then
		local serverFiles = file.Find("lua/autorun/server/*.lua", "GAME")
		for _, f in pairs(serverFiles) do
			safeInclude("autorun/server/" .. f, "autorun/server")
		end
	end
end

local function loadEntities()
	local _, folders = file.Find("lua/entities/*", "GAME")

	for _, classname in pairs(folders) do
		local base = "entities/" .. classname .. "/"

		local hasShared = file.Exists("lua/" .. base .. "shared.lua", "GAME")
		local hasInit = file.Exists("lua/" .. base .. "init.lua", "GAME")
		local hasClInit = file.Exists("lua/" .. base .. "cl_init.lua", "GAME")

		// single-file style: lua/entities/<classname>.lua
		local singleFile = file.Exists("lua/entities/" .. classname .. ".lua", "GAME")

		if !hasShared and !hasInit and !hasClInit and !singleFile then
			continue // empty/unsupported folder, skip
		end

		ENT = {}
		ENT.Type = ENT.Type or "anim"

		if singleFile then
			safeInclude("entities/" .. classname .. ".lua", classname)
		else
			if hasShared then safeInclude(base .. "shared.lua", classname) end

			if SERVER and hasInit then
				safeInclude(base .. "init.lua", classname)
			elseif CLIENT and hasClInit then
				safeInclude(base .. "cl_init.lua", classname)
			end
		end

		if ENT.Spawnable == nil then ENT.Spawnable = true end
		if ENT.AdminSpawnable == nil then ENT.AdminSpawnable = true end

		scripted_ents.Register(ENT, classname)
	end
end

local function loadWeapons()
	local _, folders = file.Find("lua/weapons/*", "GAME")

	for _, classname in pairs(folders) do
		local base = "weapons/" .. classname .. "/"

		local hasShared = file.Exists("lua/" .. base .. "shared.lua", "GAME")
		local hasInit = file.Exists("lua/" .. base .. "init.lua", "GAME")
		local hasClInit = file.Exists("lua/" .. base .. "cl_init.lua", "GAME")

		local singleFile = file.Exists("lua/weapons/" .. classname .. ".lua", "GAME")

		if !hasShared and !hasInit and !hasClInit and !singleFile then
			continue
		end

		SWEP = {Primary = {}, Secondary = {}}

		if singleFile then
			safeInclude("weapons/" .. classname .. ".lua", classname)
		else
			if hasShared then safeInclude(base .. "shared.lua", classname) end

			if SERVER and hasInit then
				safeInclude(base .. "init.lua", classname)
			elseif CLIENT and hasClInit then
				safeInclude(base .. "cl_init.lua", classname)
			end
		end

		if SWEP.Spawnable == nil then SWEP.Spawnable = true end
		if SWEP.AdminSpawnable == nil then SWEP.AdminSpawnable = true end

		weapons.Register(SWEP, classname)
	end
end

local function loadEffects()
	if !CLIENT then return end // effects are client-only in GMod

	local files = file.Find("lua/effects/*.lua", "GAME")
	for _, f in pairs(files) do
		local name = string.StripExtension(f)

		EFFECT = {}
		safeInclude("effects/" .. f, name)
		effects.Register(EFFECT, name)
	end
end

// NOTE: this one is less certain than the others. scripted_ents.Register /
// weapons.Register / effects.Register are clean, stable, documented public
// APIs - there's no equivalent single call for tool-gun tools. This
// replicates the pattern the gmod_tool SWEP itself uses to load its own
// stools/*.lua files at startup, as best as it's understood. The tool
// itself being registered/selectable is expected to work; whether it also
// makes the spawn menu's Tools TAB show it live, without reopening the
// menu, is the uncertain part. Test with the console command
// `gmod_tool <name>` directly rather than relying on the Tools tab alone.
local function loadTools()
	local files = file.Find("lua/weapons/gmod_tool/stools/*.lua", "GAME")
	if !files or #files == 0 then return end

	local toolObj = weapons.GetStored("gmod_tool")
	if !toolObj then
		print("[Cloudbox] can't load tools: gmod_tool base SWEP not found")
		return
	end

	toolObj.Tool = toolObj.Tool or {}

	for _, f in pairs(files) do
		local classname = string.StripExtension(f)

		TOOL = {}
		TOOL.Mode = classname

		local ok, err = pcall(include, "weapons/gmod_tool/stools/" .. f)
		if !ok then
			print("[Cloudbox] addon tool " .. classname .. ": error including: " .. tostring(err))
		else
			toolObj.Tool[classname] = TOOL

			list.Set("tool", classname, {
				name = TOOL.Name or classname,
				category = TOOL.Category or "Other",
			})

			print("[Cloudbox] Registered tool \"" .. classname .. "\" - test with: gmod_tool " .. classname)
		end
	end

	TOOL = nil
end

function LoadCloudboxAddon(info)
	if CloudboxLoadedAddons[info["id"]] then return end // don't double-load

	loadAutorun()
	loadEntities()
	loadWeapons()
	loadEffects()
	loadTools()

	CloudboxLoadedAddons[info["id"]] = true

	print("[Cloudbox] Loaded addon package \"" .. tostring(info["name"]) .. "\" (#" .. tostring(info["id"]) .. ")")
end
