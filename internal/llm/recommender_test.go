package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

func TestParse(t *testing.T) {
	got, err := Parse(`{"suggestions":[
		{"artist":" Slowdive ","title":"Alison","reason":"Shoegaze sognante."},
		{"artist":"","title":"Senza artista","reason":"x"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Artist != "Slowdive" {
		t.Fatalf("risultato inatteso: %+v", got)
	}
	if _, err := Parse("non è json"); err == nil {
		t.Error("atteso errore su JSON non valido")
	}
}

func TestSupportsDefaultFallback(t *testing.T) {
	for m, want := range map[string]bool{
		"claude-opus-5-5": true, "claude-fable-5-1": true, "claude-sonnet-5-5": true,
		"claude-haiku-5-5": false, "claude-sonnet-4-6": false,
	} {
		if got := supportsDefaultFallback(m); got != want {
			t.Errorf("%s: got %v want %v", m, got, want)
		}
	}
}

func TestRecommendRequestShape(t *testing.T) {
	var body map[string]any
	var beta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		beta = r.Header.Get("anthropic-beta")
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5",
			"content":[{"type":"text","text":"{\"suggestions\":[{\"artist\":\"Ride\",\"title\":\"Vapour Trail\",\"reason\":\"ok\"}]}"}],
			"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer srv.Close()

	r := New("claude-opus-5-5", "medium", option.WithBaseURL(srv.URL), option.WithAPIKey("test"), option.WithMaxRetries(0))
	got, err := r.Recommend(context.Background(), Request{Profile: "p", Count: 3, Mood: "notturno"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Vapour Trail" {
		t.Fatalf("risultato inatteso: %+v", got)
	}
	oc, _ := body["output_config"].(map[string]any)
	format, _ := oc["format"].(map[string]any)
	if oc["effort"] != "medium" || format["type"] != "json_schema" || format["schema"] == nil {
		t.Errorf("output_config inatteso: %v", body["output_config"])
	}
	if body["fallbacks"] != "default" || !strings.Contains(beta, "server-side-fallback-2026-07-01") {
		t.Errorf("fallback mancante: fallbacks=%v beta=%q", body["fallbacks"], beta)
	}
	if _, ok := body["thinking"]; ok {
		t.Error("thinking non deve essere impostato")
	}
}

func TestParsePrune(t *testing.T) {
	got, err := ParsePrune(`{"picks":[
		{"id":2,"reason":" fuori genere "},
		{"id":2,"reason":"duplicato"},
		{"id":7,"reason":"fuori elenco"},
		{"id":-1,"reason":"negativo"},
		{"id":0,"reason":"ok"}]}`, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != 2 || got[0].Reason != "fuori genere" || got[1].ID != 0 {
		t.Fatalf("risultato inatteso: %+v", got)
	}
}
