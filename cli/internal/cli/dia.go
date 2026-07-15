// `pnn dia` — a tela AZUL de organizar (spec/CLI.md §4): agenda do dia, lista
// A/B/C numerada e cartões vencidos, em uma única tela.
package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/ui"
	"github.com/spf13/cobra"
)

func newDiaCmd(a *app.App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "dia",
		Short: "mostra a agenda, a lista A/B/C e os cartões vencidos",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDia(a, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emite as projeções em JSON")
	return cmd
}

func runDia(a *app.App, asJSON bool) error {
	view, err := buildDayView(a)
	if err != nil {
		return err
	}

	if asJSON {
		raw, err := json.MarshalIndent(map[string]any{
			"today":    view.today,
			"stats":    view.stats,
			"tasks":    view.tasks,
			"leitner":  view.leitner,
			"calendar": view.calendar,
			"inbox":    view.inbox,
		}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(a.Out, string(raw))
		return nil
	}

	th := ui.Theme{On: a.Color}
	rule := th.Dim(strings.Repeat("─", 60))

	streak := th.Dim("sem sequência ainda")
	if view.stats.Streak > 0 {
		streak = plural(view.stats.Streak, "dia", "dias")
	}
	fmt.Fprintf(a.Out, "🐘 %s   %s · %s hoje\n", th.Title("Procrastina Não"), streak,
		plural(view.stats.ReviewsToday, "revisão", "revisões"))
	fmt.Fprintln(a.Out, rule)
	fmt.Fprintf(a.Out, " %s\n\n", th.Bold(longDatePT(view.today)))

	if appts := view.todayAppointments(a.TZ); len(appts) > 0 {
		for _, id := range appts {
			appt := view.calendar.Appointments[id]
			fmt.Fprintf(a.Out, " %s–%s  %s\n",
				th.Blue(localClock(appt.StartsAt, a.TZ)), th.Blue(localClock(appt.EndsAt, a.TZ)), appt.Title)
		}
		fmt.Fprintln(a.Out)
	}

	if len(view.rows) == 0 {
		fmt.Fprintf(a.Out, " %s\n", th.Dim("nenhuma tarefa ativa — capture uma com `pnn t \"...\"`"))
	}
	for _, row := range view.rows {
		fmt.Fprintln(a.Out, row.line(th))
	}
	if view.tasks.Alerts.TooManyA {
		fmt.Fprintf(a.Out, " %s\n", th.Warn("⚠ mais de 4 tarefas A ativas — repriorize para focar"))
	}

	if due := len(view.leitner.Queue); due > 0 {
		fmt.Fprintf(a.Out, "\n %s → %s\n", plural(due, "cartão vencido", "cartões vencidos"), th.Blue("pnn revisar"))
	}

	fmt.Fprintln(a.Out, rule)
	if pending := view.pendingInbox(); pending > 0 {
		fmt.Fprintf(a.Out, " caixa de entrada: %s → %s\n", plural(pending, "pendente", "pendentes"), th.Blue("pnn triagem"))
	} else {
		fmt.Fprintf(a.Out, " %s\n", th.Dim("caixa de entrada vazia"))
	}
	return nil
}

// todayAppointments devolve os compromissos cujo início cai na data de hoje,
// na ordem de início (a agenda do dia, encerrados inclusive).
func (v *dayView) todayAppointments(tz string) []string {
	var ids []string
	for _, id := range sortedAppointmentIDs(v.calendar) {
		appt := v.calendar.Appointments[id]
		if day, err := localDateOf(appt.StartsAt, tz); err == nil && day == v.today {
			ids = append(ids, id)
		}
	}
	return ids
}
