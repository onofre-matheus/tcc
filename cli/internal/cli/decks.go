// `pnn decks` lista os decks com vencidos por deck; `pnn deck "Pai::Filho"`
// cria um deck (hierarquia por convenção de nome, spec/SPEC.md §4.5).
package cli

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/ui"
	"github.com/onofre-matheus/tcc/cli/store"
	"github.com/spf13/cobra"
)

func newDecksCmd(a *app.App) *cobra.Command {
	var asJSON, arquivados bool
	cmd := &cobra.Command{
		Use:   "decks",
		Short: "lista os decks e quantos cartões venceram em cada um",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
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

			if asJSON {
				raw, err := json.MarshalIndent(map[string]any{"decks": decks.Decks, "leitner": leitner}, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(a.Out, string(raw))
				return nil
			}

			th := ui.Theme{On: a.Color}
			if len(decks.Decks) == 0 {
				fmt.Fprintf(a.Out, "%s\n", th.Dim("nenhum deck — crie com `pnn deck \"Nome\"`"))
				return nil
			}

			dueByDeck := map[string]int{}
			for _, cardID := range leitner.Queue {
				dueByDeck[leitner.Cards[cardID].DeckID]++
			}

			ids := make([]string, 0, len(decks.Decks))
			for id := range decks.Decks {
				ids = append(ids, id)
			}
			slices.SortFunc(ids, func(x, y string) int {
				return strings.Compare(decks.Decks[x].Name, decks.Decks[y].Name)
			})

			var refs []app.ViewRef
			for _, id := range ids {
				deck := decks.Decks[id]
				if deck.Archived && !arquivados {
					continue
				}
				refs = append(refs, app.ViewRef{Kind: "deck", ID: id})
				line := fmt.Sprintf(" %d %s", len(refs), deck.Name)
				if deck.Archived {
					line += "  " + th.Dim("(arquivado)")
				}
				if due := dueByDeck[id]; due > 0 {
					line += "  " + th.Red(plural(due, "vencido", "vencidos"))
				}
				if len(deck.Tags) > 0 {
					line += "  " + th.Dim("#"+strings.Join(deck.Tags, " #"))
				}
				fmt.Fprintln(a.Out, line)
			}
			if len(refs) == 0 {
				fmt.Fprintf(a.Out, "%s\n", th.Dim("todos os decks estão arquivados — veja com `pnn decks --arquivados`"))
			}
			return a.SaveView(refs)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emite as projeções em JSON")
	cmd.Flags().BoolVar(&arquivados, "arquivados", false, "inclui decks arquivados")
	return cmd
}

func newDeckCmd(a *app.App) *cobra.Command {
	var tags []string
	cmd := &cobra.Command{
		Use:   `deck "Pai::Filho"`,
		Short: "cria um deck (deck.created)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if _, err := a.Store.Append("deck.created", map[string]any{
				"deck_id": store.UUIDv7(), "name": args[0], "tags": tags,
			}); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "✔ deck criado: %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "tags transversais (repetível)")
	return cmd
}
