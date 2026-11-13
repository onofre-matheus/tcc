// Edição e arquivamento sobre a última tela (catálogo v1.1): `pnn editar N`
// corrige, `pnn apagar N` apaga (tombstone), `pnn arquivar N` tira da fila
// preservando a história. O log segue imutável — como no livro-razão, a
// correção é um lançamento novo, nunca uma rasura.
package cli

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
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

// tagEdit descreve a edição de tags de um cartão: --tags substitui a lista
// (vazia limpa), --tag acrescenta, --untag remove. O evento leva a lista
// final inteira — tags é um campo só no LWW por campo do card.edited.
type tagEdit struct {
	replace    *string
	add, untag []string
}

func (e tagEdit) any() bool { return e.replace != nil || len(e.add) > 0 || len(e.untag) > 0 }

func (e tagEdit) apply(current []string) []string {
	tags := current
	if e.replace != nil {
		tags = core.ParseTags(*e.replace)
	}
	tags = core.NormalizeTags(append(slices.Clone(tags), e.add...))
	remove := core.NormalizeTags(e.untag)
	return slices.DeleteFunc(tags, func(t string) bool { return slices.Contains(remove, t) })
}

func newEditarCmd(a *app.App) *cobra.Command {
	var data, frente, verso, tagsFlag string
	var tags tagEdit
	cmd := &cobra.Command{
		Use:   `editar N ["novo texto"]`,
		Short: "edita o item N da última tela (task.edited, deck.renamed, card.edited, tag.renamed, study.saved, page.edited, book.edited, annotation.edited)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(c *cobra.Command, args []string) error {
			ref, err := resolveRef(a, args[0])
			if err != nil {
				return err
			}
			text := ""
			if len(args) == 2 {
				text = args[1]
			}
			if c.Flags().Changed("tags") {
				tags.replace = &tagsFlag
			}
			if ref.Kind != "card" && ref.Kind != core.NodePage && tags.any() {
				return fmt.Errorf("--tags/--tag/--untag valem para cartões e páginas (deck tem tags fixas na criação)")
			}
			switch ref.Kind {
			case core.NodePage:
				if frente != "" || verso != "" || data != "" {
					return fmt.Errorf("página se edita no editor (`pnn editar %s`), pelo título (`pnn editar %s \"novo título\"`) ou pelas tags (--tag/--untag/--tags)", args[0], args[0])
				}
				if text == "" && !tags.any() {
					return editPage(a, ref.ID)
				}
				return editarPaginaCampos(a, ref.ID, text, tags)
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
			case kindStudy:
				if text == "" {
					return fmt.Errorf(`informe o novo nome: pnn editar %s "Véspera da P2"`, args[0])
				}
				return renomearSessao(a, ref.ID, text)
			case core.NodeTag:
				if frente != "" || verso != "" || data != "" {
					return fmt.Errorf("tag só se renomeia: pnn editar %s \"novo-nome\"", args[0])
				}
				if text == "" {
					return fmt.Errorf(`informe o novo nome: pnn editar %s "so/escalonamento"`, args[0])
				}
				return renomearTag(a, ref.ID, text)
			case "card":
				if text != "" {
					return fmt.Errorf("cartão se edita por campo: use --frente, --verso e/ou --tag/--untag/--tags")
				}
				return editarCartao(a, ref.ID, frente, verso, tags)
			case kindBook:
				if frente != "" || verso != "" || data != "" {
					return fmt.Errorf(`livro se renomeia: pnn editar %s "Título"`, args[0])
				}
				return editarLivro(a, ref.ID, text)
			case kindAnnotation:
				if frente != "" || verso != "" || data != "" {
					return fmt.Errorf(`anotação muda o comentário: pnn editar %s "comentário"`, args[0])
				}
				return editarAnotacao(a, ref.ID, text)
			}
			return fmt.Errorf("o item %s da última tela não é editável", args[0])
		},
	}
	cmd.Flags().StringVar(&data, "data", "", "novo prazo AAAA-MM-DD (tarefa)")
	cmd.Flags().StringVar(&frente, "frente", "", "nova frente (cartão)")
	cmd.Flags().StringVar(&verso, "verso", "", "novo verso (cartão)")
	cmd.Flags().StringVar(&tagsFlag, "tags", "", `substitui as tags do cartão ou da página ("so prova-p2"; "" limpa)`)
	cmd.Flags().StringSliceVar(&tags.add, "tag", nil, "acrescenta tag ao cartão ou à página (repetível)")
	cmd.Flags().StringSliceVar(&tags.untag, "untag", nil, "remove tag do cartão ou da página (repetível)")
	return cmd
}

func newApagarCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "apagar N",
		Short: "apaga a tarefa, cancela o compromisso, apaga a sessão salva ou a anotação N da última tela (tombstone)",
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
			if ref.Kind == kindStudy {
				return apagarSessao(a, ref.ID)
			}
			if ref.Kind == kindAnnotation {
				return apagarAnotacao(a, ref.ID)
			}
			if ref.Kind == kindBook {
				return fmt.Errorf("livro não se apaga (as anotações ficam no log) — arquive com `pnn arquivar %s`", args[0])
			}
			if ref.Kind == core.NodeTag {
				return fmt.Errorf("tag não se apaga — renomeie com `pnn editar %s \"novo-nome\"` ou tire dos cartões com `pnn editar N --untag`", args[0])
			}
			if ref.Kind == core.NodePage {
				return fmt.Errorf("página não se apaga (o texto fica no log) — arquive com `pnn arquivar %s`", args[0])
			}
			return fmt.Errorf("decks e cartões não se apagam (o histórico de estudo fica) — arquive com `pnn arquivar %s`", args[0])
		},
	}
}

func newArquivarCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "arquivar N",
		Short: "arquiva o cartão, deck, página ou livro N da última tela (fora da fila/lista, história preservada)",
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
			case core.NodePage:
				page, err := pageByRef(a, ref.ID)
				if err != nil {
					return err
				}
				if _, err := a.Store.Append("page.archived", map[string]any{"page_id": ref.ID}); err != nil {
					return err
				}
				fmt.Fprintf(a.Out, "✔ página arquivada: %s (fora da lista, texto preservado — `pnn paginas --arquivadas`)\n", page.Title)
				return nil
			case kindBook:
				return arquivarLivro(a, ref.ID)
			case kindAnnotation:
				return fmt.Errorf("anotação não se arquiva — apague com `pnn apagar %s`", args[0])
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

func editarCartao(a *app.App, id, frente, verso string, tags tagEdit) error {
	if frente == "" && verso == "" && !tags.any() {
		return fmt.Errorf("nada para editar — use --frente, --verso e/ou --tag/--untag/--tags")
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
	if tags.any() {
		if next := tags.apply(card.Tags); !slices.Equal(next, card.Tags) {
			payload["tags"] = next
			card.Tags = next
		}
	}
	if len(payload) == 1 {
		fmt.Fprintf(a.Out, "nada mudou: %s\n", card.Front)
		return nil
	}
	if _, err := a.Store.Append("card.edited", payload); err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "✔ cartão editado: %s", card.Front)
	if _, ok := payload["tags"]; ok {
		if len(card.Tags) == 0 {
			fmt.Fprint(a.Out, "  (sem tags)")
		} else {
			fmt.Fprintf(a.Out, "  #%s", strings.Join(card.Tags, " #"))
		}
	}
	fmt.Fprintln(a.Out)
	return nil
}

// renomearTag emite tag.renamed (catálogo 1.4): vale para as ocorrências já
// gravadas da tag e das descendentes (`so/x` → `novo/x`), em cartões,
// páginas, decks e sinapses; renomear para uma tag existente funde as duas.
func renomearTag(a *app.App, from, raw string) error {
	to := core.NormalizeTag(raw)
	if to == "" {
		return fmt.Errorf("nome de tag vazio")
	}
	if to == from {
		fmt.Fprintf(a.Out, "nada mudou: #%s\n", from)
		return nil
	}
	g, err := loadGraph(a)
	if err != nil {
		return err
	}
	under := func(tags []string) bool {
		return slices.ContainsFunc(tags, func(t string) bool { return core.TagUnder(t, from) })
	}
	var parts []string
	cards, pages, decks, links := 0, 0, 0, 0
	for _, card := range g.leitner.Cards {
		if under(card.Tags) {
			cards++
		}
	}
	for _, page := range g.pages.Pages {
		if under(page.Tags) {
			pages++
		}
	}
	for _, deck := range g.decks.Decks {
		if under(deck.Tags) {
			decks++
		}
	}
	for _, link := range g.links.Links {
		for _, end := range []string{link.From, link.To} {
			if n, _ := core.ParseNode(end); n.Kind == core.NodeTag && core.TagUnder(n.ID, from) {
				links++
				break
			}
		}
	}
	for _, c := range []struct {
		n         int
		one, many string
	}{{cards, "cartão", "cartões"}, {pages, "página", "páginas"}, {decks, "deck", "decks"}, {links, "sinapse", "sinapses"}} {
		if c.n > 0 {
			parts = append(parts, plural(c.n, c.one, c.many))
		}
	}
	merged := g.knownTag(to)
	if _, err := a.Store.Append("tag.renamed", map[string]any{"from": from, "to": to}); err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "✔ tag renomeada: #%s → #%s", from, to)
	if len(parts) > 0 {
		fmt.Fprintf(a.Out, " (%s)", strings.Join(parts, ", "))
	}
	if merged {
		fmt.Fprintf(a.Out, " — fundida com a #%s que já existia", to)
	}
	fmt.Fprintln(a.Out)
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
