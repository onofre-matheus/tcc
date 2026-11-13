// A leitura dentro do foco (etapa 2 do catálogo 1.7): a tarefa ligada a um
// livro abre o leitor com o relógio na barra; Esc volta ao relógio grande
// gravando onde parou (com a sessão), Ctrl+O retoma, os check-ins e a
// /distracao seguem os da sessão, e a pausa e o fim do bloco gravam o
// progresso antes.
package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

var safren = LinkedBook{ID: "b1", Title: "Mastering Your Adult ADHD"}

func newFocoLeitura(events *[]recorded, emit func(string, map[string]any) error, fr *fakeRead, fw *fakeWrite, pages []LinkedPage, books ...LinkedBook) Foco {
	m := NewFoco(FocoConfig{
		SessionID: "s1", NewSessionID: func() string { return "s2" },
		TaskID: "t1", TaskTitle: "Ler a sessão 1",
		Minutes: 25, PauseMinutes: 5, Emit: emit, Pages: pages, Books: books,
		Now:   func() time.Time { return time.Date(2026, 11, 12, 9, 30, 0, 0, time.UTC) },
		Write: fw, Read: fr, StartWriting: true,
	})
	m.Init()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return next.(Foco)
}

func typesOf(events []recorded) []string {
	var out []string
	for _, e := range events {
		out = append(out, e.eventType)
	}
	return out
}

func TestFocoAbreOLivroLigadoComORelogio(t *testing.T) {
	events, emit := recorder()
	fr := &fakeRead{text: sampleBook()}
	m := newFocoLeitura(events, emit, fr, &fakeWrite{}, nil, safren)
	if !m.reading || m.writing {
		t.Fatal("a tarefa ligada só a um livro abre direto na leitura")
	}
	view := m.View()
	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[0], "● FOCO ⏱ 25:00 · Ler a sessão 1") || !strings.Contains(lines[0], "cap. 1/3") ||
		!strings.Contains(lines[1], "📖 Mastering Your Adult ADHD · Psicoeducação") || !strings.Contains(view, "Esc relógio") {
		t.Fatalf("relógio na barra, livro na régua:\n%s", view)
	}

	m = hit(m, "jj")
	m = press(m, tea.KeyEsc)
	if m.reading || !strings.Contains(m.View(), "parou em cap. 1/3") || !strings.Contains(m.View(), "⚡ 📖 Mastering Your Adult ADHD") {
		t.Fatalf("Esc volta ao relógio grande com onde parou:\n%s", m.View())
	}
	if len(fr.progress) != 1 || fr.progress[0] != (BookLoc{Chapter: 2, Para: 3}) || fr.sessions[0] != "s1" {
		t.Fatalf("o progresso leva a sessão: %v %v", fr.progress, fr.sessions)
	}
	if typesOf(*events)[0] != "session.started" || len(*events) != 1 {
		t.Fatalf("voltar ao relógio não encerra a sessão: %v", typesOf(*events))
	}

	m = press(m, tea.KeyCtrlO)
	if !m.reading || m.read.para != 2 {
		t.Fatalf("Ctrl+O retoma no mesmo parágrafo: %v %d", m.reading, m.read.para)
	}
	m = press(m, tea.KeyEsc)
	if len(fr.progress) != 1 {
		t.Fatal("sem andar, não grava de novo")
	}
}

func TestFocoLeituraCheckinDistracaoEPausa(t *testing.T) {
	events, emit := recorder()
	fr := &fakeRead{text: sampleBook()}
	m := newFocoLeitura(events, emit, fr, &fakeWrite{}, nil, safren)
	m.cfg.CheckinEvery = 1
	for range 60 {
		next, _ := m.Update(tickMsg{})
		m = next.(Foco)
	}
	if !strings.Contains(m.View(), "🔔") || !m.reading {
		t.Fatalf("o check-in aparece na leitura:\n%s", m.View())
	}
	m = hit(m, "s")
	if m.Checkins != 1 || m.read.para != 0 {
		t.Fatal("o s responde o check-in, não mexe na leitura")
	}

	m = command(m, "distracao ver o e-mail")
	if len(fr.notes) != 0 || m.Distractions != 1 {
		t.Fatalf("no foco, a distração é da sessão: %v %d", fr.notes, m.Distractions)
	}
	last := (*events)[len(*events)-1]
	if last.eventType != "distraction.captured" || last.payload["session_id"] != "s1" {
		t.Fatalf("distraction.captured com a sessão: %+v", last)
	}

	m = hit(m, "j")
	m = press(m, tea.KeyCtrlP)
	if m.phase != focoBreak || len(fr.progress) != 1 {
		t.Fatalf("Ctrl+P grava onde parou e pergunta a pausa: %v %v", m.phase, fr.progress)
	}
	m = press(m, tea.KeyEsc)
	if m.phase != focoRunning || !strings.Contains(m.View(), "📖") {
		t.Fatal("desistindo da pausa, a leitura continua")
	}

	m = hit(m, "j")
	m.timer = newCountdown(0) // o bloco acaba no próximo tique
	next, _ := m.Update(tickMsg{})
	m = next.(Foco)
	if m.phase != focoPaused || len(fr.progress) != 2 {
		t.Fatalf("o fim do bloco grava onde parou antes da pausa: %v %v", m.phase, fr.progress)
	}
}

