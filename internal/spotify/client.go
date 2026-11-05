// Package spotify è un client minimale per la Spotify Web API che usa gli
// endpoint in vigore dopo le modifiche di febbraio 2026
// (/me/library, /me/playlists, /playlists/{id}/items, search limit ≤ 10).
package spotify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const apiBase = "https://api.spotify.com/v1"

type Client struct {
	http *http.Client
}

// New crea un client che usa h (già autenticato, es. oauth2.NewClient).
func New(h *http.Client) *Client { return &Client{http: h} }

type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("spotify: HTTP %d: %s", e.Status, e.Message)
}

// do esegue una richiesta con retry su 429 (rispettando Retry-After) e 5xx.
func (c *Client) do(ctx context.Context, method, rawURL string, body any, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	if !strings.HasPrefix(rawURL, "http") {
		rawURL = apiBase + rawURL
	}
	for attempt := 0; ; attempt++ {
		var rd io.Reader
		if payload != nil {
			rd = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, rawURL, rd)
		if err != nil {
			return err
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500) && attempt < 4 {
			wait := time.Duration(1<<attempt) * time.Second
			if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
				wait = time.Duration(s) * time.Second
			}
			select {
			case <-time.After(wait):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if resp.StatusCode >= 300 {
			var e struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			_ = json.Unmarshal(data, &e)
			msg := e.Error.Message
			if msg == "" {
				msg = strings.TrimSpace(string(data))
			}
			return &APIError{Status: resp.StatusCode, Message: msg}
		}
		if out != nil && len(data) > 0 {
			return json.Unmarshal(data, out)
		}
		return nil
	}
}

// --- modelli ---

type Artist struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Genres []string `json:"genres"`
}

type Image struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type Album struct {
	Name        string  `json:"name"`
	ReleaseDate string  `json:"release_date"`
	Images      []Image `json:"images"`
}

// Cover restituisce l'URL della copertina più piccola con lato ≥ 200px,
// altrimenti la prima disponibile.
func (a Album) Cover() string {
	best := -1
	for i, im := range a.Images {
		if im.Width >= 200 && (best < 0 || im.Width < a.Images[best].Width) {
			best = i
		}
	}
	if best < 0 && len(a.Images) > 0 {
		best = 0
	}
	if best < 0 {
		return ""
	}
	return a.Images[best].URL
}

type Track struct {
	ID           string            `json:"id"`
	URI          string            `json:"uri"`
	Name         string            `json:"name"`
	Artists      []Artist          `json:"artists"`
	Album        Album             `json:"album"`
	ExternalURLs map[string]string `json:"external_urls"`
	IsLocal      bool              `json:"is_local"`
}

func (t Track) URL() string { return t.ExternalURLs["spotify"] }

func (t Track) ArtistNames() []string {
	out := make([]string, len(t.Artists))
	for i, a := range t.Artists {
		out[i] = a.Name
	}
	return out
}

func (t Track) Year() string {
	if len(t.Album.ReleaseDate) >= 4 {
		return t.Album.ReleaseDate[:4]
	}
	return ""
}

type User struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type Playlist struct {
	ID    string            `json:"id"`
	Name  string            `json:"name"`
	Owner User              `json:"owner"`
	URL   string            `json:"-"`
	Ext   map[string]string `json:"external_urls"`
}

type page[T any] struct {
	Items []T    `json:"items"`
	Next  string `json:"next"`
	Total int    `json:"total"`
}

// paginate segue i link `next` finché max (0 = illimitato) non è raggiunto.
func paginate[T any](ctx context.Context, c *Client, first string, max int, onPage func(n int)) ([]T, error) {
	var all []T
	next := first
	for next != "" {
		var p page[T]
		if err := c.do(ctx, http.MethodGet, next, nil, &p); err != nil {
			return all, err
		}
		all = append(all, p.Items...)
		if onPage != nil {
			onPage(len(all))
		}
		if max > 0 && len(all) >= max {
			return all[:max], nil
		}
		next = p.Next
	}
	return all, nil
}

// --- lettura libreria ---

func (c *Client) Me(ctx context.Context) (*User, error) {
	var u User
	return &u, c.do(ctx, http.MethodGet, "/me", nil, &u)
}

// SavedTrack è un brano della libreria con la data in cui è stato salvato.
type SavedTrack struct {
	AddedAt time.Time `json:"added_at"`
	Track   Track     `json:"track"`
}

