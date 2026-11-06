package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type User struct {
	ID          int64     `json:"id"`
	SpotifyID   string    `json:"spotifyId"`
	DisplayName string    `json:"displayName"`
	CreatedAt   time.Time `json:"createdAt"`
}

// UpsertUser crea l'utente al primo login o ne aggiorna il nome.
func (d *DB) UpsertUser(ctx context.Context, spotifyID, displayName string) (*User, error) {
	var u User
	err := d.pool.QueryRow(ctx, `INSERT INTO users(spotify_id, display_name) VALUES ($1,$2)
		ON CONFLICT (spotify_id) DO UPDATE SET display_name=excluded.display_name
		RETURNING id, spotify_id, display_name, created_at`, spotifyID, displayName).
		Scan(&u.ID, &u.SpotifyID, &u.DisplayName, &u.CreatedAt)
	return &u, err
}

func (d *DB) User(ctx context.Context, id int64) (*User, error) {
	var u User
	err := d.pool.QueryRow(ctx, `SELECT id, spotify_id, display_name, created_at FROM users WHERE id=$1`, id).
		Scan(&u.ID, &u.SpotifyID, &u.DisplayName, &u.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &u, nil
}

// DeleteUser cancella l'utente e, a cascata, tutti i suoi dati.
func (d *DB) DeleteUser(ctx context.Context, id int64) error {
	_, err := d.pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, id)
	return err
}

// --- token Spotify (già cifrati dal chiamante) ---

func (d *DB) SaveToken(ctx context.Context, userID int64, enc []byte) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO spotify_tokens(user_id, token) VALUES ($1,$2)
		ON CONFLICT (user_id) DO UPDATE SET token=excluded.token, updated_at=now()`, userID, enc)
	return err
}

func (d *DB) LoadToken(ctx context.Context, userID int64) ([]byte, error) {
	var b []byte
	err := d.pool.QueryRow(ctx, `SELECT token FROM spotify_tokens WHERE user_id=$1`, userID).Scan(&b)
	return b, notFound(err)
}

// --- sessioni ---

func (d *DB) CreateSession(ctx context.Context, idHash []byte, userID int64, expires time.Time) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO sessions(id_hash, user_id, expires_at) VALUES ($1,$2,$3)`,
		idHash, userID, expires)
	return err
}

// SessionUser restituisce l'utente della sessione se non è scaduta.
func (d *DB) SessionUser(ctx context.Context, idHash []byte) (int64, error) {
	var uid int64
	err := d.pool.QueryRow(ctx, `SELECT user_id FROM sessions WHERE id_hash=$1 AND expires_at > now()`, idHash).Scan(&uid)
	return uid, notFound(err)
}

func (d *DB) DeleteSession(ctx context.Context, idHash []byte) error {
	_, err := d.pool.Exec(ctx, `DELETE FROM sessions WHERE id_hash=$1`, idHash)
	return err
}

func (d *DB) PurgeExpiredSessions(ctx context.Context) error {
	_, err := d.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= now()`)
	return err
}

// --- job ---

type Job struct {
	ID         string          `json:"id"`
	UserID     int64           `json:"-"`
	Kind       string          `json:"kind"`
	Status     string          `json:"status"` // running, done, failed
	Params     json.RawMessage `json:"params"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
	CreatedAt  time.Time       `json:"createdAt"`
	FinishedAt *time.Time      `json:"finishedAt,omitempty"`
}

const (
	JobRunning = "running"
	JobDone    = "done"
	JobFailed  = "failed"
)

func (d *DB) CreateJob(ctx context.Context, j *Job) error {
	return d.pool.QueryRow(ctx, `INSERT INTO jobs(id, user_id, kind, status, params) VALUES ($1,$2,$3,$4,$5)
		RETURNING created_at`, j.ID, j.UserID, j.Kind, j.Status, j.Params).Scan(&j.CreatedAt)
}

func (d *DB) FinishJob(ctx context.Context, id, status string, result []byte, errMsg string) error {
	_, err := d.pool.Exec(ctx, `UPDATE jobs SET status=$2, result=$3, error=$4, finished_at=now() WHERE id=$1`,
		id, status, result, errMsg)
	return err
}

// Job restituisce un job solo se appartiene all'utente.
func (d *DB) Job(ctx context.Context, userID int64, id string) (*Job, error) {
	var j Job
	err := d.pool.QueryRow(ctx, `SELECT id, user_id, kind, status, params, result, error, created_at, finished_at
		FROM jobs WHERE id=$1 AND user_id=$2`, id, userID).
		Scan(&j.ID, &j.UserID, &j.Kind, &j.Status, &j.Params, &j.Result, &j.Error, &j.CreatedAt, &j.FinishedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &j, nil
}

// FailStaleJobs marca come falliti i job rimasti "running" (es. dopo un riavvio).
func (d *DB) FailStaleJobs(ctx context.Context) error {
	_, err := d.pool.Exec(ctx, `UPDATE jobs SET status=$1, error='interrotto dal riavvio del server',
		finished_at=now() WHERE status=$2`, JobFailed, JobRunning)
	return err
}

// --- limiti di utilizzo ---

// todayUTC è la data odierna (UTC) a cui si riferisce il limite giornaliero.
const todayUTC = "(now() AT TIME ZONE 'UTC')::date"

// TakeRun consuma un'esecuzione dal limite giornaliero (UTC) dell'utente.
// Restituisce false se il limite è già stato raggiunto.
func (d *DB) TakeRun(ctx context.Context, userID int64, limit int) (bool, error) {
	if limit <= 0 {
		return true, nil
	}
	var runs int
	err := d.pool.QueryRow(ctx, `INSERT INTO usage(user_id, day, runs) VALUES ($1, `+todayUTC+`, 1)
		ON CONFLICT (user_id, day) DO UPDATE SET runs = usage.runs + 1 WHERE usage.runs < $2
		RETURNING runs`, userID, limit).Scan(&runs)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// RefundRun restituisce un'esecuzione (es. job fallito prima di chiamare il modello).
func (d *DB) RefundRun(ctx context.Context, userID int64) error {
	_, err := d.pool.Exec(ctx, `UPDATE usage SET runs = GREATEST(runs - 1, 0)
		WHERE user_id=$1 AND day=`+todayUTC, userID)
	return err
}

// RunsToday restituisce le esecuzioni consumate oggi.
func (d *DB) RunsToday(ctx context.Context, userID int64) (int, error) {
	var n int
	err := d.pool.QueryRow(ctx, `SELECT COALESCE((SELECT runs FROM usage
		WHERE user_id=$1 AND day=`+todayUTC+`), 0)`, userID).Scan(&n)
	return n, err
}

// notFound traduce pgx.ErrNoRows in ErrNotFound; gli altri errori passano invariati.
func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
