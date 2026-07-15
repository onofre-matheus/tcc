// `pnn revisar` — revisão Leitner imersiva (spec/CLI.md §3, RF08/RF11): a fila
// frágil→consolidada é consumida cartão a cartão até o timer tocar. Antes de
// revelar o verso, a etapa Feynman: explicar com as próprias palavras
// (persistida como card.explained; vazio = pular) e comparar com a explicação
// anterior para fechar a lacuna iterativamente.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/onofre-matheus/tcc/cli/store"
)

type Card struct {
	ID              string
	Front, Back     string
	SourceURL       *string
	SourceTitle     *string
	LastExplanation *string
	LastFrame       string // andaime da explicação anterior (default "feynman")
	Box             int
}

// Andaimes de explicação (spec/SPEC.md §4.1). O Feynman é o padrão; as 4 causas
// enquadram o conceito passo a passo. Campo aberto: um método novo é só mais um
// frame aqui + os rótulos, sem tocar na máquina de estados.
const (
	frameFeynman = "feynman"
	frame4Causas = "4causas"
)

// As quatro causas traduzidas em perguntas de estudo (não em jargão metafísico).
var quatroCausas = [4]struct{ key, prompt string }{
	{"material", "Material — do que é feito? quais os componentes?"},
	{"formal", "Formal — qual a forma, estrutura ou definição?"},
	{"eficiente", "Eficiente — o que o causa / de onde vem / como surge?"},
	{"final", "Final — para que serve? qual o propósito?"},
}

type RevisarConfig struct {
	SessionID    string
	GenID        func() string // pause_id
	Cards        []Card        // fila vencida, já na ordem frágil→consolidada
	Minutes      int
	PauseMinutes int
	Emit         func(eventType string, payload map[string]any) error
	// EmitV emite com versão de payload explícita (session.ended v2, com
	// reason?). Ausente, degrada para Emit — os testes antigos seguem válidos.
	EmitV func(eventType string, v int, payload map[string]any) error
}

type revisarPhase int

const (
	askPhase revisarPhase = iota
	revealPhase
	reviewPause
	reasonPhase // Esc: uma linha opcional de motivo antes de encerrar
)

type Revisar struct {
	cfg            RevisarConfig
	phase          revisarPhase
	timer          countdown
	idx            int
	pauseID        string
	input          textinput.Model
	frame          string    // andaime escolhido para o cartão atual (Tab alterna)
	causeIdx       int       // passo corrente do wizard das 4 causas (0..3)
	causes         [4]string // respostas coletadas por causa
	Correct, Wrong int
	Err            error
}

func NewRevisar(cfg RevisarConfig) Revisar {
	if cfg.GenID == nil {
		cfg.GenID = store.UUIDv7
	}
	if cfg.EmitV == nil {
		emit := cfg.Emit
		cfg.EmitV = func(eventType string, _ int, payload map[string]any) error {
			return emit(eventType, payload)
		}
	}
	input := textinput.New()
	input.Placeholder = "vazio pula"
	input.Prompt = " > "
	input.Focus()
	return Revisar{cfg: cfg, timer: newCountdown(cfg.Minutes), input: input, frame: frameFeynman}
}

func (m Revisar) Init() tea.Cmd {
	if err := m.cfg.Emit("session.started", map[string]any{
		"session_id":      m.cfg.SessionID,
		"kind":            "review",
		"planned_minutes": m.cfg.Minutes,
	}); err != nil {
		return func() tea.Msg { return errMsg{err} }
	}
	return tea.Batch(textinput.Blink, tick())
}

