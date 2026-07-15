// `pnn foco` — sessão de foco (spec/CLI.md §2/§4): tela VERMELHA com timer
// grande e a linha de distração sempre focada (adiamento de distrações, RF06);
// concluir leva à pausa VERDE, interromper (Esc) pula a pausa — o descanso
// cronometrado é recompensa de completar o bloco.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/onofre-matheus/tcc/cli/store"
)

type FocoConfig struct {
	SessionID    string
	NewSessionID func() string // sessão nova a partir da pausa
	GenID        func() string // distraction_id / pause_id / checkin_id
	TaskID       string        // vazio = foco livre
	TaskTitle    string
	Minutes      int
	PauseMinutes int
	// CheckinEvery (minutos; 0 = sem) liga o check-in de atenção: a cada N
	// minutos a tela pergunta "na tarefa?" — self-monitoring (Safren). A
	// resposta [s/n] vira checkin.logged.
	CheckinEvery int
	Emit         func(eventType string, payload map[string]any) error
	// EmitV emite com versão de payload explícita (session.ended v2, com
	// reason?). Ausente, degrada para Emit — os testes antigos seguem válidos.
	EmitV func(eventType string, v int, payload map[string]any) error
}

type focoPhase int

const (
	focoRunning focoPhase = iota
	focoPaused
	focoReason  // Esc: uma linha opcional de motivo antes de encerrar
	focoCheckin // check-in de atenção: "na tarefa? [s/n]"
	focoBreak   // Ctrl+P: escolher a duração da pausa (encerra a sessão)
)

// Durações de pausa oferecidas no break ([1]..[5]).
var breakMinutes = [5]int{5, 10, 15, 20, 25}

type Foco struct {
	cfg          FocoConfig
	phase        focoPhase
	sessionID    string
	pauseID      string
	pauseHead    string // cabeçalho da tela verde (recompensa ≠ break)
	timer        countdown
	elapsedSec   int // segundos corridos da sessão (agenda os check-ins)
	input        textinput.Model
	Distractions int
	Checkins     int // respondidos nesta sessão
	CheckinsOn   int // respondidos com "na tarefa"
	WantTriage   bool
	Err          error
}

func NewFoco(cfg FocoConfig) Foco {
	if cfg.GenID == nil {
		cfg.GenID = store.UUIDv7
	}
	if cfg.NewSessionID == nil {
		cfg.NewSessionID = store.UUIDv7
	}
	if cfg.EmitV == nil {
		emit := cfg.Emit
		cfg.EmitV = func(eventType string, _ int, payload map[string]any) error {
			return emit(eventType, payload)
		}
	}
	input := textinput.New()
	input.Placeholder = "anote e volte ao trabalho"
	input.Prompt = " Distração? "
	input.Focus()
	return Foco{
		cfg:       cfg,
		sessionID: cfg.SessionID,
		timer:     newCountdown(cfg.Minutes),
		input:     input,
	}
}

func (m Foco) startPayload() map[string]any {
	payload := map[string]any{
		"session_id":      m.sessionID,
		"kind":            "task",
		"planned_minutes": m.cfg.Minutes,
	}
	if m.cfg.TaskID != "" {
		payload["target_id"] = m.cfg.TaskID
	}
	return payload
}

// emitStarted grava session.started — v2 quando há checkin_every (campo
// aberto do catálogo; sem check-in, o v1 segue byte a byte idêntico).
func (m Foco) emitStarted() error {
	if m.cfg.CheckinEvery > 0 {
		payload := m.startPayload()
		payload["checkin_every"] = m.cfg.CheckinEvery
		return m.cfg.EmitV("session.started", 2, payload)
	}
	return m.cfg.Emit("session.started", m.startPayload())
}

func (m Foco) Init() tea.Cmd {
	if err := m.emitStarted(); err != nil {
		return func() tea.Msg { return errMsg{err} }
	}
	return tea.Batch(textinput.Blink, tick())
}

