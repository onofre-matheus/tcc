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

// notifyGrace é o quanto a saída espera pelo último aviso de desktop ainda em
// voo — quem sai logo depois de um alarme não pode cortá-lo.
const notifyGrace = 3 * time.Second

func newFocoCmd(a *app.App) *cobra.Command {
	var minutes, pause, checkin int
	var relogio bool
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
			var pages []tui.LinkedPage
			var books []tui.LinkedBook
			if len(args) == 1 {
				var err error
				taskID, title, err = resolveTask(a, args[0])
				if err != nil {
					return err
				}
				if pages, books, err = taskLinked(a, taskID); err != nil {
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
				Pages:        pages,
				EditPage:     focusEditPage(a),
				Write:        writeActions{a},
				Read:         newReadActions(a),
				Books:        books,
				StartWriting: !relogio,
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
	cmd.Flags().BoolVar(&relogio, "relogio", false, "abre no relógio grande mesmo com página ligada (Ctrl+O escreve depois)")
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

// taskLinked: as páginas e os livros (não arquivados) ligados à tarefa por
// sinapse — o contexto da tarefa, que a tela de foco mostra e abre (Ctrl+O:
// a página na escrita, o livro na leitura; Ctrl+E: a página no $EDITOR).
func taskLinked(a *app.App, taskID string) ([]tui.LinkedPage, []tui.LinkedBook, error) {
	g, err := loadGraph(a)
	if err != nil {
		return nil, nil, err
	}
	var pages []tui.LinkedPage
	var books []tui.LinkedBook
	for _, key := range g.links.Neighbors[core.Node{Kind: core.NodeTask, ID: taskID}.Key()] {
		node, ok := core.ParseNode(key)
		if !ok {
			continue
		}
		switch node.Kind {
		case core.NodePage:
			if page, ok := g.pages.Pages[node.ID]; ok && !page.Archived {
				pages = append(pages, tui.LinkedPage{ID: node.ID, Title: page.Title})
			}
		case core.NodeBook:
			if book, ok := g.books.Books[node.ID]; ok && !book.Archived {
				books = append(books, tui.LinkedBook{ID: node.ID, Title: book.Title})
			}
		}
	}
	return pages, books, nil
}

// focusEditPage edita a página de dentro do foco: o resultado vira a linha de
// status da tela, em vez de ser impresso por cima dela.
func focusEditPage(a *app.App) func(id string) (string, error) {
	return func(id string) (string, error) {
		edit, err := editPageBody(a, id)
		if err != nil {
			return "", err
		}
		status := edit.status
		if edit.empty {
			status += " (ficou vazia — a versão anterior continua no log)"
		}
		for _, w := range edit.warnings {
			status = "⚠ " + w + " · " + status
		}
		return status, nil
	}
}
