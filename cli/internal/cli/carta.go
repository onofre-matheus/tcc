// `pnn carta [deck] [--tag x]…` — abre a criação de cartões em série. Sem
// decks (ou com --sem-deck), o cartão nasce solto, só com tags.
package cli

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/tui"
	"github.com/spf13/cobra"
)

func newCartaCmd(a *app.App) *cobra.Command {
	var tags []string
	var noDeck bool
	cmd := &cobra.Command{
		Use:   "carta [deck]",
		Short: "cria cartões em série (frente → verso → fonte → tags, opcionais as duas últimas)",
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
			options := deckOptions(decks)
			cfg := tui.CartaConfig{
				Decks:  options,
				NoDeck: noDeck,
				Tags:   core.NormalizeTags(tags),
				Emit:   emitTo(a),
				EmitV:  emitVTo(a),
			}
			if len(args) == 1 {
				if noDeck {
					return fmt.Errorf("escolha um deck OU --sem-deck, não os dois")
				}
				i := slices.IndexFunc(options, func(o tui.DeckOption) bool { return o.Name == args[0] })
				if i < 0 {
					return fmt.Errorf("deck %q não encontrado — veja `pnn decks`", args[0])
				}
				cfg.DeckID, cfg.DeckName = options[i].ID, options[i].Name
			}

			final, err := tea.NewProgram(tui.NewCarta(cfg), tea.WithAltScreen()).Run()
			if err != nil {
				return err
			}
			carta := final.(tui.Carta)
			if carta.Err != nil {
				return carta.Err
			}
			if carta.Created > 0 {
				fmt.Fprintf(a.Out, "✔ %s\n", plural(carta.Created, "cartão criado", "cartões criados"))
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "tag inicial da série (repetível; editável no formulário)")
	cmd.Flags().BoolVar(&noDeck, "sem-deck", false, "cria cartões soltos, organizados só por tags")
	return cmd
}

// deckOptions lista os decks em ordem alfabética de caminho (a árvore `::`
// fica agrupada naturalmente).
func deckOptions(state core.DecksState) []tui.DeckOption {
	options := make([]tui.DeckOption, 0, len(state.Decks))
	for id, deck := range state.Decks {
		options = append(options, tui.DeckOption{ID: id, Name: deck.Name})
	}
	slices.SortFunc(options, func(a, b tui.DeckOption) int {
		return strings.Compare(a.Name, b.Name)
	})
	return options
}
