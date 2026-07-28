// `pnn sync` — troca eventos com o bucket S3 (spec/SPEC.md §5). Uma operação
// só, idempotente: pode rodar quantas vezes quiser, de quantas máquinas
// quiser, em qualquer ordem.
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/sync"
	"github.com/onofre-matheus/tcc/cli/internal/ui"
	"github.com/spf13/cobra"
)

// syncTimeout: o sync é uma operação de rede num comando interativo — melhor
// falhar rápido e pedir de novo do que pendurar o terminal.
const syncTimeout = 60 * time.Second

func newSyncCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "troca eventos com o bucket (envia os seus, recebe os dos outros)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := sync.ConfigFromEnv()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), syncTimeout)
			defer cancel()

			bucket, err := sync.NewBucket(ctx, cfg)
			if err != nil {
				return err
			}
			result, err := sync.Run(ctx, a.Store, bucket, cfg.Prefix)
			if err != nil {
				return err
			}

			th := ui.Theme{On: a.Color}
			fmt.Fprintf(a.Out, "▲ %d enviado(s) · ▼ %d recebido(s)\n", result.Sent, result.Received)
			if result.Received > 0 {
				fmt.Fprintf(a.Out, "→ %s\n", th.Blue("pnn"))
			}
			return nil
		},
	}
}
