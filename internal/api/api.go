// Package api espone l'API HTTP JSON di tunescout-web e serve il frontend.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rubenpari/tunescout-web/internal/auth"
	"github.com/rubenpari/tunescout-web/internal/discover"
	"github.com/rubenpari/tunescout-web/internal/jobs"
	"github.com/rubenpari/tunescout-web/internal/profile"
	"github.com/rubenpari/tunescout-web/internal/prune"
	"github.com/rubenpari/tunescout-web/internal/spotify"
	"github.com/rubenpari/tunescout-web/internal/store"
)

// Spotify è l'insieme di operazioni Spotify usate dal server (mockabile nei test).
type Spotify interface {
	profile.Source
	discover.Searcher
	prune.Library
	SaveToLibrary(ctx context.Context, uris []string) error
	RemoveFromLibrary(ctx context.Context, uris []string) error
	CreatePlaylist(ctx context.Context, name, description string) (*spotify.Playlist, error)
	AddToPlaylist(ctx context.Context, playlistID string, uris []string) error
}

// LLM raggruppa le due operazioni del modello.
type LLM interface {
	discover.Recommender
	prune.Picker
}

type Server struct {
	DB            *store.DB
	Auth          *auth.Manager
	Jobs          *jobs.Runner
	LLM           LLM
	DailyRunLimit int
	BaseURL       string
	Static        fs.FS // build del frontend; nil = nessun frontend

	// Hook sostituibili nei test.
	SpotifyFor func(ctx context.Context, userID int64) (Spotify, error)
}

// Handler costruisce il router con tutti i middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	authLimit := newIPLimiter(20, time.Minute)

	mux.HandleFunc("GET /api/health", s.health)
	mux.Handle("GET /api/auth/login", authLimit.wrap(http.HandlerFunc(s.login)))
	mux.Handle("GET /api/auth/callback", authLimit.wrap(http.HandlerFunc(s.callback)))
	mux.HandleFunc("POST /api/auth/logout", s.logout)

	mux.HandleFunc("GET /api/me", s.user(s.me))
	mux.HandleFunc("DELETE /api/me", s.user(s.deleteMe))

	mux.HandleFunc("POST /api/jobs/sync", s.user(s.startSync))
	mux.HandleFunc("POST /api/jobs/discover", s.user(s.startDiscover))
	mux.HandleFunc("POST /api/jobs/prune", s.user(s.startPrune))
	mux.HandleFunc("GET /api/jobs/{id}", s.user(s.getJob))
	mux.HandleFunc("GET /api/jobs/{id}/events", s.user(s.jobEvents))

	mux.HandleFunc("GET /api/history", s.user(s.history))
	mux.HandleFunc("POST /api/feedback", s.user(s.feedback))
	mux.HandleFunc("POST /api/playlists", s.user(s.createPlaylist))
	mux.HandleFunc("POST /api/jobs/{id}/remove", s.user(s.pruneRemove))
	mux.HandleFunc("POST /api/jobs/{id}/keep", s.user(s.pruneKeep))
	mux.HandleFunc("DELETE /api/jobs/{id}/keep", s.user(s.pruneKeep))

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "endpoint inesistente")
	})
	if s.Static != nil {
		mux.Handle("/", spaHandler(s.Static))
	}

	cop := http.NewCrossOriginProtection()
	if err := cop.AddTrustedOrigin(s.BaseURL); err != nil {
		slog.Warn("origine non valida per CrossOriginProtection", "url", s.BaseURL, "err", err)
	}
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusForbidden, "richiesta cross-origin rifiutata")
	}))
	return securityHeaders(recoverer(cop.Handler(mux)))
}

// --- utenti e sessione ---

type userHandler func(w http.ResponseWriter, r *http.Request, uid int64)

func (s *Server) user(h userHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, err := s.Auth.UserID(r)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "accesso richiesto")
			return
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		h(w, r, uid)
	}
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.DB.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database non raggiungibile")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) { s.Auth.StartLogin(w, r) }

