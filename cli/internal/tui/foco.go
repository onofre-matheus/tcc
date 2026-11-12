// `pnn foco` — sessão de foco (spec/CLI.md §2/§4): tela VERMELHA com timer
// grande e a linha de distração sempre focada (adiamento de distrações, RF06);
// concluir leva à pausa VERDE, interromper (Esc) pula a pausa — o descanso
// cronometrado é recompensa de completar o bloco. Quando a pausa toca, a tela
// não fecha: um Enter devolve à mesma tarefa, para que cumprir a pausa não
// custe mais do que pulá-la. As páginas ligadas à tarefa (sinapses, catálogo
// 1.6) aparecem sob o relógio, e Ctrl+E abre uma delas no editor sem
// encerrar o bloco — tarefa não tem descrição; o contexto dela é a página.
package tui

import (
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"

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
	// Pages são as páginas ligadas à tarefa; EditPage abre uma delas no editor
	// do usuário e grava a edição, devolvendo a linha de status. O editor toma
	// o terminal (tea.Exec): a tela e os tiques param enquanto ele está
	// aberto, e o Update desconta o tempo ao voltar.
	Pages    []LinkedPage
	EditPage func(id string) (string, error)
	// Now é o relógio de parede (mede quanto o editor ficou aberto); Exec roda
	// o editor com o terminal liberado. Ausentes: time.Now e tea.Exec — os
	// testes trocam os dois.
	Now  func() time.Time
	Exec func(run func() error, done func(error) tea.Msg) tea.Cmd
	// Write liga o modo de escrita (escrita.go): a página aberta num editor
	// dentro da tela, com o relógio numa barra. StartWriting abre nele quando
	// a tarefa tem página ligada; senão, Ctrl+O entra.
	Write        WriteActions
	StartWriting bool
	// Standalone é o `pnn escrever`: só o editor, sem sessão de foco — sem
	// session.started, sem relógio, sem pausa; Esc sai.
	Standalone bool
}

// LinkedPage é uma página ligada à tarefa por sinapse (catálogo 1.6).
type LinkedPage struct {
	ID, Title string
}

// editState acompanha o editor aberto no meio do foco: quando abriu e o aviso
// de fim de bloco, que dispara pelo relógio de parede — a tela está parada.
type editState struct {
	start time.Time
	alarm *time.Timer
	fired atomic.Bool
}

type pageEditedMsg struct {
	status string
	err    error
}

// execFunc adapta uma função ao tea.ExecCommand: o editor usa o terminal
// direto (os.Stdin/os.Stdout), então os Set* não fazem nada.
type execFunc func() error

func (f execFunc) Run() error        { return f() }
func (execFunc) SetStdin(io.Reader)  {}
func (execFunc) SetStdout(io.Writer) {}
func (execFunc) SetStderr(io.Writer) {}

type focoPhase int

const (
	focoRunning focoPhase = iota
	focoPaused
	focoReason  // Esc: uma linha opcional de motivo antes de encerrar
	focoCheckin // check-in de atenção: "na tarefa? [s/n]"
	focoBreak   // Ctrl+P: escolher a duração da pausa (encerra a sessão)
	// focoPauseDone: a pausa tocou e já está gravada; a tela espera a escolha
	// — Enter volta à tarefa, q sai. O relógio fica parado até lá.
	focoPauseDone
	focoPickPage // Ctrl+E com mais de uma página ligada: qual abrir
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
	width        int // largura do terminal (0 = ainda desconhecida)
	elapsedSec   int // segundos corridos da sessão (agenda os check-ins)
	input        textinput.Model
	Distractions int
	Checkins     int // respondidos nesta sessão
	CheckinsOn   int // respondidos com "na tarefa"
	WantTriage   bool
	Err          error
	status       string      // resultado da última edição ou comando
	editing      *editState  // $EDITOR aberto (Ctrl+E); nil fora dele
	write        *writeState // página carregada no modo de escrita
	writing      bool        // a tela está no modo de escrita
	palette      *palette    // comandos "/" abertos (na escrita ou no relógio grande)
	pickWrite    bool        // o seletor de página é para escrever, não para o $EDITOR
	height       int
}

