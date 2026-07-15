// `pnn triagem` — abre a triagem interativa da caixa de entrada, um item por
// vez, na ordem de captura (notas e distrações, RF06).
package cli

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/tui"
	"github.com/onofre-matheus/tcc/cli/internal/ui"
	"github.com/spf13/cobra"
)

func newTriagemCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "triagem",
		Short: "tria a caixa de entrada um item por vez (cartão/tarefa/descartar)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			events, err := a.Store.Events()
			if err != nil {
				return err
			}
			inbox, err := core.Inbox(events)
			if err != nil {
				return err
			}
			decks, err := core.Decks(events)
			if err != nil {
				return err
			}

			th := ui.Theme{On: a.Color}
			var items []tui.TriageItem
			for _, id := range inbox.PendingNotes {
				note := inbox.Notes[id]
				items = append(items, tui.TriageItem{
					Kind: "note", ID: id, Text: note.Text, URL: note.URL, PageTitle: note.PageTitle,
				})
			}
			for _, id := range inbox.PendingDistractions {
				items = append(items, tui.TriageItem{
					Kind: "distraction", ID: id, Text: inbox.Distractions[id].Text,
				})
			}
			if len(items) == 0 {
				fmt.Fprintf(a.Out, "%s\n", th.Dim("caixa de entrada vazia 🐘"))
				return nil
			}

			model := tui.NewTriagem(tui.TriagemConfig{
				Items: items,
				Decks: deckOptions(decks),
				Emit:  emitTo(a),
			})
			final, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
			if err != nil {
				return err
			}
			triagem := final.(tui.Triagem)
			if triagem.Err != nil {
				return triagem.Err
			}

			var parts []string
			for _, p := range []struct {
				n     int
				label string
			}{
				{triagem.Cards, "em cartão"},
				{triagem.Tasks, "em tarefa"},
				{triagem.Resolved, "feita(s)"},
				{triagem.Discarded, "descartada(s)"},
			} {
				if p.n > 0 {
					parts = append(parts, fmt.Sprintf("%d %s", p.n, p.label))
				}
			}
			if len(parts) > 0 {
				fmt.Fprintf(a.Out, "✔ triagem: %s\n", strings.Join(parts, " · "))
			}
			if pending := len(items) - (triagem.Cards + triagem.Tasks + triagem.Resolved + triagem.Discarded); pending > 0 {
				fmt.Fprintf(a.Out, "%s\n", th.Dim(fmt.Sprintf("%d ainda pendente(s) → pnn triagem", pending)))
			}
			return nil
		},
	}
}
