// Os modelos das sessões imersivas são funções puras de mensagens → estado,
// então as regras (spec/CLI.md §2) são testáveis sem terminal: aqui se prova
// que cada transição emite exatamente os eventos do catálogo.
package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// A suíte não acorda o desktop: os disparos viram espiões e o que fica
// registrado é só o par (título, corpo) de cada notificação.
type sentNotice struct{ title, body string }

var sent []sentNotice

func TestMain(m *testing.M) {
	spy := func(title, body string) {
		sent = append(sent, sentNotice{title, body})
	}
	notifySend, notifyAlarm = spy, spy
	os.Exit(m.Run())
}

type recorded struct {
	eventType string
	payload   map[string]any
}

func recorder() (*[]recorded, func(string, map[string]any) error) {
	var events []recorded
	return &events, func(eventType string, payload map[string]any) error {
		events = append(events, recorded{eventType, payload})
		return nil
	}
}

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func isQuit(t *testing.T, cmd tea.Cmd) bool {
	t.Helper()
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// --- foco ---

func newTestFoco(events *[]recorded, emit func(string, map[string]any) error) Foco {
	seq := 0
	m := NewFoco(FocoConfig{
		SessionID:    "s1",
		NewSessionID: func() string { seq++; return "s2" },
		TaskID:       "t1",
		TaskTitle:    "Escrever seção 4.6",
		Minutes:      25,
		PauseMinutes: 5,
		Emit:         emit,
	})
	m.Init()
	return m
}

func TestFocoIniciaSessaoDeTarefa(t *testing.T) {
	events, emit := recorder()
	m := newTestFoco(events, emit)

	if len(*events) != 1 || (*events)[0].eventType != "session.started" {
		t.Fatalf("Init deveria emitir session.started, veio %v", *events)
	}
	p := (*events)[0].payload
	if p["kind"] != "task" || p["target_id"] != "t1" || p["planned_minutes"] != 25 {
		t.Fatalf("payload errado: %v", p)
	}
	view := m.View()
	if !strings.Contains(view, "FOCO") || !strings.Contains(view, "Escrever seção 4.6") {
		t.Fatalf("tela vermelha deveria mostrar o alvo:\n%s", view)
	}
	if !strings.Contains(view, "25:00") {
		t.Fatalf("timer deveria começar em 25:00:\n%s", view)
	}
}

func TestFocoCapturaDistracaoSemSairDaSessao(t *testing.T) {
	events, emit := recorder()
	m := newTestFoco(events, emit)

	next, _ := m.Update(keyRunes("olhar twitter"))
	m = next.(Foco)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Foco)

	last := (*events)[len(*events)-1]
	if last.eventType != "distraction.captured" {
		t.Fatalf("Enter deveria gravar distraction.captured, veio %v", *events)
	}
	if last.payload["text"] != "olhar twitter" || last.payload["session_id"] != "s1" {
		t.Fatalf("payload errado: %v", last.payload)
	}
	if got := m.input.Value(); got != "" {
		t.Fatalf("input deveria limpar após gravar, veio %q", got)
	}
}

func TestFocoEscPedeMotivoEEncerraInterrompida(t *testing.T) {
	events, emit := recorder()
	m := newTestFoco(events, emit)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Foco)
	if isQuit(t, cmd) {
		t.Fatal("Esc abre a linha de motivo antes de encerrar")
	}
	if view := m.View(); !strings.Contains(view, "Motivo") {
		t.Fatalf("a tela deveria pedir o motivo opcional:\n%s", view)
	}

	next, _ = m.Update(keyRunes("celular"))
	m = next.(Foco)
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Foco)

	last := (*events)[len(*events)-1]
	if last.eventType != "session.ended" || last.payload["outcome"] != "interrupted" {
		t.Fatalf("Enter no motivo deveria encerrar como interrupted, veio %v", *events)
	}
	if last.payload["reason"] != "celular" {
		t.Fatalf("motivo deveria ir no payload (v2), veio %v", last.payload)
	}
	if !isQuit(t, cmd) {
		t.Fatal("sessão interrompida volta direto ao azul (quit), sem pausa")
	}
}

func TestFocoEscDuasVezesPulaOMotivo(t *testing.T) {
	events, emit := recorder()
	m := newTestFoco(events, emit)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Foco)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Foco)

	last := (*events)[len(*events)-1]
	if last.eventType != "session.ended" || last.payload["outcome"] != "interrupted" {
		t.Fatalf("Esc de novo encerra sem motivo, veio %v", *events)
	}
	if _, has := last.payload["reason"]; has {
		t.Fatalf("sem texto não há reason no payload: %v", last.payload)
	}
	if !isQuit(t, cmd) {
		t.Fatal("deveria sair após pular o motivo")
	}
}

