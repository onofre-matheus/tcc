// O alias-intervenção (spec/CLI.md §5): no impulso de desistir, o custo de
// recomeçar cai para uma linha — mascote, streak e o menor próximo passo.
package cli

import (
	"fmt"

	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/ui"
	"github.com/spf13/cobra"
)

func newProcrastinaCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "procrastina",
		Short: "a intervenção que dá nome ao sistema",
		// A intervenção não pode falhar por erro de digitação no momento do
		// impulso: aceita o typo comum (sem o 1º "r") e o infinitivo.
		Aliases: []string{"procastina", "procrastinar", "procastinar"},
		Hidden:  true, // chega pelo alias `procrastina`, não pela ajuda do pnn
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			view, err := buildDayView(a)
			if err != nil {
				return err
			}
			th := ui.Theme{On: a.Color}

			fmt.Fprintf(a.Out, "\n        %s 🐘\n\n", th.Bold("Não."))
			if view.stats.Streak > 0 {
				fmt.Fprintf(a.Out, " %s de sequência — não quebra hoje.\n",
					plural(view.stats.Streak, "dia", "dias"))
			}

			if row, ok := smallestActiveA(view.rows); ok {
				fmt.Fprintf(a.Out, " Um passo pequeno: %s %s\n", th.Badge("A"), row.task.Title)
				fmt.Fprintf(a.Out, " → %s\n", th.Blue(fmt.Sprintf("pnn foco %d", row.num)))
				return nil
			}
			if due := len(view.leitner.Queue); due > 0 {
				fmt.Fprintf(a.Out, " %s esperando — revisar é recomeçar pequeno.\n",
					plural(due, "cartão vencido", "cartões vencidos"))
				fmt.Fprintf(a.Out, " → %s\n", th.Blue("pnn revisar"))
				return nil
			}
			fmt.Fprintf(a.Out, " Nada pendente. Mandou bem — descansa, que amanhã tem mais.\n")
			return nil
		},
	}
}

// smallestActiveA acha a menor subtarefa A ativa: a primeira tarefa A sem
// filhos ativos, na ordem da tela do dia (quebra de tarefas — o próximo passo
// mais fácil de começar). Sem folha A, cai na primeira A da tela.
func smallestActiveA(rows []taskRow) (taskRow, bool) {
	var firstA *taskRow
	for i, row := range rows {
		if priorityLabel(row.task) != "A" || row.task.Priority == nil {
			continue
		}
		if firstA == nil {
			firstA = &rows[i]
		}
		if !row.hasChild {
			return row, true
		}
	}
	if firstA != nil {
		return *firstA, true
	}
	return taskRow{}, false
}