func (m Revisar) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case errMsg:
		m.Err = msg.err
		return m, tea.Quit

	case tickMsg:
		if m.phase == reasonPhase {
			return m, tick() // o relógio não apressa a resposta
		}
		if !m.timer.tick() {
			return m, tick()
		}
		if m.phase == reviewPause {
			if err := m.endPause(); err != nil {
				m.Err = err
			}
			return m, tea.Quit // fim da pausa
		}
		// o timer tocou no meio da revisão: a sessão vai até aqui (RF11)
		next, err := m.complete()
		if err != nil {
			next.Err = err
			return next, tea.Quit
		}
		return next, tick()

	case tea.KeyMsg:
		switch m.phase {
		case reviewPause:
			if s := msg.String(); s == "q" || msg.Type == tea.KeyEnter || msg.Type == tea.KeyEsc || msg.Type == tea.KeyCtrlC {
				if err := m.endPause(); err != nil {
					m.Err = err
				}
				return m, tea.Quit
			}
			return m, nil
		case askPhase:
			return m.updateAsking(msg)
		case revealPhase:
			return m.updateRevealed(msg)
		case reasonPhase:
			return m.updateReason(msg)
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Revisar) updateAsking(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		return m.interrupt()

	case tea.KeyTab:
		// alterna o andaime; recomeça a coleta para o método novo
		if m.frame == frameFeynman {
			m.frame = frame4Causas
		} else {
			m.frame = frameFeynman
		}
		m.causeIdx = 0
		m.causes = [4]string{}
		m.input.Reset()
		return m, nil

	case tea.KeyEnter:
		if m.frame == frame4Causas {
			return m.advanceCausa()
		}
		return m.finishExplain(strings.TrimSpace(m.input.Value()))
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// advanceCausa grava a causa corrente e avança; ao passar da última, concatena
// e emite. Enter em branco pula a causa; todas em branco = nenhuma explicação.
func (m Revisar) advanceCausa() (tea.Model, tea.Cmd) {
	m.causes[m.causeIdx] = strings.TrimSpace(m.input.Value())
	m.input.Reset()
	m.causeIdx++
	if m.causeIdx < len(quatroCausas) {
		return m, nil
	}
	return m.finishExplain(joinCausas(m.causes))
}

// finishExplain emite card.explained (v2 com frame quando não for Feynman) e
// revela o verso. Texto vazio não vira evento (explicação pulada).
func (m Revisar) finishExplain(text string) (tea.Model, tea.Cmd) {
	if text != "" {
		payload := map[string]any{
			"card_id":    m.card().ID,
			"text":       text,
			"session_id": m.cfg.SessionID,
		}
		var err error
		if m.frame == frameFeynman {
			err = m.cfg.Emit("card.explained", payload) // v1: caminho inalterado
		} else {
			payload["frame"] = m.frame
			err = m.cfg.EmitV("card.explained", 2, payload)
		}
		if err != nil {
			m.Err = err
			return m, tea.Quit
		}
	}
	m.phase = revealPhase
	return m, nil
}

// joinCausas monta "causa: resposta" por linha, ignorando as causas em branco.
func joinCausas(answers [4]string) string {
	var lines []string
	for i, a := range answers {
		if a != "" {
			lines = append(lines, quatroCausas[i].key+": "+a)
		}
	}
	return strings.Join(lines, "\n")
}

func (m Revisar) updateRevealed(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEsc, msg.Type == tea.KeyCtrlC:
		return m.interrupt()
	case msg.String() == "a":
		return m.review("correct")
	case msg.String() == "e":
		return m.review("wrong")
	}
	return m, nil
}

func (m Revisar) review(result string) (tea.Model, tea.Cmd) {
	if err := m.cfg.Emit("card.reviewed", map[string]any{
		"card_id":    m.card().ID,
		"result":     result,
		"session_id": m.cfg.SessionID,
	}); err != nil {
		m.Err = err
		return m, tea.Quit
	}
	if result == "correct" {
		m.Correct++
	} else {
		m.Wrong++
	}
	m.idx++
	m.input.Reset()
	m.causeIdx = 0 // o frame persiste na sessão; o wizard reinicia por cartão
	m.causes = [4]string{}
	if m.idx >= len(m.cfg.Cards) {
		next, err := m.complete()
		if err != nil {
			next.Err = err
			return next, tea.Quit
		}
		return next, nil // o encadeamento de ticks segue vivo
	}
	m.phase = askPhase
	return m, nil
}

// interrupt abre a linha opcional de motivo (Esc de novo pula) — a
// interrupção vira dado revisável (pnn semana), não fracasso.
func (m Revisar) interrupt() (tea.Model, tea.Cmd) {
	m.phase = reasonPhase
	m.input.Reset()
	m.input.Prompt = " Motivo? "
	m.input.Placeholder = "Enter pula"
	return m, nil
}

// updateReason coleta o motivo opcional da interrupção e encerra a sessão.
func (m Revisar) updateReason(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		if err := m.emitEnded("interrupted", strings.TrimSpace(m.input.Value())); err != nil {
			m.Err = err
		}
		return m, tea.Quit // interrompida pula a pausa
	case tea.KeyEsc, tea.KeyCtrlC:
		if err := m.emitEnded("interrupted", ""); err != nil {
			m.Err = err
		}
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Revisar) emitEnded(outcome, reason string) error {
	payload := map[string]any{"session_id": m.cfg.SessionID, "outcome": outcome}
	if reason != "" {
		payload["reason"] = reason
	}
	return m.cfg.EmitV("session.ended", 2, payload)
}

