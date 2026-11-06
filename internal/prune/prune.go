// Package prune individua brani salvati poco ascoltati e poco in linea con i
// gusti dell'utente, candidati a essere rimossi dalla libreria.
//
// Spotify non espone il numero di ascolti: un brano è considerato "poco
// ascoltato" se è salvato da un po', non compare tra i top brani (4 settimane,
// 6 mesi, 1 anno) e nessuno dei suoi artisti è tra i più ascoltati.
package prune

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/rubenpari/tunescout-web/internal/llm"
	"github.com/rubenpari/tunescout-web/internal/norm"
	"github.com/rubenpari/tunescout-web/internal/profile"
	"github.com/rubenpari/tunescout-web/internal/spotify"
	"github.com/rubenpari/tunescout-web/internal/store"
)

const (
	// topArtistCutoff: gli artisti del profilo in queste posizioni proteggono i loro brani.
	topArtistCutoff = 50
	// maxCandidates limita i brani inviati a Claude.
	maxCandidates = 250
)

type Options struct {
	Count  int
	MinAge time.Duration // ignora i brani salvati più di recente
}

type Library interface {
	SavedTracks(ctx context.Context, progress func(int)) ([]spotify.SavedTrack, error)
	TopTracks(ctx context.Context, timeRange string) ([]spotify.Track, error)
}

type Picker interface {
	PickPrune(ctx context.Context, req llm.PruneRequest) ([]llm.PrunePick, error)
}

type Result struct {
	spotify.SavedTrack
	Reason string
}

type Stats struct {
	Saved      int `json:"saved"`
	Candidates int `json:"candidates"`
}

func (s Stats) String() string {
	return fmt.Sprintf("brani salvati %d · poco ascoltati %d", s.Saved, s.Candidates)
}

type Pipeline struct {
	SP  Library
	LLM Picker
	ST  *store.Store
	Log func(string, ...any)
	Now func() time.Time // per i test; default time.Now
}

func (p *Pipeline) Run(ctx context.Context, opt Options) ([]Result, Stats, error) {
	var stats Stats
	profText, snap, err := profile.Prompt(p.ST, false)
	if err != nil {
		return nil, stats, err
	}

	p.Log("Scarico i brani salvati…")
	saved, err := p.SP.SavedTracks(ctx, nil)
	if err != nil {
		return nil, stats, fmt.Errorf("brani salvati: %w", err)
	}
	stats.Saved = len(saved)
	var top []spotify.Track
	for _, tr := range spotify.TimeRanges {
		t, err := p.SP.TopTracks(ctx, tr)
		if err != nil {
			return nil, stats, fmt.Errorf("top brani (%s): %w", tr, err)
		}
		top = append(top, t...)
	}

	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	cand := Candidates(saved, top, snap.TopArtists, now().Add(-opt.MinAge))
	kept, err := p.ST.Kept()
	if err != nil {
		return nil, stats, err
	}
	cand = slices.DeleteFunc(cand, func(s spotify.SavedTrack) bool {
		return kept[s.Track.ID] || kept[s.Track.Key()]
	})
	stats.Candidates = len(cand)
	if len(cand) == 0 {
		return nil, stats, nil
	}
	if len(cand) > maxCandidates {
		cand = cand[:maxCandidates]
	}

	req := llm.PruneRequest{Profile: profText, Count: opt.Count}
	for _, c := range cand {
		req.Candidates = append(req.Candidates, llm.PruneCandidate{
			Artist: strings.Join(c.Track.ArtistNames(), ", "), Title: c.Track.Name, Year: c.Track.Year(),
			AddedAt: c.AddedAt.Format("2006-01"),
		})
	}
	p.Log("Chiedo a Claude quali dei %d brani poco ascoltati sono meno in linea con i tuoi gusti…", len(cand))
	picks, err := p.LLM.PickPrune(ctx, req)
	if err != nil {
		return nil, stats, err
	}
	var out []Result
	for _, pk := range picks {
		if len(out) >= opt.Count {
			break
		}
		out = append(out, Result{SavedTrack: cand[pk.ID], Reason: pk.Reason})
	}
	return out, stats, nil
}

// Candidates filtra i brani salvati prima di before che non sono tra i top
// brani e i cui artisti non sono tra i più ascoltati. Ordina mettendo prima
// gli artisti con meno brani in libreria (i più "isolati"), poi i più vecchi.
func Candidates(saved []spotify.SavedTrack, top []spotify.Track, topArtists []profile.ArtistScore, before time.Time) []spotify.SavedTrack {
	topIDs := map[string]bool{}
	for _, t := range top {
		topIDs[t.ID] = true
		if len(t.Artists) > 0 {
			topIDs[t.Key()] = true
		}
	}
	protected := map[string]bool{}
	for i, a := range topArtists {
		if i >= topArtistCutoff {
			break
		}
		protected[norm.Artist(a.Name)] = true
	}
	perArtist := map[string]int{}
	for _, s := range saved {
		if len(s.Track.Artists) > 0 {
			perArtist[norm.Artist(s.Track.Artists[0].Name)]++
		}
	}

	var out []spotify.SavedTrack
	for _, s := range saved {
		t := s.Track
		if t.ID == "" || t.IsLocal || len(t.Artists) == 0 || !s.AddedAt.Before(before) {
			continue
		}
		if topIDs[t.ID] || topIDs[t.Key()] {
			continue
		}
		isProtected := false
		for _, a := range t.Artists {
			if protected[norm.Artist(a.Name)] {
				isProtected = true
				break
			}
		}
		if isProtected {
			continue
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ci := perArtist[norm.Artist(out[i].Track.Artists[0].Name)]
		cj := perArtist[norm.Artist(out[j].Track.Artists[0].Name)]
		if ci != cj {
			return ci < cj
		}
		return out[i].AddedAt.Before(out[j].AddedAt)
	})
	return out
}
