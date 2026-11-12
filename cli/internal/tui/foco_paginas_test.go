// Páginas ligadas à tarefa na tela de foco (catálogo 1.6): aparecem sob o
// relógio, Ctrl+E abre uma no editor sem encerrar o bloco, e o tempo que a
// tela ficou parada com o editor aberto é descontado ao voltar. O editor e o
// relógio de parede são simulados: Exec roda a edição na hora, e a edição
// "dura" editFor.
package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type pageEditor struct {
	clock   time.Time
	editFor time.Duration
	opened  []string
	err     error
}

func newFocoComPaginas(events *[]recorded, emit func(string, map[string]any) error, ed *pageEditor, pages ...LinkedPage) Foco {
	m := NewFoco(FocoConfig{
		SessionID:    "s1",
		NewSessionID: func() string { return "s2" },
		TaskID:       "t1",
		TaskTitle:    "Escrever seção 4.6",
		Minutes:      25,
		PauseMinutes: 5,
		Emit:         emit,
		Pages:        pages,
		EditPage: func(id string) (string, error) {
			ed.opened = append(ed.opened, id)
			ed.clock = ed.clock.Add(ed.editFor)
			if ed.err != nil {
				return "", ed.err
			}
			return "✔ página salva: docs cli", nil
		},
		Now: func() time.Time { return ed.clock },
		Exec: func(run func() error, done func(error) tea.Msg) tea.Cmd {
			err := run()
			return func() tea.Msg { return done(err) }
		},
	})
	m.Init()
	return m
}

var docsCli = LinkedPage{ID: "p1", Title: "docs cli"}

func ctrlE(t *testing.T, m Foco) (Foco, tea.Msg) {
	t.Helper()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	if cmd == nil {
		t.Fatal("Ctrl+E com página ligada abre o editor")
	}
	return next.(Foco), cmd()
}

func TestFocoMostraPaginaLigadaEEditaComCtrlE(t *testing.T) {
	events, emit := recorder()
	ed := &pageEditor{clock: time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC), editFor: 3 * time.Minute}
	m := newFocoComPaginas(events, emit, ed, docsCli)

	if view := m.View(); !strings.Contains(view, "⚡ página docs cli") || !strings.Contains(view, "Ctrl+E $EDITOR") {
		t.Fatalf("a página ligada aparece sob o relógio, com a dica:\n%s", view)
	}

	m, msg := ctrlE(t, m)
	next, _ := m.Update(msg)
	m = next.(Foco)

	if len(ed.opened) != 1 || ed.opened[0] != "p1" {
		t.Fatalf("Ctrl+E abre a página ligada: %v", ed.opened)
	}
	view := m.View()
	if !strings.Contains(view, "✔ página salva: docs cli") {
		t.Fatalf("o resultado da edição aparece na tela:\n%s", view)
	}
	if !strings.Contains(view, "22:00") {
		t.Fatalf("os 3 min com o editor aberto são descontados do relógio:\n%s", view)
	}
	for _, e := range *events {
		if e.eventType == "session.ended" {
			t.Fatalf("editar a página não encerra o bloco: %v", *events)
		}
	}
}

func TestFocoBlocoAcabaComOEditorAberto(t *testing.T) {
	got := notices(t, func() {
		events, emit := recorder()
		ed := &pageEditor{clock: time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC), editFor: 30 * time.Minute}
		m := newFocoComPaginas(events, emit, ed, docsCli)

		m, msg := ctrlE(t, m)
		next, cmd := m.Update(msg)
		m = next.(Foco)

		ended := (*events)[len(*events)-2]
		if ended.eventType != "session.ended" || ended.payload["outcome"] != "completed" {
			t.Fatalf("o bloco que acabou no editor se encerra como concluído na volta: %v", *events)
		}
		if (*events)[len(*events)-1].eventType != "pause.started" || !strings.Contains(m.View(), "05:00") {
			t.Fatalf("a pausa começa quando o usuário volta:\n%s", m.View())
		}
		if cmd != nil {
			t.Fatal("o tique represado continua a contagem — sem iniciar outro encadeamento")
		}
	})
	if len(got) != 1 || !strings.Contains(got[0].title, "25 min") {
		t.Fatalf("um aviso de fim de bloco: %v", got)
	}
}

func TestFocoAvisoDoEditorNaoSeRepete(t *testing.T) {
	got := notices(t, func() {
		events, emit := recorder()
		ed := &pageEditor{clock: time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC), editFor: 30 * time.Minute}
		m := newFocoComPaginas(events, emit, ed, docsCli)

		m, msg := ctrlE(t, m)
		m.editing.fired.Store(true) // o despertador de parede tocou com o vim aberto
		m.Update(msg)
	})
	if len(got) != 0 {
		t.Fatalf("o aviso já saiu pelo relógio de parede — não se repete na volta: %v", got)
	}
}

func TestFocoCtrlESemPaginaSegueParaALinha(t *testing.T) {
	events, emit := recorder()
	ed := &pageEditor{}
	m := newFocoComPaginas(events, emit, ed)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	if len(ed.opened) != 0 || next.(Foco).editing != nil {
		t.Fatal("sem página ligada, Ctrl+E não abre nada")
	}
	if strings.Contains(next.(Foco).View(), "Ctrl+E") {
		t.Fatal("sem página ligada, a dica do Ctrl+E não aparece")
	}
}

func TestFocoVariasPaginasEscolheQualEditar(t *testing.T) {
	events, emit := recorder()
	ed := &pageEditor{clock: time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)}
	m := newFocoComPaginas(events, emit, ed, docsCli, LinkedPage{ID: "p2", Title: "Kubernetes"})

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	m = next.(Foco)
	if cmd != nil || !strings.Contains(m.View(), "[1] docs cli · [2] Kubernetes") {
		t.Fatalf("com várias páginas, Ctrl+E pergunta qual:\n%s", m.View())
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if next.(Foco).phase != focoRunning || len(ed.opened) != 0 {
		t.Fatal("Esc volta ao foco sem abrir nada")
	}

	next, _ = next.(Foco).Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	next, cmd = next.(Foco).Update(keyRunes("2"))
	if cmd == nil || len(ed.opened) != 1 || ed.opened[0] != "p2" {
		t.Fatalf("o número escolhe a página: %v", ed.opened)
	}
}

func TestFocoErroAoEditarAparecaNaTela(t *testing.T) {
	events, emit := recorder()
	ed := &pageEditor{clock: time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC), err: errors.New("o editor saiu com erro — o texto ficou em /tmp/x.md")}
	m := newFocoComPaginas(events, emit, ed, docsCli)

	m, msg := ctrlE(t, m)
	next, cmd := m.Update(msg)
	if cmd != nil && isQuit(t, cmd) {
		t.Fatal("erro ao editar não derruba o foco")
	}
	if view := next.(Foco).View(); !strings.Contains(view, "⚠ o editor saiu com erro — o texto ficou em /tmp/x.md") {
		t.Fatalf("o erro (com o caminho do texto) aparece na tela:\n%s", view)
	}
}
