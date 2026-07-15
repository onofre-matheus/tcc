// Edição e arquivamento sobre a última tela (catálogo v1.1): `pnn editar N`
// corrige, `pnn apagar N` apaga (tombstone), `pnn arquivar N` tira da fila
// preservando a história. O log segue imutável — como no livro-razão, a
// correção é um lançamento novo, nunca uma rasura.
package cli

import (
	"fmt"
	"strconv"
	"time"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/spf13/cobra"
)

// resolveRef traduz o argumento numérico para o item da última tela.
func resolveRef(a *app.App, arg string) (app.ViewRef, error) {
	n, err := strconv.Atoi(arg)
	if err != nil {
		return app.ViewRef{}, fmt.Errorf("esperava o número de um item da última tela, veio %q", arg)
	}
	return a.ResolveView(n)
}

func newEditarCmd(a *app.App) *cobra.Command {
	var data, frente, verso string
	cmd := &cobra.Command{
		Use:   `editar N ["novo texto"]`,
		Short: "edita o item N da última tela (task.edited, deck.renamed, card.edited)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			ref, err := resolveRef(a, args[0])
			if err != nil {
				return err
			}
			text := ""
			if len(args) == 2 {
				text = args[1]
			}
			switch ref.Kind {
			case "task":
				if frente != "" || verso != "" {
					return fmt.Errorf("--frente/--verso valem para cartões; tarefa se edita com `pnn editar N \"novo título\"` e/ou --data")
				}
				return editarTarefa(a, ref.ID, text, data)
			case "deck":
				if text == "" {
					return fmt.Errorf(`informe o novo nome: pnn editar %s "Pai::Filho"`, args[0])
				}
				return renomearDeck(a, ref.ID, text)
			case "card":
				if text != "" {
					return fmt.Errorf("cartão se edita por campo: use --frente e/ou --verso")
				}
				return editarCartao(a, ref.ID, frente, verso)
			}
			return fmt.Errorf("o item %s da última tela não é editável", args[0])
		},
	}
	cmd.Flags().StringVar(&data, "data", "", "novo prazo AAAA-MM-DD (tarefa)")
	cmd.Flags().StringVar(&frente, "frente", "", "nova frente (cartão)")
	cmd.Flags().StringVar(&verso, "verso", "", "novo verso (cartão)")
	return cmd
}

func newApagarCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "apagar N",
		Short: "apaga a tarefa ou cancela o compromisso N da última tela (tombstone)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			ref, err := resolveRef(a, args[0])
			if err != nil {
				return err
			}
			switch ref.Kind {
			case "task":
				taskID, title, err := resolveTask(a, args[0])
				if err != nil {
					return err
				}
				if _, err := a.Store.Append("task.deleted", map[string]any{"task_id": taskID}); err != nil {
					return err
				}
				fmt.Fprintf(a.Out, "✗ apagada: %s\n", title)
				return nil
			case "appointment":
				appt, err := apptByRef(a, ref.ID)
				if err != nil {
					return err
				}
				if _, err := a.Store.Append("appointment.cancelled", map[string]any{"appointment_id": ref.ID}); err != nil {
					return err
				}
				fmt.Fprintf(a.Out, "✗ cancelado: %s\n", appt.Title)
				return nil
			}
			return fmt.Errorf("decks e cartões não se apagam (o histórico de estudo fica) — arquive com `pnn arquivar %s`", args[0])
		},
	}
}

func newArquivarCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "arquivar N",
		Short: "arquiva o cartão ou deck N da última tela (fora da fila, história preservada)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			ref, err := resolveRef(a, args[0])
			if err != nil {
				return err
			}
			switch ref.Kind {
			case "deck":
				deck, err := deckByRef(a, ref.ID)
				if err != nil {
					return err
				}
				if _, err := a.Store.Append("deck.archived", map[string]any{"deck_id": ref.ID}); err != nil {
					return err
				}
				fmt.Fprintf(a.Out, "✔ deck arquivado: %s (cartões fora da fila, histórico preservado)\n", deck.Name)
				return nil
			case "card":
				card, err := cardByRef(a, ref.ID)
				if err != nil {
					return err
				}
				if _, err := a.Store.Append("card.archived", map[string]any{"card_id": ref.ID}); err != nil {
					return err
				}
				fmt.Fprintf(a.Out, "✔ cartão arquivado: %s\n", card.Front)
				return nil
			case "task":
				return fmt.Errorf("tarefa não se arquiva — conclua com `pnn feito %s` ou apague com `pnn apagar %s`", args[0], args[0])
			case "appointment":
				return fmt.Errorf("compromisso se cancela com `pnn apagar %s`", args[0])
			}
			return fmt.Errorf("o item %s da última tela não é arquivável", args[0])
		},
	}
}

func editarTarefa(a *app.App, id, title, data string) error {
	events, err := a.Store.Events()
	if err != nil {
		return err
	}
	state, err := core.Tasks(events)
	if err != nil {
		return err
	}
	task, ok := state.Tasks[id]
	if !ok {
		return fmt.Errorf("a tarefa não existe mais — rode `pnn dia`")
	}

	payload := map[string]any{"task_id": id}
	if title != "" {
		payload["title"] = title
		task.Title = title
	}
	if data != "" {
		if _, err := time.Parse("2006-01-02", data); err != nil {
			return fmt.Errorf("data inválida %q (use AAAA-MM-DD)", data)
		}
		payload["due_date"] = data
	}
	if len(payload) == 1 {
		return fmt.Errorf("nada para editar — informe um novo título e/ou --data")
	}
	if _, err := a.Store.Append("task.edited", payload); err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "✔ editada: %s\n", task.Title)
	return nil
}

func renomearDeck(a *app.App, id, name string) error {
	deck, err := deckByRef(a, id)
	if err != nil {
		return err
	}
	if _, err := a.Store.Append("deck.renamed", map[string]any{"deck_id": id, "name": name}); err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "✔ deck renomeado: %s → %s\n", deck.Name, name)
	return nil
}

func editarCartao(a *app.App, id, frente, verso string) error {
	if frente == "" && verso == "" {
		return fmt.Errorf("nada para editar — use --frente e/ou --verso")
	}
	card, err := cardByRef(a, id)
	if err != nil {
		return err
	}
	payload := map[string]any{"card_id": id}
	if frente != "" {
		payload["front"] = frente
		card.Front = frente
	}
	if verso != "" {
		payload["back"] = verso
	}
	if _, err := a.Store.Append("card.edited", payload); err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "✔ cartão editado: %s\n", card.Front)
	return nil
}

// --- lookups por id (existência + rótulo para a confirmação) ---

func deckByRef(a *app.App, id string) (core.Deck, error) {
	events, err := a.Store.Events()
	if err != nil {
		return core.Deck{}, err
	}
	state, err := core.Decks(events)
	if err != nil {
		return core.Deck{}, err
	}
	deck, ok := state.Decks[id]
	if !ok {
		return core.Deck{}, fmt.Errorf("o deck não existe mais — rode `pnn decks`")
	}
	return deck, nil
}

func cardByRef(a *app.App, id string) (core.LeitnerCard, error) {
	events, err := a.Store.Events()
	if err != nil {
		return core.LeitnerCard{}, err
	}
	state, err := core.Leitner(events, a.Params())
	if err != nil {
		return core.LeitnerCard{}, err
	}
	card, ok := state.Cards[id]
	if !ok {
		return core.LeitnerCard{}, fmt.Errorf("o cartão não existe mais — rode `pnn cartas`")
	}
	return card, nil
}

func apptByRef(a *app.App, id string) (core.Appointment, error) {
	events, err := a.Store.Events()
	if err != nil {
		return core.Appointment{}, err
	}
	state, err := core.Calendar(events, a.Params())
	if err != nil {
		return core.Appointment{}, err
	}
	appt, ok := state.Appointments[id]
	if !ok {
		return core.Appointment{}, fmt.Errorf("o compromisso não existe mais — rode `pnn agenda`")
	}
	return appt, nil
}
