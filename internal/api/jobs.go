package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rubenpari/tunescout-web/internal/discover"
	"github.com/rubenpari/tunescout-web/internal/jobs"
	"github.com/rubenpari/tunescout-web/internal/norm"
	"github.com/rubenpari/tunescout-web/internal/profile"
	"github.com/rubenpari/tunescout-web/internal/prune"
	"github.com/rubenpari/tunescout-web/internal/store"
)

const syncMaxAge = 24 * time.Hour

type DiscoverParams struct {
	Count      int      `json:"count"`
	Mood       string   `json:"mood"`
	Genres     []string `json:"genres"`
	NewArtists bool     `json:"newArtists"`
}

type DiscoverResult struct {
	Suggestions []store.Suggestion `json:"suggestions"`
	Stats       discover.Stats     `json:"stats"`
	Requested   int                `json:"requested"`
}

type PruneParams struct {
	Count      int `json:"count"`
	MinAgeDays int `json:"minAgeDays"`
}

type PruneItem struct {
	store.Suggestion
	AddedAt time.Time `json:"addedAt"`
}

type PruneResult struct {
	Items []PruneItem `json:"items"`
	Stats prune.Stats `json:"stats"`
}

type SyncResult struct {
	Tracks  int `json:"tracks"`
	Artists int `json:"artists"`
}

func (s *Server) startSync(w http.ResponseWriter, r *http.Request, uid int64) {
	s.start(w, r, uid, "sync", struct{}{}, false, func(ctx context.Context, sp Spotify, st *store.Store, log func(string, ...any)) (any, error) {
		if err := profile.Sync(ctx, sp, st, log); err != nil {
			return nil, err
		}
		t, a, err := st.CountKnown()
		if err != nil {
			return nil, err
		}
		log("✓ Libreria sincronizzata: %d brani noti, %d artisti.", t, a)
		return SyncResult{Tracks: t, Artists: a}, nil
	})
}

func (s *Server) startDiscover(w http.ResponseWriter, r *http.Request, uid int64) {
	p := DiscoverParams{Count: 20}
	if !readJSON(w, r, &p) {
		return
	}
	p.Mood = strings.TrimSpace(p.Mood)
	genres := p.Genres[:0]
	for _, g := range p.Genres {
		if g = strings.TrimSpace(g); g != "" {
			genres = append(genres, g)
		}
	}
	p.Genres = genres
	switch {
	case p.Count < 1 || p.Count > 50:
		writeError(w, http.StatusBadRequest, "il numero di brani deve essere tra 1 e 50")
		return
	case len(p.Mood) > 200:
		writeError(w, http.StatusBadRequest, "mood troppo lungo (max 200 caratteri)")
		return
	case len(p.Genres) > 10:
		writeError(w, http.StatusBadRequest, "al massimo 10 generi")
		return
	}
	for _, g := range p.Genres {
		if len(g) > 50 {
			writeError(w, http.StatusBadRequest, "nome di genere troppo lungo (max 50 caratteri)")
			return
		}
	}
	s.start(w, r, uid, "discover", p, true, func(ctx context.Context, sp Spotify, st *store.Store, log func(string, ...any)) (any, error) {
		if err := ensureSynced(ctx, sp, st, log); err != nil {
			return nil, err
		}
		pl := &discover.Pipeline{SP: sp, LLM: s.LLM, ST: st, Log: log}
		res, stats, err := pl.Run(ctx, discover.Options{
			Count: p.Count, Mood: p.Mood, Genres: p.Genres, NewArtistsOnly: p.NewArtists,
		})
		if err != nil {
			return nil, err
		}
		log("%s", stats)
		return DiscoverResult{Suggestions: nonNil(res), Stats: stats, Requested: p.Count}, nil
	})
}

func (s *Server) startPrune(w http.ResponseWriter, r *http.Request, uid int64) {
	p := PruneParams{Count: 20, MinAgeDays: 90}
	if !readJSON(w, r, &p) {
		return
	}
	if p.Count < 1 || p.Count > 50 {
		writeError(w, http.StatusBadRequest, "il numero di brani deve essere tra 1 e 50")
		return
	}
	if p.MinAgeDays < 0 || p.MinAgeDays > 3650 {
		writeError(w, http.StatusBadRequest, "l'età minima deve essere tra 0 e 3650 giorni")
		return
	}
	s.start(w, r, uid, "prune", p, true, func(ctx context.Context, sp Spotify, st *store.Store, log func(string, ...any)) (any, error) {
		if err := ensureSynced(ctx, sp, st, log); err != nil {
			return nil, err
		}
		pl := &prune.Pipeline{SP: sp, LLM: s.LLM, ST: st, Log: log}
		res, stats, err := pl.Run(ctx, prune.Options{
			Count: p.Count, MinAge: time.Duration(p.MinAgeDays) * 24 * time.Hour,
		})
		if err != nil {
			return nil, err
		}
		log("%s", stats)
		items := make([]PruneItem, 0, len(res))
		for _, x := range res {
			t := x.Track
			if len(t.Artists) == 0 {
				continue
			}
			items = append(items, PruneItem{
				Suggestion: store.Suggestion{
					Key: norm.Key(t.Artists[0].Name, t.Name), SpotifyID: t.ID, URI: t.URI,
					Artist: strings.Join(t.ArtistNames(), ", "), Title: t.Name,
					Album: t.Album.Name, Year: t.Year(), Reason: x.Reason, URL: t.URL(), Image: t.Album.Cover(),
				},
				AddedAt: x.AddedAt,
			})
		}
		return PruneResult{Items: items, Stats: stats}, nil
	})
}

