// `pnn cartas [deck]` — a tela de manutenção do acervo: lista os cartões com
// números efêmeros para `pnn editar N --frente/--verso` e `pnn arquivar N`.
// Com deck, restringe à subárvore `Pai::Filho` (mesma regra do revisar).
package cli

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/ui"
	"github.com/spf13/cobra"
)

func newCartasCmd(a *app.App) *cobra.Command {
	var asJSON, arquivados bool
	cmd := &cobra.Command{
		Use:   "cartas [deck]",
		Short: "lista os cartões para editar ou arquivar",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			events, err := a.Store.Events()
			if err != nil {
				return err
			}
			decks, err := core.Decks(events)
			if err != nil {
				return err
			}
			leitner, err := core.Leitner(events, a.Params())
			if err != nil {
				return err
			}

			var allowed map[string]bool
			if len(args) == 1 {
				if allowed = deckSubtree(decks, args[0]); len(allowed) == 0 {
					return fmt.Errorf("deck %q não encontrado — veja `pnn decks`", args[0])
				}
			}

			if asJSON {
				raw, err := json.MarshalIndent(map[string]any{"decks": decks.Decks, "leitner": leitner}, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(a.Out, string(raw))
				return nil
			}

			type row struct {
				id   string
				card core.LeitnerCard
				deck string
			}
			var rows []row
			for id, card := range leitner.Cards {
				if allowed != nil && !allowed[card.DeckID] {
					continue
				}
				if (card.Archived || decks.Decks[card.DeckID].Archived) && !arquivados {
					continue
				}
				rows = append(rows, row{id: id, card: card, deck: decks.Decks[card.DeckID].Name})
			}
			slices.SortFunc(rows, func(x, y row) int {
				if x.deck != y.deck {
					return strings.Compare(x.deck, y.deck)
				}
				if x.card.Due != y.card.Due {
					return strings.Compare(x.card.Due, y.card.Due)
				}
				return strings.Compare(x.id, y.id)
			})

			th := ui.Theme{On: a.Color}
			if len(rows) == 0 {
				fmt.Fprintf(a.Out, "%s\n", th.Dim("nenhum cartão — crie com `pnn carta`"))
				return a.SaveView(nil)
			}

			refs := make([]app.ViewRef, len(rows))
			lastDeck := ""
			for i, r := range rows {
				refs[i] = app.ViewRef{Kind: "card", ID: r.id}
				if r.deck != lastDeck {
					if lastDeck != "" {
						fmt.Fprintln(a.Out)
					}
					fmt.Fprintf(a.Out, " %s\n", th.Bold(r.deck))
					lastDeck = r.deck
				}
				status := fmt.Sprintf("caixa %d · vence %s", r.card.Box, r.card.Due)
				if r.card.Archived || decks.Decks[r.card.DeckID].Archived {
					status = "arquivado"
				}
				fmt.Fprintf(a.Out, " %d  %s  %s\n", i+1, r.card.Front, th.Dim(status))
			}
			return a.SaveView(refs)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emite as projeções em JSON")
	cmd.Flags().BoolVar(&arquivados, "arquivados", false, "inclui cartões e decks arquivados")
	return cmd
}
