// Ordem total e idempotência do log (spec/SPEC.md §2).
// Toda projeção recebe eventos já normalizados por esta função.
package core

import (
	"cmp"
	"slices"
)

func compareEvents(a, b Event) int {
	if a.LC != b.LC {
		return cmp.Compare(a.LC, b.LC)
	}
	if a.TS != b.TS {
		return cmp.Compare(a.TS, b.TS)
	}
	if a.Device != b.Device {
		return cmp.Compare(a.Device, b.Device)
	}
	return cmp.Compare(a.ID, b.ID)
}

// Normalize ordena por (lc, ts, device, id) e deduplica por id (mantém a
// primeira ocorrência). Qualquer permutação da mesma entrada converge.
func Normalize(events []Event) []Event {
	sorted := slices.Clone(events)
	slices.SortStableFunc(sorted, compareEvents)

	seen := make(map[string]bool, len(sorted))
	result := sorted[:0]
	for _, event := range sorted {
		if seen[event.ID] {
			continue
		}
		seen[event.ID] = true
		result = append(result, event)
	}
	return result
}
