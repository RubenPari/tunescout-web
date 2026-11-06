// Package llm genera suggerimenti musicali con Claude.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

type Suggestion struct {
	Artist string `json:"artist"`
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

type Request struct {
	Profile        string   // profilo di gusto renderizzato
	Count          int      // quanti suggerimenti chiedere
	Mood           string   // vincolo opzionale
	Genres         []string // vincolo opzionale
	NewArtistsOnly bool
	Exclude        []string // "Artista — Titolo" già proposti o scartati in questo giro
}

// Client genera suggerimenti (Recommend) e sceglie brani da eliminare (PickPrune) con Claude.
type Client struct {
	client anthropic.Client
	model  string
	effort string
}

func New(model, effort string, opts ...option.RequestOption) *Client {
	// Legge ANTHROPIC_API_KEY (o un profilo `ant auth login`) dall'ambiente.
	return &Client{client: anthropic.NewClient(opts...), model: model, effort: effort}
}

const systemPrompt = `Sei un curatore musicale esperto con una conoscenza ampia e profonda di generi, scene ed epoche diverse.
Ricevi il profilo d'ascolto di un utente Spotify e proponi brani che probabilmente non conosce ma che gli piaceranno.

Linee guida:
- Proponi solo brani realmente esistenti e pubblicati, con il nome dell'artista e il titolo esatti come appaiono su Spotify. Se non sei sicuro che un brano esista, non proporlo: ogni suggerimento viene verificato su Spotify e quelli non trovati vengono scartati.
- Scegli brani nuovi per l'utente: evita i brani elencati nel profilo e privilegia artisti diversi da quelli che ascolta di più.
- Varia la selezione: mescola artisti affermati e meno noti, epoche e paesi diversi, restando coerente con i gusti dell'utente. Al massimo due brani dello stesso artista.
- Tieni conto dei suggerimenti che l'utente ha apprezzato o scartato in passato.
- Per ogni brano scrivi in italiano un motivo breve e concreto (una frase) che lo colleghi ad artisti o brani del profilo.`

var schema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "suggestions": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "artist": {"type": "string", "description": "Artista principale, nome esatto"},
          "title":  {"type": "string", "description": "Titolo esatto del brano"},
          "reason": {"type": "string", "description": "Perché è adatto all'utente, una frase in italiano"}
        },
        "required": ["artist", "title", "reason"],
        "additionalProperties": false
      }
    }
  },
  "required": ["suggestions"],
  "additionalProperties": false
}`)

// supportsDefaultFallback: modelli che accettano `fallbacks: "default"` sulla Claude API.
func supportsDefaultFallback(model string) bool {
	for _, p := range []string{"claude-opus-5", "claude-fable-5", "claude-sonnet-5-5", "claude-mythos-5"} {
		if strings.HasPrefix(model, p) {
			return true
		}
	}
	return false
}

func (r *Client) Recommend(ctx context.Context, req Request) ([]Suggestion, error) {
	var u strings.Builder
	writeProfile(&u, req.Profile)
	u.WriteString("# Richiesta\n")
	fmt.Fprintf(&u, "Proponi %d brani.\n", req.Count)
	if req.Mood != "" {
		fmt.Fprintf(&u, "Atmosfera/mood desiderato: %s\n", req.Mood)
	}
	if len(req.Genres) > 0 {
		fmt.Fprintf(&u, "Generi richiesti: %s\n", strings.Join(req.Genres, ", "))
	}
	if req.NewArtistsOnly {
		u.WriteString("Solo artisti che non compaiono nel profilo: l'utente vuole scoprire artisti nuovi.\n")
	}
	if len(req.Exclude) > 0 {
		u.WriteString("\nNon riproporre questi brani (già proposti, già noti o non trovati su Spotify):\n")
		for _, e := range req.Exclude {
			fmt.Fprintf(&u, "- %s\n", e)
		}
	}

	text, err := r.complete(ctx, systemPrompt, u.String(), schema)
	if err != nil {
		return nil, err
	}
	return Parse(text)
}

// writeProfile apre il messaggio utente con il profilo d'ascolto.
func writeProfile(b *strings.Builder, profile string) {
	b.WriteString("# Profilo d'ascolto\n\n")
	b.WriteString(profile)
	b.WriteString("\n\n")
}

// complete invia una richiesta con output JSON vincolato allo schema e ne
// restituisce il testo.
func (r *Client) complete(ctx context.Context, system, user string, schema json.RawMessage) (string, error) {
	params := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(r.model),
		MaxTokens: 16000,
		System:    []anthropic.BetaTextBlockParam{{Text: system}},
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(user))},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: anthropic.BetaOutputConfigEffort(r.effort),
			Format: anthropic.BetaJSONOutputFormatParam{Schema: schema},
		},
	}
	if supportsDefaultFallback(r.model) {
		// Se un classificatore di sicurezza rifiuta la richiesta, il server la
		// ripropone a un modello di fallback scelto in base alla categoria.
		params.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
		params.Fallbacks = anthropic.BetaFallbacksParamOfDefault()
	}

	msg, err := r.client.Beta.Messages.New(ctx, params)
	if err != nil {
		return "", describeAPIError(err)
	}
	switch msg.StopReason {
	case anthropic.BetaStopReasonRefusal:
		return "", fmt.Errorf("Claude ha rifiutato la richiesta (categoria: %s)", msg.StopDetails.Category)
	case anthropic.BetaStopReasonMaxTokens:
		return "", errors.New("risposta del modello troncata: riduci il numero di brani")
	}

	var text strings.Builder
	for _, b := range msg.Content {
		if b.Type == "text" {
			text.WriteString(b.Text)
		}
	}
	return text.String(), nil
}

// Parse decodifica l'output JSON del modello.
func Parse(s string) ([]Suggestion, error) {
	var out struct {
		Suggestions []Suggestion `json:"suggestions"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &out); err != nil {
		return nil, fmt.Errorf("risposta del modello non valida: %w", err)
	}
	var clean []Suggestion
	for _, x := range out.Suggestions {
		x.Artist, x.Title, x.Reason = strings.TrimSpace(x.Artist), strings.TrimSpace(x.Title), strings.TrimSpace(x.Reason)
		if x.Artist != "" && x.Title != "" {
			clean = append(clean, x)
		}
	}
	return clean, nil
}

func describeAPIError(err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized:
			return errors.New("Anthropic: credenziali non valide (imposta ANTHROPIC_API_KEY)")
		case http.StatusNotFound:
			return fmt.Errorf("Anthropic: modello non trovato: %w", err)
		case http.StatusTooManyRequests:
			return fmt.Errorf("Anthropic: rate limit raggiunto, riprova più tardi: %w", err)
		}
		return fmt.Errorf("Anthropic API (HTTP %d): %w", apiErr.StatusCode, err)
	}
	return fmt.Errorf("Anthropic: %w", err)
}
