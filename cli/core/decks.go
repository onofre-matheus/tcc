// Listagem de decks (spec/SPEC.md §4.5) — RF07.
// Deck = unidade de estudo com nome e tags transversais; hierarquia é
// convenção de nome com "::" (derivada na exibição, não aqui).
package core

type Deck struct {
	Name     string   `json:"name"`
	Tags     []string `json:"tags"`
	Archived bool     `json:"archived"`
}

type DecksState struct {
	Decks map[string]Deck `json:"decks"`
}

func init() {
	Register("decks", func(events []Event, _ Params) (any, error) {
		return Decks(events)
	})
}

func Decks(events []Event) (DecksState, error) {
	state := DecksState{Decks: map[string]Deck{}}

	for _, event := range Normalize(events) {
		switch event.Type {
		case "deck.created":
			pl, err := decodePayload[struct {
				DeckID string   `json:"deck_id"`
				Name   string   `json:"name"`
				Tags   []string `json:"tags"`
			}](event)
			if err != nil {
				return DecksState{}, err
			}
			state.Decks[pl.DeckID] = Deck{Name: pl.Name, Tags: orEmpty(pl.Tags)}

		case "deck.renamed":
			pl, err := decodePayload[struct {
				DeckID string `json:"deck_id"`
				Name   string `json:"name"`
			}](event)
			if err != nil {
				return DecksState{}, err
			}
			if deck, ok := state.Decks[pl.DeckID]; ok {
				deck.Name = pl.Name // último nome vence
				state.Decks[pl.DeckID] = deck
			}

		case "deck.archived":
			pl, err := decodePayload[struct {
				DeckID string `json:"deck_id"`
			}](event)
			if err != nil {
				return DecksState{}, err
			}
			if deck, ok := state.Decks[pl.DeckID]; ok {
				deck.Archived = true
				state.Decks[pl.DeckID] = deck
			}
		}
	}
	return state, nil
}