func TestFocoTimerCompletaEEntraNaPausaVerde(t *testing.T) {
	events, emit := recorder()
	m := newTestFoco(events, emit)
	m.timer.remaining = 1

	next, cmd := m.Update(tickMsg{})
	m = next.(Foco)

	last := (*events)[len(*events)-1]
	if last.eventType != "pause.started" {
		t.Fatalf("a pausa verde agora é registrada (catálogo 1.2), veio %v", *events)
	}
	ended := (*events)[len(*events)-2]
	if ended.eventType != "session.ended" || ended.payload["outcome"] != "completed" {
		t.Fatalf("timer no zero deveria encerrar como completed, veio %v", *events)
	}
	if isQuit(t, cmd) {
		t.Fatal("sessão concluída não sai: entra na pausa verde")
	}
	view := m.View()
	if !strings.Contains(view, "pausa") || !strings.Contains(view, "05:00") {
		t.Fatalf("tela verde deveria mostrar a pausa de 5 min:\n%s", view)
	}
}

func TestFocoEnterNaPausaComecaNovoFoco(t *testing.T) {
	events, emit := recorder()
	m := newTestFoco(events, emit)
	m.timer.remaining = 1
	next, _ := m.Update(tickMsg{})
	m = next.(Foco)

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Foco)

	last := (*events)[len(*events)-1]
	if last.eventType != "session.started" || last.payload["session_id"] != "s2" {
		t.Fatalf("Enter na pausa deveria abrir sessão nova (s2), veio %v", *events)
	}
	prev := (*events)[len(*events)-2]
	if prev.eventType != "pause.ended" {
		t.Fatalf("sair da pausa verde deveria fechá-la (pause.ended), veio %v", *events)
	}
	if !strings.Contains(m.View(), "25:00") {
		t.Fatalf("novo foco recomeça o timer:\n%s", m.View())
	}
}

func TestFocoCheckinPerguntaEGravaResposta(t *testing.T) {
	events, emit := recorder()
	var versions []int
	m := NewFoco(FocoConfig{
		SessionID:    "s1",
		GenID:        func() string { return "k1" },
		TaskID:       "t1",
		TaskTitle:    "Escrever seção 4.6",
		Minutes:      25,
		PauseMinutes: 5,
		CheckinEvery: 1,
		Emit:         emit,
		EmitV: func(eventType string, v int, payload map[string]any) error {
			versions = append(versions, v)
			return emit(eventType, payload)
		},
	})
	m.Init()

	first := (*events)[0]
	if first.eventType != "session.started" || first.payload["checkin_every"] != 1 {
		t.Fatalf("com check-in, session.started leva checkin_every: %v", first)
	}
	if len(versions) != 1 || versions[0] != 2 {
		t.Fatalf("session.started com check-in é v2, veio %v", versions)
	}

	// 60 ticks = 1 min corrido → a pergunta toma a linha.
	var model tea.Model = m
	for i := 0; i < 60; i++ {
		model, _ = model.(Foco).Update(tickMsg{})
	}
	m = model.(Foco)
	if view := m.View(); !strings.Contains(view, "Você está na tarefa") {
		t.Fatalf("após 1 min deveria perguntar o check-in:\n%s", view)
	}

	next, _ := m.Update(keyRunes("s"))
	m = next.(Foco)
	last := (*events)[len(*events)-1]
	if last.eventType != "checkin.logged" || last.payload["on_task"] != true || last.payload["session_id"] != "s1" {
		t.Fatalf("[s] deveria gravar checkin.logged on_task=true, veio %v", last)
	}
	if strings.Contains(m.View(), "Você está na tarefa") {
		t.Fatalf("após responder, a pergunta sai da tela:\n%s", m.View())
	}
}

func TestFocoCheckinEscPulaSemGravar(t *testing.T) {
	events, emit := recorder()
	m := newTestFoco(events, emit)
	m.cfg.CheckinEvery = 1

	var model tea.Model = m
	for i := 0; i < 60; i++ {
		model, _ = model.(Foco).Update(tickMsg{})
	}
	m = model.(Foco)
	before := len(*events)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Foco)
	if len(*events) != before {
		t.Fatalf("Esc no check-in não grava evento, veio %v", (*events)[before:])
	}
	if strings.Contains(m.View(), "Você está na tarefa") {
		t.Fatalf("Esc dispensa a pergunta:\n%s", m.View())
	}
}

