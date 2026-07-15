// Ações sobre tarefas da última tela: `pnn feito N` conclui, `pnn pri N A|B|C`
// repriorizada — números efêmeros no lugar de UUIDs (spec/CLI.md §1).
package cli

import (
	"fmt"
	"strconv"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/ui"
	"github.com/spf13/cobra"
)

func newFeitoCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "feito N",
		Short: "conclui a tarefa N da última tela (task.completed)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			taskID, title, err := resolveTask(a, args[0])
			if err != nil {
				return err
			}
			if _, err := a.Store.Append("task.completed", map[string]any{"task_id": taskID}); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "✔ concluída: %s\n", title)
			return nil
		},
	}
}

func newPriCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "pri N A|B|C",
		Short: "reprioriza a tarefa N da última tela (task.prioritized)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			priority := args[1]
			if !validPriority(priority) {
				return fmt.Errorf("prioridade inválida %q (use A, B ou C)", priority)
			}
			taskID, title, err := resolveTask(a, args[0])
			if err != nil {
				return err
			}
			if _, err := a.Store.Append("task.prioritized", map[string]any{
				"task_id": taskID, "priority": priority,
			}); err != nil {
				return err
			}
			th := ui.Theme{On: a.Color}
			fmt.Fprintf(a.Out, "✔ %s %s\n", th.Badge(priority), title)
			return nil
		},
	}
}

// resolveTask traduz o argumento numérico para (task_id, título).
func resolveTask(a *app.App, arg string) (string, string, error) {
	n, err := strconv.Atoi(arg)
	if err != nil {
		return "", "", fmt.Errorf("esperava o número de uma tarefa da última tela, veio %q", arg)
	}
	ref, err := a.ResolveView(n)
	if err != nil {
		return "", "", err
	}
	if ref.Kind != "task" {
		return "", "", fmt.Errorf("o item %d da última tela não é uma tarefa", n)
	}

	events, err := a.Store.Events()
	if err != nil {
		return "", "", err
	}
	state, err := core.Tasks(events)
	if err != nil {
		return "", "", err
	}
	task, ok := state.Tasks[ref.ID]
	if !ok {
		return "", "", fmt.Errorf("tarefa %d não existe mais — rode `pnn dia`", n)
	}
	return ref.ID, task.Title, nil
}
