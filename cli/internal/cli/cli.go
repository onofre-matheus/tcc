// Raiz da CLI pnn (spec/CLI.md). `pnn` sem argumentos responde "e agora?" com a
// tela do dia; cada subcomando vive no próprio arquivo.
package cli

import (
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/spf13/cobra"
)

func NewRoot(a *app.App) *cobra.Command {
	root := &cobra.Command{
		Use:           "pnn",
		Short:         "Procrastina Não 🐘 — assistente de consistência nos estudos",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDia(a, false)
		},
	}
	root.CompletionOptions.HiddenDefaultCmd = true
	root.AddCommand(
		newDiaCmd(a),
		newTCmd(a),
		newNCmd(a),
		newCCmd(a),
		newFeitoCmd(a),
		newPriCmd(a),
		newFocoCmd(a),
		newRevisarCmd(a),
		newCaixaCmd(a),
		newTriagemCmd(a),
		newCartaCmd(a),
		newCartasCmd(a),
		newDecksCmd(a),
		newDeckCmd(a),
		newAgendaCmd(a),
		newEditarCmd(a),
		newApagarCmd(a),
		newArquivarCmd(a),
		newPausaCmd(a),
		newVoltaCmd(a),
		newSemanaCmd(a),
		newSyncCmd(a),
		newProcrastinaCmd(a),
	)
	return root
}
