package api_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rubenpari/tunescout-web/internal/api"
	"github.com/rubenpari/tunescout-web/internal/auth"
	"github.com/rubenpari/tunescout-web/internal/jobs"
	"github.com/rubenpari/tunescout-web/internal/llm"
	"github.com/rubenpari/tunescout-web/internal/spotify"
	"github.com/rubenpari/tunescout-web/internal/store"
	"github.com/rubenpari/tunescout-web/internal/store/storetest"
)

func TestMain(m *testing.M) { storetest.Main(m) }

// --- finti Spotify e Claude ---

func trk(id, artist, title string) spotify.Track {
	return spotify.Track{ID: id, URI: "spotify:track:" + id, Name: title,
		Artists: []spotify.Artist{{Name: artist}}, Album: spotify.Album{Name: "Album", ReleaseDate: "2001-01-01"}}
}

// id22 crea un id Spotify valido (22 caratteri alfanumerici).
func id22(s string) string { return (s + strings.Repeat("0", 22))[:22] }

type fakeSpotify struct {
	mu      sync.Mutex
	userID  string
	saved   []spotify.SavedTrack
	catalog map[string]spotify.Track
	savedTo []string
	removed []string
	plists  map[string][]string
}

func newFakeSpotify(userID string) *fakeSpotify {
	old := time.Now().AddDate(-2, 0, 0)
	return &fakeSpotify{
		userID: userID,
		saved: []spotify.SavedTrack{
			{AddedAt: old, Track: trk(id22("known1"), "Radiohead", "Creep")},
			{AddedAt: old, Track: trk(id22("old1"), "Band Dimenticata", "Brano Vecchio")},
		},
		catalog: map[string]spotify.Track{
			"slowdive|alison":   trk(id22("slow1"), "Slowdive", "Alison"),
			"ride|vapour trail": trk(id22("ride1"), "Ride", "Vapour Trail"),
		},
		plists: map[string][]string{},
	}
}

func (f *fakeSpotify) Me(context.Context) (*spotify.User, error) {
	return &spotify.User{ID: f.userID, DisplayName: f.userID}, nil
}
func (f *fakeSpotify) SavedTracks(context.Context, func(int)) ([]spotify.SavedTrack, error) {
	return f.saved, nil
}
func (f *fakeSpotify) TopArtists(context.Context, string) ([]spotify.Artist, error) {
	return []spotify.Artist{{Name: "Radiohead", Genres: []string{"alternative rock"}}}, nil
}
func (f *fakeSpotify) TopTracks(context.Context, string) ([]spotify.Track, error) {
	return []spotify.Track{f.saved[0].Track}, nil
}
func (f *fakeSpotify) MyPlaylists(context.Context) ([]spotify.Playlist, error) { return nil, nil }
func (f *fakeSpotify) PlaylistTracks(context.Context, string) ([]spotify.Track, error) {
	return nil, nil
}
func (f *fakeSpotify) SearchTrack(_ context.Context, artist, title string, _ int) ([]spotify.Track, error) {
	if t, ok := f.catalog[strings.ToLower(artist+"|"+title)]; ok {
		return []spotify.Track{t}, nil
	}
	return nil, nil
}
func (f *fakeSpotify) SaveToLibrary(_ context.Context, uris []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.savedTo = append(f.savedTo, uris...)
	return nil
}
func (f *fakeSpotify) RemoveFromLibrary(_ context.Context, uris []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, uris...)
	return nil
}
func (f *fakeSpotify) CreatePlaylist(_ context.Context, name, _ string) (*spotify.Playlist, error) {
	return &spotify.Playlist{ID: "pl1", Name: name, URL: "https://open.spotify.com/playlist/pl1"}, nil
}
func (f *fakeSpotify) AddToPlaylist(_ context.Context, id string, uris []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.plists[id] = append(f.plists[id], uris...)
	return nil
}

type fakeLLM struct{ block chan struct{} }

