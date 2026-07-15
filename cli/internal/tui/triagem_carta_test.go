// Triagem e criação de cartão são modelos AZUL (organizar) testados como os
// demais: mensagens entram, eventos do catálogo saem.
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func find(events []recorded, eventType string) (recorded, bool) {
	for _, e := range events {
		if e.eventType == eventType {
			return e, true
		}
	}
	return recorded{}, false
}

// --- carta ---

func newTestCarta(events *[]recorded, emit func(string, map[string]any) error, preset bool) Carta {
	n := 0
	cfg := CartaConfig{
		Decks: []DeckOption{{ID: "d1", Name: "Cálculo::Limites"}, {ID: "d2", Name: "História"}},
		GenID: func() string { n++; return "id-" + string(rune('0'+n)) },
		Emit:  emit,
	}
	if preset {
		cfg.DeckID, cfg.DeckName = "d1", "Cálculo::Limites"
	}
	m := NewCarta(cfg)
	m.Init()
	return m
}

func TestCartaCriaCartaoComFonte(t *testing.T) {
	events, emit := recorder()
	m := newTestCarta(events, emit, true)

	steps := []tea.Msg{
		keyRunes("O que é limite?"), tea.KeyMsg{Type: tea.KeyEnter},
		keyRunes("O valor de convergência."), tea.KeyMsg{Type: tea.KeyEnter},
		keyRunes("https://exemplo.dev"), tea.KeyMsg{Type: tea.KeyEnter},
	}
	for _, msg := range steps {
		next, _ := m.Update(msg)
		m = next.(Carta)
	}

	created, ok := find(*events, "card.created")
	if !ok {
		t.Fatalf("formulário completo deveria emitir card.created: %v", *events)
	}
	p := created.payload
	if p["deck_id"] != "d1" || p["front"] != "O que é limite?" || p["back"] != "O valor de convergência." {
		t.Fatalf("payload errado: %v", p)
	}
	if p["source_url"] != "https://exemplo.dev" {
		t.Fatalf("fonte preenchida deveria virar source_url: %v", p)
	}
	if m.Created != 1 {
		t.Fatalf("contador de criados deveria ser 1, veio %d", m.Created)
	}
	if !strings.Contains(m.View(), "cartão criado") {
		t.Fatalf("depois de criar, o formulário reinicia para o próximo:\n%s", m.View())
	}
}

func TestCartaFonteVaziaNaoViraCampo(t *testing.T) {
	events, emit := recorder()
	m := newTestCarta(events, emit, true)

	steps := []tea.Msg{
		keyRunes("frente"), tea.KeyMsg{Type: tea.KeyEnter},
		keyRunes("verso"), tea.KeyMsg{Type: tea.KeyEnter},
		tea.KeyMsg{Type: tea.KeyEnter}, // fonte vazia = sem fonte
	}
	for _, msg := range steps {
		next, _ := m.Update(msg)
		m = next.(Carta)
	}

	created, _ := find(*events, "card.created")
	if _, has := created.payload["source_url"]; has {
		t.Fatalf("fonte vazia não deveria entrar no payload: %v", created.payload)
	}
}

func TestCartaFrenteObrigatoria(t *testing.T) {
	events, emit := recorder()
	m := newTestCarta(events, emit, true)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // frente vazia
	m = next.(Carta)

	if len(*events) != 0 {
		t.Fatalf("frente vazia não avança nem emite: %v", *events)
	}
	if !strings.Contains(m.View(), "Frente") {
		t.Fatalf("continua pedindo a frente:\n%s", m.View())
	}
}

func TestCartaEscolheDeckPeloPicker(t *testing.T) {
	events, emit := recorder()
	m := newTestCarta(events, emit, false)

	if !strings.Contains(m.View(), "Cálculo::Limites") || !strings.Contains(m.View(), "História") {
		t.Fatalf("picker deveria listar os decks:\n%s", m.View())
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Carta)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Carta)

	steps := []tea.Msg{
		keyRunes("f"), tea.KeyMsg{Type: tea.KeyEnter},
		keyRunes("v"), tea.KeyMsg{Type: tea.KeyEnter},
		tea.KeyMsg{Type: tea.KeyEnter},
	}
	for _, msg := range steps {
		next, _ := m.Update(msg)
		m = next.(Carta)
	}

	created, _ := find(*events, "card.created")
	if created.payload["deck_id"] != "d2" {
		t.Fatalf("seta para baixo + Enter deveria escolher o segundo deck: %v", created.payload)
	}
}

func TestCartaEscSai(t *testing.T) {
	events, emit := recorder()
	m := newTestCarta(events, emit, true)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !isQuit(t, cmd) {
		t.Fatal("Esc deveria sair")
	}
	if len(*events) != 0 {
		t.Fatalf("sair não emite nada: %v", *events)
	}
}

// --- triagem ---

