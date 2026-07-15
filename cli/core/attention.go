// Janela de atenção (spec/SPEC.md §4.2) — RF05.
// Mediana das últimas 10 sessões concluídas; menos de 3 amostras → 25 min.
package core

import (
	"slices"
	"time"
)

const (
	defaultAttentionSeconds = 1500
	minSamples              = 3
	sampleWindow            = 10
)

type AttentionState struct {
	AttentionSeconds float64 `json:"attention_seconds"`
}

func init() {
	Register("attention", func(events []Event, _ Params) (any, error) {
		seconds, err := AttentionWindowSeconds(events)
		return AttentionState{AttentionSeconds: seconds}, err
	})
}

func AttentionWindowSeconds(events []Event) (float64, error) {
	startedAt := map[string]string{}
	var durations []float64

	for _, event := range Normalize(events) {
		switch event.Type {
		case "session.started":
			pl, err := decodePayload[struct {
				SessionID string `json:"session_id"`
			}](event)
			if err != nil {
				return 0, err
			}
			startedAt[pl.SessionID] = event.TS

		case "session.ended":
			pl, err := decodePayload[struct {
				SessionID string `json:"session_id"`
				Outcome   string `json:"outcome"`
			}](event)
			if err != nil {
				return 0, err
			}
			start, ok := startedAt[pl.SessionID]
			if !ok || pl.Outcome != "completed" {
				continue
			}
			startTS, err := time.Parse(time.RFC3339, start)
			if err != nil {
				return 0, err
			}
			endTS, err := time.Parse(time.RFC3339, event.TS)
			if err != nil {
				return 0, err
			}
			durations = append(durations, endTS.Sub(startTS).Seconds())
		}
	}

	samples := durations[max(0, len(durations)-sampleWindow):]
	if len(samples) < minSamples {
		return defaultAttentionSeconds, nil
	}

	sorted := slices.Clone(samples)
	slices.Sort(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid], nil
	}
	return (sorted[mid-1] + sorted[mid]) / 2, nil
}