func (f *fakeLLM) Recommend(ctx context.Context, _ llm.Request) ([]llm.Suggestion, error) {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return []llm.Suggestion{
		{Artist: "Slowdive", Title: "Alison", Reason: "shoegaze sognante"},
		{Artist: "Ride", Title: "Vapour Trail", Reason: "chitarre stratificate"},
		{Artist: "Radiohead", Title: "Creep", Reason: "già noto"},
		{Artist: "Inventati", Title: "Non Esiste", Reason: "allucinazione"},
	}, nil
}

func (f *fakeLLM) PickPrune(_ context.Context, req llm.PruneRequest) ([]llm.PrunePick, error) {
	return []llm.PrunePick{{ID: 0, Reason: "lontano dai tuoi gusti"}}, nil
}

// --- ambiente di test ---

type env struct {
	t      *testing.T
	db     *store.DB
	srv    *api.Server
	ts     *httptest.Server
	am     *auth.Manager
	fakeSP map[int64]*fakeSpotify
	llm    *fakeLLM
}

func newEnv(t *testing.T, limit int) *env {
	db := storetest.DB(t)
	sealer, err := auth.NewSealer(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, db: db, fakeSP: map[int64]*fakeSpotify{}, llm: &fakeLLM{}}
	e.am = &auth.Manager{DB: db, Sealer: sealer, OAuth: spotify.OAuthConfig("id", "secret", "http://x/api/auth/callback")}
	runner := jobs.NewRunner(db, 2)
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })
	e.srv = &api.Server{
		DB: db, Auth: e.am, Jobs: runner, LLM: e.llm, DailyRunLimit: limit,
		SpotifyFor: func(_ context.Context, uid int64) (api.Spotify, error) {
			if sp, ok := e.fakeSP[uid]; ok {
				return sp, nil
			}
			return nil, auth.ErrReauth
		},
	}
	e.ts = httptest.NewServer(e.srv.Handler())
	e.srv.BaseURL = e.ts.URL
	t.Cleanup(e.ts.Close)
	return e
}

type client struct {
	e      *env
	cookie *http.Cookie
	uid    int64
	sp     *fakeSpotify
}

func (e *env) login(spotifyID string) *client {
	u, err := e.db.UpsertUser(context.Background(), spotifyID, spotifyID)
	if err != nil {
		e.t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if _, err := e.am.IssueSession(context.Background(), rec, u.ID); err != nil {
		e.t.Fatal(err)
	}
	sp := newFakeSpotify(spotifyID)
	e.fakeSP[u.ID] = sp
	return &client{e: e, cookie: rec.Result().Cookies()[0], uid: u.ID, sp: sp}
}

func (c *client) do(method, path string, body any, out any) int {
	c.e.t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, c.e.ts.URL+path, rd)
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

// runJob avvia un job, segue lo stream SSE fino a "done" e restituisce il job finale.
func (c *client) runJob(kind string, params any) (*store.Job, []string) {
	c.e.t.Helper()
	var j store.Job
	if code := c.do("POST", "/api/jobs/"+kind, params, &j); code != http.StatusAccepted {
		c.e.t.Fatalf("avvio job %s: HTTP %d", kind, code)
	}
	return c.follow(j.ID)
}

func (c *client) follow(id string) (*store.Job, []string) {
	c.e.t.Helper()
	req, _ := http.NewRequest("GET", c.e.ts.URL+"/api/jobs/"+id+"/events", nil)
	req.AddCookie(c.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		c.e.t.Fatalf("events: HTTP %d", resp.StatusCode)
	}
	var logs []string
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok {
			continue
		}
		var ev jobs.Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			c.e.t.Fatal(err)
		}
		if ev.Type == jobs.EventLog {
			logs = append(logs, ev.Message)
		}
		if ev.Type == jobs.EventDone {
			return ev.Job, logs
		}
	}
	c.e.t.Fatal("stream chiuso senza evento done")
	return nil, nil
}

