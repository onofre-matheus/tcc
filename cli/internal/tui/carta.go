// `pnn carta [deck]` — criação de cartões em série (modo AZUL): escolhe o deck
// (se não veio por argumento; "(sem deck)" cria cartão solto), depois frente →
// verso → fonte opcional → tags opcionais; cada confirmação emite card.created
// e reinicia o formulário para o próximo, mantendo as tags da série.
package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/store"
)

type CartaConfig struct {
	Decks    []DeckOption
	DeckID   string // preenchidos quando o deck veio por argumento
	DeckName string
	NoDeck   bool     // deck escolhido de antemão: nenhum (cartão solto)
	Tags     []string // tags iniciais da série (--tag), já normalizadas
	GenID    func() string
	Emit     func(eventType string, payload map[string]any) error
	// EmitV emite com versão explícita (card.created v2, sem deck). Ausente,
	// degrada para Emit.
	EmitV func(eventType string, v int, payload map[string]any) error
}

type cartaStage int

const (
	cartaPickDeck cartaStage = iota
	cartaEditing
)

type Carta struct {
	cfg      CartaConfig
	stage    cartaStage
	picker   deckPicker
	form     miniForm
	deckID   string
	deckName string
	tags     string // valor do campo de tags, mantido entre os cartões da série
	flash    string
	Created  int
	Err      error
}

func NewCarta(cfg CartaConfig) Carta {
	if cfg.GenID == nil {
		cfg.GenID = store.UUIDv7
	}
	if cfg.EmitV == nil {
		cfg.EmitV = degradeEmitV(cfg.Emit)
	}
	m := Carta{cfg: cfg, picker: newDeckPicker(withNoDeck(cfg.Decks)), tags: tagsLine(cfg.Tags)}
	switch {
	case cfg.DeckID != "":
		m.deckID, m.deckName = cfg.DeckID, cfg.DeckName
		m.stage = cartaEditing
	case cfg.NoDeck || len(cfg.Decks) == 0: // sem decks, o picker só teria "(sem deck)"
		m.deckName = NoDeck.Name
		m.stage = cartaEditing
	}
	m.form = newCardForm("", m.tags)
	return m
}

func newCardForm(front, tags string) miniForm {
	return newMiniForm(
		formField{Label: "Frente", Placeholder: "pergunta ou conceito", Value: front, Required: true},
		formField{Label: "Verso", Placeholder: "resposta", Required: true},
		formField{Label: "Fonte (URL, opcional)", Placeholder: "https://…"},
		formField{Label: "Tags (opcional)", Placeholder: "#so #prova-p2 (espaço ou vírgula; / cria hierarquia)", Value: tags},
	)
}

func (m Carta) Init() tea.Cmd {
	return textinput.Blink
}

func (m Carta) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case errMsg:
		m.Err = msg.err
		return m, tea.Quit

	case tea.KeyMsg:
		if msg.Type == tea.KeyEsc || msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		if m.stage == cartaPickDeck {
			var chosen *DeckOption
			m.picker, chosen = m.picker.update(msg)
			if chosen != nil {
				m.deckID, m.deckName = chosen.ID, chosen.Name
				m.stage = cartaEditing
				m.form = newCardForm("", m.tags)
			}
			return m, nil
		}

		var done bool
		var cmd tea.Cmd
		m.form, done, cmd = m.form.update(msg)
		if !done {
			return m, cmd
		}
		values := m.form.values()
		payload := map[string]any{
			"card_id": m.cfg.GenID(),
			"front":   values[0],
			"back":    values[1],
		}
		if values[2] != "" {
			payload["source_url"] = values[2]
		}
		tags := core.ParseTags(values[3])
		if err := emitCardCreated(m.cfg.Emit, m.cfg.EmitV, payload, m.deckID, tags); err != nil {
			m.Err = err
			return m, tea.Quit
		}
		m.Created++
		m.flash = "✔ cartão criado — próximo (Esc sai)"
		m.tags = tagsLine(tags)
		m.form = newCardForm("", m.tags)
		return m, nil
	}

	if m.stage == cartaEditing {
		var cmd tea.Cmd
		m.form, _, cmd = m.form.update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Carta) View() string {
	if m.Err != nil {
		return ""
	}
	var b strings.Builder
	if m.stage == cartaPickDeck {
		b.WriteString(blueTitle.Render("● NOVO CARTÃO — escolha o deck") + "\n\n")
		b.WriteString(m.picker.view())
		b.WriteString("\n " + dim.Render("↑↓ seleciona · Enter confirma · Esc sai") + "\n")
		return b.String()
	}

	b.WriteString(blueTitle.Render("● NOVO CARTÃO · "+m.deckName) + "\n")
	if m.flash != "" {
		b.WriteString(" " + greenTitle.Render(m.flash) + "\n")
	}
	b.WriteString("\n" + m.form.view())
	b.WriteString("\n " + dim.Render("Enter avança · Esc sai") + "\n")
	return b.String()
}
