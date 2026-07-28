// Estilos dos modos por cor (spec/CLI.md §2): VERMELHO = sessão em andamento,
// VERDE = pausa. Cor nunca é o único sinal — símbolo e texto acompanham; o
// Lipgloss degrada sozinho em terminais sem cor (NO_COLOR, pipe).
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	redAccent   = lipgloss.AdaptiveColor{Light: "1", Dark: "9"}
	greenAccent = lipgloss.AdaptiveColor{Light: "2", Dark: "10"}
	blueAccent  = lipgloss.AdaptiveColor{Light: "4", Dark: "12"}

	redTitle   = lipgloss.NewStyle().Bold(true).Foreground(redAccent)
	redTimer   = lipgloss.NewStyle().Bold(true).Foreground(redAccent)
	greenTitle = lipgloss.NewStyle().Bold(true).Foreground(greenAccent)
	greenTimer = lipgloss.NewStyle().Bold(true).Foreground(greenAccent)
	blueTitle  = lipgloss.NewStyle().Bold(true).Foreground(blueAccent)
	blueText   = lipgloss.NewStyle().Foreground(blueAccent)
	dim        = lipgloss.NewStyle().Faint(true)
)

// hyperlink emite OSC 8 — a fonte do cartão vira link clicável no terminal.
func hyperlink(url, text string) string {
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// pauseView é a tela VERDE compartilhada: descanso cronometrado + o que a
// sessão rendeu + próximos passos.
func pauseView(head string, timer countdown, summary []string, actions string, width int) string {
	var b strings.Builder
	b.WriteString(greenTitle.Render("✔ " + head))
	b.WriteString("\n\n")
	b.WriteString(bigClock(timer, "PAUSA", greenTimer, width))
	b.WriteString("\n\n " + dim.Render("a pausa é parte do bloco — descanse até o despertador") + "\n\n")
	for _, line := range summary {
		b.WriteString(" " + line + "\n")
	}
	if len(summary) > 0 {
		b.WriteString("\n")
	}
	b.WriteString(" " + dim.Render(actions) + "\n")
	return b.String()
}
