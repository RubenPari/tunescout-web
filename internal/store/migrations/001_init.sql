CREATE TABLE users (
	id           BIGSERIAL PRIMARY KEY,
	spotify_id   TEXT NOT NULL UNIQUE,
	display_name TEXT NOT NULL DEFAULT '',
	created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Token OAuth Spotify serializzato in JSON e cifrato con AES-GCM.
CREATE TABLE spotify_tokens (
	user_id    BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
	token      BYTEA NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Si salva solo lo SHA-256 dell'id di sessione contenuto nel cookie.
CREATE TABLE sessions (
	id_hash    BYTEA PRIMARY KEY,
	user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX sessions_user_idx ON sessions(user_id);

CREATE TABLE known_tracks (
	user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	key        TEXT NOT NULL,
	spotify_id TEXT NOT NULL DEFAULT '',
	artist     TEXT NOT NULL,
	title      TEXT NOT NULL,
	source     TEXT NOT NULL,
	PRIMARY KEY (user_id, key)
);
CREATE INDEX known_tracks_sid_idx ON known_tracks(user_id, spotify_id);

CREATE TABLE known_artists (
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	norm    TEXT NOT NULL,
	name    TEXT NOT NULL,
	PRIMARY KEY (user_id, norm)
);

CREATE TABLE suggested (
	user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	key        TEXT NOT NULL,
	seq        BIGSERIAL,
	spotify_id TEXT NOT NULL DEFAULT '',
	uri        TEXT NOT NULL DEFAULT '',
	artist     TEXT NOT NULL,
	title      TEXT NOT NULL,
	album      TEXT NOT NULL DEFAULT '',
	year       TEXT NOT NULL DEFAULT '',
	reason     TEXT NOT NULL DEFAULT '',
	url        TEXT NOT NULL DEFAULT '',
	image      TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	feedback   TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (user_id, key)
);
CREATE INDEX suggested_sid_idx ON suggested(user_id, spotify_id);
CREATE INDEX suggested_created_idx ON suggested(user_id, created_at DESC, seq DESC);

CREATE TABLE removed (
	user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	key        TEXT NOT NULL,
	spotify_id TEXT NOT NULL DEFAULT '',
	artist     TEXT NOT NULL,
	title      TEXT NOT NULL,
	removed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	PRIMARY KEY (user_id, key)
);
CREATE INDEX removed_sid_idx ON removed(user_id, spotify_id);

CREATE TABLE meta (
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	key     TEXT NOT NULL,
	value   JSONB NOT NULL,
	PRIMARY KEY (user_id, key)
);

CREATE TABLE jobs (
	id          TEXT PRIMARY KEY,
	user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	kind        TEXT NOT NULL,
	status      TEXT NOT NULL,
	params      JSONB NOT NULL DEFAULT '{}',
	result      JSONB,
	error       TEXT NOT NULL DEFAULT '',
	created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
	finished_at TIMESTAMPTZ
);
CREATE INDEX jobs_user_idx ON jobs(user_id, created_at DESC);

CREATE TABLE usage (
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	day     DATE NOT NULL,
	runs    INT NOT NULL DEFAULT 0,
	PRIMARY KEY (user_id, day)
);