func (m Foco) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case errMsg:
		m.Err = msg.err
		return m, tea.Quit

	case tickMsg:
		if m.phase == focoReason {
			return m, tick() // o relógio não apressa a resposta
		}
		if m.phase != focoPaused {
			m.elapsedSec++
		}
		if !m.timer.tick() {
			// A cada checkin_every minutos corridos, a pergunta toma a linha
			// (com sino); check-in não respondido simplesmente dá lugar ao
			// próximo — nenhum evento é gravado.
			if m.phase == focoRunning && m.cfg.CheckinEvery > 0 &&
				m.elapsedSec%(m.cfg.CheckinEvery*60) == 0 {
				m.phase = focoCheckin
				fmt.Print("\a") // sino do terminal
			}
			return m, tick()
		}
		if m.phase == focoPaused {
			if err := m.endPause(); err != nil {
				m.Err = err
			}
			return m, tea.Quit // fim da pausa → volta ao azul
		}
		if err := m.emitEnded("completed", ""); err != nil {
			m.Err = err
			return m, tea.Quit
		}
		if err := m.startPause(m.cfg.PauseMinutes); err != nil {
			m.Err = err
			return m, tea.Quit
		}
		m.phase = focoPaused
		m.pauseHead = fmt.Sprintf("%d min de foco · 🐘 mandou bem!", m.cfg.Minutes)
		m.timer = newCountdown(m.cfg.PauseMinutes)
		return m, tick()

	case tea.KeyMsg:
		switch m.phase {
		case focoPaused:
			return m.updatePaused(msg)
		case focoReason:
			return m.updateReason(msg)
		case focoCheckin:
			return m.updateCheckin(msg)
		case focoBreak:
			return m.updateBreak(msg)
		}
		return m.updateRunning(msg)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Foco) updateRunning(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		// Antes de encerrar, uma linha opcional: por quê? A interrupção
		// vira dado revisável (pnn semana), não fracasso — Safren.
		m.phase = focoReason
		m.input.Reset()
		m.input.Prompt = " Motivo? "
		m.input.Placeholder = "Enter pula"
		return m, nil

	case tea.KeyCtrlC:
		if err := m.emitEnded("interrupted", ""); err != nil {
			m.Err = err
		}
		return m, tea.Quit // interrompida pula a pausa

	case tea.KeyCtrlP:
		// Break: escolher a duração encerra a sessão (a tarefa "desmarca") e
		// entra na pausa cronometrada — pausa é estado próprio, não parêntese.
		m.phase = focoBreak
		return m, nil

	case tea.KeyEnter:
		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			return m, nil
		}
		if err := m.cfg.Emit("distraction.captured", map[string]any{
			"distraction_id": m.cfg.GenID(),
			"session_id":     m.sessionID,
			"text":           text,
		}); err != nil {
			m.Err = err
			return m, tea.Quit
		}
		m.Distractions++
		m.input.Reset()
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Foco) updatePaused(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEnter: // novo foco
		if err := m.endPause(); err != nil {
			m.Err = err
			return m, tea.Quit
		}
		m.sessionID = m.cfg.NewSessionID()
		if err := m.emitStarted(); err != nil {
			m.Err = err
			return m, tea.Quit
		}
		m.phase = focoRunning
		m.timer = newCountdown(m.cfg.Minutes)
		m.elapsedSec = 0
		m.Distractions = 0
		m.Checkins, m.CheckinsOn = 0, 0
		m.input.Reset()
		return m, nil // o encadeamento de ticks segue vivo

	case msg.String() == "t":
		if err := m.endPause(); err != nil {
			m.Err = err
		}
		m.WantTriage = true
		return m, tea.Quit

	case msg.String() == "q", msg.Type == tea.KeyEsc, msg.Type == tea.KeyCtrlC:
		if err := m.endPause(); err != nil {
			m.Err = err
		}
		return m, tea.Quit
	}
	return m, nil
}

// updateCheckin responde o check-in de atenção: [s]im / [n]ão → checkin.logged;
// Esc pula sem gravar. O timer da sessão segue correndo por baixo.
func (m Foco) updateCheckin(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	answer := func(onTask bool) (tea.Model, tea.Cmd) {
		if err := m.cfg.Emit("checkin.logged", map[string]any{
			"checkin_id": m.cfg.GenID(),
			"session_id": m.sessionID,
			"on_task":    onTask,
		}); err != nil {
			m.Err = err
			return m, tea.Quit
		}
		m.Checkins++
		if onTask {
			m.CheckinsOn++
		}
		m.phase = focoRunning
		return m, nil
	}
	switch {
	case msg.String() == "s":
		return answer(true)
	case msg.String() == "n":
		return answer(false)
	case msg.Type == tea.KeyEsc:
		m.phase = focoRunning // pulou: nenhum evento
		return m, nil
	case msg.Type == tea.KeyCtrlC:
		if err := m.emitEnded("interrupted", ""); err != nil {
			m.Err = err
		}
		return m, tea.Quit
	}
	return m, nil
}

