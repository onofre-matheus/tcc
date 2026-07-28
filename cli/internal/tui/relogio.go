// Relógio das sessões: um despertador 8-bit desenhado em blocos. O tamanho é
// a mensagem — no modo VERMELHO o tempo restante tem de ser legível de longe,
// sem que o usuário precise "consultar" a tela (spec/CLI.md §2).
//
// O desenho não depende de cor: gabinete, campainhas e dígitos continuam
// legíveis com NO_COLOR ou saída redirecionada. O horário também vai em texto
// simples na borda de baixo — leitor de tela e `grep` leem "23:41".
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Dígitos de 6×5 blocos, no traço grosso dos mostradores de 7 segmentos.
var clockGlyphs = map[rune][5]string{
	'0': {"██████", "██  ██", "██  ██", "██  ██", "██████"},
	'1': {"    ██", "    ██", "    ██", "    ██", "    ██"},
	'2': {"██████", "    ██", "██████", "██    ", "██████"},
	'3': {"██████", "    ██", "██████", "    ██", "██████"},
	'4': {"██  ██", "██  ██", "██████", "    ██", "    ██"},
	'5': {"██████", "██    ", "██████", "    ██", "██████"},
	'6': {"██████", "██    ", "██████", "██  ██", "██████"},
	'7': {"██████", "    ██", "    ██", "    ██", "    ██"},
	'8': {"██████", "██  ██", "██████", "██  ██", "██████"},
	'9': {"██████", "██  ██", "██████", "    ██", "██████"},
	':': {"  ", "██", "  ", "██", "  "},
	' ': {"  ", "  ", "  ", "  ", "  "}, // dois-pontos apagado, no piscar
}

// clockWidth é a largura do gabinete no caso normal, MM:SS: 6*4 dígitos + 2 do
// dois-pontos + 5 espaços + 4 de recuo interno + 2 de borda. Abaixo disso o
// relógio degrada; sessões de 100 min ou mais alargam o gabinete (ver bigClock).
const clockWidth = 38

// bigClock monta o despertador para o tempo restante. width é a largura do
// terminal (0 = desconhecida); abaixo do gabinete o relógio degrada para a
// versão compacta de uma linha, que é o que cabe.
func bigClock(c countdown, label string, style lipgloss.Style, width int) string {
	clock := c.clock()
	// O dois-pontos pisca a cada segundo, como todo despertador de cabeceira —
	// é também o sinal de que a sessão está correndo, e não congelada.
	rows := clockDigits(clock, c.remaining%2 == 0)
	inner := lipgloss.Width(rows[0]) + 4
	// A largura sai dos dígitos, não de uma constante: uma sessão de duas
	// horas mostra 120:00 e o gabinete cresce junto.
	cabinet := inner + 2

	if width > 0 && width < cabinet {
		return "  " + style.Render("▐█  "+clock+"  █▌")
	}

	var art strings.Builder
	art.WriteString("   ▄▄▄" + pad(inner-10) + "▄▄▄\n")
	art.WriteString("  ▐███▌" + pad(inner-12) + "▐███▌\n")
	head := "━━ " + label + " "
	art.WriteString("┏" + head + bar(inner-lipgloss.Width(head)) + "┓\n")
	art.WriteString("┃" + pad(inner) + "┃\n")
	for _, row := range rows {
		art.WriteString("┃  " + row + "  ┃\n")
	}
	art.WriteString("┃" + pad(inner) + "┃\n")
	foot := " " + clock + " ━━" // horário em texto, para quem não vê os blocos
	art.WriteString("┗" + bar(inner-lipgloss.Width(foot)) + foot + "┛\n")
	art.WriteString("  ▀▀" + pad(inner-6) + "▀▀")

	return indent(style.Render(art.String()), width, cabinet)
}

// clockDigits transforma "23:41" nas cinco linhas de blocos.
func clockDigits(clock string, colon bool) [5]string {
	var rows [5]string
	for i, r := range clock {
		if r == ':' && !colon {
			r = ' '
		}
		glyph, ok := clockGlyphs[r]
		if !ok {
			continue
		}
		for k := range rows {
			if i > 0 {
				rows[k] += " "
			}
			rows[k] += glyph[k]
		}
	}
	return rows
}

func pad(n int) string { return strings.Repeat(" ", max(n, 0)) }
func bar(n int) string { return strings.Repeat("━", max(n, 0)) }

// indent recua cada linha — o relógio fica centrado quando a largura é
// conhecida (encostado à esquerda quando o gabinete mal cabe) e com um recuo
// de cortesia enquanto o terminal ainda não se anunciou.
func indent(art string, width, cabinet int) string {
	n := 2
	if width > 0 {
		n = max((width-cabinet)/2, 0)
	}
	prefix := strings.Repeat(" ", n)
	lines := strings.Split(art, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
