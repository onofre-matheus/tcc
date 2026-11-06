// Sessão personalizada (spec/SPEC.md §4.11): a fila Leitner filtrada por
// decks, tags e/ou vizinhança de conexões, mais a lista de treino (não
// vencidos) para o modo livre.
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
// (normalizado aqui, então "#SO" ≡ "so"); around por nó "kind:id" (1.4).
type StudyFilter struct {
	Decks  []string `json:"decks"`
	Tags   []string `json:"tags"`
	Around []string `json:"around"`
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

// deckUnder: o deck id está na subárvore "::" do deck root (o próprio ou
// nome que começa com "nome::"). Deck inexistente não está em nada.
func deckUnder(decks DecksState, id, root string) bool {
	r, okRoot := decks.Decks[root]
	d, okDeck := decks.Decks[id]
	return okRoot && okDeck && (d.Name == r.Name || strings.HasPrefix(d.Name, r.Name+"::"))
}

// nodeIncludes: x inclui y — o mesmo cartão, a tag e as descendentes "/", o
// deck e a subárvore "::" (a mesma regra de inclusão dos filtros).
func nodeIncludes(decks DecksState, x, y Node) bool {
	if x.Kind != y.Kind {
		return false
	}
	switch x.Kind {
	case NodeTag:
		return TagUnder(y.ID, x.ID)
	case NodeDeck:
		return deckUnder(decks, y.ID, x.ID)
	}
	return x.ID == y.ID
}

// aroundNodes expande o around (spec §4.11): cada nó X mais os vizinhos de
// todo nó que X inclui — um salto só. nil = sem restrição; lista vazia (só
// nós inválidos) = nada casa.
func aroundNodes(decks DecksState, links LinksState, around []string) []Node {
	if len(around) == 0 {
		return nil
	}
	out := []Node{}
	seen := map[Node]bool{}
	add := func(n Node) {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, raw := range around {
		x, ok := ParseNode(raw)
		if !ok {
			continue
		}
		add(x)
		for _, key := range links.Nodes {
			if y, _ := ParseNode(key); nodeIncludes(decks, x, y) {
				for _, neighbor := range links.Neighbors[key] {
					if n, ok := ParseNode(neighbor); ok {
						add(n)
					}
				}
			}
		}
	}
	return out
}

// StudyMatcher devolve a regra de casamento do filtro, reutilizada fora da
// sessão (ex.: `pnn cartas --tag`, que lista também os não vencidos). As
// conexões só são lidas pelo around; sem ele, LinksState{} basta.
func StudyMatcher(decks DecksState, links LinksState, f StudyFilter) func(id string, card LeitnerCard) bool {
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
	around := aroundNodes(decks, links, f.Around)

	hasTagUnder := func(card LeitnerCard, prefixes ...string) bool {
		for _, have := range EffectiveTags(card, decks) {
			for _, want := range prefixes {
				if TagUnder(have, want) {
					return true
				}
			}
		}
		return false
	}
	return func(id string, card LeitnerCard) bool {
		if deckIDs != nil && (card.DeckID == nil || !deckIDs[*card.DeckID]) {
			return false
		}
		if len(tags) > 0 && !hasTagUnder(card, tags...) {
			return false
		}
		if around == nil {
			return true
		}
		return slices.ContainsFunc(around, func(n Node) bool {
			switch n.Kind {
			case NodeCard:
				return n.ID == id
			case NodeTag:
				return hasTagUnder(card, n.ID)
			case NodeDeck:
				return card.DeckID != nil && deckUnder(decks, *card.DeckID, n.ID)
			}
			return false
		})
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
	links, err := Links(events)
	if err != nil {
		return StudyState{}, err
	}
	match := StudyMatcher(decks, links, p.Study)

	state := StudyState{Queue: []string{}, Practice: []string{}}
	due := make(map[string]bool, len(leitner.Queue))
	for _, id := range leitner.Queue {
		due[id] = true
		if match(id, leitner.Cards[id]) {
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
		if !due[id] && activeCard(card, decks) && match(id, card) {
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