func (s *Server) callback(w http.ResponseWriter, r *http.Request) {
	if _, err := s.Auth.FinishLogin(w, r); err != nil {
		slog.Warn("login fallito", "err", err)
		http.Redirect(w, r, "/login?error="+url.QueryEscape(loginErrorMessage(err)), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func loginErrorMessage(err error) string {
	var apiErr *spotify.APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden {
		return "Spotify ha rifiutato l'accesso: il tuo account deve essere aggiunto in User Management dell'app (Development Mode)."
	}
	msg := err.Error()
	if strings.Contains(msg, "access_denied") {
		return "Hai negato l'autorizzazione su Spotify."
	}
	if strings.Contains(msg, "state OAuth") {
		return "Sessione di login scaduta: riprova."
	}
	return "Accesso non riuscito: riprova."
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if err := s.Auth.Logout(w, r); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type libraryStatus struct {
	LastSync *time.Time `json:"lastSync"`
	Tracks   int        `json:"tracks"`
	Artists  int        `json:"artists"`
}

type usageInfo struct {
	Today int `json:"today"`
	Limit int `json:"limit"` // 0 = illimitato
}

type meResponse struct {
	User      *store.User   `json:"user"`
	Library   libraryStatus `json:"library"`
	Usage     usageInfo     `json:"usage"`
	ActiveJob *store.Job    `json:"activeJob,omitempty"`
}

func (s *Server) me(w http.ResponseWriter, r *http.Request, uid int64) {
	ctx := r.Context()
	u, err := s.DB.User(ctx, uid)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	st := s.DB.ForUser(ctx, uid)
	var lib libraryStatus
	if last, err := st.LastSync(); err != nil {
		s.fail(w, r, err)
		return
	} else if !last.IsZero() {
		lib.LastSync = &last
	}
	if lib.Tracks, lib.Artists, err = st.CountKnown(); err != nil {
		s.fail(w, r, err)
		return
	}
	runs, err := s.DB.RunsToday(ctx, uid)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	resp := meResponse{User: u, Library: lib, Usage: usageInfo{Today: runs, Limit: s.DailyRunLimit}}
	if id, ok := s.Jobs.Active(uid); ok {
		if j, err := s.DB.Job(ctx, uid, id); err == nil {
			resp.ActiveJob = j
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) deleteMe(w http.ResponseWriter, r *http.Request, uid int64) {
	if _, busy := s.Jobs.Active(uid); busy {
		writeError(w, http.StatusConflict, jobs.ErrBusy.Error())
		return
	}
	if err := s.DB.DeleteUser(r.Context(), uid); err != nil {
		s.fail(w, r, err)
		return
	}
	_ = s.Auth.Logout(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) history(w http.ResponseWriter, r *http.Request, uid int64) {
	fb := r.URL.Query().Get("feedback")
	if fb != "" && !store.ValidFeedback(fb) {
		writeError(w, http.StatusBadRequest, "feedback deve essere like o skip")
		return
	}
	limit, ok := intParam(r, "limit", 100, 0, 1000)
	if !ok {
		writeError(w, http.StatusBadRequest, "limit non valido (0-1000)")
		return
	}
	list, err := s.DB.ForUser(r.Context(), uid).History(limit, fb)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": nonNil(list)})
}

// --- errori ---

type apiError struct {
	Error  string `json:"error"`
	Reauth bool   `json:"reauth,omitempty"`
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, apiError{Error: msg})
}

// fail traduce gli errori noti in risposte leggibili; il resto diventa 500.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	code, body := classify(err)
	if code >= 500 {
		slog.Error("errore richiesta", "method", r.Method, "path", r.URL.Path, "err", err)
	}
	writeJSON(w, code, body)
}

func classify(err error) (int, apiError) {
	var apiErr *spotify.APIError
	switch {
	case errors.Is(err, auth.ErrReauth):
		return http.StatusUnauthorized, apiError{Error: err.Error(), Reauth: true}
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, apiError{Error: "non trovato"}
	case errors.Is(err, jobs.ErrBusy):
		return http.StatusConflict, apiError{Error: err.Error()}
	case errors.As(err, &apiErr):
		switch apiErr.Status {
		case http.StatusUnauthorized:
			return http.StatusUnauthorized, apiError{Error: auth.ErrReauth.Error(), Reauth: true}
		case http.StatusForbidden:
			return http.StatusBadGateway, apiError{Error: "Spotify ha negato l'operazione (403): verifica che il tuo account sia in User Management dell'app Spotify."}
		case http.StatusTooManyRequests:
			return http.StatusServiceUnavailable, apiError{Error: "Spotify è sovraccarico (rate limit): riprova tra qualche minuto."}
		}
		return http.StatusBadGateway, apiError{Error: "errore da Spotify: " + apiErr.Message}
	case errors.Is(err, context.Canceled):
		return 499, apiError{Error: "richiesta annullata"}
	}
	return http.StatusInternalServerError, apiError{Error: "errore interno del server"}
}

// userMessage restituisce il messaggio di errore da mostrare per un job fallito.
// Gli errori delle pipeline e del modello sono già messaggi in italiano per l'utente.
func userMessage(err error) string {
	if code, body := classify(err); code != http.StatusInternalServerError {
		return body.Error
	}
	return err.Error()
}

// --- helper ---

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// readJSON decodifica il body (max 64 KiB) rifiutando campi sconosciuti.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "body JSON non valido: "+err.Error())
		return false
	}
	return true
}

// intParam legge un parametro intero opzionale della query compreso tra lo e hi.
func intParam(r *http.Request, name string, def, lo, hi int) (int, bool) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def, true
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		return 0, false
	}
	return n, true
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
