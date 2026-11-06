// Package profile sincronizza la libreria Spotify e costruisce un profilo di
// gusto compatto da passare al modello.
package profile

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/rubenpari/tunescout-web/internal/norm"
	"github.com/rubenpari/tunescout-web/internal/spotify"
	"github.com/rubenpari/tunescout-web/internal/store"
)

type ArtistScore struct {
	Name   string   `json:"name"`
	Genres []string `json:"genres,omitempty"`
	Score  float64  `json:"score"`
}

type TrackRef struct {
	Artist string `json:"artist"`
	Title  string `json:"title"`
	Year   string `json:"year,omitempty"`
}

type Snapshot struct {
	TopArtists    []ArtistScore `json:"top_artists"`
	RecentTracks  []TrackRef    `json:"recent_tracks"`   // top short_term
	AllTimeTracks []TrackRef    `json:"all_time_tracks"` // top long_term
	SavedSample   []TrackRef    `json:"saved_sample"`    // ultimi brani salvati
	LibrarySize   int           `json:"library_size"`
	ArtistCount   int           `json:"artist_count"`
}

const snapshotKey = "profile_snapshot"

// Source è la parte del client Spotify usata dalla sincronizzazione.
type Source interface {
	Me(ctx context.Context) (*spotify.User, error)
	SavedTracks(ctx context.Context, progress func(int)) ([]spotify.SavedTrack, error)
	TopArtists(ctx context.Context, timeRange string) ([]spotify.Artist, error)
	TopTracks(ctx context.Context, timeRange string) ([]spotify.Track, error)
	MyPlaylists(ctx context.Context) ([]spotify.Playlist, error)
	PlaylistTracks(ctx context.Context, id string) ([]spotify.Track, error)
}

// Sync scarica la libreria e aggiorna store + snapshot del profilo.
func Sync(ctx context.Context, sp Source, st *store.Store, log func(string, ...any)) error {
	var known []store.KnownTrack
	artists := map[string]string{}
	add := func(t spotify.Track, source string) {
		if t.ID == "" || len(t.Artists) == 0 {
			return
		}
		known = append(known, store.KnownTrack{
			Key: t.Key(), SpotifyID: t.ID,
			Artist: t.Artists[0].Name, Title: t.Name, Source: source,
		})
		for _, a := range t.Artists {
			artists[norm.Artist(a.Name)] = a.Name
		}
	}

	log("Scarico i brani salvati…")
	saved, err := sp.SavedTracks(ctx, func(n int) {
		if n%500 == 0 {
			log("  %d brani…", n)
		}
	})
	if err != nil {
		return fmt.Errorf("brani salvati: %w", err)
	}
	savedTracks := make([]spotify.Track, len(saved))
	for i, s := range saved {
		savedTracks[i] = s.Track
		add(s.Track, store.SourceSaved)
	}
	log("  %d brani salvati", len(saved))

	snap := Snapshot{}
	scores := map[string]*ArtistScore{}
	rangeWeight := map[string]float64{"short_term": 3, "medium_term": 2, "long_term": 1}

	log("Scarico top artisti e top brani…")
	for _, tr := range spotify.TimeRanges {
		top, err := sp.TopArtists(ctx, tr)
		if err != nil {
			return fmt.Errorf("top artisti (%s): %w", tr, err)
		}
		for i, a := range top {
			k := norm.Artist(a.Name)
			artists[k] = a.Name
			s, ok := scores[k]
			if !ok {
				s = &ArtistScore{Name: a.Name, Genres: a.Genres}
				scores[k] = s
			}
			if len(s.Genres) == 0 {
				s.Genres = a.Genres
			}
			s.Score += rangeWeight[tr] * float64(len(top)-i) / float64(len(top))
		}

		tracks, err := sp.TopTracks(ctx, tr)
		if err != nil {
			return fmt.Errorf("top brani (%s): %w", tr, err)
		}
		for _, t := range tracks {
			add(t, store.SourceTop)
		}
		switch tr {
		case "short_term":
			snap.RecentTracks = refs(tracks, 25)
		case "long_term":
			snap.AllTimeTracks = refs(tracks, 25)
		}
	}

	log("Scarico le tue playlist…")
	me, err := sp.Me(ctx)
	if err != nil {
		return fmt.Errorf("profilo utente: %w", err)
	}
	pls, err := sp.MyPlaylists(ctx)
	if err != nil {
		return fmt.Errorf("playlist: %w", err)
	}
	nPl := 0
	for _, p := range pls {
		if p.Owner.ID != me.ID {
			continue // GET /playlists/{id}/items è consentito solo per playlist proprie
		}
		tracks, err := sp.PlaylistTracks(ctx, p.ID)
		var apiErr *spotify.APIError
		if errors.As(err, &apiErr) && apiErr.Status == 403 {
			continue
		}
		if err != nil {
			return fmt.Errorf("playlist %q: %w", p.Name, err)
		}
		for _, t := range tracks {
			add(t, store.SourcePlaylist)
		}
		nPl++
	}
	log("  %d playlist lette", nPl)

	for _, s := range scores {
		snap.TopArtists = append(snap.TopArtists, *s)
	}
	sort.Slice(snap.TopArtists, func(i, j int) bool { return snap.TopArtists[i].Score > snap.TopArtists[j].Score })
	snap.SavedSample = refs(savedTracks, 30)
	snap.ArtistCount = len(artists)

	if err := st.ReplaceLibrary(known, artists); err != nil {
		return err
	}
	n, _, err := st.CountKnown()
	if err != nil {
		return err
	}
	snap.LibrarySize = n
	if err := st.SetMetaJSON(snapshotKey, snap); err != nil {
		return err
	}
	return st.MarkSynced()
}

