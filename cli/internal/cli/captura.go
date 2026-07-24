// Captura one-shot (spec/CLI.md §1): uma linha, nunca uma tela — grava o
// evento e devolve o prompt. `pnn t` tarefa, `pnn n` nota, `pnn c` compromisso.
package cli

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/ui"
	"github.com/onofre-matheus/tcc/cli/store"
	"github.com/spf13/cobra"
)

func newTCmd(a *app.App) *cobra.Command {
	var priority, dueDate string
	var sub int
	cmd := &cobra.Command{
		Use:   `t "título"`,
		Short: "captura uma tarefa (task.created)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			payload := map[string]any{"task_id": store.UUIDv7(), "title": args[0]}

			if sub > 0 {
				ref, err := a.ResolveView(sub)
				if err != nil {
					return err
				}
				if ref.Kind != "task" {
					return fmt.Errorf("o item %d da última tela não é uma tarefa", sub)
				}
				payload["parent_id"] = ref.ID
			}
			if dueDate != "" {
				if _, err := time.Parse("2006-01-02", dueDate); err != nil {
					return fmt.Errorf("data inválida %q (use AAAA-MM-DD)", dueDate)
				}
				payload["due_date"] = dueDate
			}
			if priority != "" && !validPriority(priority) {
				return fmt.Errorf("prioridade inválida %q (use A, B ou C)", priority)
			}

			if _, err := a.Store.Append("task.created", payload); err != nil {
				return err
			}
			if priority != "" {
				if _, err := a.Store.Append("task.prioritized", map[string]any{
					"task_id": payload["task_id"], "priority": priority,
				}); err != nil {
					return err
				}
			}

			th := ui.Theme{On: a.Color}
			badge := ""
			if priority != "" {
				badge = th.Badge(priority) + " "
			}
			fmt.Fprintf(a.Out, "✔ tarefa capturada: %s%s\n", badge, args[0])
			return nil
		},
	}
	cmd.Flags().StringVarP(&priority, "pri", "p", "", "prioridade A|B|C")
	cmd.Flags().IntVar(&sub, "sub", 0, "cria como subtarefa do item N da última tela")
	cmd.Flags().StringVar(&dueDate, "data", "", "data-alvo AAAA-MM-DD")
	return cmd
}

func newNCmd(a *app.App) *cobra.Command {
	var link string
	cmd := &cobra.Command{
		Use:   `n "texto"`,
		Short: "captura uma nota para triagem (note.captured)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			payload := map[string]any{"note_id": store.UUIDv7(), "text": args[0]}
			// --link anexa uma URL à nota (o mesmo campo `url` que a captura na
			// web preenche): vira link clicável no `pnn caixa` e na triagem.
			if link != "" {
				payload["url"] = normalizeLink(link)
			}
			if _, err := a.Store.Append("note.captured", payload); err != nil {
				return err
			}
			th := ui.Theme{On: a.Color}
			suffix := ""
			if link != "" {
				suffix = " " + th.Dim("🔗")
			}
			fmt.Fprintf(a.Out, "✔ anotado%s — triagem depois com %s\n", suffix, th.Blue("pnn triagem"))
			return nil
		},
	}
	cmd.Flags().StringVar(&link, "link", "", "anexa um link à anotação (clicável no `pnn caixa`)")
	return cmd
}

// normalizeLink assume https:// quando o usuário digita sem esquema, para o
// link ser clicável — igual ao campo de link da extensão.
func normalizeLink(raw string) string {
	if schemePattern.MatchString(raw) {
		return raw
	}
	return "https://" + raw
}

var schemePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://`)

var timeRangePattern = regexp.MustCompile(`^(\d{2}:\d{2})-(\d{2}:\d{2})$`)

func newCCmd(a *app.App) *cobra.Command {
	var day string
	var important bool
	cmd := &cobra.Command{
		Use:   `c "título" HH:MM-HH:MM`,
		Short: "agenda um compromisso no calendário único (appointment.created)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			title, timeRange := args[0], args[1]
			match := timeRangePattern.FindStringSubmatch(timeRange)
			if match == nil {
				return fmt.Errorf("horário inválido %q (use HH:MM-HH:MM)", timeRange)
			}

			if day == "" {
				today, err := a.Today()
				if err != nil {
					return err
				}
				day = today
			}
			startsAt, err := localToUTC(day, match[1], a.TZ)
			if err != nil {
				return err
			}
			endsAt, err := localToUTC(day, match[2], a.TZ)
			if err != nil {
				return err
			}
			if endsAt <= startsAt {
				return fmt.Errorf("fim %s não vem depois do início %s", match[2], match[1])
			}

			payload := map[string]any{
				"appointment_id": store.UUIDv7(),
				"title":          title,
				"starts_at":      startsAt,
				"ends_at":        endsAt,
			}
			// --importante grava appointment.created v2 com importance="important":
			// a extensão o lembra com semanas de antecedência (marca d'água + cascata).
			var appendErr error
			if important {
				payload["importance"] = "important"
				_, appendErr = a.Store.AppendV("appointment.created", 2, payload)
			} else {
				_, appendErr = a.Store.Append("appointment.created", payload)
			}
			if appendErr != nil {
				return appendErr
			}
			star := ""
			if important {
				star = "⭐ "
			}
			fmt.Fprintf(a.Out, "✔ agendado: %s%s, %s %s\n", star, title, longDatePT(day), strings.Replace(timeRange, "-", "–", 1))
			return nil
		},
	}
	cmd.Flags().StringVar(&day, "dia", "", "data AAAA-MM-DD (padrão: hoje)")
	cmd.Flags().BoolVar(&important, "importante", false, "marca como importante (lembretes com antecedência na extensão)")
	return cmd
}

func validPriority(p string) bool {
	return p == "A" || p == "B" || p == "C"
}

// localToUTC converte data civil + hora local no fuso do usuário para o
// instante UTC do envelope.
func localToUTC(day, hhmm, tz string) (string, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return "", fmt.Errorf("fuso inválido %q: %w", tz, err)
	}
	t, err := time.ParseInLocation("2006-01-02 15:04", day+" "+hhmm, loc)
	if err != nil {
		return "", fmt.Errorf("data/hora inválida %q %q: %w", day, hhmm, err)
	}
	return t.UTC().Format("2006-01-02T15:04:05.000Z"), nil
}
