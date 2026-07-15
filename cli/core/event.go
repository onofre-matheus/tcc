// Envelope de evento (spec/SPEC.md §1).
// O payload fica opaco (json.RawMessage): cada projeção decodifica apenas os
// campos dos tipos de evento que lhe interessam.
package core

import "encoding/json"

type Event struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	V       int             `json:"v"`
	LC      int64           `json:"lc"`
	TS      string          `json:"ts"` // UTC ISO-8601; ordem lexicográfica = cronológica
	Device  string          `json:"device"`
	Payload json.RawMessage `json:"payload"`
}

// decodePayload lê o payload de um evento nos campos do tipo T.
func decodePayload[T any](e Event) (T, error) {
	var p T
	err := json.Unmarshal(e.Payload, &p)
	return p, err
}

// orEmpty troca slice ausente por vazia: no JSON projetado, listas são sempre
// `[]`, nunca `null` (mesma saída do cliente JS).
func orEmpty(tags []string) []string {
	if tags == nil {
		return []string{}
	}
	return tags
}