func refs(tracks []spotify.Track, max int) []TrackRef {
	var out []TrackRef
	for _, t := range tracks {
		if len(out) >= max {
			break
		}
		if len(t.Artists) == 0 {
			continue
		}
		out = append(out, TrackRef{Artist: strings.Join(t.ArtistNames(), ", "), Title: t.Name, Year: t.Year()})
	}
	return out
}

func Load(st *store.Store) (*Snapshot, error) {
	var s Snapshot
	ok, err := st.GetMetaJSON(snapshotKey, &s)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("nessun profilo: sincronizza prima la libreria")
	}
	return &s, nil
}

// historyInPrompt è quanti suggerimenti apprezzati/scartati includere nel profilo.
const historyInPrompt = 30

// Prompt carica il profilo e lo rende come testo per il modello, insieme agli
// ultimi suggerimenti apprezzati (e, se withSkipped, scartati).
func Prompt(st *store.Store, withSkipped bool) (string, *Snapshot, error) {
	snap, err := Load(st)
	if err != nil {
		return "", nil, err
	}
	liked, err := st.History(historyInPrompt, store.FeedbackLike)
	if err != nil {
		return "", nil, err
	}
	var skipped []store.Suggestion
	if withSkipped {
		if skipped, err = st.History(historyInPrompt, store.FeedbackSkip); err != nil {
			return "", nil, err
		}
	}
	return snap.Render(liked, skipped), snap, nil
}

// Render produce la descrizione testuale del profilo per il prompt.
func (s *Snapshot) Render(liked, skipped []store.Suggestion) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Libreria: %d brani, %d artisti.\n\n", s.LibrarySize, s.ArtistCount)

	b.WriteString("## Artisti più ascoltati (dal più al meno rilevante)\n")
	genreCount := map[string]float64{}
	for i, a := range s.TopArtists {
		for _, g := range a.Genres {
			genreCount[g] += a.Score
		}
		if i < 40 {
			if len(a.Genres) > 0 {
				fmt.Fprintf(&b, "- %s (%s)\n", a.Name, strings.Join(first(a.Genres, 3), ", "))
			} else {
				fmt.Fprintf(&b, "- %s\n", a.Name)
			}
		}
	}
	if len(genreCount) > 0 {
		type gc struct {
			g string
			c float64
		}
		var gs []gc
		for g, c := range genreCount {
			gs = append(gs, gc{g, c})
		}
		sort.Slice(gs, func(i, j int) bool { return gs[i].c > gs[j].c })
		var names []string
		for i := 0; i < len(gs) && i < 15; i++ {
			names = append(names, gs[i].g)
		}
		fmt.Fprintf(&b, "\n## Generi principali\n%s\n", strings.Join(names, ", "))
	}
	writeTracks(&b, "Ascolti recenti (ultime settimane)", s.RecentTracks)
	writeTracks(&b, "Preferiti di sempre", s.AllTimeTracks)
	writeTracks(&b, "Ultimi brani salvati", s.SavedSample)
	writeSugg(&b, "Suggerimenti precedenti che l'utente ha apprezzato", liked)
	writeSugg(&b, "Suggerimenti precedenti che l'utente ha scartato", skipped)
	return b.String()
}

func writeTracks(b *strings.Builder, title string, ts []TrackRef) {
	if len(ts) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n", title)
	for _, t := range ts {
		if t.Year != "" {
			fmt.Fprintf(b, "- %s — %s (%s)\n", t.Artist, t.Title, t.Year)
		} else {
			fmt.Fprintf(b, "- %s — %s\n", t.Artist, t.Title)
		}
	}
}

func writeSugg(b *strings.Builder, title string, ss []store.Suggestion) {
	if len(ss) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n", title)
	for _, s := range ss {
		fmt.Fprintf(b, "- %s — %s\n", s.Artist, s.Title)
	}
}

func first(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
