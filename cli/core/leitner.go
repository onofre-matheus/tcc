// Projeção Leitner (spec/SPEC.md §4.1) — RF08/RF11.
// 5 caixas; acerto promove, erro regride UMA caixa (variante atenuada).
package core

import (
	"cmp"
	"slices"
)

const maxBox = 5

// intervalDays[caixa] = dias até a próxima revisão (índice 0 não é usado).
var intervalDays = [maxBox + 1]int{0, 1, 3, 7, 14, 30}

type LeitnerCard struct {
	DeckID           string   `json:"deck_id"`
	Box              int      `json:"box"`
	Due              string   `json:"due"`
	Front            string   `json:"front"`
	Back             string   `json:"back"`
	SourceURL        *string  `json:"source_url"`
	SourceTitle      *string  `json:"source_title"`
	Tags             []string `json:"tags"`
	LastExplanation  *string  `json:"last_explanation"`
	ExplanationCount int      `json:"explanation_count"`
	LastFrame        string   `json:"last_frame"` // último andaime; ausente ≡ "feynman"
	Archived         bool     `json:"archived"`
}

type LeitnerState struct {
	Cards map[string]LeitnerCard `json:"cards"`
	Queue []string               `json:"queue"`
}

func init() {
	Register("leitner", func(events []Event, p Params) (any, error) {
		return Leitner(events, p)
	})
}

func Leitner(events []Event, p Params) (LeitnerState, error) {
	type cardState struct {
		LeitnerCard
		id        string
		createdTS string
	}
	cards := map[string]*cardState{}
	var order []*cardState // ordem de criação no log normalizado
	archivedDecks := map[string]bool{}

	for _, event := range Normalize(events) {
		switch event.Type {
		case "card.created":
			pl, err := decodePayload[struct {
				CardID      string   `json:"card_id"`
				DeckID      string   `json:"deck_id"`
				Front       string   `json:"front"`
				Back        string   `json:"back"`
				SourceURL   *string  `json:"source_url"`
				SourceTitle *string  `json:"source_title"`
				Tags        []string `json:"tags"`
			}](event)
			if err != nil {
				return LeitnerState{}, err
			}
			due, err := LocalDate(event.TS, p.TZ)
			if err != nil {
				return LeitnerState{}, err
			}
			card := &cardState{
				LeitnerCard: LeitnerCard{
					DeckID:      pl.DeckID,
					Box:         1,
					Due:         due,
					Front:       pl.Front,
					Back:        pl.Back,
					SourceURL:   pl.SourceURL,
					SourceTitle: pl.SourceTitle,
					Tags:        orEmpty(pl.Tags),
					LastFrame:   "feynman",
				},
				id:        pl.CardID,
				createdTS: event.TS,
			}
			cards[pl.CardID] = card
			order = append(order, card)

		case "card.edited":
			pl, err := decodePayload[struct {
				CardID string    `json:"card_id"`
				Front  *string   `json:"front"`
				Back   *string   `json:"back"`
				Tags   *[]string `json:"tags"`
			}](event)
			if err != nil {
				return LeitnerState{}, err
			}
			card, ok := cards[pl.CardID]
			if !ok {
				continue
			}
			// last-writer-wins por campo; caixa e vencimento não mudam
			if pl.Front != nil {
				card.Front = *pl.Front
			}
			if pl.Back != nil {
				card.Back = *pl.Back
			}
			if pl.Tags != nil {
				card.Tags = orEmpty(*pl.Tags)
			}

		case "card.archived":
			pl, err := decodePayload[struct {
				CardID string `json:"card_id"`
			}](event)
			if err != nil {
				return LeitnerState{}, err
			}
			if card, ok := cards[pl.CardID]; ok {
				card.Archived = true
			}

		case "deck.archived":
			pl, err := decodePayload[struct {
				DeckID string `json:"deck_id"`
			}](event)
			if err != nil {
				return LeitnerState{}, err
			}
			archivedDecks[pl.DeckID] = true

		case "card.explained":
			pl, err := decodePayload[struct {
				CardID string `json:"card_id"`
				Text   string `json:"text"`
				Frame  string `json:"frame"` // v2; ausente ≡ "feynman"
			}](event)
			if err != nil {
				return LeitnerState{}, err
			}
			card, ok := cards[pl.CardID]
			if !ok {
				continue // explicação de cartão inexistente é ignorada
			}
			text := pl.Text
			card.LastExplanation = &text
			card.ExplanationCount++
			if pl.Frame != "" {
				card.LastFrame = pl.Frame
			} else {
				card.LastFrame = "feynman"
			}

		case "card.reviewed":
			pl, err := decodePayload[struct {
				CardID string `json:"card_id"`
				Result string `json:"result"`
			}](event)
			if err != nil {
				return LeitnerState{}, err
			}
			card, ok := cards[pl.CardID]
			if !ok {
				continue
			}
			if pl.Result == "correct" {
				card.Box = min(card.Box+1, maxBox)
			} else {
				card.Box = max(card.Box-1, 1)
			}
			reviewDay, err := LocalDate(event.TS, p.TZ)
			if err != nil {
				return LeitnerState{}, err
			}
			card.Due, err = AddDays(reviewDay, intervalDays[card.Box])
			if err != nil {
				return LeitnerState{}, err
			}
		}
	}

	today, err := LocalDate(p.Now, p.TZ)
	if err != nil {
		return LeitnerState{}, err
	}

	var due []*cardState
	for _, card := range order {
		if card.Due <= today && !card.Archived && !archivedDecks[card.DeckID] {
			due = append(due, card)
		}
	}
	slices.SortStableFunc(due, func(a, b *cardState) int {
		if a.Box != b.Box {
			return a.Box - b.Box
		}
		if a.Due != b.Due {
			return cmp.Compare(a.Due, b.Due)
		}
		return cmp.Compare(a.createdTS, b.createdTS)
	})

	state := LeitnerState{Cards: make(map[string]LeitnerCard, len(cards)), Queue: []string{}}
	for id, card := range cards {
		state.Cards[id] = card.LeitnerCard
	}
	for _, card := range due {
		state.Queue = append(state.Queue, card.id)
	}
	return state, nil
}
