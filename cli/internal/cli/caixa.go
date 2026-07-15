// `pnn caixa` — pendências de triagem (notas e distrações capturadas), na
// ordem de captura. A triagem interativa é `pnn triagem` (TUI, etapa futura).
package cli

import (
	"encoding/json"
	"fmt"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/ui"
	"github.com/spf13/cobra"
)

func newCaixaCmd(a *app.App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "caixa",
		Short: "lista as pendências de triagem (notas e distrações)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			events, err := a.Store.Events()
			if err != nil {
				return err
			}
			state, err := core.Inbox(events)
			if err != nil {
				return err
			}

			if asJSON {
				raw, err := json.MarshalIndent(state, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(a.Out, string(raw))
				return nil
			}

			th := ui.Theme{On: a.Color}
			if len(state.PendingNotes)+len(state.PendingDistractions) == 0 {
				fmt.Fprintf(a.Out, "%s\n", th.Dim("caixa de entrada vazia 🐘"))
				return nil
			}
			for _, id := range state.PendingNotes {
				note := state.Notes[id]
				fmt.Fprintf(a.Out, " • %s", note.Text)
				if note.PageTitle != nil {
					fmt.Fprintf(a.Out, " %s", th.Dim("("+*note.PageTitle+")"))
				}
				fmt.Fprintln(a.Out)
			}
			for _, id := range state.PendingDistractions {
				fmt.Fprintf(a.Out, " • %s %s\n", state.Distractions[id].Text, th.Dim("(distração)"))
			}
			fmt.Fprintf(a.Out, "\ntriar um a um: %s\n", th.Blue("pnn triagem"))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emite a projeção inbox em JSON")
	return cmd
}
