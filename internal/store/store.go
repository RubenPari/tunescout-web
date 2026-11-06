// Package store persiste utenti, sessioni, libreria, profilo e storico
// suggerimenti su Postgres. Tutti i dati musicali sono separati per utente:
// si accede tramite DB.ForUser, che restituisce uno Store con le stesse
// operazioni della CLI tunescout.
package store

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

type DB struct{ pool *pgxpool.Pool }

// Open si connette a Postgres e applica le migrazioni mancanti.
func Open(ctx context.Context, url string) (*DB, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connessione a Postgres: %w", err)
	}
	d := &DB{pool: pool}
	if err := d.migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrazioni: %w", err)
	}
	return d, nil
}

func (d *DB) Close() { d.pool.Close() }

func (d *DB) Ping(ctx context.Context) error { return d.pool.Ping(ctx) }

func (d *DB) migrate(ctx context.Context) error {
	if _, err := d.pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY)`); err != nil {
		return err
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, n := range names {
		sqlText, err := migrations.ReadFile(n)
		if err != nil {
			return err
		}
		err = pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
			// Lock per evitare che due istanze applichino la stessa migrazione.
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7243001)`); err != nil {
				return err
			}
			var done bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name=$1)`, n).Scan(&done); err != nil || done {
				return err
			}
			if _, err := tx.Exec(ctx, string(sqlText)); err != nil {
				return fmt.Errorf("%s: %w", n, err)
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES ($1)`, n)
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Store è la vista dei dati di un singolo utente, legata al contesto della
// richiesta o del job che la usa.
type Store struct {
	ctx  context.Context
	pool *pgxpool.Pool
	uid  int64
}

func (d *DB) ForUser(ctx context.Context, userID int64) *Store {
	return &Store{ctx: ctx, pool: d.pool, uid: userID}
}

func (s *Store) UserID() int64 { return s.uid }

type KnownTrack struct {
	Key, SpotifyID, Artist, Title, Source string
}

// ReplaceLibrary sostituisce atomicamente brani e artisti noti.
func (s *Store) ReplaceLibrary(tracks []KnownTrack, artists map[string]string) error {
	return pgx.BeginFunc(s.ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(s.ctx, `DELETE FROM known_tracks WHERE user_id=$1`, s.uid); err != nil {
			return err
		}
		if _, err := tx.Exec(s.ctx, `DELETE FROM known_artists WHERE user_id=$1`, s.uid); err != nil {
			return err
		}
		seen := make(map[string]bool, len(tracks))
		rows := make([][]any, 0, len(tracks))
		for _, t := range tracks {
			if seen[t.Key] {
				continue // come INSERT OR IGNORE: vince la prima occorrenza
			}
			seen[t.Key] = true
			rows = append(rows, []any{s.uid, t.Key, t.SpotifyID, t.Artist, t.Title, t.Source})
		}
		if _, err := tx.CopyFrom(s.ctx, pgx.Identifier{"known_tracks"},
			[]string{"user_id", "key", "spotify_id", "artist", "title", "source"}, pgx.CopyFromRows(rows)); err != nil {
			return err
		}
		arows := make([][]any, 0, len(artists))
		for n, name := range artists {
			arows = append(arows, []any{s.uid, n, name})
		}
		_, err := tx.CopyFrom(s.ctx, pgx.Identifier{"known_artists"},
			[]string{"user_id", "norm", "name"}, pgx.CopyFromRows(arows))
		return err
	})
}

func (s *Store) SetMetaJSON(k string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(s.ctx, `INSERT INTO meta(user_id, key, value) VALUES ($1,$2,$3)
		ON CONFLICT (user_id, key) DO UPDATE SET value=excluded.value`, s.uid, k, b)
	return err
}

// GetMetaJSON restituisce false se la chiave non esiste.
func (s *Store) GetMetaJSON(k string, v any) (bool, error) {
	var raw []byte
	err := s.pool.QueryRow(s.ctx, `SELECT value FROM meta WHERE user_id=$1 AND key=$2`, s.uid, k).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(raw, v)
}

func (s *Store) LastSync() (time.Time, error) {
	var ts int64
	ok, err := s.GetMetaJSON("synced_at", &ts)
	if !ok || err != nil {
		return time.Time{}, err
	}
	return time.Unix(ts, 0), nil
}

func (s *Store) MarkSynced() error { return s.SetMetaJSON("synced_at", time.Now().Unix()) }

func (s *Store) CountKnown() (tracks, artists int, err error) {
	err = s.pool.QueryRow(s.ctx, `SELECT
		(SELECT COUNT(*) FROM known_tracks WHERE user_id=$1),
		(SELECT COUNT(*) FROM known_artists WHERE user_id=$1)`, s.uid).Scan(&tracks, &artists)
	return
}

// Seen indica se un brano è già in libreria, è già stato suggerito o è stato
// rimosso dalla libreria con prune.
func (s *Store) Seen(key, spotifyID string) (bool, error) {
	var seen bool
	err := s.pool.QueryRow(s.ctx, `SELECT
		   EXISTS(SELECT 1 FROM known_tracks WHERE user_id=$1 AND (key=$2 OR ($3 <> '' AND spotify_id=$3)))
		OR EXISTS(SELECT 1 FROM suggested    WHERE user_id=$1 AND (key=$2 OR ($3 <> '' AND spotify_id=$3)))
		OR EXISTS(SELECT 1 FROM removed      WHERE user_id=$1 AND (key=$2 OR ($3 <> '' AND spotify_id=$3)))`,
		s.uid, key, spotifyID).Scan(&seen)
	return seen, err
}

