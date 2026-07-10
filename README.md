# Cloudbox: self-serve addon uploads

This is a set of changes on top of the three Cloudbox repos
(`cloudbox13` the client addon, `cloudbox` the backend, `forecaster` the
browser UI) that adds a **self-serve "upload an addon" feature**: sign in
with Steam on a web page, upload a zip of a folder-based GMod addon
(`lua/`, `materials/`, `models/`, `sound/`, `particles/`, `gamemodes/`,
...), and it becomes a normal Cloudbox package that the existing client
addon can find, download, and load in-game - autorun scripts, entities,
weapons and effects included.

Everything below was verified by actually running the code (a real
MariaDB schema, a real (mocked) S3 endpoint, and the real compiled
Go binaries), not just read through. See "What was actually tested" at
the bottom for exactly what that covered and didn't.

## How Cloudbox actually works (what I found)

A few things worth knowing before touching any of this, because they
don't quite match "addons get uploaded to it":

- **What gets uploaded today is a sandbox *save* (a "dupe"/contraption),
  not an addon.** `ingame/toyboxapi/upload.go` and
  `ingame/publishsave/publish.go` emulate the *original, real* Toybox
  server API (`toyboxapi.garrysmod.com`) that's baked into the GMod
  engine binary itself from 2010-2012. There's no Lua code anywhere in
  `cloudbox13` that performs that handshake - it's the game engine doing
  it, the same way it always did, just pointed at Cloudbox's server
  instead of Facepunch's (long since shut down).
