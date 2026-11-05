package prune

import (
	"testing"
	"time"

	"github.com/rubenpari/tunescout-web/internal/profile"
	"github.com/rubenpari/tunescout-web/internal/spotify"
)

func saved(id, artist, title string, added time.Time) spotify.SavedTrack {
	return spotify.SavedTrack{AddedAt: added, Track: spotify.Track{
		ID: id, URI: "spotify:track:" + id, Name: title, Artists: []spotify.Artist{{Name: artist}},
	}}
}

func TestCandidates(t *testing.T) {
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	older := old.AddDate(-1, 0, 0)
	recent := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	cutoff := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	lib := []spotify.SavedTrack{
		saved("1", "Slowdive", "Alison", old),        // artista top → protetto
		saved("2", "Random Band", "Hit", old),        // nei top brani (per ID)
		saved("3", "Other Band", "Song A", old),      // candidato, artista con 2 brani
		saved("4", "Other Band", "Song B", older),    // candidato, più vecchio
		saved("5", "Lonely Artist", "Only One", old), // candidato, artista isolato → primo
		saved("6", "New Thing", "Fresh", recent),     // salvato di recente
		saved("7", "Remaster Co", "Classic", old),    // nei top brani (per chiave normalizzata)
		{AddedAt: old, Track: spotify.Track{ID: "8", Name: "Locale", IsLocal: true, Artists: []spotify.Artist{{Name: "X"}}}},
	}
	top := []spotify.Track{
		{ID: "2", Name: "Hit", Artists: []spotify.Artist{{Name: "Random Band"}}},
		{ID: "other-id", Name: "Classic - 2011 Remaster", Artists: []spotify.Artist{{Name: "Remaster Co"}}},
	}
	artists := []profile.ArtistScore{{Name: "Slowdive", Score: 5}}

	got := Candidates(lib, top, artists, cutoff)
	var ids []string
	for _, c := range got {
		ids = append(ids, c.Track.ID)
	}
	want := []string{"5", "4", "3"}
	if len(ids) != len(want) {
		t.Fatalf("got %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("got %v, want %v", ids, want)
		}
	}
}
