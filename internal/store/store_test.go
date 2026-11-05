package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/rubenpari/tunescout-web/internal/store"
	"github.com/rubenpari/tunescout-web/internal/store/storetest"
)

func TestMain(m *testing.M) { storetest.Main(m) }

func TestRemovedAreSeen(t *testing.T) {
	st := storetest.User(t, storetest.DB(t), "u1")

	if seen, _ := st.Seen("ride|vapour trail", "abc"); seen {
		t.Fatal("brano non ancora registrato risulta visto")
	}
	rm := []store.KnownTrack{{Key: "ride|vapour trail", SpotifyID: "abc", Artist: "Ride", Title: "Vapour Trail"}}
	if err := st.AddRemoved(rm); err != nil {
		t.Fatal(err)
	}
	// Una seconda rimozione dello stesso brano non deve fallire.
	if err := st.AddRemoved(rm); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ key, id string }{{"ride|vapour trail", ""}, {"altra|chiave", "abc"}} {
		seen, err := st.Seen(c.key, c.id)
		if err != nil {
			t.Fatal(err)
		}
		if !seen {
			t.Errorf("Seen(%q, %q) = false, atteso true", c.key, c.id)
		}
	}
	// ReplaceLibrary (sync) non deve cancellare i brani rimossi.
	if err := st.ReplaceLibrary(nil, nil); err != nil {
		t.Fatal(err)
	}
	if seen, _ := st.Seen("ride|vapour trail", ""); !seen {
		t.Error("i brani rimossi devono sopravvivere a una sync")
	}
}

func TestReplaceLibraryDedup(t *testing.T) {
	st := storetest.User(t, storetest.DB(t), "u1")
	tracks := []store.KnownTrack{
		{Key: "a|x", SpotifyID: "1", Artist: "A", Title: "X", Source: "saved"},
		{Key: "a|x", SpotifyID: "1", Artist: "A", Title: "X", Source: "top"},
		{Key: "b|y", SpotifyID: "2", Artist: "B", Title: "Y", Source: "playlist"},
	}
	if err := st.ReplaceLibrary(tracks, map[string]string{"a": "A", "b": "B"}); err != nil {
		t.Fatal(err)
	}
	n, a, err := st.CountKnown()
	if err != nil || n != 2 || a != 2 {
		t.Fatalf("CountKnown = %d, %d, %v; atteso 2, 2", n, a, err)
	}
}

func TestUsersAreIsolated(t *testing.T) {
	db := storetest.DB(t)
	alice := storetest.User(t, db, "alice")
	bob := storetest.User(t, db, "bob")

	if err := alice.ReplaceLibrary([]store.KnownTrack{{Key: "a|x", SpotifyID: "1", Artist: "A", Title: "X", Source: "saved"}},
		map[string]string{"a": "A"}); err != nil {
		t.Fatal(err)
	}
	if err := alice.AddSuggestions([]store.Suggestion{{Key: "s|t", SpotifyID: "9", Artist: "S", Title: "T"}}); err != nil {
		t.Fatal(err)
	}
	if err := alice.SetMetaJSON("k", 1); err != nil {
		t.Fatal(err)
	}
	// Bob può ricevere gli stessi suggerimenti in modo indipendente.
	if err := bob.AddSuggestions([]store.Suggestion{{Key: "s|t", SpotifyID: "9", Artist: "S", Title: "T"}}); err != nil {
		t.Fatal(err)
	}
	if err := bob.SetFeedback("s|t", "like"); err != nil {
		t.Fatal(err)
	}

	if seen, _ := bob.Seen("a|x", "1"); seen {
		t.Error("bob vede la libreria di alice")
	}
	if known, _ := bob.KnownArtist("a"); known {
		t.Error("bob vede gli artisti di alice")
	}
	if ok, _ := bob.GetMetaJSON("k", new(int)); ok {
		t.Error("bob vede i meta di alice")
	}
	h, _ := alice.History(0, "")
	if len(h) != 1 || h[0].Feedback != "" {
		t.Errorf("storico di alice alterato dal feedback di bob: %+v", h)
	}
	if liked, _ := alice.History(0, "like"); len(liked) != 0 {
		t.Error("alice vede i like di bob")
	}

	ctx := context.Background()
	j := &store.Job{ID: "job1", UserID: alice.UserID(), Kind: "sync", Status: store.JobRunning, Params: []byte(`{}`)}
	if err := db.CreateJob(ctx, j); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Job(ctx, bob.UserID(), "job1"); err != store.ErrNotFound {
		t.Errorf("bob legge il job di alice: err=%v", err)
	}
}

func TestTakeRun(t *testing.T) {
	db := storetest.DB(t)
	u := storetest.User(t, db, "u")
	ctx := context.Background()
	for i := range 3 {
		ok, err := db.TakeRun(ctx, u.UserID(), 2)
		if err != nil {
			t.Fatal(err)
		}
		if want := i < 2; ok != want {
			t.Errorf("run %d: ok=%v, atteso %v", i, ok, want)
		}
	}
	if err := db.RefundRun(ctx, u.UserID()); err != nil {
		t.Fatal(err)
	}
	if ok, _ := db.TakeRun(ctx, u.UserID(), 2); !ok {
		t.Error("dopo il rimborso deve esserci un'esecuzione disponibile")
	}
}

func TestSessions(t *testing.T) {
	db := storetest.DB(t)
	u := storetest.User(t, db, "u")
	ctx := context.Background()
	if err := db.CreateSession(ctx, []byte("h"), u.UserID(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if uid, err := db.SessionUser(ctx, []byte("h")); err != nil || uid != u.UserID() {
		t.Fatalf("SessionUser = %d, %v", uid, err)
	}
	if err := db.CreateSession(ctx, []byte("old"), u.UserID(), time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SessionUser(ctx, []byte("old")); err != store.ErrNotFound {
		t.Errorf("sessione scaduta accettata: %v", err)
	}
}
