package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// A tela de escrita cabe no terminal: o bubbletea descarta as linhas de CIMA
// do que não cabe — e a de cima é a do relógio.
func TestEscritaCabeNoTerminal(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {100, 30}, {60, 20}, {120, 40}, {200, 50}} {
		for _, body := range []string{"", "uma linha", strings.Repeat("linha longa de texto que quebra na largura do terminal ", 8) + "\n" + strings.Repeat("x\n", 80)} {
			events, emit := recorder()
			m := newEscrita(events, emit, &fakeWrite{body: body, refs: []string{`"O que é um Pod?"`, "tarefa Revisar o console de administração do portal"}})
			next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m = next.(Foco)
			for _, palette := range []bool{false, true} {
				if palette {
					m = press(m, tea.KeyCtrlEnd)
					m = press(m, tea.KeyEnter)
					m = typeText(m, "/")
				}
				// como o bubbletea: split por "\n", sem aparar — um "\n" no fim
				// vira uma linha a mais e empurra o relógio para fora
				lines := strings.Split(m.View(), "\n")
				name := fmt.Sprintf("%dx%d corpo=%d paleta=%v", size[0], size[1], len(body), palette)
				if len(lines) > size[1] {
					t.Errorf("%s: %d linhas num terminal de %d — o relógio some", name, len(lines), size[1])
				}
				if !strings.Contains(lines[0], "⏱") {
					t.Errorf("%s: a 1ª linha não tem o relógio: %q", name, lines[0])
				}
				for i, l := range lines {
					if w := ansi.StringWidth(l); w > size[0]-1 {
						t.Errorf("%s: linha %d com %d colunas (o limite é %d): %q", name, i, w, size[0]-1, l)
						break
					}
				}
			}
		}
	}
}
