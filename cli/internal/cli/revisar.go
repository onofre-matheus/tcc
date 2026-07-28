// `pnn revisar [deck]` — abre a revisão Leitner em tela cheia sobre a fila
// vencida (frágil→consolidada). Com deck, restringe à subárvore `Pai::Filho`.
package cli

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/notify"
	"github.com/onofre-matheus/tcc/cli/internal/tui"
	"github.com/onofre-matheus/tcc/cli/internal/ui"
	"github.com/onofre-matheus/tcc/cli/store"
	"github.com/spf13/cobra"
)

func newRevisarCmd(a *app.App) *cobra.Command {
	var minutes, pause int
	cmd := &cobra.Command{
		Use:   "revisar [deck]",
		Short: "revisa os cartões vencidos até o timer tocar (modo VERMELHO)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			events, err := a.Store.Events()
			if err != nil {
				return err
			}
			leitner, err := core.Leitner(events, a.Params())
			if err != nil {
				return err
			}

			var allowed map[string]bool
			if len(args) == 1 {
				decks, err := core.Decks(events)
				if err != nil {
					return err
				}
				if allowed = deckSubtree(decks, args[0]); len(allowed) == 0 {
					return fmt.Errorf("deck %q não encontrado — veja `pnn decks`", args[0])
				}
			}

			var cards []tui.Card
			for _, id := range leitner.Queue {
				card := leitner.Cards[id]
				if allowed != nil && !allowed[card.DeckID] {
					continue
				}
				cards = append(cards, tui.Card{
					ID:              id,
					Front:           card.Front,
					Back:            card.Back,
					SourceURL:       card.SourceURL,
					SourceTitle:     card.SourceTitle,
					LastExplanation: card.LastExplanation,
					LastFrame:       card.LastFrame,
					Box:             card.Box,
				})
			}
			th := ui.Theme{On: a.Color}
			if len(cards) == 0 {
				fmt.Fprintf(a.Out, "%s\n", th.Dim("nenhum cartão vencido 🐘 — volte amanhã"))
				return nil
			}

			if minutes <= 0 {
				suggested, err := suggestedMinutes(a)
				if err != nil {
					return err
				}
				minutes = suggested
			}

			model := tui.NewRevisar(tui.RevisarConfig{
				SessionID:    store.UUIDv7(),
				Cards:        cards,
				Minutes:      minutes,
				PauseMinutes: pause,
				Emit:         emitTo(a),
				EmitV:        emitVTo(a),
			})
			final, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
			notify.Flush(notifyGrace) // o aviso de fim de pausa sai junto com a saída
			if err != nil {
				return err
			}
			revisar := final.(tui.Revisar)
			if revisar.Err != nil {
				return revisar.Err
			}

			if done := revisar.Correct + revisar.Wrong; done > 0 {
				fmt.Fprintf(a.Out, "✔ %s: %d acerto(s) · %d erro(s)\n",
					plural(done, "cartão revisado", "cartões revisados"), revisar.Correct, revisar.Wrong)
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&minutes, "minutos", "m", 0, "duração em minutos (padrão: janela de atenção)")
	cmd.Flags().IntVar(&pause, "pausa", 5, "minutos de pausa após concluir o bloco")
	return cmd
}

// deckSubtree resolve um nome de deck para o conjunto de deck_ids da sua
// subárvore (`Pai` inclui `Pai::Filho`; hierarquia por convenção de nome,
// spec/SPEC.md §4.5).
func deckSubtree(state core.DecksState, name string) map[string]bool {
	ids := map[string]bool{}
	for id, deck := range state.Decks {
		if deck.Name == name || strings.HasPrefix(deck.Name, name+"::") {
			ids[id] = true
		}
	}
	return ids
}
