// Package auth gestisce login Spotify, sessioni e token per utente.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"github.com/rubenpari/tunescout-web/internal/spotify"
	"github.com/rubenpari/tunescout-web/internal/store"
)

const (
	SessionCookie = "ts_session"
	stateCookie   = "ts_oauth_state"
	SessionTTL    = 30 * 24 * time.Hour
)

// ErrReauth indica che il token Spotify non è più valido e serve un nuovo login.
var ErrReauth = errors.New("autorizzazione Spotify scaduta o revocata: effettua di nuovo l'accesso")

type Manager struct {
	DB     *store.DB
	Sealer *Sealer
	OAuth  *oauth2.Config
	Secure bool // cookie Secure (HTTPS)

	// HTTPClient è usato per le chiamate OAuth e Spotify (sostituibile nei test).
	HTTPClient *http.Client
}

func (m *Manager) ctx(ctx context.Context) context.Context {
	if m.HTTPClient != nil {
		return context.WithValue(ctx, oauth2.HTTPClient, m.HTTPClient)
	}
	return ctx
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func hash(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

// StartLogin imposta il cookie di state e reindirizza a Spotify.
func (m *Manager) StartLogin(w http.ResponseWriter, r *http.Request) {
	state := randomToken()
	http.SetCookie(w, &http.Cookie{
		Name: stateCookie, Value: state, Path: "/api/auth", MaxAge: 600,
		HttpOnly: true, Secure: m.Secure, SameSite: http.SameSiteLaxMode,
	})
	// show_dialog permette di cambiare account dopo un logout.
	http.Redirect(w, r, m.OAuth.AuthCodeURL(state, oauth2.SetAuthURLParam("show_dialog", "true")), http.StatusFound)
}

// FinishLogin valida la callback OAuth, salva utente e token e crea la sessione.
func (m *Manager) FinishLogin(w http.ResponseWriter, r *http.Request) (*store.User, error) {
	q := r.URL.Query()
	c, err := r.Cookie(stateCookie)
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Path: "/api/auth", MaxAge: -1, HttpOnly: true, Secure: m.Secure})
	if err != nil || c.Value == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(q.Get("state"))) != 1 {
		return nil, errors.New("state OAuth non valido: riprova l'accesso")
	}
	if e := q.Get("error"); e != "" {
		return nil, fmt.Errorf("autorizzazione negata da Spotify: %s", e)
	}
	ctx := m.ctx(r.Context())
	tok, err := m.OAuth.Exchange(ctx, q.Get("code"))
	if err != nil {
		return nil, fmt.Errorf("scambio del codice OAuth: %w", err)
	}
	ts := oauth2.ReuseTokenSource(tok, m.OAuth.TokenSource(ctx, tok))
	me, err := spotify.New(oauth2.NewClient(ctx, ts)).Me(ctx)
	if err != nil {
		return nil, fmt.Errorf("lettura profilo Spotify: %w", err)
	}
	// Salva il token più recente (potrebbe essere stato rinnovato da Me).
	if tok, err = ts.Token(); err != nil {
		return nil, err
	}
	name := me.DisplayName
	if name == "" {
		name = me.ID
	}
	u, err := m.DB.UpsertUser(r.Context(), me.ID, name)
	if err != nil {
		return nil, err
	}
	if err := m.saveToken(r.Context(), u.ID, tok); err != nil {
		return nil, err
	}
	if _, err := m.IssueSession(r.Context(), w, u.ID); err != nil {
		return nil, err
	}
	return u, nil
}

// IssueSession crea una sessione per l'utente e imposta il cookie.
// Restituisce l'id di sessione (solo il suo hash è salvato nel DB).
func (m *Manager) IssueSession(ctx context.Context, w http.ResponseWriter, userID int64) (string, error) {
	sid := randomToken()
	if err := m.DB.CreateSession(ctx, hash(sid), userID, time.Now().Add(SessionTTL)); err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: sid, Path: "/", MaxAge: int(SessionTTL.Seconds()),
		HttpOnly: true, Secure: m.Secure, SameSite: http.SameSiteLaxMode,
	})
	return sid, nil
}

func (m *Manager) Logout(w http.ResponseWriter, r *http.Request) error {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: m.Secure})
	if c, err := r.Cookie(SessionCookie); err == nil {
		return m.DB.DeleteSession(r.Context(), hash(c.Value))
	}
	return nil
}

// UserID restituisce l'utente della sessione corrente.
func (m *Manager) UserID(r *http.Request) (int64, error) {
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return 0, store.ErrNotFound
	}
	return m.DB.SessionUser(r.Context(), hash(c.Value))
}

// --- token Spotify per utente ---

func aad(userID int64) []byte { return []byte("spotify-token:" + strconv.FormatInt(userID, 10)) }

func (m *Manager) saveToken(ctx context.Context, userID int64, t *oauth2.Token) error {
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return m.DB.SaveToken(ctx, userID, m.Sealer.Seal(b, aad(userID)))
}

func (m *Manager) loadToken(ctx context.Context, userID int64) (*oauth2.Token, error) {
	enc, err := m.DB.LoadToken(ctx, userID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrReauth
	}
	if err != nil {
		return nil, err
	}
	b, err := m.Sealer.Open(enc, aad(userID))
	if err != nil {
		return nil, ErrReauth // chiave cambiata o dato corrotto
	}
	var t oauth2.Token
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, ErrReauth
	}
	return &t, nil
}

// savingTokenSource salva su DB i token rinnovati (Spotify può ruotare il refresh token).
type savingTokenSource struct {
	mu   sync.Mutex
	m    *Manager
	ctx  context.Context
	uid  int64
	base oauth2.TokenSource
	last string
}

func (s *savingTokenSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.base.Token()
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) {
			return nil, ErrReauth
		}
		return nil, err
	}
	if t.AccessToken != s.last {
		s.last = t.AccessToken
		_ = s.m.saveToken(context.WithoutCancel(s.ctx), s.uid, t)
	}
	return t, nil
}

// Spotify restituisce un client Spotify autenticato per l'utente.
func (m *Manager) Spotify(ctx context.Context, userID int64) (*spotify.Client, error) {
	tok, err := m.loadToken(ctx, userID)
	if err != nil {
		return nil, err
	}
	ctx = m.ctx(ctx)
	ts := &savingTokenSource{m: m, ctx: ctx, uid: userID, base: m.OAuth.TokenSource(ctx, tok), last: tok.AccessToken}
	return spotify.New(oauth2.NewClient(ctx, oauth2.ReuseTokenSource(nil, ts))), nil
}
