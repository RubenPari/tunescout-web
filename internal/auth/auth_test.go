package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/rubenpari/tunescout-web/internal/auth"
	"github.com/rubenpari/tunescout-web/internal/spotify"
	"github.com/rubenpari/tunescout-web/internal/store/storetest"
)

func TestMain(m *testing.M) { storetest.Main(m) }

func TestSealer(t *testing.T) {
	s, err := auth.NewSealer(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	sealed := s.Seal([]byte("segreto"), []byte("user:1"))
	if bytes.Contains(sealed, []byte("segreto")) {
		t.Fatal("il testo in chiaro compare nel dato cifrato")
	}
	if got, err := s.Open(sealed, []byte("user:1")); err != nil || string(got) != "segreto" {
		t.Fatalf("Open = %q, %v", got, err)
	}
	if _, err := s.Open(sealed, []byte("user:2")); err == nil {
		t.Error("un token cifrato per un utente non deve aprirsi per un altro")
	}
}

// fakeSpotifyTransport risponde al posto di accounts.spotify.com e api.spotify.com.
type fakeSpotifyTransport struct {
	refreshes int
}

func (f *fakeSpotifyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	switch {
	case r.URL.Host == "accounts.spotify.com" && r.URL.Path == "/api/token":
		_ = r.ParseForm()
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			if r.Form.Get("code") != "good-code" {
				rec.WriteHeader(400)
				_, _ = rec.WriteString(`{"error":"invalid_grant"}`)
				break
			}
			rec.Header().Set("Content-Type", "application/json")
			_, _ = rec.WriteString(`{"access_token":"at1","refresh_token":"rt1","token_type":"Bearer","expires_in":-10}`)
		case "refresh_token":
			f.refreshes++
			rec.Header().Set("Content-Type", "application/json")
			_, _ = rec.WriteString(`{"access_token":"at2","refresh_token":"rt2","token_type":"Bearer","expires_in":3600}`)
		}
	case r.URL.Host == "api.spotify.com" && r.URL.Path == "/v1/me":
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			rec.WriteHeader(401)
			break
		}
		_ = json.NewEncoder(rec).Encode(map[string]string{"id": "spotify-alice", "display_name": "Alice"})
	default:
		rec.WriteHeader(404)
	}
	return rec.Result(), nil
}

func TestLoginFlow(t *testing.T) {
	db := storetest.DB(t)
	sealer, _ := auth.NewSealer(bytes.Repeat([]byte{7}, 32))
	tr := &fakeSpotifyTransport{}
	m := &auth.Manager{
		DB: db, Sealer: sealer,
		OAuth:      spotify.OAuthConfig("cid", "secret", "http://app/api/auth/callback"),
		HTTPClient: &http.Client{Transport: tr},
	}

	// 1. /login imposta lo state e reindirizza a Spotify.
	rec := httptest.NewRecorder()
	m.StartLogin(rec, httptest.NewRequest("GET", "/api/auth/login", nil))
	loc, _ := url.Parse(rec.Header().Get("Location"))
	state := loc.Query().Get("state")
	if loc.Host != "accounts.spotify.com" || state == "" {
		t.Fatalf("redirect inatteso: %s", loc)
	}
	stateCookie := rec.Result().Cookies()[0]

	// 2. state diverso → rifiutato.
	bad := httptest.NewRequest("GET", "/api/auth/callback?code=good-code&state=altro", nil)
	bad.AddCookie(stateCookie)
	if _, err := m.FinishLogin(httptest.NewRecorder(), bad); err == nil {
		t.Fatal("callback con state errato accettata")
	}

	// 3. callback valida → utente, token cifrato e sessione.
	req := httptest.NewRequest("GET", "/api/auth/callback?code=good-code&state="+state, nil)
	req.AddCookie(stateCookie)
	rec = httptest.NewRecorder()
	u, err := m.FinishLogin(rec, req)
	if err != nil {
		t.Fatal(err)
	}
	if u.SpotifyID != "spotify-alice" || u.DisplayName != "Alice" {
		t.Errorf("utente = %+v", u)
	}
	enc, err := db.LoadToken(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(enc, []byte("rt1")) {
		t.Error("il refresh token è salvato in chiaro")
	}
	var session *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SessionCookie {
			session = c
		}
	}
	if session == nil || !session.HttpOnly {
		t.Fatalf("cookie di sessione mancante o non HttpOnly: %+v", session)
	}
	r2 := httptest.NewRequest("GET", "/api/me", nil)
	r2.AddCookie(session)
	if uid, err := m.UserID(r2); err != nil || uid != u.ID {
		t.Fatalf("UserID = %d, %v", uid, err)
	}

	// 4. Il client Spotify rinnova il token scaduto e salva quello nuovo.
	sp, err := m.Spotify(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sp.Me(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tr.refreshes != 1 {
		t.Errorf("refresh eseguiti = %d, atteso 1", tr.refreshes)
	}
	enc2, _ := db.LoadToken(context.Background(), u.ID)
	plain, err := sealer.Open(enc2, []byte("spotify-token:"+itoa(u.ID)))
	if err != nil || !bytes.Contains(plain, []byte("rt2")) {
		t.Errorf("token rinnovato non salvato: %s %v", plain, err)
	}

	// 5. Logout invalida la sessione.
	r3 := httptest.NewRequest("POST", "/api/auth/logout", nil)
	r3.AddCookie(session)
	if err := m.Logout(httptest.NewRecorder(), r3); err != nil {
		t.Fatal(err)
	}
	if _, err := m.UserID(r2); err == nil {
		t.Error("sessione ancora valida dopo il logout")
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
