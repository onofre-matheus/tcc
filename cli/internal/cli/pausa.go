// `pnn pausa` / `pnn volta` — pausa avulsa com duração definida (Safren: sem
// limite, "30 minutos de trabalho" viram "três horas de pausa"). A retroativa
// (--das/--ate) é o lançamento retroativo do livro-razão: pause.logged carrega
// o tempo do domínio no payload; o ts do envelope é só o momento do registro.
package cli

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/ui"
	"github.com/onofre-matheus/tcc/cli/store"
	"github.com/spf13/cobra"
)

func newPausaCmd(a *app.App) *cobra.Command {
	var das, ate, dia string
	cmd := &cobra.Command{
		Use:   "pausa [MIN]",
		Short: "inicia uma pausa cronometrada (encerre com `pnn volta`); retroativa com --das/--ate",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if das != "" || ate != "" {
				return pausaRetroativa(a, das, ate, dia)
			}

			minutes := 5
			if len(args) == 1 {
				n, err := strconv.Atoi(args[0])
				if err != nil || n <= 0 {
					return fmt.Errorf("duração inválida %q (minutos inteiros > 0)", args[0])
				}
				minutes = n
			}

			if open, _, err := openPause(a); err != nil {
				return err
			} else if open != "" {
				return fmt.Errorf("já existe uma pausa aberta — encerre com `pnn volta`")
			}

			if _, err := a.Store.Append("pause.started", map[string]any{
				"pause_id": store.UUIDv7(), "planned_minutes": minutes,
			}); err != nil {
				return err
			}
			th := ui.Theme{On: a.Color}
			fmt.Fprintf(a.Out, "☕ pausa de %d min iniciada — encerre com %s\n", minutes, th.Blue("pnn volta"))
			return nil
		},
	}
	cmd.Flags().StringVar(&das, "das", "", "início HH:MM (pausa retroativa, pause.logged)")
	cmd.Flags().StringVar(&ate, "ate", "", "fim HH:MM (pausa retroativa)")
	cmd.Flags().StringVar(&dia, "dia", "", "data AAAA-MM-DD da pausa retroativa (padrão: hoje)")
	return cmd
}

func newVoltaCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "volta",
		Short: "encerra a pausa aberta (pause.ended)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			pauseID, startedTS, err := openPause(a)
			if err != nil {
				return err
			}
			if pauseID == "" {
				return fmt.Errorf("nenhuma pausa aberta — inicie com `pnn pausa`")
			}
			if _, err := a.Store.Append("pause.ended", map[string]any{"pause_id": pauseID}); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "✔ pausa encerrada (iniciada %s) — de volta ao trabalho 🐘\n", localClock(startedTS, a.TZ))
			return nil
		},
	}
}

// pausaRetroativa registra uma pausa que já aconteceu ("esqueci de apertar o
// botão"): pause.logged com starts_at/ends_at no payload.
func pausaRetroativa(a *app.App, das, ate, dia string) error {
	if das == "" || ate == "" {
		return fmt.Errorf("pausa retroativa exige --das e --ate (HH:MM)")
	}
	if dia == "" {
		today, err := a.Today()
		if err != nil {
			return err
		}
		dia = today
	}
	startsAt, err := localToUTC(dia, das, a.TZ)
	if err != nil {
		return err
	}
	endsAt, err := localToUTC(dia, ate, a.TZ)
	if err != nil {
		return err
	}
	if endsAt <= startsAt {
		return fmt.Errorf("fim %s não vem depois do início %s", ate, das)
	}
	if _, err := a.Store.Append("pause.logged", map[string]any{
		"pause_id": store.UUIDv7(), "starts_at": startsAt, "ends_at": endsAt,
	}); err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "✔ pausa registrada: %s, %s–%s\n", longDatePT(dia), das, ate)
	return nil
}

// openPause devolve a última pausa iniciada e ainda não encerrada (id, ts).
func openPause(a *app.App) (string, string, error) {
	events, err := a.Store.Events()
	if err != nil {
		return "", "", err
	}
	open := map[string]string{} // pause_id → ts de início
	var order []string
	for _, event := range core.Normalize(events) {
		if event.Type != "pause.started" && event.Type != "pause.ended" {
			continue
		}
		var pl struct {
			PauseID string `json:"pause_id"`
		}
		if err := json.Unmarshal(event.Payload, &pl); err != nil {
			return "", "", err
		}
		if event.Type == "pause.started" {
			open[pl.PauseID] = event.TS
			order = append(order, pl.PauseID)
		} else {
			delete(open, pl.PauseID)
		}
	}
	for i := len(order) - 1; i >= 0; i-- {
		if ts, ok := open[order[i]]; ok {
			return order[i], ts, nil
		}
	}
	return "", "", nil
}
