// Comando server: avvia l'API HTTP di tunescout-web e serve il frontend.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rubenpari/tunescout-web/internal/api"
	"github.com/rubenpari/tunescout-web/internal/auth"
	"github.com/rubenpari/tunescout-web/internal/config"
	"github.com/rubenpari/tunescout-web/internal/jobs"
	"github.com/rubenpari/tunescout-web/internal/llm"
	"github.com/rubenpari/tunescout-web/internal/spotify"
	"github.com/rubenpari/tunescout-web/internal/store"
	"github.com/rubenpari/tunescout-web/web"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err := run(); err != nil {
		slog.Error("avvio fallito", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.FailStaleJobs(ctx); err != nil {
		return err
	}
	go purgeSessions(ctx, db)

	sealer, err := auth.NewSealer(cfg.TokenKey)
	if err != nil {
		return err
	}
	am := &auth.Manager{
		DB: db, Sealer: sealer, Secure: cfg.SecureCookies(),
		OAuth: spotify.OAuthConfig(cfg.SpotifyClientID, cfg.SpotifyClientSecret, cfg.RedirectURL()),
	}
	runner := jobs.NewRunner(db, cfg.MaxConcurrentJobs)
	static := web.Dist()
	if static == nil {
		slog.Warn("frontend non compilato: servo solo l'API (esegui `npm run build` in web/)")
	}
	srv := &api.Server{
		DB: db, Auth: am, Jobs: runner,
		LLM:           llm.New(cfg.Model, cfg.Effort),
		DailyRunLimit: cfg.DailyRunLimit,
		BaseURL:       cfg.BaseURL,
		Static:        static,
		SpotifyFor: func(ctx context.Context, uid int64) (api.Spotify, error) {
			return am.Spotify(ctx, uid)
		},
	}

	hs := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		slog.Info("tunescout-web in ascolto", "addr", cfg.Addr, "url", cfg.BaseURL, "redirect", cfg.RedirectURL(), "model", cfg.Model)
		errc <- hs.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	slog.Info("arresto in corso…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_ = hs.Shutdown(shutdownCtx)
	return runner.Shutdown(shutdownCtx)
}

func purgeSessions(ctx context.Context, db *store.DB) {
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for {
		if err := db.PurgeExpiredSessions(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("pulizia sessioni", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
