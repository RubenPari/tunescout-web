// Package storetest avvia un Postgres per i test.
//
// Se TEST_DATABASE_URL è impostata usa quel server; altrimenti avvia un
// Postgres embedded (scaricato al primo uso) per l'intero binario di test.
// Ogni chiamata a DB crea un database nuovo e isolato.
package storetest

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"sync/atomic"
	"testing"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/jackc/pgx/v5"

	"github.com/rubenpari/tunescout-web/internal/store"
)

var (
	baseURL string
	counter atomic.Int64
)

// Main va chiamata da TestMain nei pacchetti che usano DB.
func Main(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	if u := os.Getenv("TEST_DATABASE_URL"); u != "" {
		baseURL = u
		return m.Run()
	}
	port, err := freePort()
	if err != nil {
		fmt.Fprintln(os.Stderr, "storetest:", err)
		return 1
	}
	dir, err := os.MkdirTemp("", "tunescout-pg-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "storetest:", err)
		return 1
	}
	defer os.RemoveAll(dir)
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Port(uint32(port)).RuntimePath(dir).Logger(nil))
	if err := pg.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "storetest: avvio Postgres embedded:", err)
		return 1
	}
	defer pg.Stop()
	baseURL = fmt.Sprintf("postgres://postgres:postgres@localhost:%d/postgres?sslmode=disable", port)
	return m.Run()
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// DB restituisce un database nuovo, con le migrazioni applicate.
func DB(t testing.TB) *store.DB {
	t.Helper()
	if baseURL == "" {
		t.Fatal("storetest: manca storetest.Main in TestMain")
	}
	ctx := context.Background()
	name := fmt.Sprintf("t_%d_%d", os.Getpid(), counter.Add(1))
	admin, err := pgx.Connect(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	admin.Close(ctx)

	u, _ := url.Parse(baseURL)
	u.Path = "/" + name
	db, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		if c, err := pgx.Connect(ctx, baseURL); err == nil {
			_, _ = c.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
			c.Close(ctx)
		}
	})
	return db
}

// User crea un utente e restituisce il suo Store.
func User(t testing.TB, db *store.DB, spotifyID string) *store.Store {
	t.Helper()
	u, err := db.UpsertUser(context.Background(), spotifyID, spotifyID)
	if err != nil {
		t.Fatal(err)
	}
	return db.ForUser(context.Background(), u.ID)
}
