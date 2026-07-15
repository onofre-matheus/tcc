// Cronômetro das sessões: um único encadeamento de ticks de 1 s por programa
// — transições disparadas por tecla nunca agendam tick novo, senão o tempo
// dobraria de velocidade.
package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type tickMsg struct{}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

// errMsg carrega uma falha de gravação no log para dentro do loop do programa.
type errMsg struct{ err error }

type countdown struct{ remaining int } // segundos

func newCountdown(minutes int) countdown {
	return countdown{remaining: minutes * 60}
}

// tick desconta um segundo; devolve true quando o timer toca.
func (c *countdown) tick() bool {
	if c.remaining > 0 {
		c.remaining--
	}
	return c.remaining == 0
}

func (c countdown) clock() string {
	return fmt.Sprintf("%02d:%02d", c.remaining/60, c.remaining%60)
}
