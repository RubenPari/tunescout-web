package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// PruneCandidate è un brano della libreria che potrebbe essere rimosso.
type PruneCandidate struct {
	Artist, Title, Year string
	AddedAt             string // data di salvataggio, es. "2019-03"
}

// PrunePick è un brano scelto da Claude, identificato dall'indice in Candidates.
type PrunePick struct {
	ID     int    `json:"id"`
	Reason string `json:"reason"`
}

type PruneRequest struct {
	Profile    string
	Count      int
	Candidates []PruneCandidate
}

const pruneSystemPrompt = `Sei un curatore musicale esperto. Aiuti un utente Spotify a fare pulizia nella sua libreria di brani salvati.
Ricevi il profilo d'ascolto dell'utente e un elenco numerato di brani salvati che l'utente non ascolta quasi mai (non compaiono tra i suoi brani più ascoltati e i loro artisti non sono tra quelli che ascolta di più).

Linee guida:
- Scegli i brani che sono meno coerenti con i gusti attuali dell'utente: generi, scene, epoche o atmosfere lontani dal suo profilo.
- Non scegliere brani che, pur poco ascoltati, sono chiaramente in linea con il profilo (stessi generi o artisti affini a quelli che ascolta di più).
- Usa solo gli id dell'elenco, ognuno al massimo una volta. Se i brani davvero fuori dal profilo sono meno di quelli richiesti, restituiscine meno.
- Ordina dal brano più fuori profilo al meno.
- Per ogni brano scrivi in italiano un motivo breve e concreto (una frase) che spieghi perché si discosta dai gusti dell'utente.`

var pruneSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "picks": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "id":     {"type": "integer", "description": "Numero del brano nell'elenco"},
          "reason": {"type": "string", "description": "Perché si discosta dai gusti dell'utente, una frase in italiano"}
        },
        "required": ["id", "reason"],
        "additionalProperties": false
      }
    }
  },
  "required": ["picks"],
  "additionalProperties": false
}`)

// PickPrune chiede a Claude quali candidati sono meno in linea con il profilo.
func (r *Recommender) PickPrune(ctx context.Context, req PruneRequest) ([]PrunePick, error) {
	var u strings.Builder
	u.WriteString("# Profilo d'ascolto\n\n")
	u.WriteString(req.Profile)
	u.WriteString("\n\n# Brani salvati poco ascoltati\n")
	for i, c := range req.Candidates {
		fmt.Fprintf(&u, "%d. %s — %s", i, c.Artist, c.Title)
		var extra []string
		if c.Year != "" {
			extra = append(extra, c.Year)
		}
		if c.AddedAt != "" {
			extra = append(extra, "salvato "+c.AddedAt)
		}
		if len(extra) > 0 {
			fmt.Fprintf(&u, " (%s)", strings.Join(extra, ", "))
		}
		u.WriteString("\n")
	}
	fmt.Fprintf(&u, "\n# Richiesta\nScegli al massimo %d brani da eliminare dalla libreria.\n", req.Count)

	text, err := r.complete(ctx, pruneSystemPrompt, u.String(), pruneSchema)
	if err != nil {
		return nil, err
	}
	return ParsePrune(text, len(req.Candidates))
}

// ParsePrune decodifica l'output del modello scartando id fuori elenco o ripetuti.
func ParsePrune(s string, n int) ([]PrunePick, error) {
	var out struct {
		Picks []PrunePick `json:"picks"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &out); err != nil {
		return nil, fmt.Errorf("risposta del modello non valida: %w", err)
	}
	seen := map[int]bool{}
	var clean []PrunePick
	for _, p := range out.Picks {
		if p.ID < 0 || p.ID >= n || seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		p.Reason = strings.TrimSpace(p.Reason)
		clean = append(clean, p)
	}
	return clean, nil
}