// SavedTracks scarica i brani salvati ("Brani che ti piacciono"), dal più recente.
func (c *Client) SavedTracks(ctx context.Context, progress func(int)) ([]SavedTrack, error) {
	return paginate[SavedTrack](ctx, c, "/me/tracks?limit=50", 0, progress)
}

// TimeRange: short_term (~4 settimane), medium_term (~6 mesi), long_term (~1 anno).
var TimeRanges = []string{"short_term", "medium_term", "long_term"}

func (c *Client) TopArtists(ctx context.Context, timeRange string) ([]Artist, error) {
	return paginate[Artist](ctx, c, "/me/top/artists?limit=50&time_range="+timeRange, 50, nil)
}

func (c *Client) TopTracks(ctx context.Context, timeRange string) ([]Track, error) {
	return paginate[Track](ctx, c, "/me/top/tracks?limit=50&time_range="+timeRange, 50, nil)
}

func (c *Client) MyPlaylists(ctx context.Context) ([]Playlist, error) {
	return paginate[Playlist](ctx, c, "/me/playlists?limit=50", 0, nil)
}

// PlaylistTracks legge gli item di una playlist (solo playlist proprie o collaborative).
func (c *Client) PlaylistTracks(ctx context.Context, id string) ([]Track, error) {
	type item struct {
		Item  *Track `json:"item"`
		Track *Track `json:"track"` // deprecato, fallback
	}
	items, err := paginate[item](ctx, c, "/playlists/"+id+"/items?limit=50&additional_types=track", 0, nil)
	var out []Track
	for _, it := range items {
		t := it.Item
		if t == nil {
			t = it.Track
		}
		if t != nil && t.ID != "" && !t.IsLocal {
			out = append(out, *t)
		}
	}
	return out, err
}

// SearchTrack cerca un brano (limit massimo 10 dopo febbraio 2026).
func (c *Client) SearchTrack(ctx context.Context, artist, title string, limit int) ([]Track, error) {
	if limit <= 0 || limit > 10 {
		limit = 10
	}
	q := fmt.Sprintf("track:%s artist:%s", quoteTerm(title), quoteTerm(artist))
	v := url.Values{"q": {q}, "type": {"track"}, "limit": {strconv.Itoa(limit)}}
	var res struct {
		Tracks page[Track] `json:"tracks"`
	}
	if err := c.do(ctx, http.MethodGet, "/search?"+v.Encode(), nil, &res); err != nil {
		return nil, err
	}
	return res.Tracks.Items, nil
}

func quoteTerm(s string) string {
	s = strings.ReplaceAll(s, `"`, "")
	return `"` + s + `"`
}

// --- scrittura ---

func (c *Client) CreatePlaylist(ctx context.Context, name, description string) (*Playlist, error) {
	body := map[string]any{"name": name, "description": description, "public": false}
	var p Playlist
	if err := c.do(ctx, http.MethodPost, "/me/playlists", body, &p); err != nil {
		return nil, err
	}
	p.URL = p.Ext["spotify"]
	return &p, nil
}

func (c *Client) AddToPlaylist(ctx context.Context, playlistID string, uris []string) error {
	for _, chunk := range chunks(uris, 100) {
		if err := c.do(ctx, http.MethodPost, "/playlists/"+playlistID+"/items", map[string]any{"uris": chunk}, nil); err != nil {
			return err
		}
	}
	return nil
}

// SaveToLibrary salva brani nella libreria (PUT /me/library, max 40 URI per chiamata).
func (c *Client) SaveToLibrary(ctx context.Context, uris []string) error {
	for _, chunk := range chunks(uris, 40) {
		v := url.Values{"uris": {strings.Join(chunk, ",")}}
		if err := c.do(ctx, http.MethodPut, "/me/library?"+v.Encode(), nil, nil); err != nil {
			return err
		}
	}
	return nil
}

// RemoveFromLibrary rimuove brani dalla libreria (DELETE /me/library, max 40 URI per chiamata).
func (c *Client) RemoveFromLibrary(ctx context.Context, uris []string) error {
	for _, chunk := range chunks(uris, 40) {
		v := url.Values{"uris": {strings.Join(chunk, ",")}}
		if err := c.do(ctx, http.MethodDelete, "/me/library?"+v.Encode(), nil, nil); err != nil {
			return err
		}
	}
	return nil
}

func chunks(s []string, n int) [][]string {
	var out [][]string
	for len(s) > 0 {
		k := min(n, len(s))
		out = append(out, s[:k])
		s = s[k:]
	}
	return out
}