// updateBreak escolhe a duração da pausa ([1]..[5] → 5/10/15/20/25 min),
// encerra a sessão (reason "pausa") e entra na tela verde.
func (m Foco) updateBreak(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEsc:
		m.phase = focoRunning // desistiu do break
		return m, nil
	case msg.Type == tea.KeyCtrlC:
		if err := m.emitEnded("interrupted", ""); err != nil {
			m.Err = err
		}
		return m, tea.Quit
	}
	key := msg.String()
	if key < "1" || key > "5" {
		return m, nil
	}
	minutes := breakMinutes[key[0]-'1']
	if err := m.emitEnded("interrupted", "pausa"); err != nil {
		m.Err = err
		return m, tea.Quit
	}
	if err := m.startPause(minutes); err != nil {
		m.Err = err
		return m, tea.Quit
	}
	m.phase = focoPaused
	m.pauseHead = fmt.Sprintf("☕ pausa de %d min", minutes)
	m.timer = newCountdown(minutes)
	return m, nil
}

// updateReason coleta o motivo opcional da interrupção e encerra a sessão.
func (m Foco) updateReason(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		if err := m.emitEnded("interrupted", strings.TrimSpace(m.input.Value())); err != nil {
			m.Err = err
		}
		return m, tea.Quit
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

func (m Foco) emitEnded(outcome, reason string) error {
	payload := map[string]any{"session_id": m.sessionID, "outcome": outcome}
	if reason != "" {
		payload["reason"] = reason
	}
	return m.cfg.EmitV("session.ended", 2, payload)
}

// A pausa verde é registrada (catálogo 1.2): Safren pede pausa com duração
// definida, e o registro alimenta a retrospectiva semanal.
func (m *Foco) startPause(minutes int) error {
	m.pauseID = m.cfg.GenID()
	return m.cfg.Emit("pause.started", map[string]any{
		"pause_id": m.pauseID, "planned_minutes": minutes,
	})
}

func (m *Foco) endPause() error {
	if m.pauseID == "" {
		return nil
	}
	id := m.pauseID
	m.pauseID = ""
	return m.cfg.Emit("pause.ended", map[string]any{"pause_id": id})
}

func (m Foco) View() string {
	if m.Err != nil {
		return ""
	}
	if m.phase == focoPaused {
		var summary []string
		if m.Distractions > 0 {
			summary = append(summary, fmt.Sprintf("Nesta sessão: %d distração(ões) anotada(s) → triar depois", m.Distractions))
		}
		if m.Checkins > 0 {
			summary = append(summary, fmt.Sprintf("Check-ins: na tarefa em %d de %d", m.CheckinsOn, m.Checkins))
		}
		head := m.pauseHead
		if head == "" {
			head = fmt.Sprintf("%d min de foco · 🐘 mandou bem!", m.cfg.Minutes)
		}
		return pauseView(head, m.timer, summary, "[Enter] novo foco · [t] triagem · [q] sair")
	}
	if m.phase == focoReason {
		var b strings.Builder
		b.WriteString(redTitle.Render("● sessão interrompida"))
		b.WriteString("\n\n O que tirou você da sessão? " + dim.Render("(opcional)") + "\n")
		b.WriteString(m.input.View())
		b.WriteString("\n " + dim.Render("Enter registra · vazio pula") + "\n")
		return b.String()
	}

	title := "● FOCO"
	if m.cfg.TaskTitle != "" {
		title += " · " + m.cfg.TaskTitle
	}
	var b strings.Builder
	b.WriteString(redTitle.Render(title))
	b.WriteString("\n\n")
	b.WriteString("        " + redTimer.Render("▐█  "+m.timer.clock()+"  █▌"))
	b.WriteString("\n\n")
	switch m.phase {
	case focoCheckin:
		question := "Você está na tarefa?"
		if m.cfg.TaskTitle != "" {
			question = fmt.Sprintf("Você está na tarefa %q?", m.cfg.TaskTitle)
		}
		b.WriteString(" 🔔 " + question + "\n")
		b.WriteString(" " + dim.Render("[s] sim, na tarefa · [n] não, distraí · Esc pula") + "\n")
	case focoBreak:
		b.WriteString(" ☕ Pausa de quantos minutos? " + dim.Render("(encerra a sessão)") + "\n")
		b.WriteString(" " + dim.Render("[1] 5 · [2] 10 · [3] 15 · [4] 20 · [5] 25 · Esc volta") + "\n")
	default:
		b.WriteString(m.input.View())
		b.WriteString("\n " + dim.Render("Enter grava e limpa · Esc encerra · Ctrl+P pausa") + "\n")
	}
	return b.String()
}
