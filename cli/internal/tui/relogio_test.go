// O despertador 8-bit é só uma string: dá para provar que ele mostra a hora
// certa, pisca, cabe no terminal e nunca esconde o horário em texto.
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestRelogioDesenhaDigitosGrandesEHorarioEmTexto(t *testing.T) {
	art := bigClock(countdown{remaining: 23*60 + 41}, "FOCO", redTimer, 80)

	if !strings.Contains(art, "23:41") {
		t.Fatalf("o horário em texto tem de continuar na moldura (leitor de tela, NO_COLOR):\n%s", art)
	}
	if !strings.Contains(art, "┏") || !strings.Contains(art, "▐███▌") {
		t.Fatalf("faltou o gabinete com campainhas:\n%s", art)
	}
	if n := strings.Count(art, "██████"); n < 4 {
		t.Fatalf("os dígitos deveriam ser blocos grandes, veio %d traços:\n%s", n, art)
	}
	if !strings.Contains(art, "FOCO") {
		t.Fatalf("o modo é rotulado na moldura — cor nunca é o único sinal:\n%s", art)
	}
}

func TestRelogioPiscaOsDoisPontos(t *testing.T) {
	linha := func(seconds int) string {
		// a 2ª linha de blocos é a única em que o dois-pontos acende
		rows := clockDigits(countdown{remaining: seconds}.clock(), seconds%2 == 0)
		return rows[1]
	}
	par, impar := linha(300), linha(301)
	if strings.Count(par, "██") == strings.Count(impar, "██") {
		t.Fatalf("o dois-pontos deveria piscar entre um segundo e outro:\n%q\n%q", par, impar)
	}
}

func TestRelogioDegradaEmTerminalEstreito(t *testing.T) {
	art := bigClock(countdown{remaining: 90}, "FOCO", redTimer, 30)

	if strings.Contains(art, "\n") {
		t.Fatalf("sem largura para o gabinete, o relógio cabe em uma linha:\n%s", art)
	}
	if !strings.Contains(art, "01:30") {
		t.Fatalf("a versão compacta ainda mostra o tempo restante: %q", art)
	}
}

func TestRelogioSemLarguraConhecidaAindaDesenha(t *testing.T) {
	// Antes do primeiro WindowSizeMsg a largura é 0: assume-se que cabe.
	if art := bigClock(countdown{remaining: 60}, "FOCO", redTimer, 0); !strings.Contains(art, "┏") {
		t.Fatalf("largura desconhecida deveria desenhar o gabinete:\n%s", art)
	}
}

// No limite, o gabinete encosta na margem em vez de transbordar — uma linha
// mais larga que o terminal quebra e desmonta o desenho inteiro.
func TestRelogioNuncaTransbordaAMargem(t *testing.T) {
	// `pnn foco -m 120` mostra 120:00 e alarga o gabinete: a margem tem de
	// valer nos dois tamanhos.
	for _, remaining := range []int{60, 120 * 60} {
		for width := 20; width <= 120; width++ {
			art := bigClock(countdown{remaining: remaining}, "FOCO", redTimer, width)
			for _, line := range strings.Split(art, "\n") {
				if got := lipgloss.Width(line); got > width {
					t.Fatalf("restando %ds em %d colunas, uma linha ficou com %d: %q",
						remaining, width, got, line)
				}
			}
		}
	}
}
