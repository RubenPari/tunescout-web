// Package discover orchestra: profilo → Claude → filtro → verifica su Spotify.
package discover

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/rubenpari/tunescout-web/internal/llm"
	"github.com/rubenpari/tunescout-web/internal/norm"
	"github.com/rubenpari/tunescout-web/internal/profile"
	"github.com/rubenpari/tunescout-web/internal/spotify"
	"github.com/rubenpari/tunescout-web/internal/store"
)

type Options struct {
	Count          int
	Mood           string
	Genres         []string
	NewArtistsOnly bool
	MaxRounds      int
}

// Searcher è la parte del client Spotify usata dalla verifica (mockabile nei test).
type Searcher interface {
	SearchTrack(ctx context.Context, artist, title string, limit int) ([]spotify.Track, error)
}

type Recommender interface {
	Recommend(ctx context.Context, req llm.Request) ([]llm.Suggestion, error)
}

type Stats struct {
	Proposed     int `json:"proposed"`
	AlreadyKnown int `json:"alreadyKnown"`
	KnownArtist  int `json:"knownArtist"`
	NotFound     int `json:"notFound"`
	Rounds       int `json:"rounds"`
}

type Pipeline struct {
	SP  Searcher
	LLM Recommender
	ST  *store.Store
	Log func(string, ...any)
}

func (p *Pipeline) Run(ctx context.Context, opt Options) ([]store.Suggestion, Stats, error) {
	var stats Stats
	snap, err := profile.Load(p.ST)
	if err != nil {
		return nil, stats, err
	}
	liked, err := p.ST.History(30, "like")
	if err != nil {
		return nil, stats, err
	}
	skipped, err := p.ST.History(30, "skip")
	if err != nil {
		return nil, stats, err
	}
	profText := snap.Render(liked, skipped)
	if opt.MaxRounds <= 0 {
		opt.MaxRounds = 2
	}

	var results []store.Suggestion
	seenKeys := map[string]bool{}
	perArtist := map[string]int{}
	var exclude []string

	for round := 1; round <= opt.MaxRounds && len(results) < opt.Count; round++ {
		stats.Rounds = round
		missing := opt.Count - len(results)
		ask := min(missing*2, 60) // margine per gli scarti
		p.Log("Chiedo a Claude %d suggerimenti (giro %d)…", ask, round)
		sugg, err := p.LLM.Recommend(ctx, llm.Request{
			Profile: profText, Count: ask, Mood: opt.Mood, Genres: opt.Genres,
			NewArtistsOnly: opt.NewArtistsOnly, Exclude: exclude,
		})
		if err != nil {
			return results, stats, err
		}
		stats.Proposed += len(sugg)

		// 1) filtro locale: già noti / già suggeriti / duplicati / artisti noti
		var cand []llm.Suggestion
		for _, s := range sugg {
			key := norm.Key(s.Artist, s.Title)
			exclude = append(exclude, s.Artist+" — "+s.Title)
			if seenKeys[key] {
				continue
			}
			seenKeys[key] = true
			if seen, err := p.ST.Seen(key, ""); err != nil {
				return results, stats, err
			} else if seen {
				stats.AlreadyKnown++
				continue
			}
			if opt.NewArtistsOnly {
				if known, err := p.ST.KnownArtist(norm.Artist(s.Artist)); err != nil {
					return results, stats, err
				} else if known {
					stats.KnownArtist++
					continue
				}
			}
			cand = append(cand, s)
		}

		// 2) verifica su Spotify in parallelo
		p.Log("Verifico %d brani su Spotify…", len(cand))
		verified := p.verify(ctx, cand)

		// 3) controllo finale (ID Spotify già noto?) e raccolta, mantenendo l'ordine di Claude
		for i, v := range verified {
			if len(results) >= opt.Count {
				break
			}
			if v == nil {
				stats.NotFound++
				continue
			}
			if seen, err := p.ST.Seen(v.Key, v.SpotifyID); err != nil {
				return results, stats, err
			} else if seen || seenKeys["id:"+v.SpotifyID] {
				stats.AlreadyKnown++
				continue
			}
			a := norm.Artist(cand[i].Artist)
			if perArtist[a] >= 2 {
				continue
			}
			perArtist[a]++
			seenKeys["id:"+v.SpotifyID] = true
			results = append(results, *v)
		}
	}

	if err := p.ST.AddSuggestions(results); err != nil {
		return results, stats, err
	}
	return results, stats, nil
}

// verify cerca ogni candidato su Spotify; restituisce nil per quelli non trovati.
func (p *Pipeline) verify(ctx context.Context, cand []llm.Suggestion) []*store.Suggestion {
	out := make([]*store.Suggestion, len(cand))
	sem := make(chan struct{}, 5)
	var wg sync.WaitGroup
	for i, c := range cand {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			t, err := p.find(ctx, c)
			if err != nil {
				p.Log("  ! ricerca %q: %v", c.Artist+" — "+c.Title, err)
				return
			}
			if t == nil {
				return
			}
			out[i] = &store.Suggestion{
				Key:       norm.Key(t.Artists[0].Name, t.Name),
				SpotifyID: t.ID, URI: t.URI,
				Artist: strings.Join(t.ArtistNames(), ", "),
				Title:  t.Name, Album: t.Album.Name, Year: t.Year(), Image: t.Album.Cover(),
				Reason: c.Reason, URL: t.URL(),
			}
		}()
	}
	wg.Wait()
	return out
}

func (p *Pipeline) find(ctx context.Context, c llm.Suggestion) (*spotify.Track, error) {
	tracks, err := p.SP.SearchTrack(ctx, c.Artist, c.Title, 10)
	if err != nil {
		return nil, err
	}
	return BestMatch(c, tracks), nil
}

// BestMatch sceglie il primo risultato con artista e titolo compatibili.
func BestMatch(c llm.Suggestion, tracks []spotify.Track) *spotify.Track {
	for i := range tracks {
		t := &tracks[i]
		if t.ID == "" || len(t.Artists) == 0 {
			continue
		}
		if norm.ArtistMatch(c.Artist, t.ArtistNames()) && norm.TitleMatch(c.Title, t.Name) {
			return t
		}
	}
	return nil
}

func (s Stats) String() string {
	return fmt.Sprintf("proposti %d · già noti %d · artisti già in libreria %d · non trovati su Spotify %d · giri %d",
		s.Proposed, s.AlreadyKnown, s.KnownArtist, s.NotFound, s.Rounds)
}
