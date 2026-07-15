// `pnn semana [N]` — a retrospectiva (projeção review, spec/SPEC.md §4.7): a
// revisão à la Safren do que foi feito e do que interrompeu. N semanas atrás
// ou `--de AAAA-MM-DD`; como a projeção é pura em (eventos, now, tz), navegar
// no histórico é só variar o instante de referência.
package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/ui"
	"github.com/spf13/cobra"
)

var weekdayShortPT = [...]string{"dom", "seg", "ter", "qua", "qui", "sex", "sáb"}

func newSemanaCmd(a *app.App) *cobra.Command {
	var de string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "semana [N]",
		Short: "retrospectiva da semana (N semanas atrás; --de para uma data qualquer)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			nowISO := a.NowISO()
			switch {
			case de != "":
				ts, err := localToUTC(de, "12:00", a.TZ)
				if err != nil {
					return err
				}
				nowISO = ts
			case len(args) == 1:
				n, err := strconv.Atoi(args[0])
				if err != nil || n < 0 {
					return fmt.Errorf("esperava um número de semanas atrás, veio %q", args[0])
				}
				nowISO = a.Now().AddDate(0, 0, -7*n).UTC().Format("2006-01-02T15:04:05.000Z")
			}

			events, err := a.Store.Events()
			if err != nil {
				return err
			}
			state, err := core.Review(events, core.Params{Now: nowISO, TZ: a.TZ})
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
			return renderSemana(a, state)
		},
	}
	cmd.Flags().StringVar(&de, "de", "", "AAAA-MM-DD: a semana que contém essa data")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emite a projeção em JSON")
	return cmd
}

func renderSemana(a *app.App, s core.ReviewState) error {
	th := ui.Theme{On: a.Color}
	fmt.Fprintf(a.Out, "🐘 %s   %s – %s\n", th.Title("Semana"), th.Bold(shortDatePT(s.WeekStart)), th.Bold(shortDatePT(s.WeekEnd)))
	fmt.Fprintln(a.Out, th.Dim("────────────────────────────────────────────────────────────"))

	for i := 0; i < 7; i++ {
		day, err := core.AddDays(s.WeekStart, i)
		if err != nil {
			return err
		}
		d := s.Days[day]
		label := fmt.Sprintf(" %s %s", weekdayShortPT[weekdayOf(day)], shortDatePT(day))
		if d.FocusMinutes == 0 && d.Reviews == 0 && d.PauseMinutes == 0 && d.Checkins == 0 {
			fmt.Fprintf(a.Out, "%s  %s\n", label, th.Dim("—"))
			continue
		}
		line := fmt.Sprintf("%s  %d min de foco", label, d.FocusMinutes)
		if d.Reviews > 0 {
			line += fmt.Sprintf(" · %s", plural(d.Reviews, "revisão", "revisões"))
		}
		if d.PauseMinutes > 0 {
			line += fmt.Sprintf(" · %d min de pausa", d.PauseMinutes)
		}
		if d.Completed > 0 || d.Interrupted > 0 {
			line += fmt.Sprintf(" · %s✔ %s✗", th.Bold(strconv.Itoa(d.Completed)), strconv.Itoa(d.Interrupted))
		}
		if d.Checkins > 0 {
			line += fmt.Sprintf(" · na tarefa %d/%d", d.CheckinsOnTask, d.Checkins)
		}
		fmt.Fprintln(a.Out, line)
	}

	fmt.Fprintln(a.Out, th.Dim("────────────────────────────────────────────────────────────"))
	t := s.Totals
	fmt.Fprintf(a.Out, " total: %d min de foco · %s · %d min de pausa · %d sessão(ões) concluída(s) · %d interrompida(s)\n",
		t.FocusMinutes, plural(t.Reviews, "revisão", "revisões"), t.PauseMinutes, t.Completed, t.Interrupted)
	if t.Checkins > 0 {
		fmt.Fprintf(a.Out, " check-ins de atenção: na tarefa em %d de %d\n", t.CheckinsOnTask, t.Checkins)
	}
	if s.AppointmentMinutes > 0 {
		fmt.Fprintf(a.Out, " agendado no calendário: %d min · executado em foco: %d min\n", s.AppointmentMinutes, t.FocusMinutes)
	}
	if len(s.Reasons) > 0 {
		fmt.Fprintf(a.Out, "\n %s\n", th.Bold("O que interrompeu:"))
		for reason, n := range s.Reasons {
			fmt.Fprintf(a.Out, "  %d× %s\n", n, reason)
		}
	}
	fmt.Fprintf(a.Out, "\n %s\n", th.Dim("semanas anteriores: `pnn semana 1`, `pnn semana 2`… ou `pnn semana --de AAAA-MM-DD`"))
	return nil
}

// shortDatePT formata "2026-07-06" como "06/07".
func shortDatePT(isoDate string) string {
	t, err := time.Parse("2006-01-02", isoDate)
	if err != nil {
		return isoDate
	}
	return t.Format("02/01")
}

func weekdayOf(isoDate string) time.Weekday {
	t, err := time.Parse("2006-01-02", isoDate)
	if err != nil {
		return time.Sunday
	}
	return t.Weekday()
}
