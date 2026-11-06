package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/rubenpari/tunescout-web/internal/jobs"
	"github.com/rubenpari/tunescout-web/internal/store"
)

var trackURI = regexp.MustCompile(`^spotify:track:[A-Za-z0-9]{22}$`)

type feedbackReq struct {
	Keys     []string `json:"keys"`
	Feedback string   `json:"feedback"` // like | skip
}

// feedback registra like/skip sui suggerimenti. Like salva anche i brani
// nella libreria Spotify (come il tasto "l" della CLI e l'opzione --save).
func (s *Server) feedback(w http.ResponseWriter, r *http.Request, uid int64) {
	var req feedbackReq
	if !readJSON(w, r, &req) {
		return
	}
	if !store.ValidFeedback(req.Feedback) {
		writeError(w, http.StatusBadRequest, "feedback deve essere like o skip")
		return
	}
	if len(req.Keys) == 0 || len(req.Keys) > 50 {
		writeError(w, http.StatusBadRequest, "indica da 1 a 50 brani")
		return
	}
	ctx := r.Context()
	st := s.DB.ForUser(ctx, uid)
	list := make([]*store.Suggestion, 0, len(req.Keys))
	for _, k := range req.Keys {
		sg, err := st.Suggestion(k)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "suggerimento non trovato: "+k)
			return
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		list = append(list, sg)
	}
	if req.Feedback == store.FeedbackLike {
		var uris []string
		for _, sg := range list {
			if sg.URI != "" {
				uris = append(uris, sg.URI)
			}
		}
		sp, err := s.SpotifyFor(ctx, uid)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if err := sp.SaveToLibrary(ctx, uris); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	for _, sg := range list {
		if err := st.SetFeedback(sg.Key, req.Feedback); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"updated": len(list)})
}

type playlistReq struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	URIs        []string `json:"uris"`
}

// createPlaylist crea una playlist privata con i brani indicati.
func (s *Server) createPlaylist(w http.ResponseWriter, r *http.Request, uid int64) {
	var req playlistReq
	if !readJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 100 {
		writeError(w, http.StatusBadRequest, "nome playlist obbligatorio (max 100 caratteri)")
		return
	}
	if len(req.Description) > 300 {
		writeError(w, http.StatusBadRequest, "descrizione troppo lunga (max 300 caratteri)")
		return
	}
	if len(req.URIs) == 0 || len(req.URIs) > 100 {
		writeError(w, http.StatusBadRequest, "indica da 1 a 100 brani")
		return
	}
	for _, u := range req.URIs {
		if !trackURI.MatchString(u) {
			writeError(w, http.StatusBadRequest, "URI Spotify non valido: "+u)
			return
		}
	}
	ctx := r.Context()
	sp, err := s.SpotifyFor(ctx, uid)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	desc := req.Description
	if desc == "" {
		desc = "Suggerimenti generati da tunescout"
	}
	pl, err := sp.CreatePlaylist(ctx, req.Name, desc+" il "+time.Now().Format("02/01/2006"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := sp.AddToPlaylist(ctx, pl.ID, slices.Compact(req.URIs)); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": pl.ID, "name": pl.Name, "url": pl.URL, "tracks": len(req.URIs)})
}

type removeReq struct {
	URIs []string `json:"uris"`
}

// pruneRemove rimuove dalla libreria i brani scelti tra quelli proposti da un
// job prune. Solo i brani presenti nel risultato del job sono accettati.
func (s *Server) pruneRemove(w http.ResponseWriter, r *http.Request, uid int64) {
	var req removeReq
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.URIs) == 0 {
		writeError(w, http.StatusBadRequest, "nessun brano selezionato")
		return
	}
	ctx := r.Context()
	byURI, ok := s.pruneItems(w, r, uid)
	if !ok {
		return
	}
	var chosen []store.KnownTrack
	var uris []string
	for _, u := range slices.Compact(slices.Sorted(slices.Values(req.URIs))) {
		it, ok := byURI[u]
		if !ok {
			writeError(w, http.StatusBadRequest, "brano non proposto da questo prune: "+u)
			return
		}
		chosen = append(chosen, it.knownTrack(store.SourceRemoved))
		uris = append(uris, u)
	}
	sp, err := s.SpotifyFor(ctx, uid)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// Registra prima di rimuovere: se la rimozione fallisce a metà, i brani
	// rimasti in libreria sono comunque esclusi da discover (come nella CLI).
	st := s.DB.ForUser(ctx, uid)
	if err := st.AddRemoved(chosen); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := sp.RemoveFromLibrary(ctx, uris); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"removed": len(uris)})
}

// pruneItems carica il risultato del job prune indicato nel path, indicizzato
// per URI. Se il job non è un prune completato dell'utente scrive l'errore.
func (s *Server) pruneItems(w http.ResponseWriter, r *http.Request, uid int64) (map[string]PruneItem, bool) {
	j, err := s.DB.Job(r.Context(), uid, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return nil, false
	}
	if j.Kind != jobs.KindPrune || j.Status != store.JobDone {
		writeError(w, http.StatusBadRequest, "il job non è un prune completato")
		return nil, false
	}
	var res PruneResult
	if err := json.Unmarshal(j.Result, &res); err != nil {
		s.fail(w, r, err)
		return nil, false
	}
	byURI := make(map[string]PruneItem, len(res.Items))
	for _, it := range res.Items {
		byURI[it.URI] = it
	}
	return byURI, true
}

// knownTrack converte un brano proposto da prune nel formato delle tabelle removed/kept.
func (it PruneItem) knownTrack(source string) store.KnownTrack {
	return store.KnownTrack{Key: it.Key, SpotifyID: it.SpotifyID, Artist: it.Artist, Title: it.Title, Source: source}
}

type keepReq struct {
	URI string `json:"uri"`
}

// pruneKeep segna (POST) o toglie (DELETE) un brano proposto da un job prune
// come "da tenere": i prune successivi non lo propongono più.
func (s *Server) pruneKeep(w http.ResponseWriter, r *http.Request, uid int64) {
	var req keepReq
	if !readJSON(w, r, &req) {
		return
	}
	byURI, ok := s.pruneItems(w, r, uid)
	if !ok {
		return
	}
	it, ok := byURI[req.URI]
	if !ok {
		writeError(w, http.StatusBadRequest, "brano non proposto da questo prune: "+req.URI)
		return
	}
	st := s.DB.ForUser(r.Context(), uid)
	keep := r.Method == http.MethodPost
	var err error
	if keep {
		err = st.AddKept(it.knownTrack(""))
	} else {
		err = st.RemoveKept(it.Key)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"kept": keep})
}
