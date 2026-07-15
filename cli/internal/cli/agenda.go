// `pnn agenda` — os próximos compromissos com números efêmeros; cancelar é
// `pnn apagar N` (appointment.cancelled, tombstone no calendário único).
package cli

import (
	"encoding/json"
	"fmt"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/ui"
	"github.com/spf13/cobra"
)

func newAgendaCmd(a *app.App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "agenda",
		Short: "lista os próximos compromissos (cancele com `pnn apagar N`)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			events, err := a.Store.Events()
			if err != nil {
				return err
			}
			calendar, err := core.Calendar(events, a.Params())
			if err != nil {
				return err
			}

			if asJSON {
				raw, err := json.MarshalIndent(calendar, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(a.Out, string(raw))
				return nil
			}

			th := ui.Theme{On: a.Color}
			if len(calendar.Upcoming) == 0 {
				fmt.Fprintf(a.Out, "%s\n", th.Dim("agenda vazia — capture com `pnn c \"título\" 16:00-17:00`"))
				return a.SaveView(nil)
			}
			refs := make([]app.ViewRef, len(calendar.Upcoming))
			for i, id := range calendar.Upcoming {
				appt := calendar.Appointments[id]
				refs[i] = app.ViewRef{Kind: "appointment", ID: id}
				day, err := localDateOf(appt.StartsAt, a.TZ)
				if err != nil {
					return err
				}
				fmt.Fprintf(a.Out, " %d  %s  %s–%s  %s\n", i+1, th.Blue(day),
					th.Blue(localClock(appt.StartsAt, a.TZ)), th.Blue(localClock(appt.EndsAt, a.TZ)), appt.Title)
			}
			return a.SaveView(refs)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emite a projeção em JSON")
	return cmd
}
