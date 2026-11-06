package discover

import (
	"context"
	"strings"
	"testing"

	"github.com/rubenpari/tunescout-web/internal/llm"
	"github.com/rubenpari/tunescout-web/internal/norm"
	"github.com/rubenpari/tunescout-web/internal/profile"
	"github.com/rubenpari/tunescout-web/internal/spotify"
	"github.com/rubenpari/tunescout-web/internal/store"
	"github.com/rubenpari/tunescout-web/internal/store/storetest"
)

type fakeLLM struct {
	rounds [][]llm.Suggestion
	reqs   []llm.Request
}

func (f *fakeLLM) Recommend(_ context.Context, r llm.Request) ([]llm.Suggestion, error) {
	f.reqs = append(f.reqs, r)
	if len(f.reqs) > len(f.rounds) {
		return nil, nil
	}
	return f.rounds[len(f.reqs)-1], nil
}

// fakeSpotify conosce solo i brani nel catalogo; il resto è "allucinato".
type fakeSpotify struct{ catalog map[string]spotify.Track }

func (f *fakeSpotify) SearchTrack(_ context.Context, artist, title string, _ int) ([]spotify.Track, error) {
	if t, ok := f.catalog[strings.ToLower(artist+"|"+title)]; ok {
		return []spotify.Track{t}, nil
	}
	return nil, nil
}

func track(id, artist, title string) spotify.Track {
	return spotify.Track{ID: id, URI: "spotify:track:" + id, Name: title + " - 2005 Remaster",
		Artists: []spotify.Artist{{Name: artist}}, Album: spotify.Album{Name: "Album", ReleaseDate: "1993-05-01"}}
}

func TestMain(m *testing.M) { storetest.Main(m) }

func newStore(t *testing.T) *store.Store {
	st := storetest.User(t, storetest.DB(t), "u1")
	if err := st.ReplaceLibrary([]store.KnownTrack{
		{Key: norm.Key("Radiohead", "Creep"), SpotifyID: "known1", Artist: "Radiohead", Title: "Creep", Source: "saved"},
	}, map[string]string{"radiohead": "Radiohead"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetMetaJSON("profile_snapshot", profile.Snapshot{
		TopArtists: []profile.ArtistScore{{Name: "Radiohead", Score: 3}}, LibrarySize: 1, ArtistCount: 1,
	}); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestPipeline(t *testing.T) {
	st := newStore(t)
	sp := &fakeSpotify{catalog: map[string]spotify.Track{
		"slowdive|alison":                   track("s1", "Slowdive", "Alison"),
		"ride|vapour trail":                 track("r1", "Ride", "Vapour Trail"),
		"radiohead|creep":                   track("known1", "Radiohead", "Creep"),
		"radiohead|nude":                    track("rh2", "Radiohead", "Nude"),
		"cocteau twins|heaven or las vegas": track("c1", "Cocteau Twins", "Heaven or Las Vegas"),
	}}
	f := &fakeLLM{rounds: [][]llm.Suggestion{
		{
			{Artist: "Slowdive", Title: "Alison", Reason: "a"},
			{Artist: "Radiohead", Title: "Creep", Reason: "già in libreria"},
			{Artist: "Band Inventata", Title: "Non Esiste", Reason: "allucinazione"},
			{Artist: "Radiohead", Title: "Nude", Reason: "artista noto"},
		},
		{
			{Artist: "Ride", Title: "Vapour Trail", Reason: "b"},
			{Artist: "Cocteau Twins", Title: "Heaven or Las Vegas", Reason: "c"},
		},
	}}
	p := &Pipeline{SP: sp, LLM: f, ST: st, Log: func(string, ...any) {}}

	res, stats, err := p.Run(context.Background(), Options{Count: 3, NewArtistsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range res {
		got = append(got, r.Artist)
	}
	if strings.Join(got, ",") != "Slowdive,Ride,Cocteau Twins" {
		t.Fatalf("risultati inattesi: %v (%s)", got, stats)
	}
	if stats.AlreadyKnown != 1 || stats.KnownArtist != 1 || stats.NotFound != 1 || stats.Rounds != 2 {
		t.Errorf("statistiche inattese: %s", stats)
	}
	// il secondo giro deve escludere quanto già proposto
	if ex := strings.Join(f.reqs[1].Exclude, ";"); !strings.Contains(ex, "Band Inventata") {
		t.Errorf("exclude mancante nel secondo giro: %q", ex)
	}

	// una seconda esecuzione non deve riproporre gli stessi brani
	f2 := &fakeLLM{rounds: [][]llm.Suggestion{{{Artist: "Slowdive", Title: "Alison"}}}}
	p.LLM = f2
	res2, _, err := p.Run(context.Background(), Options{Count: 1, MaxRounds: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(res2) != 0 {
		t.Fatalf("brano già suggerito riproposto: %+v", res2)
	}
}

// Duplicati nello stesso giro e brani già noti solo per id Spotify (chiave diversa).
func TestPipelineDuplicatesAndKnownIDs(t *testing.T) {
	st := newStore(t)
	sp := &fakeSpotify{catalog: map[string]spotify.Track{
		"slowdive|alison": track("s1", "Slowdive", "Alison"),
		// stesso id del brano in libreria, ma artista/titolo diversi
		"thom|creep (versione)": track("known1", "Thom", "Creep (versione)"),
	}}
	f := &fakeLLM{rounds: [][]llm.Suggestion{{
		{Artist: "Slowdive", Title: "Alison"},
		{Artist: "Slowdive", Title: "Alison (Remastered)"},
		{Artist: "Thom", Title: "Creep (versione)"},
	}}}
	p := &Pipeline{SP: sp, LLM: f, ST: st, Log: func(string, ...any) {}}
	res, stats, err := p.Run(context.Background(), Options{Count: 3, MaxRounds: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].SpotifyID != "s1" {
		t.Fatalf("risultati inattesi: %+v (%s)", res, stats)
	}
	if stats.AlreadyKnown != 1 || stats.NotFound != 0 {
		t.Errorf("statistiche inattese: %s", stats)
	}
}
