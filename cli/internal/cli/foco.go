// `pnn foco [N]` — abre a sessão de foco em tela cheia. A duração padrão é a
// janela de atenção do usuário (mediana das últimas sessões concluídas, RF05).
package cli

import (
	"fmt"
	"math"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/notify"
	"github.com/onofre-matheus/tcc/cli/internal/tui"
	"github.com/onofre-matheus/tcc/cli/internal/ui"
	"github.com/onofre-matheus/tcc/cli/store"
	"github.com/spf13/cobra"
)

// notifyGrace é o quanto a saída espera pelo último aviso de desktop — o fim
// da pausa acontece no mesmo instante em que o programa fecha.
const notifyGrace = 3 * time.Second

func newFocoCmd(a *app.App) *cobra.Command {
	var minutes, pause, checkin int
	cmd := &cobra.Command{
		Use:   "foco [N]",
		Short: "inicia uma sessão de foco (modo VERMELHO)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if minutes <= 0 {
				suggested, err := suggestedMinutes(a)
				if err != nil {
					return err
				}
				minutes = suggested
			}

			var taskID, title string
			if len(args) == 1 {
				var err error
				taskID, title, err = resolveTask(a, args[0])
				if err != nil {
					return err
				}
			}

			model := tui.NewFoco(tui.FocoConfig{
				SessionID:    store.UUIDv7(),
				TaskID:       taskID,
				TaskTitle:    title,
				Minutes:      minutes,
				PauseMinutes: pause,
				CheckinEvery: checkin,
				Emit:         emitTo(a),
				EmitV:        emitVTo(a),
			})
			final, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
			notify.Flush(notifyGrace) // o aviso de fim de pausa sai junto com a saída
			if err != nil {
				return err
			}
			foco := final.(tui.Foco)
			if foco.Err != nil {
				return foco.Err
			}

			th := ui.Theme{On: a.Color}
			if foco.Distractions > 0 {
				fmt.Fprintf(a.Out, "%s anotada(s) nesta sessão\n", plural(foco.Distractions, "distração", "distrações"))
			}
			if foco.WantTriage {
				fmt.Fprintf(a.Out, "→ %s\n", th.Blue("pnn triagem"))
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&minutes, "minutos", "m", 0, "duração em minutos (padrão: janela de atenção)")
	cmd.Flags().IntVar(&pause, "pausa", 5, "minutos de pausa após concluir o bloco")
	cmd.Flags().IntVar(&checkin, "checkin", 0, "pergunta \"na tarefa?\" a cada N minutos (0 = sem)")
	return cmd
}

// suggestedMinutes converte a janela de atenção (segundos) em minutos inteiros.
func suggestedMinutes(a *app.App) (int, error) {
	events, err := a.Store.Events()
	if err != nil {
		return 0, err
	}
	seconds, err := core.AttentionWindowSeconds(events)
	if err != nil {
		return 0, err
	}
	return max(1, int(math.Round(seconds/60))), nil
}

func emitTo(a *app.App) func(string, map[string]any) error {
	return func(eventType string, payload map[string]any) error {
		_, err := a.Store.Append(eventType, payload)
		return err
	}
}

func emitVTo(a *app.App) func(string, int, map[string]any) error {
	return func(eventType string, v int, payload map[string]any) error {
		_, err := a.Store.AppendV(eventType, v, payload)
		return err
	}
}
