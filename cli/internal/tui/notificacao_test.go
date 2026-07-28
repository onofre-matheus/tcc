// Os três momentos em que a sessão precisa alcançar quem está em outra janela:
// o timer do foco, o fim da pausa e o check-in de atenção.
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// notices roda o cenário com o espião zerado e devolve o que foi disparado.
func notices(t *testing.T, run func()) []sentNotice {
	t.Helper()
	sent = nil
	run()
	return sent
}

func TestFocoNotificaFimDoBlocoEFimDaPausa(t *testing.T) {
	_, emit := recorder()

	got := notices(t, func() {
		m := newTestFoco(nil, emit)
		m.timer.remaining = 1
		next, _ := m.Update(tickMsg{}) // toca o timer do foco
		m = next.(Foco)
		m.timer.remaining = 1
		m.Update(tickMsg{}) // toca o timer da pausa
	})

	if len(got) != 2 {
		t.Fatalf("esperava avisos de fim de bloco e de fim de pausa, veio %v", got)
	}
	if !strings.Contains(got[0].title, "25 min") || !strings.Contains(got[0].body, "5 min") {
		t.Fatalf("o aviso do bloco deveria dizer o que acabou e o que começou: %v", got[0])
	}
	if !strings.Contains(strings.ToLower(got[1].title), "pausa") {
		t.Fatalf("o fim da pausa deveria ter aviso próprio: %v", got[1])
	}
}

func TestFocoInterrompidoNaoNotifica(t *testing.T) {
	_, emit := recorder()

	got := notices(t, func() {
		m := newTestFoco(nil, emit)
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		next.(Foco).Update(tea.KeyMsg{Type: tea.KeyEsc})
	})

	if len(got) != 0 {
		t.Fatalf("quem interrompe está no teclado — não há o que avisar: %v", got)
	}
}

func TestCheckinNotificaComAMesmaPergunta(t *testing.T) {
	_, emit := recorder()

	got := notices(t, func() {
		m := newTestFoco(nil, emit)
		m.cfg.CheckinEvery = 1
		var model tea.Model = m
		for i := 0; i < 60; i++ {
			model, _ = model.(Foco).Update(tickMsg{})
		}
		if view := model.(Foco).View(); !strings.Contains(view, "Escrever seção 4.6") {
			t.Fatalf("a pergunta deveria estar na tela:\n%s", view)
		}
	})

	if len(got) != 1 {
		t.Fatalf("o check-in deveria notificar uma vez, veio %v", got)
	}
	if !strings.Contains(got[0].body, "Escrever seção 4.6") {
		t.Fatalf("a notificação repete a pergunta da tela, para ser reconhecida: %v", got[0])
	}
}