func TestFocoLeituraCtrlCGravaEEncerra(t *testing.T) {
	events, emit := recorder()
	fr := &fakeRead{text: sampleBook()}
	m := newFocoLeitura(events, emit, fr, &fakeWrite{}, nil, safren)
	m = hit(m, "j")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = next.(Foco)
	types := typesOf(*events)
	if !isQuit(t, cmd) || len(fr.progress) != 1 || types[len(types)-1] != "session.ended" {
		t.Fatalf("Ctrl+C grava onde parou e encerra a sessão: %v %v", fr.progress, types)
	}
}

// Página e livro ligados: o foco pergunta o que abrir; o livro sem arquivo
// neste dispositivo vira aviso, não derruba a sessão.
func TestFocoPerguntaEntrePaginaELivro(t *testing.T) {
	events, emit := recorder()
	fr := &fakeRead{text: sampleBook()}
	fw := &fakeWrite{body: "notas"}
	m := newFocoLeitura(events, emit, fr, fw, []LinkedPage{docsCli}, safren)
	if m.phase != focoPickPage || !strings.Contains(m.View(), "Abrir o quê?") ||
		!strings.Contains(m.View(), "[1] página docs cli") || !strings.Contains(m.View(), "[2] 📖 Mastering Your Adult ADHD") {
		t.Fatalf("página e livro na mesma escolha:\n%s", m.View())
	}
	m = hit(m, "2")
	if !m.reading || m.writing {
		t.Fatal("o 2 abre o livro")
	}
	m = press(m, tea.KeyEsc)
	m = press(m, tea.KeyCtrlO)
	m = hit(m, "1")
	if !m.writing || m.reading {
		t.Fatal("o 1 abre a página na escrita")
	}

	broken := &failingRead{fakeRead: fakeRead{text: sampleBook()}}
	m = newFocoLeitura(events, emit, nil, fw, nil)
	m.cfg.Read, m.cfg.Books = broken, []LinkedBook{safren}
	m = press(m, tea.KeyCtrlO)
	if m.reading || m.Err != nil || !strings.Contains(m.View(), "⚠ sem arquivo neste dispositivo") {
		t.Fatalf("livro que não abre vira aviso:\n%s", m.View())
	}
}

type failingRead struct{ fakeRead }

func (f *failingRead) Load(string) (BookText, error) {
	return BookText{}, fmt.Errorf("sem arquivo neste dispositivo")
}

// /ligar no relógio grande acha livros também; o livro ligado entra no
// Ctrl+O na hora.
func TestLigarLivroNoRelogioEntraNoCtrlO(t *testing.T) {
	events, emit := recorder()
	fr := &fakeRead{text: sampleBook()}
	fw := &fakeWrite{search: []Choice{{Kind: "book", ID: "b1", Label: "livro Mastering Your Adult ADHD"}}}
	m := newFocoLeitura(events, emit, fr, fw, nil)
	m = typeText(m, "/ligar adhd\n")
	if len(fw.linked) != 1 || len(m.cfg.Books) != 1 || m.cfg.Books[0].Title != "Mastering Your Adult ADHD" {
		t.Fatalf("o livro ligado entra na lista: %v %v", fw.linked, m.cfg.Books)
	}
	m = press(m, tea.KeyCtrlO)
	if !m.reading {
		t.Fatal("Ctrl+O abre o livro recém-ligado")
	}
}

func TestLeituraNoFocoCabeNoTerminal(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {60, 20}, {40, 12}, {120, 40}} {
		events, emit := recorder()
		m := newFocoLeitura(events, emit, &fakeRead{text: sampleBook()}, &fakeWrite{}, nil, safren)
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(Foco)
		m.cfg.CheckinEvery = 1
		for range 60 {
			next, _ := m.Update(tickMsg{})
			m = next.(Foco)
		}
		lines := strings.Split(m.View(), "\n")
		if len(lines) > size[1] || !strings.Contains(lines[0], "⏱") {
			t.Errorf("%dx%d: %d linhas, 1ª %q", size[0], size[1], len(lines), lines[0])
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > size[0]-1 {
				t.Errorf("%dx%d: linha %d com %d colunas", size[0], size[1], i, w)
			}
		}
	}
}
