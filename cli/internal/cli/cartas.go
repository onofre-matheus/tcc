// `pnn cartas [deck] [--tag x]…` — a tela de manutenção do acervo: lista os
// cartões com números efêmeros para `pnn editar N --frente/--verso/--tag` e
// `pnn arquivar N`. Deck e tags filtram com a mesma regra da sessão
// personalizada (spec §4.11: subárvore `::`, tag inclui as descendentes `/`),
// mas valem também para os não vencidos. Cartões soltos vão em "(sem deck)".
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
	var tags []string
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
			links, err := core.Links(events)
			if err != nil {
				return err
			}

			filter, err := studyFilter(decks, args, tags)
			if err != nil {
				return err
			}
			match := core.StudyMatcher(decks, core.LinksState{}, filter)

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
				if !match(id, card) {
					continue
				}
				if (card.Archived || decks.Decks[card.Deck()].Archived) && !arquivados {
					continue
				}
				card.Tags = core.EffectiveTags(card, decks)
				rows = append(rows, row{id: id, card: card, deck: decks.Decks[card.Deck()].Name})
			}
			slices.SortFunc(rows, func(x, y row) int {
				if x.deck != y.deck {
					if x.deck == "" || y.deck == "" { // soltos por último
						return strings.Compare(y.deck, x.deck)
					}
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
				if i == 0 || r.deck != lastDeck {
					if i > 0 {
						fmt.Fprintln(a.Out)
					}
					title := r.deck
					if title == "" {
						title = "(sem deck)"
					}
					fmt.Fprintf(a.Out, " %s\n", th.Bold(title))
					lastDeck = r.deck
				}
				status := fmt.Sprintf("caixa %d · vence %s", r.card.Box, r.card.Due)
				if r.card.Archived || decks.Decks[r.card.Deck()].Archived {
					status = "arquivado"
				}
				if len(r.card.Tags) > 0 {
					status = "#" + strings.Join(r.card.Tags, " #") + " · " + status
				}
				if n := len(links.Neighbors[core.NodeCard+":"+r.id]); n > 0 {
					status += fmt.Sprintf(" · ⚡%d", n)
				}
				fmt.Fprintf(a.Out, " %d  %s  %s\n", i+1, r.card.Front, th.Dim(status))
			}
			return a.SaveView(refs)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emite as projeções em JSON")
	cmd.Flags().BoolVar(&arquivados, "arquivados", false, "inclui cartões e decks arquivados")
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "filtra por tag, incluindo as descendentes (repetível: casa qualquer uma)")
	return cmd
}