// --- test ---

func TestRequiresLogin(t *testing.T) {
	e := newEnv(t, 0)
	anon := &client{e: e}
	for _, p := range []string{"/api/me", "/api/history", "/api/jobs/x"} {
		if code := anon.do("GET", p, nil, nil); code != http.StatusUnauthorized {
			t.Errorf("%s: HTTP %d, atteso 401", p, code)
		}
	}
	if code := anon.do("POST", "/api/jobs/sync", struct{}{}, nil); code != http.StatusUnauthorized {
		t.Errorf("POST sync anonimo: HTTP %d", code)
	}
}

func TestDiscoverFlow(t *testing.T) {
	e := newEnv(t, 0)
	c := e.login("alice")

	// discover senza sync preventivo: la sync parte da sola.
	j, logs := c.runJob("discover", api.DiscoverParams{Count: 2})
	if j.Status != store.JobDone {
		t.Fatalf("discover: stato %s, errore %q", j.Status, j.Error)
	}
	if !slices.ContainsFunc(logs, func(s string) bool { return strings.Contains(s, "sincronizzo") }) {
		t.Errorf("attesa sync automatica nei log: %v", logs)
	}
	var res api.DiscoverResult
	if err := json.Unmarshal(j.Result, &res); err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, s := range res.Suggestions {
		titles = append(titles, s.Title)
	}
	slices.Sort(titles)
	if !slices.Equal(titles, []string{"Alison", "Vapour Trail"}) {
		t.Fatalf("suggerimenti = %v (attesi solo i brani nuovi e verificati)", titles)
	}

	// like: salva in libreria e registra il feedback.
	key := res.Suggestions[0].Key
	if code := c.do("POST", "/api/feedback", map[string]any{"keys": []string{key}, "feedback": "like"}, nil); code != 200 {
		t.Fatalf("feedback: HTTP %d", code)
	}
	if !slices.Contains(c.sp.savedTo, res.Suggestions[0].URI) {
		t.Errorf("like non ha salvato in libreria: %v", c.sp.savedTo)
	}
	var hist struct{ Items []store.Suggestion }
	c.do("GET", "/api/history?feedback=like", nil, &hist)
	if len(hist.Items) != 1 || hist.Items[0].Key != key {
		t.Errorf("storico like = %+v", hist.Items)
	}

	// playlist
	var pl map[string]any
	uris := []string{res.Suggestions[0].URI, res.Suggestions[1].URI}
	if code := c.do("POST", "/api/playlists", map[string]any{"name": "Scoperte", "uris": uris}, &pl); code != http.StatusCreated {
		t.Fatalf("playlist: HTTP %d %v", code, pl)
	}
	if code := c.do("POST", "/api/playlists", map[string]any{"name": "X", "uris": []string{"javascript:alert(1)"}}, nil); code != 400 {
		t.Errorf("URI non valido accettato: HTTP %d", code)
	}

	// /api/me riflette la sync
	var me struct {
		Library struct{ Tracks int }
	}
	c.do("GET", "/api/me", nil, &me)
	if me.Library.Tracks == 0 {
		t.Error("/api/me: libreria vuota dopo la sync")
	}
}