// AddRemoved registra i brani rimossi dalla libreria, così discover non li ripropone.
func (s *Store) AddRemoved(tracks []KnownTrack) error {
	b := &pgx.Batch{}
	for _, t := range tracks {
		b.Queue(`INSERT INTO removed(user_id, key, spotify_id, artist, title) VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (user_id, key) DO UPDATE SET spotify_id=excluded.spotify_id, removed_at=now()`,
			s.uid, t.Key, t.SpotifyID, t.Artist, t.Title)
	}
	return s.sendBatch(b)
}

// AddKept registra un brano che l'utente vuole tenere: prune non lo propone più.
func (s *Store) AddKept(t KnownTrack) error {
	_, err := s.pool.Exec(s.ctx, `INSERT INTO kept(user_id, key, spotify_id, artist, title) VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (user_id, key) DO UPDATE SET spotify_id=excluded.spotify_id, kept_at=now()`,
		s.uid, t.Key, t.SpotifyID, t.Artist, t.Title)
	return err
}

// RemoveKept annulla AddKept: il brano torna un possibile candidato di prune.
func (s *Store) RemoveKept(key string) error {
	_, err := s.pool.Exec(s.ctx, `DELETE FROM kept WHERE user_id=$1 AND key=$2`, s.uid, key)
	return err
}

// Kept restituisce l'insieme delle chiavi e degli id Spotify dei brani da tenere.
func (s *Store) Kept() (map[string]bool, error) {
	rows, err := s.pool.Query(s.ctx, `SELECT key, spotify_id FROM kept WHERE user_id=$1`, s.uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var k, id string
		if err := rows.Scan(&k, &id); err != nil {
			return nil, err
		}
		out[k] = true
		if id != "" {
			out[id] = true
		}
	}
	return out, rows.Err()
}

func (s *Store) KnownArtist(normName string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(s.ctx, `SELECT EXISTS(SELECT 1 FROM known_artists WHERE user_id=$1 AND norm=$2)`,
		s.uid, normName).Scan(&ok)
	return ok, err
}

type Suggestion struct {
	Key       string    `json:"key"`
	SpotifyID string    `json:"spotifyId"`
	URI       string    `json:"uri"`
	Artist    string    `json:"artist"`
	Title     string    `json:"title"`
	Album     string    `json:"album"`
	Year      string    `json:"year"`
	Reason    string    `json:"reason"`
	URL       string    `json:"url"`
	Image     string    `json:"image"`
	CreatedAt time.Time `json:"createdAt"`
	Feedback  string    `json:"feedback"` // "", "like", "skip"
}

func (s *Store) AddSuggestions(list []Suggestion) error {
	b := &pgx.Batch{}
	for _, x := range list {
		b.Queue(`INSERT INTO suggested(user_id, key, spotify_id, uri, artist, title, album, year, reason, url, image)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT DO NOTHING`,
			s.uid, x.Key, x.SpotifyID, x.URI, x.Artist, x.Title, x.Album, x.Year, x.Reason, x.URL, x.Image)
	}
	return s.sendBatch(b)
}

func (s *Store) sendBatch(b *pgx.Batch) error {
	if b.Len() == 0 {
		return nil
	}
	return pgx.BeginFunc(s.ctx, s.pool, func(tx pgx.Tx) error {
		return tx.SendBatch(s.ctx, b).Close()
	})
}

// ErrNotFound indica che la risorsa non esiste per l'utente corrente.
var ErrNotFound = errors.New("non trovato")

func (s *Store) SetFeedback(key, fb string) error {
	tag, err := s.pool.Exec(s.ctx, `UPDATE suggested SET feedback=$3 WHERE user_id=$1 AND key=$2`, s.uid, key, fb)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// Suggestion restituisce un suggerimento dello storico dell'utente.
func (s *Store) Suggestion(key string) (*Suggestion, error) {
	list, err := s.querySuggestions(`WHERE user_id=$1 AND key=$2`, s.uid, key)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return &list[0], nil
}

// History restituisce i suggerimenti più recenti (limit 0 = tutti).
func (s *Store) History(limit int, feedback string) ([]Suggestion, error) {
	where := `WHERE user_id=$1 AND ($2 = '' OR feedback=$2) ORDER BY created_at DESC, seq DESC`
	args := []any{s.uid, feedback}
	if limit > 0 {
		where += ` LIMIT $3`
		args = append(args, limit)
	}
	return s.querySuggestions(where, args...)
}

func (s *Store) querySuggestions(where string, args ...any) ([]Suggestion, error) {
	rows, err := s.pool.Query(s.ctx, `SELECT key, spotify_id, uri, artist, title, album, year, reason, url, image,
		created_at, feedback FROM suggested `+where, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Suggestion, error) {
		var x Suggestion
		err := r.Scan(&x.Key, &x.SpotifyID, &x.URI, &x.Artist, &x.Title, &x.Album, &x.Year, &x.Reason, &x.URL,
			&x.Image, &x.CreatedAt, &x.Feedback)
		return x, err
	})
}