func TestFocoCtrlPBreakEncerraSessaoEEntraNaPausaEscolhida(t *testing.T) {
	events, emit := recorder()
	m := newTestFoco(events, emit)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = next.(Foco)
	if view := m.View(); !strings.Contains(view, "Pausa de quantos minutos") {
		t.Fatalf("Ctrl+P deveria abrir a escolha da duração:\n%s", view)
	}

	next, cmd := m.Update(keyRunes("2")) // [2] = 10 min
	m = next.(Foco)
	if isQuit(t, cmd) {
		t.Fatal("break entra na tela verde, não sai do programa")
	}
	pauseEvt := (*events)[len(*events)-1]
	endedEvt := (*events)[len(*events)-2]
	if endedEvt.eventType != "session.ended" || endedEvt.payload["outcome"] != "interrupted" || endedEvt.payload["reason"] != "pausa" {
		t.Fatalf("break encerra a sessão como interrupted/pausa, veio %v", endedEvt)
	}
	if pauseEvt.eventType != "pause.started" || pauseEvt.payload["planned_minutes"] != 10 {
		t.Fatalf("pause.started deveria planejar 10 min, veio %v", pauseEvt)
	}
	if view := m.View(); !strings.Contains(view, "10:00") || !strings.Contains(view, "pausa de 10 min") {
		t.Fatalf("tela verde da pausa de 10 min:\n%s", view)
	}
}

func TestFocoCtrlPEscDesisteDoBreak(t *testing.T) {
	events, emit := recorder()
	m := newTestFoco(events, emit)
	before := len(*events)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = next.(Foco)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Foco)

	if len(*events) != before {
		t.Fatalf("desistir do break não grava nada, veio %v", (*events)[before:])
	}
	if view := m.View(); !strings.Contains(view, "FOCO") || strings.Contains(view, "Pausa de quantos minutos") {
		t.Fatalf("Esc volta à tela de foco:\n%s", view)
	}
}

// --- revisar ---

func twoCards() []Card {
	source := "https://exemplo.dev"
	previous := "explicação antiga"
	return []Card{
		{ID: "c1", Front: "frente um", Back: "verso um", Box: 2, SourceURL: &source, LastExplanation: &previous},
		{ID: "c2", Front: "frente dois", Back: "verso dois", Box: 1},
	}
}

func newTestRevisar(events *[]recorded, emit func(string, map[string]any) error) Revisar {
	m := NewRevisar(RevisarConfig{
		SessionID:    "r1",
		Cards:        twoCards(),
		Minutes:      25,
		PauseMinutes: 5,
		Emit:         emit,
	})
	m.Init()
	return m
}

func TestRevisarIniciaSessaoDeRevisao(t *testing.T) {
	events, emit := recorder()
	m := newTestRevisar(events, emit)

	if len(*events) != 1 || (*events)[0].eventType != "session.started" {
		t.Fatalf("Init deveria emitir session.started, veio %v", *events)
	}
	if (*events)[0].payload["kind"] != "review" {
		t.Fatalf("kind deveria ser review: %v", (*events)[0].payload)
	}
	view := m.View()
	if !strings.Contains(view, "frente um") || strings.Contains(view, "verso um") {
		t.Fatalf("antes de revelar só a frente aparece:\n%s", view)
	}
}

func TestRevisarExplicacaoFeynmanAntesDeRevelar(t *testing.T) {
	events, emit := recorder()
	m := newTestRevisar(events, emit)

	next, _ := m.Update(keyRunes("é sobre limites"))
	m = next.(Revisar)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Revisar)

	last := (*events)[len(*events)-1]
	if last.eventType != "card.explained" {
		t.Fatalf("revelar com texto deveria emitir card.explained, veio %v", *events)
	}
	if last.payload["card_id"] != "c1" || last.payload["text"] != "é sobre limites" {
		t.Fatalf("payload errado: %v", last.payload)
	}
	view := m.View()
	if !strings.Contains(view, "verso um") || !strings.Contains(view, "explicação antiga") {
		t.Fatalf("tela revelada mostra o verso e a explicação anterior:\n%s", view)
	}
}

func TestRevisar4CausasEmiteFrameConcatenado(t *testing.T) {
	events, emit := recorder()
	m := newTestRevisar(events, emit)

	// Tab troca o andaime para as 4 causas; a tela passa a guiar passo a passo.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(Revisar)
	if !strings.Contains(m.View(), "4 causas") || !strings.Contains(m.View(), "passo 1 de 4") {
		t.Fatalf("Tab deveria abrir o wizard das 4 causas:\n%s", m.View())
	}

	// Preenche material e formal, pula eficiente, preenche final.
	steps := []string{"luz e água", "reação química", "", "gerar glicose"}
	for _, s := range steps {
		if s != "" {
			next, _ = m.Update(keyRunes(s))
			m = next.(Revisar)
		}
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = next.(Revisar)
	}

	last := (*events)[len(*events)-1]
	if last.eventType != "card.explained" {
		t.Fatalf("as 4 causas deveriam emitir card.explained, veio %v", *events)
	}
	if last.payload["frame"] != "4causas" {
		t.Fatalf("frame deveria ser 4causas: %v", last.payload)
	}
	want := "material: luz e água\nformal: reação química\nfinal: gerar glicose"
	if last.payload["text"] != want {
		t.Fatalf("texto concatenado errado (causa pulada deve sumir):\n%q", last.payload["text"])
	}
	if last.payload["card_id"] != "c1" {
		t.Fatalf("card_id errado: %v", last.payload)
	}
	if !strings.Contains(m.View(), "verso um") {
		t.Fatalf("após a última causa o verso é revelado:\n%s", m.View())
	}
}

