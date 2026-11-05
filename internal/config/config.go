// Package config legge la configurazione del server dalle variabili d'ambiente.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const (
	DefaultModel  = "claude-opus-5-5"
	DefaultEffort = "medium"
)

type Config struct {
	Addr                string // indirizzo di ascolto, es. ":8080"
	BaseURL             string // URL pubblico, es. "https://tunescout.example.com"
	DatabaseURL         string
	SpotifyClientID     string
	SpotifyClientSecret string
	TokenKey            []byte // 32 byte, cifratura AES-256-GCM dei token Spotify
	Model               string
	Effort              string
	DailyRunLimit       int // esecuzioni discover/prune per utente al giorno (0 = illimitate)
	MaxConcurrentJobs   int
}

// RedirectURL è l'URL di callback da registrare nella dashboard Spotify.
func (c *Config) RedirectURL() string { return c.BaseURL + "/api/auth/callback" }

// SecureCookies è true quando il sito è servito in HTTPS.
func (c *Config) SecureCookies() bool { return strings.HasPrefix(c.BaseURL, "https://") }

func Load() (*Config, error) {
	c := &Config{
		Addr:                env("ADDR", ":8080"),
		BaseURL:             strings.TrimRight(env("BASE_URL", "http://127.0.0.1:8080"), "/"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		SpotifyClientID:     os.Getenv("SPOTIFY_CLIENT_ID"),
		SpotifyClientSecret: os.Getenv("SPOTIFY_CLIENT_SECRET"),
		Model:               env("TUNESCOUT_MODEL", DefaultModel),
		Effort:              env("TUNESCOUT_EFFORT", DefaultEffort),
	}
	var errs []error
	need := func(name, v string) {
		if v == "" {
			errs = append(errs, fmt.Errorf("%s non impostata", name))
		}
	}
	need("DATABASE_URL", c.DatabaseURL)
	need("SPOTIFY_CLIENT_ID", c.SpotifyClientID)
	need("SPOTIFY_CLIENT_SECRET", c.SpotifyClientSecret)
	need("ANTHROPIC_API_KEY", os.Getenv("ANTHROPIC_API_KEY"))

	if u, err := url.Parse(c.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, fmt.Errorf("BASE_URL non valido: %q", c.BaseURL))
	}
	switch c.Effort {
	case "low", "medium", "high", "xhigh", "max":
	default:
		errs = append(errs, fmt.Errorf("TUNESCOUT_EFFORT non valido: %q (low|medium|high|xhigh|max)", c.Effort))
	}

	if k := os.Getenv("TOKEN_ENC_KEY"); k == "" {
		errs = append(errs, errors.New("TOKEN_ENC_KEY non impostata (genera con: openssl rand -base64 32)"))
	} else if b, err := base64.StdEncoding.DecodeString(k); err != nil || len(b) != 32 {
		errs = append(errs, errors.New("TOKEN_ENC_KEY deve essere 32 byte in base64 (openssl rand -base64 32)"))
	} else {
		c.TokenKey = b
	}

	var err error
	if c.DailyRunLimit, err = envInt("DAILY_RUN_LIMIT", 20); err != nil {
		errs = append(errs, err)
	}
	if c.MaxConcurrentJobs, err = envInt("MAX_CONCURRENT_JOBS", 4); err != nil {
		errs = append(errs, err)
	} else if c.MaxConcurrentJobs < 1 {
		errs = append(errs, errors.New("MAX_CONCURRENT_JOBS deve essere ≥ 1"))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return c, nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) (int, error) {
	v := os.Getenv(k)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s non valido: %q", k, v)
	}
	return n, nil
}