// A pausa verde é registrada (catálogo 1.2), como no foco.
func (m *Revisar) startPause() error {
	m.pauseID = m.cfg.GenID()
	return m.cfg.Emit("pause.started", map[string]any{
		"pause_id": m.pauseID, "planned_minutes": m.cfg.PauseMinutes,
	})
}

func (m *Revisar) endPause() error {
	if m.pauseID == "" {
		return nil
	}
	id := m.pauseID
	m.pauseID = ""
	return m.cfg.Emit("pause.ended", map[string]any{"pause_id": id})
}

// complete encerra a sessão e arma a pausa VERDE; quem chama decide se o
// encadeamento de ticks precisa ser (re)iniciado.
func (m Revisar) complete() (Revisar, error) {
	if err := m.emitEnded("completed", ""); err != nil {
		return m, err
	}
	if err := m.startPause(); err != nil {
		return m, err
	}
	m.phase = reviewPause
	m.timer = newCountdown(m.cfg.PauseMinutes)
	return m, nil
}

func (m Revisar) card() Card {
	return m.cfg.Cards[m.idx]
}

func (m Revisar) View() string {
	if m.Err != nil {
		return ""
	}
	if m.phase == reviewPause {
		done := m.Correct + m.Wrong
		summary := []string{fmt.Sprintf("%d cartão(ões) revisado(s) · %d acerto(s) · %d erro(s)", done, m.Correct, m.Wrong)}
		return pauseView("revisão concluída · 🐘 mandou bem!", m.timer, summary, "[q] sair")
	}
	if m.phase == reasonPhase {
		var b strings.Builder
		b.WriteString(redTitle.Render("● revisão interrompida"))
		b.WriteString("\n\n O que tirou você da sessão? " + dim.Render("(opcional)") + "\n")
		b.WriteString(m.input.View())
		b.WriteString("\n " + dim.Render("Enter registra · vazio pula") + "\n")
		return b.String()
	}

	card := m.card()
	var b strings.Builder
	header := fmt.Sprintf("● REVISÃO · cartão %d de %d · caixa %d", m.idx+1, len(m.cfg.Cards), card.Box)
	b.WriteString(redTitle.Render(header))
	b.WriteString("   " + redTimer.Render("▐█  "+m.timer.clock()+"  █▌"))
	b.WriteString("\n\n " + card.Front + "\n\n")

	if m.phase == askPhase {
		if m.frame == frame4Causas {
			b.WriteString(fmt.Sprintf(" Explique pelas 4 causas · passo %d de %d\n", m.causeIdx+1, len(quatroCausas)))
			for i := 0; i < m.causeIdx; i++ {
				if m.causes[i] != "" {
					b.WriteString(" " + dim.Render("✓ "+quatroCausas[i].key+": "+m.causes[i]) + "\n")
				} else {
					b.WriteString(" " + dim.Render("· "+quatroCausas[i].key+" (pulada)") + "\n")
				}
			}
			b.WriteString(" " + quatroCausas[m.causeIdx].prompt + "\n")
			b.WriteString(m.input.View())
			b.WriteString("\n " + dim.Render("Enter avança (vazio pula) · Tab volta ao Feynman · Esc encerra") + "\n")
			return b.String()
		}
		b.WriteString(" Explique com suas palavras (Feynman):\n")
		b.WriteString(m.input.View())
		b.WriteString("\n " + dim.Render("Enter revela · Tab usa as 4 causas · Esc encerra") + "\n")
		return b.String()
	}

	b.WriteString(" " + dim.Render(strings.Repeat("─", 40)) + "\n")
	b.WriteString(" " + card.Back + "\n")
	if card.SourceURL != nil {
		label := *card.SourceURL
		if card.SourceTitle != nil && *card.SourceTitle != "" {
			label = *card.SourceTitle
		}
		b.WriteString(" " + dim.Render("fonte: ") + hyperlink(*card.SourceURL, label) + "\n")
	}
	if card.LastExplanation != nil && *card.LastExplanation != "" {
		label := "Sua explicação anterior"
		if card.LastFrame == frame4Causas {
			label += " (4 causas)"
		}
		b.WriteString("\n " + dim.Render(label+": "+*card.LastExplanation) + "\n")
	}
	b.WriteString("\n " + dim.Render("[a] acertei · [e] errei · Esc encerra") + "\n")
	return b.String()
}