func TestRevisar4CausasTodasVaziasNaoEmite(t *testing.T) {
	events, emit := recorder()
	m := newTestRevisar(events, emit)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab}) // vira 4 causas
	m = next.(Revisar)
	for i := 0; i < 4; i++ { // Enter em branco em todas
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = next.(Revisar)
	}

	for _, e := range *events {
		if e.eventType == "card.explained" {
			t.Fatalf("nenhuma causa preenchida não vira evento: %v", *events)
		}
	}
	if !strings.Contains(m.View(), "verso um") {
		t.Fatal("mesmo sem explicação, o verso é revelado")
	}
}

func TestRevisarPularExplicacaoNaoEmiteEvento(t *testing.T) {
	events, emit := recorder()
	m := newTestRevisar(events, emit)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // vazio = pular
	m = next.(Revisar)

	for _, e := range *events {
		if e.eventType == "card.explained" {
			t.Fatalf("explicação vazia não vira evento: %v", *events)
		}
	}
	if !strings.Contains(m.View(), "verso um") {
		t.Fatal("mesmo pulando a explicação, o verso é revelado")
	}
}

func TestRevisarAcertoAvancaParaOProximo(t *testing.T) {
	events, emit := recorder()
	m := newTestRevisar(events, emit)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Revisar)
	next, _ = m.Update(keyRunes("a"))
	m = next.(Revisar)

	last := (*events)[len(*events)-1]
	if last.eventType != "card.reviewed" || last.payload["result"] != "correct" || last.payload["card_id"] != "c1" {
		t.Fatalf("'a' deveria emitir card.reviewed correct, veio %v", *events)
	}
	if !strings.Contains(m.View(), "frente dois") {
		t.Fatalf("depois do acerto vem o próximo da fila:\n%s", m.View())
	}
}

func TestRevisarFilaEsgotadaConcluiSessao(t *testing.T) {
	events, emit := recorder()
	m := newTestRevisar(events, emit)

	for range twoCards() {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = next.(Revisar)
		next, _ = m.Update(keyRunes("e"))
		m = next.(Revisar)
	}

	last := (*events)[len(*events)-1]
	if last.eventType != "pause.started" {
		t.Fatalf("a pausa verde agora é registrada (catálogo 1.2), veio %v", *events)
	}
	ended := (*events)[len(*events)-2]
	if ended.eventType != "session.ended" || ended.payload["outcome"] != "completed" {
		t.Fatalf("fila vazia deveria concluir a sessão, veio %v", *events)
	}
	view := m.View()
	if !strings.Contains(view, "pausa") {
		t.Fatalf("sessão concluída entra na pausa verde:\n%s", view)
	}
}

func TestRevisarTimerTocaEConcluiMesmoComFila(t *testing.T) {
	events, emit := recorder()
	m := newTestRevisar(events, emit)
	m.timer.remaining = 1

	next, _ := m.Update(tickMsg{})
	m = next.(Revisar)

	ended := (*events)[len(*events)-2]
	if ended.eventType != "session.ended" || ended.payload["outcome"] != "completed" {
		t.Fatalf("a sessão vai até o timer tocar (RF11), veio %v", *events)
	}
}

func TestRevisarEscPedeMotivoEInterrompe(t *testing.T) {
	events, emit := recorder()
	m := newTestRevisar(events, emit)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Revisar)
	if isQuit(t, cmd) {
		t.Fatal("Esc abre a linha de motivo antes de encerrar")
	}

	next, _ = m.Update(keyRunes("cansaço"))
	m = next.(Revisar)
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = next

	last := (*events)[len(*events)-1]
	if last.eventType != "session.ended" || last.payload["outcome"] != "interrupted" {
		t.Fatalf("Enter no motivo deveria interromper, veio %v", *events)
	}
	if last.payload["reason"] != "cansaço" {
		t.Fatalf("motivo deveria ir no payload (v2), veio %v", last.payload)
	}
	if !isQuit(t, cmd) {
		t.Fatal("interrompida sai sem pausa")
	}
}