func pendingItems() []TriageItem {
	url := "https://artigo.dev"
	title := "Artigo"
	return []TriageItem{
		{Kind: "note", ID: "n1", Text: "revisitar heurísticas de Nielsen", URL: &url, PageTitle: &title},
		{Kind: "distraction", ID: "x1", Text: "responder e-mail do orientador"},
	}
}

func newTestTriagem(events *[]recorded, emit func(string, map[string]any) error) Triagem {
	n := 0
	m := NewTriagem(TriagemConfig{
		Items: pendingItems(),
		Decks: []DeckOption{{ID: "d1", Name: "IHC"}},
		GenID: func() string { n++; return "gen-" + string(rune('0'+n)) },
		Emit:  emit,
	})
	m.Init()
	return m
}

func TestTriagemNotaViraTarefa(t *testing.T) {
	events, emit := recorder()
	m := newTestTriagem(events, emit)

	next, _ := m.Update(keyRunes("t"))
	m = next.(Triagem)

	task, ok := find(*events, "task.created")
	if !ok || task.payload["title"] != "revisitar heurísticas de Nielsen" {
		t.Fatalf("'t' deveria criar a tarefa com o texto da nota: %v", *events)
	}
	triaged, ok := find(*events, "note.triaged")
	if !ok || triaged.payload["action"] != "to_task" || triaged.payload["task_id"] != task.payload["task_id"] {
		t.Fatalf("note.triaged deveria apontar a tarefa criada: %v", *events)
	}
	if !strings.Contains(m.View(), "responder e-mail") {
		t.Fatalf("depois de triar vem o próximo item:\n%s", m.View())
	}
}

func TestTriagemNotaViraCartaoComFonteHerdada(t *testing.T) {
	events, emit := recorder()
	m := newTestTriagem(events, emit)

	next, _ := m.Update(keyRunes("c"))
	m = next.(Triagem)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // único deck
	m = next.(Triagem)

	if got := m.form.inputs[0].Value(); got != "revisitar heurísticas de Nielsen" {
		t.Fatalf("frente deveria vir preenchida com o texto da nota, veio %q", got)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // aceita a frente
	m = next.(Triagem)
	next, _ = m.Update(keyRunes("dez heurísticas de usabilidade"))
	m = next.(Triagem)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Triagem)

	created, ok := find(*events, "card.created")
	if !ok {
		t.Fatalf("deveria criar o cartão: %v", *events)
	}
	if created.payload["source_url"] != "https://artigo.dev" || created.payload["source_title"] != "Artigo" {
		t.Fatalf("cartão herda a fonte da nota: %v", created.payload)
	}
	triaged, ok := find(*events, "note.triaged")
	if !ok || triaged.payload["action"] != "to_card" || triaged.payload["card_id"] != created.payload["card_id"] {
		t.Fatalf("note.triaged deveria apontar o cartão: %v", *events)
	}
}

func TestTriagemDescartaESegue(t *testing.T) {
	events, emit := recorder()
	m := newTestTriagem(events, emit)

	next, _ := m.Update(keyRunes("d"))
	m = next.(Triagem)

	triaged, ok := find(*events, "note.triaged")
	if !ok || triaged.payload["action"] != "discarded" {
		t.Fatalf("'d' deveria descartar: %v", *events)
	}

	before := len(*events)
	next, cmd := m.Update(keyRunes("s")) // adia a distração, fila acaba
	m = next.(Triagem)
	if len(*events) != before {
		t.Fatalf("'s' não emite evento: %v", *events)
	}
	if !isQuit(t, cmd) {
		t.Fatal("fila esgotada deveria sair")
	}
	if m.Skipped != 1 {
		t.Fatalf("contador de adiados deveria ser 1, veio %d", m.Skipped)
	}
}

func TestTriagemDistracaoFeita(t *testing.T) {
	events, emit := recorder()
	m := newTestTriagem(events, emit)

	next, _ := m.Update(keyRunes("s")) // pula a nota
	m = next.(Triagem)
	next, cmd := m.Update(keyRunes("f"))
	m = next.(Triagem)

	triaged, ok := find(*events, "distraction.triaged")
	if !ok || triaged.payload["action"] != "done" || triaged.payload["distraction_id"] != "x1" {
		t.Fatalf("'f' deveria resolver a distração: %v", *events)
	}
	if !isQuit(t, cmd) {
		t.Fatal("último item triado encerra a triagem")
	}
}

func TestTriagemDistracaoNaoOfereceCartao(t *testing.T) {
	events, emit := recorder()
	m := newTestTriagem(events, emit)

	next, _ := m.Update(keyRunes("s"))
	m = next.(Triagem)

	view := m.View()
	if strings.Contains(view, "[c]") {
		t.Fatalf("distração não vira cartão (ações: f/t/d/s):\n%s", view)
	}
	next, _ = m.Update(keyRunes("c"))
	m = next.(Triagem)
	if _, ok := find(*events, "card.created"); ok {
		t.Fatalf("'c' em distração deveria ser ignorado: %v", *events)
	}
}
