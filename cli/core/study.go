// Sessão personalizada (spec/SPEC.md §4.11): a fila Leitner filtrada por
// decks e/ou tags, mais a lista de treino (não vencidos) para o modo livre.
// O treino grava card.practiced, que nenhuma projeção de agendamento lê —
// estudar antes da hora não promove o cartão.
package core

import (
	"cmp"
	"slices"
	"strings"
)

// StudyFilter: OU dentro de cada lista, E entre as listas; lista vazia não
// restringe. Decks por id (expandidos para a subárvore "::"); tags pelo nome
// (normalizado aqui, então "#SO" ≡ "so").
type StudyFilter struct {
	Decks []string `json:"decks"`
	Tags  []string `json:"tags"`
}

type StudyState struct {
	Queue    []string `json:"queue"`    // vencidos que casam, na ordem da fila Leitner
	Practice []string `json:"practice"` // ativos não vencidos que casam (modo treino)
}

func init() {
	Register("study", func(events []Event, p Params) (any, error) {
		return Study(events, p)
	})
}

// StudyMatcher devolve a regra de casamento do filtro, reutilizada fora da
// sessão (ex.: `pnn cartas --tag`, que lista também os não vencidos).
func StudyMatcher(decks DecksState, f StudyFilter) func(LeitnerCard) bool {
	var deckIDs map[string]bool
	if len(f.Decks) > 0 {
		deckIDs = map[string]bool{}
		for _, id := range f.Decks {
			root, ok := decks.Decks[id]
			if !ok {
				continue // deck inexistente no filtro não casa nada
			}
			for other, deck := range decks.Decks {
				if deck.Name == root.Name || strings.HasPrefix(deck.Name, root.Name+"::") {
					deckIDs[other] = true
				}
			}
		}
	}
	tags := NormalizeTags(f.Tags)

	return func(card LeitnerCard) bool {
		if deckIDs != nil && (card.DeckID == nil || !deckIDs[*card.DeckID]) {
			return false
		}
		if len(tags) == 0 {
			return true
		}
		for _, have := range EffectiveTags(card, decks) {
			for _, want := range tags {
				if TagUnder(have, want) {
					return true
				}
			}
		}
		return false
	}
}

func Study(events []Event, p Params) (StudyState, error) {
	leitner, err := Leitner(events, p)
	if err != nil {
		return StudyState{}, err
	}
	decks, err := Decks(events)
	if err != nil {
		return StudyState{}, err
	}
	match := StudyMatcher(decks, p.Study)

	state := StudyState{Queue: []string{}, Practice: []string{}}
	due := make(map[string]bool, len(leitner.Queue))
	for _, id := range leitner.Queue {
		due[id] = true
		if match(leitner.Cards[id]) {
			state.Queue = append(state.Queue, id)
		}
	}

	// Ordem do treino = critério da fila (§4.1): caixa, vencimento, ts de
	// criação; o id desempata para a saída não depender da ordem do mapa.
	created := map[string]string{}
	for _, event := range Normalize(events) {
		if event.Type != "card.created" {
			continue
		}
		pl, err := decodePayload[struct {
			CardID string `json:"card_id"`
		}](event)
		if err != nil {
			return StudyState{}, err
		}
		if _, seen := created[pl.CardID]; !seen {
			created[pl.CardID] = event.TS
		}
	}
	for id, card := range leitner.Cards {
		if !due[id] && activeCard(card, decks) && match(card) {
			state.Practice = append(state.Practice, id)
		}
	}
	slices.SortFunc(state.Practice, func(a, b string) int {
		ca, cb := leitner.Cards[a], leitner.Cards[b]
		return cmp.Or(
			cmp.Compare(ca.Box, cb.Box),
			cmp.Compare(ca.Due, cb.Due),
			cmp.Compare(created[a], created[b]),
			cmp.Compare(a, b),
		)
	})
	return state, nil
}
