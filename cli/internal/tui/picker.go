// Seletor de deck compartilhado por `pnn carta` e pela triagem nota→cartão.
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type DeckOption struct {
	ID   string
	Name string // caminho completo, ex. "Cálculo::Limites"
}

type deckPicker struct {
	options []DeckOption
	cursor  int
}

func newDeckPicker(options []DeckOption) deckPicker {
	return deckPicker{options: options}
}

// update navega a lista; devolve a opção quando o usuário confirma.
func (p deckPicker) update(msg tea.KeyMsg) (deckPicker, *DeckOption) {
	switch {
	case msg.Type == tea.KeyUp || msg.String() == "k":
		if p.cursor > 0 {
			p.cursor--
		}
	case msg.Type == tea.KeyDown || msg.String() == "j":
		if p.cursor < len(p.options)-1 {
			p.cursor++
		}
	case msg.Type == tea.KeyEnter:
		if len(p.options) > 0 {
			chosen := p.options[p.cursor]
			return p, &chosen
		}
	}
	return p, nil
}

func (p deckPicker) view() string {
	var b strings.Builder
	for i, option := range p.options {
		if i == p.cursor {
			b.WriteString(blueText.Render(" › "+option.Name) + "\n")
		} else {
			b.WriteString("   " + option.Name + "\n")
		}
	}
	return b.String()
}

// NoDeck é a opção "(sem deck)" do seletor: o cartão nasce solto, organizado
// só pelas tags (card.created v2 sem deck_id). Fica no fim da lista para o
// Enter imediato continuar escolhendo o primeiro deck, como antes.
var NoDeck = DeckOption{ID: "", Name: "(sem deck)"}

func withNoDeck(options []DeckOption) []DeckOption {
	return append(append([]DeckOption(nil), options...), NoDeck)
}

// emitCardCreated grava o card.created: com deck, o payload v1 de sempre; sem
// deck, v2 sem o campo deck_id (spec/SPEC.md §3). Tags já normalizadas.
func emitCardCreated(
	emit func(string, map[string]any) error,
	emitV func(string, int, map[string]any) error,
	payload map[string]any, deckID string, tags []string,
) error {
	payload["tags"] = tags
	if deckID != "" {
		payload["deck_id"] = deckID
		return emit("card.created", payload)
	}
	return emitV("card.created", 2, payload)
}

// degradeEmitV: sem EmitV configurado, emite pelo Emit comum (os testes
// antigos seguem válidos), como na revisão.
func degradeEmitV(emit func(string, map[string]any) error) func(string, int, map[string]any) error {
	return func(eventType string, _ int, payload map[string]any) error {
		return emit(eventType, payload)
	}
}

// tagsLine formata as tags para exibição ("#so #prova-p2").
func tagsLine(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	return "#" + strings.Join(tags, " #")
}