func TestPruneRemoveOnlyProposed(t *testing.T) {
	e := newEnv(t, 0)
	c := e.login("alice")
	j, _ := c.runJob("prune", api.PruneParams{Count: 5, MinAgeDays: 30})
	if j.Status != store.JobDone {
		t.Fatalf("prune: %s %q", j.Status, j.Error)
	}
	var res api.PruneResult
	_ = json.Unmarshal(j.Result, &res)
	if len(res.Items) != 1 || res.Items[0].Title != "Brano Vecchio" {
		t.Fatalf("prune items = %+v", res.Items)
	}
	// Un URI non proposto dal job viene rifiutato.
	other := "spotify:track:" + id22("known1")
	if code := c.do("POST", "/api/jobs/"+j.ID+"/remove", map[string]any{"uris": []string{other}}, nil); code != 400 {
		t.Errorf("rimozione di un brano non proposto: HTTP %d", code)
	}
	uri := res.Items[0].URI
	if code := c.do("POST", "/api/jobs/"+j.ID+"/remove", map[string]any{"uris": []string{uri}}, nil); code != 200 {
		t.Fatalf("remove: HTTP %d", code)
	}
	if !slices.Equal(c.sp.removed, []string{uri}) {
		t.Errorf("rimossi da Spotify = %v", c.sp.removed)
	}
	if seen, _ := e.db.ForUser(context.Background(), c.uid).Seen(res.Items[0].Key, ""); !seen {
		t.Error("il brano rimosso deve essere escluso da discover")
	}
}

func TestPruneKeepExcludesFromNextRun(t *testing.T) {
	e := newEnv(t, 0)
	c := e.login("alice")
	run := func() (string, []api.PruneItem) {
		t.Helper()
		j, _ := c.runJob("prune", api.PruneParams{Count: 5, MinAgeDays: 30})
		if j.Status != store.JobDone {
			t.Fatalf("prune: %s %q", j.Status, j.Error)
		}
		var res api.PruneResult
		_ = json.Unmarshal(j.Result, &res)
		return j.ID, res.Items
	}
	id, items := run()
	if len(items) != 1 {
		t.Fatalf("prune items = %+v", items)
	}
	uri := items[0].URI
	other := "spotify:track:" + id22("known1")
	if code := c.do("POST", "/api/jobs/"+id+"/keep", map[string]any{"uri": other}, nil); code != 400 {
		t.Errorf("keep di un brano non proposto: HTTP %d", code)
	}
	if code := c.do("POST", "/api/jobs/"+id+"/keep", map[string]any{"uri": uri}, nil); code != 200 {
		t.Fatalf("keep: HTTP %d", code)
	}
	if _, items := run(); len(items) != 0 {
		t.Errorf("il brano tenuto è stato riproposto: %+v", items)
	}
	if code := c.do("DELETE", "/api/jobs/"+id+"/keep", map[string]any{"uri": uri}, nil); code != 200 {
		t.Fatalf("unkeep: HTTP %d", code)
	}
	if _, items := run(); len(items) != 1 || items[0].URI != uri {
		t.Errorf("dopo l'annullamento il brano deve tornare candidato: %+v", items)
	}
}

func TestUsersCannotAccessOthersJobs(t *testing.T) {
	e := newEnv(t, 0)
	alice, bob := e.login("alice"), e.login("bob")
	j, _ := alice.runJob("prune", api.PruneParams{Count: 5, MinAgeDays: 30})
	var res api.PruneResult
	_ = json.Unmarshal(j.Result, &res)

	if code := bob.do("GET", "/api/jobs/"+j.ID, nil, nil); code != 404 {
		t.Errorf("bob legge il job di alice: HTTP %d", code)
	}
	if code := bob.do("GET", "/api/jobs/"+j.ID+"/events", nil, nil); code != 404 {
		t.Errorf("bob segue gli eventi di alice: HTTP %d", code)
	}
	if code := bob.do("POST", "/api/jobs/"+j.ID+"/remove", map[string]any{"uris": []string{res.Items[0].URI}}, nil); code != 404 {
		t.Errorf("bob rimuove usando il job di alice: HTTP %d", code)
	}
	if code := bob.do("POST", "/api/jobs/"+j.ID+"/keep", map[string]any{"uri": res.Items[0].URI}, nil); code != 404 {
		t.Errorf("bob tiene un brano usando il job di alice: HTTP %d", code)
	}
	if len(bob.sp.removed)+len(alice.sp.removed) != 0 {
		t.Error("nessuna rimozione doveva avvenire")
	}
}