// ensureSynced aggiorna la cache della libreria se è più vecchia di 24 ore.
func ensureSynced(ctx context.Context, sp Spotify, st *store.Store, log func(string, ...any)) error {
	last, err := st.LastSync()
	if err != nil {
		return err
	}
	if time.Since(last) < syncMaxAge {
		return nil
	}
	log("La cache della libreria è vecchia o assente: sincronizzo…")
	return profile.Sync(ctx, sp, st, log)
}

type jobFunc func(ctx context.Context, sp Spotify, st *store.Store, log func(string, ...any)) (any, error)

// start avvia un job; metered indica se consuma il limite giornaliero.
func (s *Server) start(w http.ResponseWriter, r *http.Request, uid int64, kind string, params any, metered bool, fn jobFunc) {
	ctx := r.Context()
	if _, busy := s.Jobs.Active(uid); busy {
		writeError(w, http.StatusConflict, jobs.ErrBusy.Error())
		return
	}
	// Verifica subito il token Spotify, così un login scaduto non consuma il limite.
	if _, err := s.SpotifyFor(ctx, uid); err != nil {
		s.fail(w, r, err)
		return
	}
	var refund func()
	if metered {
		ok, err := s.DB.TakeRun(ctx, uid, s.DailyRunLimit)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if !ok {
			writeError(w, http.StatusTooManyRequests,
				fmt.Sprintf("hai raggiunto il limite di %d ricerche al giorno: riprova domani", s.DailyRunLimit))
			return
		}
		refund = func() { _ = s.DB.RefundRun(context.Background(), uid) }
	}
	j, err := s.Jobs.Start(uid, kind, params, func(ctx context.Context, log func(string, ...any)) (any, error) {
		sp, err := s.SpotifyFor(ctx, uid)
		if err != nil {
			return nil, err
		}
		res, err := fn(ctx, sp, s.DB.ForUser(ctx, uid), log)
		if err != nil {
			return nil, errors.New(userMessage(err))
		}
		return res, nil
	}, refund)
	if err != nil {
		if refund != nil {
			refund()
		}
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, j)
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request, uid int64) {
	j, err := s.DB.Job(r.Context(), uid, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, j)
}

// jobEvents trasmette il progresso del job come Server-Sent Events:
// "log" per ogni riga di progresso, "done" con il job completo alla fine.
func (s *Server) jobEvents(w http.ResponseWriter, r *http.Request, uid int64) {
	id := r.PathValue("id")
	ctx := r.Context()
	past, ch, cancel, live := s.Jobs.Subscribe(uid, id)
	if !live {
		// Job non più in memoria: invia direttamente lo stato finale.
		j, err := s.DB.Job(ctx, uid, id)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if j.Status == store.JobRunning {
			writeError(w, http.StatusConflict, "job in corso su un'altra istanza")
			return
		}
		sse(w)
		sendEvent(w, jobs.Event{Type: "done", Job: j})
		return
	}
	defer cancel()
	sse(w)
	for _, m := range past {
		sendEvent(w, jobs.Event{Type: "log", Message: m})
	}
	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-keepAlive.C:
			fmt.Fprint(w, ": ping\n\n")
			flush(w)
		case e, ok := <-ch:
			if !ok {
				// Canale chiuso senza "done" (abbonato lento): rileggi lo stato dal DB.
				if j, err := s.DB.Job(context.WithoutCancel(ctx), uid, id); err == nil && j.Status != store.JobRunning {
					sendEvent(w, jobs.Event{Type: "done", Job: j})
				}
				return
			}
			sendEvent(w, e)
			if e.Type == "done" {
				return
			}
		}
	}
}

func sse(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no") // disattiva il buffering di nginx
	w.WriteHeader(http.StatusOK)
	flush(w)
}

func sendEvent(w http.ResponseWriter, e jobs.Event) {
	b, _ := json.Marshal(e)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, b)
	flush(w)
}

func flush(w http.ResponseWriter) {
	_ = http.NewResponseController(w).Flush()
}