func NewFoco(cfg FocoConfig) Foco {
	if cfg.GenID == nil {
		cfg.GenID = store.UUIDv7
	}
	if cfg.NewSessionID == nil {
		cfg.NewSessionID = store.UUIDv7
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Exec == nil {
		cfg.Exec = func(run func() error, done func(error) tea.Msg) tea.Cmd {
			return tea.Exec(execFunc(run), done)
		}
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
	if cfg.Write != nil {
		input.Placeholder = "anote e volte ao trabalho — / abre os comandos"
	}
	m := Foco{
		cfg:       cfg,
		sessionID: cfg.SessionID,
		timer:     newCountdown(cfg.Minutes),
		input:     input,
	}
	if cfg.Write != nil && cfg.StartWriting {
		switch len(cfg.Pages) {
		case 0:
		case 1:
			m.enterWrite(cfg.Pages[0])
		default:
			m.phase, m.pickWrite = focoPickPage, true
		}
	}
	return m
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
	if m.cfg.Standalone {
		return textinput.Blink
	}
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

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizeEditor()
		return m, nil

	case tickMsg:
		if m.phase == focoReason {
			return m, tick() // o relógio não apressa a resposta
		}
		if m.phase == focoPauseDone {
			return m, nil // nada corre enquanto a escolha não vem
		}
		if m.phase != focoPaused {
			m.elapsedSec++
		}
		if m.writing && m.dirty() {
			if m.write.unsaved++; m.write.unsaved >= autosaveEvery {
				m.saveWrite(true)
			}
		}
		if !m.timer.tick() {
			// A cada checkin_every minutos corridos, a pergunta toma a linha
			// (e sai também como notificação); check-in não respondido
			// simplesmente dá lugar ao próximo — nenhum evento é gravado.
			if m.phase == focoRunning && m.cfg.CheckinEvery > 0 &&
				m.elapsedSec%(m.cfg.CheckinEvery*60) == 0 {
				m.phase = focoCheckin
				notifySend("Check-in de atenção", m.checkinQuestion())
			}
			return m, tick()
		}
		if m.phase == focoPaused {
			// A pausa cumprida é gravada no instante em que toca (duração
			// cheia). A tela não fecha: antes, quem cumpria a pausa tinha de
			// redigitar `pnn foco N`, e quem pulava com Enter seguia na tarefa —
			// o incentivo era pular a pausa.
			if err := m.endPause(); err != nil {
				m.Err = err
				return m, tea.Quit
			}
			notifyAlarm("Fim da pausa", "descanso encerrado — de volta ao foco")
			m.phase = focoPauseDone
			return m, nil // o encadeamento de ticks para; Enter o religa
		}
		// Com o editor aberto, o aviso já saiu pelo relógio de parede.
		notify := m.editing == nil || !m.editing.fired.Load()
		if err := m.completeBlock(notify); err != nil {
			m.Err = err
			return m, tea.Quit
		}
		return m, tick()

	case pageEditedMsg:
		return m.afterEdit(msg)

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
		case focoPauseDone:
			return m.updatePauseDone(msg)
		case focoPickPage:
			return m.updatePickPage(msg)
		}
		if m.writing {
			return m.updateWriting(msg)
		}
		if m.palette != nil {
			return m.updatePalette(msg)
		}
		return m.updateRunning(msg)
	}

	// Demais mensagens (o piscar dos cursores): para todos os campos.
	var cmd tea.Cmd
	cmds := make([]tea.Cmd, 0, 3)
	m.input, cmd = m.input.Update(msg)
	cmds = append(cmds, cmd)
	if m.write != nil {
		m.write.editor, cmd = m.write.editor.Update(msg)
		cmds = append(cmds, cmd)
	}
	if m.palette != nil {
		m.palette.input, cmd = m.palette.input.Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
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

	case tea.KeyCtrlE:
		// Abre a página ligada à tarefa no editor, sem encerrar o bloco. Sem
		// página ligada, o Ctrl+E segue para a linha (fim da linha).
		switch len(m.cfg.Pages) {
		case 0:
		case 1:
			return m.openPage(m.cfg.Pages[0])
		default:
			m.phase, m.pickWrite = focoPickPage, false
			return m, nil
		}

	case tea.KeyCtrlO:
		// Escreve na página dentro da própria tela (modo de escrita).
		if m.cfg.Write == nil {
			return m, nil
		}
		switch len(m.cfg.Pages) {
		case 0: // sem página: os comandos já com /pagina, um Enter cria e escreve
			m.openPalette(strings.TrimSpace("pagina " + m.cfg.TaskTitle))
			return m, nil
		case 1:
			if m.write != nil && m.write.page.ID == m.cfg.Pages[0].ID {
				m.writing = true
				m.resizeEditor()
				return m, m.write.editor.Focus()
			}
			return m, m.enterWrite(m.cfg.Pages[0])
		default:
			m.phase, m.pickWrite = focoPickPage, true
			return m, nil
		}

	case tea.KeyRunes:
		// "/" como primeiro caractere da linha abre os comandos (se há acervo).
		if m.cfg.Write != nil && !msg.Paste && string(msg.Runes) == "/" && m.input.Value() == "" {
			m.openPalette("")
			return m, nil
		}

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
	case msg.Type == tea.KeyEnter: // novo foco, mesma tarefa
		if err := m.nextBlock(); err != nil {
			m.Err = err
			return m, tea.Quit
		}
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

// updatePauseDone: a pausa já tocou e está gravada. Enter abre o próximo
// bloco na mesma tarefa; [t] vai à triagem; q/Esc/Ctrl+C saem sem gravar nada.
func (m Foco) updatePauseDone(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEnter:
		if err := m.nextBlock(); err != nil {
			m.Err = err
			return m, tea.Quit
		}
		return m, tick() // o relógio parou no fim da pausa; religa
	case msg.String() == "t":
		m.WantTriage = true
		return m, tea.Quit
	case msg.String() == "q", msg.Type == tea.KeyEsc, msg.Type == tea.KeyCtrlC:
		return m, tea.Quit
	}
	return m, nil
}

// nextBlock fecha a pausa, se ainda aberta, e abre um bloco novo na mesma
// tarefa (session.started com o mesmo target_id).
func (m *Foco) nextBlock() error {
	if err := m.endPause(); err != nil {
		return err
	}
	m.sessionID = m.cfg.NewSessionID()
	if err := m.emitStarted(); err != nil {
		return err
	}
	m.phase = focoRunning
	m.timer = newCountdown(m.cfg.Minutes)
	m.elapsedSec = 0
	m.Distractions = 0
	m.Checkins, m.CheckinsOn = 0, 0
	m.input.Reset()
	return nil
}

// enterAction descreve o que o Enter faz na pausa: diz a tarefa e a duração
// do próximo bloco, para não restar dúvida de que ele volta ao mesmo ponto.
func (m Foco) enterAction() string {
	if m.cfg.TaskTitle == "" {
		return fmt.Sprintf("[Enter] novo foco (%d min)", m.cfg.Minutes)
	}
	return fmt.Sprintf("[Enter] voltar a \"%s\" (%d min)", m.cfg.TaskTitle, m.cfg.Minutes)
}

// sessionSummary é o que a sessão rendeu, repetido na pausa e no fim dela.
func (m Foco) sessionSummary() []string {
	var summary []string
	if m.Distractions > 0 {
		summary = append(summary, fmt.Sprintf("Nesta sessão: %d distração(ões) anotada(s) → triar depois", m.Distractions))
	}
	if m.Checkins > 0 {
		summary = append(summary, fmt.Sprintf("Check-ins: na tarefa em %d de %d", m.CheckinsOn, m.Checkins))
	}
	return summary
}

// completeBlock encerra o bloco como concluído e entra na pausa verde.
// notify = false quando o aviso já saiu (pelo relógio de parede, com o editor
// aberto).
func (m *Foco) completeBlock(notify bool) error {
	if m.writing {
		m.saveWrite(true) // o texto não espera a pausa
	}
	if err := m.emitEnded("completed", ""); err != nil {
		return err
	}
	if err := m.startPause(m.cfg.PauseMinutes); err != nil {
		return err
	}
	m.phase = focoPaused
	m.pauseHead = fmt.Sprintf("%d min de foco · 🐘 mandou bem!", m.cfg.Minutes)
	m.timer = newCountdown(m.cfg.PauseMinutes)
	if notify {
		notifyAlarm(
			fmt.Sprintf("%d min de foco concluídos 🐘", m.cfg.Minutes),
			fmt.Sprintf("pausa de %d min começou", m.cfg.PauseMinutes),
		)
	}
	return nil
}

// openPage entrega o terminal ao editor com a página. A tela fica parada até
// o editor fechar, então o aviso de fim de bloco é agendado pelo relógio de
// parede: quem está escrevendo no vim também ouve o despertador.
func (m Foco) openPage(p LinkedPage) (tea.Model, tea.Cmd) {
	if m.cfg.EditPage == nil {
		return m, nil
	}
	ed := &editState{start: m.cfg.Now()}
	if rem := time.Duration(m.timer.remaining) * time.Second; rem > 0 {
		minutes, pause := m.cfg.Minutes, m.cfg.PauseMinutes
		ed.alarm = time.AfterFunc(rem, func() {
			ed.fired.Store(true)
			notifyAlarm(
				fmt.Sprintf("%d min de foco concluídos 🐘", minutes),
				fmt.Sprintf("salve e volte ao pnn para a pausa de %d min", pause),
			)
		})
	}
	m.editing = ed
	m.status = ""
	edit := m.cfg.EditPage
	var status string
	return m, m.cfg.Exec(func() error {
		var err error
		status, err = edit(p.ID)
		return err
	}, func(err error) tea.Msg { return pageEditedMsg{status: status, err: err} })
}

// afterEdit volta do editor: mostra o resultado e desconta o tempo de parede
// que passou com a tela parada. Se o bloco acabou enquanto o editor estava
// aberto, ele se encerra agora — a pausa começa quando o usuário volta.
func (m Foco) afterEdit(msg pageEditedMsg) (tea.Model, tea.Cmd) {
	ed := m.editing
	m.editing = nil
	if ed == nil {
		return m, nil
	}
	if ed.alarm != nil {
		ed.alarm.Stop()
	}
	m.status = msg.status
	if msg.err != nil {
		m.status = "⚠ " + msg.err.Error()
	}
	if m.writing {
		m.reloadWrite() // o $EDITOR pode ter mudado a página
	}
	if m.cfg.Standalone {
		return m, nil // sem bloco: nada a descontar
	}
	if m.phase != focoRunning {
		return m, nil // um tique represado já encerrou o bloco
	}
	elapsed := int(m.cfg.Now().Sub(ed.start) / time.Second)
	m.elapsedSec += elapsed // check-ins do meio não são feitos
	if !m.timer.advance(elapsed) {
		return m, nil // o encadeamento de tiques segue vivo
	}
	if err := m.completeBlock(!ed.fired.Load()); err != nil {
		m.Err = err
		return m, tea.Quit
	}
	return m, nil // o tique represado segue a contagem, agora da pausa
}

// updatePickPage escolhe qual das páginas ligadas abrir ([1]..[9]); Esc volta.
func (m Foco) updatePickPage(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.phase, m.pickWrite = focoRunning, false
		return m, nil
	case tea.KeyCtrlC:
		if err := m.emitEnded("interrupted", ""); err != nil {
			m.Err = err
		}
		return m, tea.Quit
	}
	if key := msg.String(); len(key) == 1 && key >= "1" && key <= "9" {
		if i := int(key[0] - '1'); i < len(m.cfg.Pages) {
			m.phase = focoRunning
			if m.pickWrite {
				m.pickWrite = false
				return m, m.enterWrite(m.cfg.Pages[i])
			}
			return m.openPage(m.cfg.Pages[i])
		}
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

// checkinQuestion é a pergunta do self-monitoring — a mesma na tela e na
// notificação, para quem responde de volta no terminal reconhecê-la.
func (m Foco) checkinQuestion() string {
	if m.cfg.TaskTitle != "" {
		return fmt.Sprintf("Você está na tarefa %q?", m.cfg.TaskTitle)
	}
	return "Você está na tarefa?"
}

func (m Foco) View() string {
	if m.Err != nil {
		return ""
	}
	if m.phase == focoPaused {
		head := m.pauseHead
		if head == "" {
			head = fmt.Sprintf("%d min de foco · 🐘 mandou bem!", m.cfg.Minutes)
		}
		return pauseView(head, m.timer, m.sessionSummary(), m.enterAction()+" · [t] triagem · [q] sair", m.width)
	}
	if m.phase == focoPauseDone {
		var b strings.Builder
		b.WriteString(blueTitle.Render("⏰ fim da pausa · 🐘 bora de novo?"))
		b.WriteString("\n\n")
		for _, line := range m.sessionSummary() {
			b.WriteString(" " + line + "\n")
		}
		if len(m.sessionSummary()) > 0 {
			b.WriteString("\n")
		}
		b.WriteString(" " + blueText.Render(m.enterAction()) + dim.Render(" · [t] triagem · [q] sair") + "\n")
		return b.String()
	}
	if m.phase == focoReason {
		var b strings.Builder
		b.WriteString(redTitle.Render("● sessão interrompida"))
		b.WriteString("\n\n O que tirou você da sessão? " + dim.Render("(opcional)") + "\n")
		b.WriteString(m.input.View())
		b.WriteString("\n " + dim.Render("Enter registra · vazio pula") + "\n")
		return b.String()
	}

	if m.writing && m.write != nil && (m.phase == focoRunning || m.phase == focoCheckin) {
		return m.writeView()
	}

	title := "● FOCO"
	if m.cfg.TaskTitle != "" {
		title += " · " + m.cfg.TaskTitle
	}
	var b strings.Builder
	b.WriteString(redTitle.Render(title))
	b.WriteString("\n\n")
	b.WriteString(bigClock(m.timer, "FOCO", redTimer, m.width))
	b.WriteString("\n\n")
	if len(m.cfg.Pages) > 0 {
		titles := make([]string, len(m.cfg.Pages))
		for i, p := range m.cfg.Pages {
			titles[i] = "página " + p.Title
		}
		b.WriteString(" ⚡ " + strings.Join(titles, " · ") + "\n")
	}
	if m.status != "" {
		b.WriteString(" " + dim.Render(m.status) + "\n")
	}
	switch m.phase {
	case focoPickPage:
		if m.pickWrite {
			b.WriteString(" ✎ Em qual página escrever?\n")
		} else {
			b.WriteString(" ✎ Qual página editar no $EDITOR?\n")
		}
		var opts []string
		for i, p := range m.cfg.Pages {
			if i < 9 {
				opts = append(opts, fmt.Sprintf("[%d] %s", i+1, p.Title))
			}
		}
		b.WriteString(" " + dim.Render(strings.Join(opts, " · ")+" · Esc volta") + "\n")
	case focoCheckin:
		b.WriteString(" 🔔 " + m.checkinQuestion() + "\n")
		b.WriteString(" " + dim.Render("[s] sim, na tarefa · [n] não, distraí · Esc pula") + "\n")
	case focoBreak:
		b.WriteString(" ☕ Pausa de quantos minutos? " + dim.Render("(encerra a sessão)") + "\n")
		b.WriteString(" " + dim.Render("[1] 5 · [2] 10 · [3] 15 · [4] 20 · [5] 25 · Esc volta") + "\n")
	default:
		if m.palette != nil {
			for _, line := range m.paletteView() {
				b.WriteString(line + "\n")
			}
			break
		}
		b.WriteString(m.input.View())
		// Cabe em 80 colunas: o bubbletea corta o que passa da largura, e o
		// que vem no fim é justamente o que é novo para o usuário.
		help := "Enter grava e limpa · Esc encerra · Ctrl+P pausa"
		switch {
		case m.cfg.Write != nil:
			help = "Enter anota · / comandos · Ctrl+O escreve · Ctrl+P pausa · Esc encerra"
		case len(m.cfg.Pages) > 0:
			help += " · Ctrl+E $EDITOR"
		}
		b.WriteString("\n " + dim.Render(help) + "\n")
	}
	return b.String()
}

// Status é a última linha de status (gravação ou comando) — o `pnn escrever`
// a imprime ao sair.
func (m Foco) Status() string { return m.status }