func TestDailyLimitAndBusy(t *testing.T) {
	e := newEnv(t, 1)
	c := e.login("alice")
	e.llm.block = make(chan struct{})

	var j store.Job
	if code := c.do("POST", "/api/jobs/discover", api.DiscoverParams{Count: 2}, &j); code != http.StatusAccepted {
		t.Fatalf("primo discover: HTTP %d", code)
	}
	// Un secondo job mentre il primo è in corso → 409.
	if code := c.do("POST", "/api/jobs/sync", struct{}{}, nil); code != http.StatusConflict {
		t.Errorf("job concorrente: HTTP %d, atteso 409", code)
	}
	close(e.llm.block)
	if done, _ := c.follow(j.ID); done.Status != store.JobDone {
		t.Fatalf("discover: %s %q", done.Status, done.Error)
	}
	// Limite giornaliero di 1 raggiunto → 429; la sync non è conteggiata.
	if code := c.do("POST", "/api/jobs/discover", api.DiscoverParams{Count: 2}, nil); code != http.StatusTooManyRequests {
		t.Errorf("oltre il limite: HTTP %d, atteso 429", code)
	}
	if code := c.do("POST", "/api/jobs/sync", struct{}{}, nil); code != http.StatusAccepted {
		t.Errorf("sync oltre il limite: HTTP %d, attesa 202", code)
	}
}

func TestValidation(t *testing.T) {
	e := newEnv(t, 0)
	c := e.login("alice")
	cases := []struct {
		path string
		body any
	}{
		{"/api/jobs/discover", api.DiscoverParams{Count: 0}},
		{"/api/jobs/discover", api.DiscoverParams{Count: 51}},
		{"/api/jobs/discover", map[string]any{"count": 5, "unknown": true}},
		{"/api/jobs/prune", api.PruneParams{Count: 5, MinAgeDays: -1}},
		{"/api/feedback", map[string]any{"keys": []string{"x"}, "feedback": "love"}},
	}
	for _, tc := range cases {
		if code := c.do("POST", tc.path, tc.body, nil); code != 400 {
			t.Errorf("%s %v: HTTP %d, atteso 400", tc.path, tc.body, code)
		}
	}
}

func TestCrossOriginRejected(t *testing.T) {
	e := newEnv(t, 0)
	c := e.login("alice")
	req, _ := http.NewRequest("POST", e.ts.URL+"/api/jobs/sync", strings.NewReader("{}"))
	req.AddCookie(c.cookie)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("POST cross-origin: HTTP %d, atteso 403", resp.StatusCode)
	}
}

func TestLogoutAndDelete(t *testing.T) {
	e := newEnv(t, 0)
	c := e.login("alice")
	if code := c.do("DELETE", "/api/me", nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete: HTTP %d", code)
	}
	if code := c.do("GET", "/api/me", nil, nil); code != http.StatusUnauthorized {
		t.Errorf("dopo la cancellazione la sessione deve essere invalida: HTTP %d", code)
	}
}

func TestLoginCallbackErrors(t *testing.T) {
	e := newEnv(t, 0)
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	callback := func(query string, cookie *http.Cookie) string {
		t.Helper()
		req, _ := http.NewRequest("GET", e.ts.URL+"/api/auth/callback?"+query, nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		resp, err := noFollow.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("callback: HTTP %d, voglio 302", resp.StatusCode)
		}
		loc, _ := url.Parse(resp.Header.Get("Location"))
		return loc.Query().Get("error")
	}

	if msg := callback("code=c&state=s", nil); !strings.Contains(msg, "Sessione di login scaduta") {
		t.Errorf("state mancante: messaggio %q", msg)
	}
	state := &http.Cookie{Name: "ts_oauth_state", Value: "s"}
	if msg := callback("error=access_denied&state=s", state); !strings.Contains(msg, "negato") {
		t.Errorf("accesso negato: messaggio %q", msg)
	}
}
