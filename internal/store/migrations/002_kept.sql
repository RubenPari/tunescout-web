-- Brani che l'utente ha scelto di tenere: prune non li propone più.
CREATE TABLE kept (
	user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	key        TEXT NOT NULL,
	spotify_id TEXT NOT NULL DEFAULT '',
	artist     TEXT NOT NULL,
	title      TEXT NOT NULL,
	kept_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
	PRIMARY KEY (user_id, key)
);