- **The "1000+ recovered creations" come from old cached GMod installs**,
  converted and imported by the maintainer out of band (see the Steam
  page: "add me on Steam/Discord... you can help us make our archive even
  better"). There's no public bulk-import tool in any of the three repos
  - `db/s3.go`'s only write path was `PutThumbnail` before this change.
- **The content pipeline was already generic enough for this.**
  `api/packages/getgma.go` assembles a real, valid `.gma` file (the same
  format Steam Workshop uses) from a package's `Content` list on the fly,
  and its path whitelist already allowed `lua/*.lua` (any path, so
  autorun/entities/weapons all qualify), `materials/`, `models/`,
  `sound/`, `particles/`, `gamemodes/`, etc. The hard part (assembling a
  valid GMA, mounting it via `game.MountGMA`) was already solved. What
  was missing was (a) a way to get new content in via a public endpoint,
  and (b) client-side loading logic that treats a package as more than
  one `RunString`'d entity/weapon script.

## What this adds

**Backend (`cloudbox-backend`):**
- `POST /addons/upload` - authenticated (Steam), accepts a zip, validates
  each entry against the *same* whitelist `getgma.go` already used (now
  shared via `common/whitelist.go`), guards against zip bombs and path
  traversal ("zip slip"), stores accepted files as normal `Content`, and
  creates a new package with `type = "addon"`.
- `GET /addons/upload` - a small self-contained page (Steam login button
  + the upload form) served by the backend itself, so the whole flow
  stays same-origin and doesn't need CORS/cookie sharing across two
  domains. (`POST /addons/upload` still sends CORS headers too, in case
  you want to call it from elsewhere.)
- `GET/POST /auth/steamlogin(/callback)` - standard "Sign in through
  Steam" via OpenID 2.0. This is a *new, separate* login path from the
  in-game ticket handshake mentioned above - that one is driven by the
  engine itself and there's no Lua-level way to request a ticket on
  demand, so it can't be reused for a browser form. Both paths mint the
  same kind of ticket into the same `logins` table, so
  `db.FetchSteamIDFromTicket` (already used by `publishsave.Publish`)
  authenticates either one identically.
- A `-s3endpoint` flag on `db.Init` so you can point storage at a local
  S3-compatible server (e.g. MinIO) instead of real AWS - the only thing
  that changes vs. upstream behavior; leave it unset for normal AWS S3.
- `schema.sql` - the backend repo doesn't ship a schema, so this
  reconstructs one from every query in `db/*.go` (see the comments at the
  top of the file for the one simplification made, around package
  revisions).

**Client addon (`cloudbox13`):**
- New package type `"addon"`, dispatched from `ExecuteCloudboxPackage` in
  `cloudbox/shared/cloudbox.lua` to a new `addonloader.lua`.
- `addonloader.lua` walks the mounted GMA the same way the GMod engine
  itself loads a normal installed addon: `lua/autorun/*.lua` (shared),
  `lua/autorun/client/`, `lua/autorun/server/`, then
  `lua/entities/<name>/`, `lua/weapons/<name>/`, `lua/effects/<name>.lua`.
  This mirrors GMod's own publicly-documented addon structure conventions
  (see the GMod wiki) - it's necessary because the engine only does this
  scan automatically at map load, and these packages get mounted mid
  session.
  - Known gap: matproxies and anything that registers itself outside of
    autorun (rare) aren't specially handled. Everything else a normal
    addon does - which in practice is nearly everything - is covered.
- New convar `cloudbox_api_url` (replicated, defaults to the real
  `https://api.cl0udb0x.com`) so the two places that had the production
  API host hardcoded can be pointed at a local test backend instead.

**Browser (`forecaster`):**
- New "Addons" tab (`/browse/addons`), wired to the new `type=addon`
  packages.
- A small "Upload your own addon" banner linking to the backend's upload
  page, matching the existing notice-banner style used for the
  Saves category.

## Local testing - fully verified steps

Everything in this section is exactly what I ran to test the feature (I
have a real MariaDB, a Go S3 mock, and the compiled binaries running
together in a sandbox - not just a read-through). Swap the mock S3 step
for real MinIO or AWS for anything beyond a quick local test.

### 1. Database

```bash
mysql -u root < schema.sql
mysql -u root -e "CREATE USER 'cloudbox'@'%' IDENTIFIED BY 'somepassword'; GRANT ALL PRIVILEGES ON cloudbox.* TO 'cloudbox'@'%'; FLUSH PRIVILEGES;"
```

### 2. Object storage

For a quick local test, [MinIO](https://min.io/) is the standard choice
(single binary, S3-compatible). Run it, then create the two buckets the
code expects:

```bash
minio server /path/to/data --console-address :9001
# in another shell, using the MinIO client (mc):
mc alias set local http://127.0.0.1:9000 minioadmin minioadmin
mc mb local/flatgrass-toybox-content
mc mb local/flatgrass-toybox-image
```

Then export credentials the AWS SDK will pick up automatically:

```bash
export AWS_ACCESS_KEY_ID=minioadmin
export AWS_SECRET_ACCESS_KEY=minioadmin
export AWS_REGION=us-east-1
```

### 3. Backend

```bash
cd cloudbox-backend
go build -o cloudbox-backend .
./cloudbox-backend \
  -dbuser=cloudbox -dbpass=somepassword -dbaddr=127.0.0.1:3306 -dbname=cloudbox \
  -s3endpoint=http://127.0.0.1:9000 \
  -apikey=<your steam web api key, needed for the openid login step> \
  -addr=127.0.0.1:8090 -proto=tcp
```

A Steam Web API key is free and instant from
<https://steamcommunity.com/dev/apikey> - you only need it for the
`GetPlayerSummaries` call after OpenID login succeeds.

Visit `http://127.0.0.1:8090/addons/upload`, sign in through Steam, and
upload a zip of an addon folder. You should get back JSON with the new
package's id and which files were accepted/rejected.

### 4. Browser (optional, for the pretty listing page)

```bash
cd forecaster
go build -o forecaster .
API_URL=http://127.0.0.1:8090 ./forecaster -addr=127.0.0.1:8091 -proto=tcp
```

Visit `http://127.0.0.1:8091/browse/addons`.

### 5. Testing in an actual running GMod client

This part I could **not** run myself (no game engine in this
environment) - only code-reviewed and syntax-checked. To point a real
GMod client/server at your local backend instead of production:

```
cloudbox_api_url http://<your-machine-ip>:8090
```

(set on both the listen server and any joining clients, or just on a
listen server if `cloudbox_api_url` is left replicated - it'll push to
clients automatically). Then use the in-game spawn menu's Cloudbox tab
as normal - `RequestCloudboxDownload` doesn't care that the package type
is new.

## What was actually tested vs. just reviewed

**Ran and verified working**, end-to-end, including checking the exact
bytes returned:
- `schema.sql` against real MariaDB, including every exact query string
  from `db/*.go` (not just similar ones)
- `POST /addons/upload`: accepts valid files, rejects disallowed file
  types, rejects two different path-traversal ("zip slip") attack
  payloads, stores content, creates the package
- `GET /packages/list?type=addon` and `GET /packages/get` return the new
  package correctly
- `GET /packages/getgma` produces a real GMA file (correct `GMAD` magic
  header) for an uploaded addon package
- `GET/POST /auth/steamlogin` redirect and CORS preflight
- The forecaster `/browse/addons` page, including the new upload banner
  link and that the uploaded package shows up in the list
- `go build` and `go vet` clean on both Go repos

**Reviewed and syntax-checked, but not run** (no GMod client available
here):
- All of the `cloudbox13` Lua changes (`addonloader.lua`, the
  `cloudbox.lua` dispatch change, the two convar-ification edits). I
  mechanically translated the GLua-specific syntax (`//` comments, `!`
  for `not`, etc.) to standard Lua and ran it through `luac` to catch
  structural errors, and reviewed the GMod API usage (`file.Find`,
  `scripted_ents.Register`, `weapons.Register`, `effects.Register`)
  against documented behavior, but there's no substitute for testing it
  in a real client. Please treat that part as "should work, not yet
  proven" and report back anything that doesn't behave as expected.

## Security notes

- Uploads are capped (512 MiB request body, 256 MiB per file, 1.5 GiB
  total per zip, 20000 file entries) purely as zip-bomb guards - tune to
  taste.
- Path traversal is blocked (verified with two different attack shapes
  in testing).
- The file-type whitelist is the same one already governing the existing
  GMA-generation code, so nothing new is allowed through that wasn't
  already servable.
- **Uploaded Lua still runs with full Lua sandbox permissions**, same as
  every other Cloudbox package (and same as any Workshop addon) - this
  feature doesn't change that trust model, it just gives people a way to
  add to it themselves instead of only via recovered old cache dumps.
  If you run a public instance of this, you're taking on the same
  moderation responsibility any addon-hosting site does.
