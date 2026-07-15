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
