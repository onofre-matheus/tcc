// `pnn triagem` — esvazia a caixa de entrada um item por vez (modo AZUL, RF06):
// nota vira cartão (herdando url/título como fonte), tarefa ou descarte;
// distração vira feita, tarefa ou descarte; `s` adia para a próxima triagem.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/onofre-matheus/tcc/cli/store"
)

type TriageItem struct {
	Kind      string // "note" | "distraction"
	ID        string
	Text      string
	URL       *string // só notas
	PageTitle *string
}

type TriagemConfig struct {
	Items []TriageItem
	Decks []DeckOption
	GenID func() string
	Emit  func(eventType string, payload map[string]any) error
}

type triageStage int

const (
	triageChoose triageStage = iota
	triagePickDeck
	triageCardForm
)

type Triagem struct {
	cfg    TriagemConfig
	idx    int
	stage  triageStage
	picker deckPicker
	form   miniForm
	deckID string

	Cards, Tasks, Discarded, Resolved, Skipped int
	Err                                        error
}

func NewTriagem(cfg TriagemConfig) Triagem {
	if cfg.GenID == nil {
		cfg.GenID = store.UUIDv7
	}
	return Triagem{cfg: cfg, picker: newDeckPicker(cfg.Decks)}
}

func (m Triagem) Init() tea.Cmd {
	return textinput.Blink
}

func (m Triagem) item() TriageItem {
	return m.cfg.Items[m.idx]
}

func (m Triagem) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case errMsg:
		m.Err = msg.err
		return m, tea.Quit

	case tea.KeyMsg:
		switch m.stage {
		case triageChoose:
			return m.updateChoose(msg)
		case triagePickDeck:
			if msg.Type == tea.KeyEsc || msg.Type == tea.KeyCtrlC {
				m.stage = triageChoose // volta às ações do item
				return m, nil
			}
			var chosen *DeckOption
			m.picker, chosen = m.picker.update(msg)
			if chosen != nil {
				m.deckID = chosen.ID
				m.form = newMiniForm(
					formField{Label: "Frente", Placeholder: "pergunta ou conceito", Value: m.item().Text, Required: true},
					formField{Label: "Verso", Placeholder: "resposta", Required: true},
				)
				m.stage = triageCardForm
			}
			return m, nil
		case triageCardForm:
			if msg.Type == tea.KeyEsc || msg.Type == tea.KeyCtrlC {
				m.stage = triageChoose
				return m, nil
			}
			var done bool
			var cmd tea.Cmd
			m.form, done, cmd = m.form.update(msg)
			if !done {
				return m, cmd
			}
			return m.noteToCard()
		}
	}

	if m.stage == triageCardForm {
		var cmd tea.Cmd
		m.form, _, cmd = m.form.update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Triagem) updateChoose(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc || msg.Type == tea.KeyCtrlC || msg.String() == "q" {
		return m, tea.Quit // o que ficou continua pendente
	}
	item := m.item()
	switch msg.String() {
	case "c":
		if item.Kind == "note" && len(m.cfg.Decks) > 0 {
			m.picker = newDeckPicker(m.cfg.Decks)
			m.stage = triagePickDeck
		}
		return m, nil
	case "t":
		return m.toTask()
	case "d":
		return m.discard()
	case "f":
		if item.Kind == "distraction" {
			if err := m.emitTriaged(item, "done", nil); err != nil {
				m.Err = err
				return m, tea.Quit
			}
			m.Resolved++
			return m.advance()
		}
	case "s":
		m.Skipped++
		return m.advance()
	}
	return m, nil
}

func (m Triagem) toTask() (tea.Model, tea.Cmd) {
	item := m.item()
	taskID := m.cfg.GenID()
	if err := m.cfg.Emit("task.created", map[string]any{
		"task_id": taskID, "title": item.Text,
	}); err != nil {
		m.Err = err
		return m, tea.Quit
	}
	if err := m.emitTriaged(item, "to_task", map[string]any{"task_id": taskID}); err != nil {
		m.Err = err
		return m, tea.Quit
	}
	m.Tasks++
	return m.advance()
}

func (m Triagem) discard() (tea.Model, tea.Cmd) {
	if err := m.emitTriaged(m.item(), "discarded", nil); err != nil {
		m.Err = err
		return m, tea.Quit
	}
	m.Discarded++
	return m.advance()
}

func (m Triagem) noteToCard() (tea.Model, tea.Cmd) {
	item := m.item()
	values := m.form.values()
	cardID := m.cfg.GenID()
	payload := map[string]any{
		"card_id": cardID,
		"deck_id": m.deckID,
		"front":   values[0],
		"back":    values[1],
		"tags":    []string{},
	}
	if item.URL != nil {
		payload["source_url"] = *item.URL
	}
	if item.PageTitle != nil {
		payload["source_title"] = *item.PageTitle
	}
	if err := m.cfg.Emit("card.created", payload); err != nil {
		m.Err = err
		return m, tea.Quit
	}
	if err := m.emitTriaged(item, "to_card", map[string]any{"card_id": cardID}); err != nil {
		m.Err = err
		return m, tea.Quit
	}
	m.Cards++
	return m.advance()
}

// emitTriaged grava o evento de triagem do tipo certo para o item.
func (m Triagem) emitTriaged(item TriageItem, action string, extra map[string]any) error {
	eventType := "note.triaged"
	payload := map[string]any{"note_id": item.ID, "action": action}
	if item.Kind == "distraction" {
		eventType = "distraction.triaged"
		payload = map[string]any{"distraction_id": item.ID, "action": action}
	}
	for k, v := range extra {
		payload[k] = v
	}
	return m.cfg.Emit(eventType, payload)
}

func (m Triagem) advance() (tea.Model, tea.Cmd) {
	m.idx++
	m.stage = triageChoose
	if m.idx >= len(m.cfg.Items) {
		return m, tea.Quit // caixa esvaziada (ou adiada)
	}
	return m, nil
}

func (m Triagem) View() string {
	if m.Err != nil || m.idx >= len(m.cfg.Items) {
		return ""
	}
	item := m.item()

	var b strings.Builder
	b.WriteString(blueTitle.Render(fmt.Sprintf("● TRIAGEM · item %d de %d", m.idx+1, len(m.cfg.Items))) + "\n\n")
	b.WriteString(" " + item.Text + "\n")
	if item.Kind == "distraction" {
		b.WriteString(" " + dim.Render("(distração anotada numa sessão de foco)") + "\n")
	} else if item.PageTitle != nil || item.URL != nil {
		source := ""
		if item.PageTitle != nil {
			source = *item.PageTitle
		}
		if item.URL != nil {
			if source != "" {
				source += " · "
			}
			source += *item.URL
		}
		b.WriteString(" " + dim.Render("fonte: "+source) + "\n")
	}
	b.WriteString("\n")

	switch m.stage {
	case triagePickDeck:
		b.WriteString(" para qual deck?\n\n")
		b.WriteString(m.picker.view())
		b.WriteString("\n " + dim.Render("↑↓ seleciona · Enter confirma · Esc volta") + "\n")
	case triageCardForm:
		b.WriteString(m.form.view())
		b.WriteString("\n " + dim.Render("Enter avança · Esc volta") + "\n")
	default:
		if item.Kind == "note" {
			b.WriteString(" " + dim.Render("[c] cartão · [t] tarefa · [d] descartar · [s] seguir · Esc sai") + "\n")
		} else {
			b.WriteString(" " + dim.Render("[f] feita · [t] tarefa · [d] descartar · [s] seguir · Esc sai") + "\n")
		}
	}
	return b.String()
}
