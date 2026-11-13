// `pnn revisar [deck] [--tag x]… [--treino]` — abre a revisão Leitner em tela
// cheia sobre a fila vencida (frágil→consolidada): a sessão personalizada da
// spec §4.11. Deck restringe à subárvore `Pai::Filho`; --tag, às tags (e
// descendentes `/`), herdando as do deck; os dois juntos combinam com E.
// --treino acrescenta os não vencidos depois da fila: a resposta deles vira
// card.practiced, que não mexe na caixa. --ao-redor N estuda a vizinhança de
// um nó no grafo de conexões (ele e os ligados a ele, um salto). --sessao
// reabre um filtro salvo (`pnn sessao`).
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
	var tags, around []string
	var treino bool
	var sessao string
	cmd := &cobra.Command{
		Use:   "revisar [deck]",
		Short: "revisa os cartões vencidos até o timer tocar (modo VERMELHO), filtrando por deck/tag",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			g, err := loadGraph(a)
			if err != nil {
				return err
			}
			leitner, decks := g.leitner, g.decks
			params := a.Params()
			if sessao != "" {
				if len(args)+len(tags)+len(around) > 0 {
					return fmt.Errorf("--sessao já traz o filtro — use a sessão ou deck/--tag/--ao-redor, não os dois")
				}
				studies, err := core.Studies(g.events)
				if err != nil {
					return err
				}
				_, study, err := resolveStudy(a, studies, sessao)
				if err != nil {
					return err
				}
				params.Study = study.Filter()
				treino = treino || study.Practice
			} else if params.Study, err = studyFilter(decks, args, tags); err != nil {
				return err
			}
			for _, arg := range around {
				node, err := g.resolve(a, arg)
				if err != nil {
					return err
				}
				params.Study.Around = append(params.Study.Around, node.Key())
			}
			study, err := core.Study(g.events, params)
			if err != nil {
				return err
			}

			ids := study.Queue
			if treino {
				ids = append(ids, study.Practice...)
			}
			due := len(study.Queue)
			var cards []tui.Card
			for i, id := range ids {
				card := leitner.Cards[id]
				cards = append(cards, tui.Card{
					ID:              id,
					Front:           card.Front,
					Back:            card.Back,
					SourceURL:       card.SourceURL,
					SourceTitle:     card.SourceTitle,
					LastExplanation: card.LastExplanation,
					LastFrame:       card.LastFrame,
					Box:             card.Box,
					Tags:            core.EffectiveTags(card, decks),
					Links:           g.linkLines(core.Node{Kind: core.NodeCard, ID: id}),
					Practice:        i >= due,
				})
			}
			th := ui.Theme{On: a.Color}
			if len(cards) == 0 {
				switch {
				case treino:
					fmt.Fprintf(a.Out, "%s\n", th.Dim("nenhum cartão ativo casa com o filtro"))
				case len(study.Practice) > 0:
					fmt.Fprintf(a.Out, "%s\n", th.Dim(fmt.Sprintf(
						"nenhum cartão vencido 🐘 — %s para treinar sem mexer nas caixas: repita com --treino",
						plural(len(study.Practice), "cartão", "cartões"))))
				default:
					fmt.Fprintf(a.Out, "%s\n", th.Dim("nenhum cartão vencido 🐘 — volte amanhã"))
				}
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
				fmt.Fprintf(a.Out, "✔ %s: %d acerto(s) · %d erro(s)",
					plural(done, "cartão revisado", "cartões revisados"), revisar.Correct, revisar.Wrong)
				if revisar.Practiced > 0 {
					fmt.Fprintf(a.Out, " · %d de treino (caixa intacta)", revisar.Practiced)
				}
				fmt.Fprintln(a.Out)
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&minutes, "minutos", "m", 0, "duração em minutos (padrão: janela de atenção)")
	cmd.Flags().IntVar(&pause, "pausa", 5, "minutos de pausa após concluir o bloco")
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "estuda só cartões com a tag ou descendentes (repetível: casa qualquer uma)")
	cmd.Flags().BoolVar(&treino, "treino", false, "depois dos vencidos, treina os não vencidos (card.practiced: não altera a caixa)")
	cmd.Flags().StringVar(&sessao, "sessao", "", "usa o filtro de uma sessão salva (N de `pnn sessoes` ou o nome)")
	cmd.Flags().StringSliceVar(&around, "ao-redor", nil, "estuda a vizinhança do nó (N da última tela, '#tag', deck ou livro) e dos ligados a ele (repetível)")
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

// studyFilter monta o filtro da sessão personalizada a partir do argumento de
// deck (nome → ids da subárvore) e das --tag (normalizadas no core).
func studyFilter(decks core.DecksState, args, tags []string) (core.StudyFilter, error) {
	filter := core.StudyFilter{Tags: core.NormalizeTags(tags)}
	if len(tags) > 0 && len(filter.Tags) == 0 {
		return filter, fmt.Errorf("--tag vazia — informe o nome da tag (ex.: --tag so/escalonamento)")
	}
	if len(args) == 1 {
		subtree := deckSubtree(decks, args[0])
		if len(subtree) == 0 {
			return filter, fmt.Errorf("deck %q não encontrado — veja `pnn decks`", args[0])
		}
		for id := range subtree {
			filter.Decks = append(filter.Decks, id)
		}
	}
	return filter, nil
}
