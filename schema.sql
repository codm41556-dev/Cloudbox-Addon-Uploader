-- Cloudbox backend - inferred database schema
--
-- The upstream cloudbox-backend repo doesn't publish a schema file (the
-- real cl0udb0x.com database is presumably managed out-of-band). This
-- schema was reconstructed by reading every SQL statement in db/*.go, so
-- it supports every existing query in the codebase, including the new
-- addon-upload feature.
--
-- One deliberate simplification vs. whatever the real production schema
-- might do: `packages.id` is a normal AUTO_INCREMENT column and
-- `(id, rev)` is the primary key. This exactly matches how the code
-- already creates NEW packages (InsertPackage relies on LastInsertId()
-- and never sets `rev` itself, so `rev` just needs a default of 1) for
-- every package type, including the new "addon" type. Nothing in the
-- current codebase actually inserts a *second* revision of an existing
-- package, so there was nothing to reverse-engineer there - if you build
-- that later, you'll insert a row with an explicit `id` (of the existing
-- package) and `rev = MAX(rev)+1`, which this schema supports fine.
--
-- Usage:
--   mysql -u root -p < schema.sql
-- (creates and fills the `cloudbox` database; adjust the name/grants to
-- match whatever you pass to cloudbox-backend's -dbname/-dbuser flags)

CREATE DATABASE IF NOT EXISTS cloudbox CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE cloudbox;

-- packages: every browsable thing on cloudbox (old recovered Toybox
-- creations *and* new self-uploaded addons alike) is a row here.
CREATE TABLE IF NOT EXISTS packages (
	id            INT NOT NULL AUTO_INCREMENT,
	rev           INT NOT NULL DEFAULT 1,
	type          VARCHAR(32) NOT NULL,        -- entity | weapon | prop | savemap | map | addon
	name          VARCHAR(255) NOT NULL,
	dataname      VARCHAR(255) DEFAULT NULL,
	author        VARCHAR(32) DEFAULT NULL,    -- steamid64, as a string
	description   TEXT,
	data          LONGBLOB,                    -- embedded RunString script, for entity/weapon types only
	incompatible  TINYINT(1) NOT NULL DEFAULT 0,
	time          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (id, rev),
	KEY idx_packages_type (type),
	KEY idx_packages_author (author),
	KEY idx_packages_dataname (dataname)
) ENGINE=InnoDB;

-- files: one row per physical content file. Raw bytes live in S3 (bucket
-- flatgrass-toybox-content), keyed by this id - see db/s3.go.
CREATE TABLE IF NOT EXISTS files (
	id     INT NOT NULL AUTO_INCREMENT,
	path   VARCHAR(512) NOT NULL,
	size   BIGINT NOT NULL DEFAULT 0,  -- raw size
	psize  BIGINT NOT NULL DEFAULT 0,  -- "packed" size (historically shown to the old GM12 client - just == size for new uploads)
	PRIMARY KEY (id),
	KEY idx_files_path (path(255))
) ENGINE=InnoDB;

-- content: junction table, package <-> file. `id` here is a *package* id
-- (see db/package.go's FetchPackage - it queries "content c WHERE c.id = ?"
-- passing the package id), not its own independent identity.
CREATE TABLE IF NOT EXISTS content (
	id      INT NOT NULL,
	fileid  INT NOT NULL,
	KEY idx_content_id (id),
	KEY idx_content_fileid (fileid)
) ENGINE=InnoDB;

-- includes: lets one package pull in another package's content/registration
-- (e.g. a vehicle that depends on a shared base pack).
CREATE TABLE IF NOT EXISTS includes (
	id          INT NOT NULL AUTO_INCREMENT,
	rev         INT NOT NULL,
	includeid   INT NOT NULL,
	includerev  INT NOT NULL,
	PRIMARY KEY (id),
	KEY idx_includes_owner (rev, includeid, includerev)
) ENGINE=InnoDB;

-- uploads: staging area for the legacy in-game "save" upload flow
-- (ingame/toyboxapi/upload.go + ingame/publishsave). Unrelated to the new
-- addon-upload feature, which writes straight to packages/files/content.
CREATE TABLE IF NOT EXISTS uploads (
	id        INT NOT NULL AUTO_INCREMENT,
	steamid   VARCHAR(32) NOT NULL,
	type      VARCHAR(32) NOT NULL,
	meta      TEXT,
	includes  JSON,
	data      LONGBLOB,
	PRIMARY KEY (id)
) ENGINE=InnoDB;

-- logins: session tickets. Populated both by the legacy in-game Steam
-- ticket handshake (ingame/toyboxapi/auth.go) AND by the new web-based
-- Steam OpenID login (api/auth/steamlogin.go) - same table, same lookup
-- (db.FetchSteamIDFromTicket), so the addon upload endpoint authenticates
-- identically either way.
CREATE TABLE IF NOT EXISTS logins (
	steamid  VARCHAR(32) NOT NULL,
	vac      VARCHAR(16) NOT NULL DEFAULT '',
	ticket   VARBINARY(24) NOT NULL,
	PRIMARY KEY (steamid),
	UNIQUE KEY idx_logins_ticket (ticket)
) ENGINE=InnoDB;

-- profiles: a short-lived cache of Steam's GetPlayerSummaries (see
-- db/steam.go - re-fetched once a cached row is more than a week old).
CREATE TABLE IF NOT EXISTS profiles (
	steamid       VARCHAR(32) NOT NULL,
	personaname   VARCHAR(255) NOT NULL DEFAULT '',
	avatar        VARCHAR(255) NOT NULL DEFAULT '',
	avatarmedium  VARCHAR(255) NOT NULL DEFAULT '',
	avatarfull    VARCHAR(255) NOT NULL DEFAULT '',
	time          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	PRIMARY KEY (steamid)
) ENGINE=InnoDB;

-- scraped: metadata-only entries for old Toybox creations that are known
-- to have existed (scraped from somewhere, e.g. archived listings) but
-- whose actual content files haven't been recovered/imported yet. Purely
-- informational - browsable but not downloadable. You can leave this
-- table empty for local testing; it only matters for the recovered-cache
-- side of Cloudbox, not the new addon-upload feature.
CREATE TABLE IF NOT EXISTS scraped (
	id           INT NOT NULL,
	rev          INT NOT NULL DEFAULT 1,
	type         VARCHAR(32) NOT NULL,
	name         VARCHAR(255) NOT NULL,
	author       VARCHAR(255) DEFAULT NULL,
	description  TEXT,
	downloads    INT NOT NULL DEFAULT 0,
	favorites    INT NOT NULL DEFAULT 0,
	goods        INT NOT NULL DEFAULT 0,
	bads         INT NOT NULL DEFAULT 0,
	PRIMARY KEY (id, rev)
) ENGINE=InnoDB;

-- news: shown on the frontend's News tab.
CREATE TABLE IF NOT EXISTS news (
	id      INT NOT NULL AUTO_INCREMENT,
	title   VARCHAR(255) NOT NULL,
	body    TEXT NOT NULL,
	author  VARCHAR(255) NOT NULL,
	time    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (id)
) ENGINE=InnoDB;

-- maploads / errors: telemetry the in-game client reports back.
CREATE TABLE IF NOT EXISTS maploads (
	id        INT NOT NULL AUTO_INCREMENT,
	steamid   VARCHAR(32) NOT NULL,
	duration  FLOAT NOT NULL,
	map       VARCHAR(255) NOT NULL,
	platform  VARCHAR(32) NOT NULL,
	time      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS errors (
	id        INT NOT NULL AUTO_INCREMENT,
	steamid   VARCHAR(32) NOT NULL,
	error     TEXT NOT NULL,
	content   TEXT,
	realm     VARCHAR(16) NOT NULL,
	platform  VARCHAR(32) NOT NULL,
	time      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (id)
) ENGINE=InnoDB;
