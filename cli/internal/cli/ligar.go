// Sinapses — as conexões explícitas (catálogo 1.4, spec §3 e §4.12), estilo
// Obsidian; o nome nas telas vem do 1.6: cartões e páginas são os neurônios,
// a sinapse é a ligação entre eles. `pnn ligar A B` cria uma sinapse entre
// duas coisas (cartão, página, tarefa, deck, livro); com uma "#tag" de um lado, classifica o
// cartão ou a página (a tag não é ponta de sinapse). `pnn desligar` desfaz e
// `pnn sinapses [A]` mostra o grafo local. Um nó é o número da última tela,
// uma "#tag", o nome exato de um deck ou o título de uma página ou de um
// livro (sem caixa nem acento); sem "#", um nome que não é deck, página nem
// livro vale como tag se ela já existir.
package cli

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/ui"
	"github.com/onofre-matheus/tcc/cli/store"
	"github.com/spf13/cobra"
)

// graph reúne as projeções que nomeiam e resolvem nós.
type graph struct {
	events  []core.Event
	decks   core.DecksState
	leitner core.LeitnerState
	links   core.LinksState
	tags    core.TagsState
	pages   core.PagesState
	tasks   core.TasksState
	books   core.BooksState
}

func loadGraph(a *app.App) (graph, error) {
	var g graph
	var err error
	if g.events, err = a.Store.Events(); err != nil {
		return g, err
	}
	if g.decks, err = core.Decks(g.events); err != nil {
		return g, err
	}
	if g.leitner, err = core.Leitner(g.events, a.Params()); err != nil {
		return g, err
	}
	if g.links, err = core.Links(g.events); err != nil {
		return g, err
	}
	if g.tags, err = core.Tags(g.events, a.Params()); err != nil {
		return g, err
	}
	if g.pages, err = core.Pages(g.events); err != nil {
		return g, err
	}
	if g.books, err = core.Books(g.events); err != nil {
		return g, err
	}
	g.tasks, err = core.Tasks(g.events)
	return g, err
}

// knownTag: a tag está em algum cartão/deck ativo ou conexão — ou é ancestral
// implícito de uma delas (`so` quando só existe `so/threads`).
func (g graph) knownTag(tag string) bool {
	known := slices.Clone(g.tags.Names)
	for _, key := range g.links.Nodes {
		if n, _ := core.ParseNode(key); n.Kind == core.NodeTag {
			known = append(known, n.ID)
		}
	}
	return slices.ContainsFunc(known, func(t string) bool { return core.TagUnder(t, tag) })
}

func (g graph) resolve(a *app.App, arg string) (core.Node, error) {
	if n, err := strconv.Atoi(arg); err == nil {
		ref, err := a.ResolveView(n)
		if err != nil {
			return core.Node{}, err
		}
		switch ref.Kind {
		case core.NodeCard:
			if _, ok := g.leitner.Cards[ref.ID]; !ok {
				return core.Node{}, fmt.Errorf("o cartão não existe mais — rode `pnn cartas`")
			}
		case core.NodeDeck:
			if _, ok := g.decks.Decks[ref.ID]; !ok {
				return core.Node{}, fmt.Errorf("o deck não existe mais — rode `pnn decks`")
			}
		case core.NodePage:
			if _, ok := g.pages.Pages[ref.ID]; !ok {
				return core.Node{}, fmt.Errorf("a página não existe mais — rode `pnn paginas`")
			}
		case core.NodeTask:
			if _, ok := g.tasks.Tasks[ref.ID]; !ok {
				return core.Node{}, fmt.Errorf("a tarefa não existe mais — rode `pnn dia`")
			}
		case core.NodeBook:
			if _, ok := g.books.Books[ref.ID]; !ok {
				return core.Node{}, fmt.Errorf("o livro não existe mais — rode `pnn livros`")
			}
		case core.NodeTag:
		default:
			return core.Node{}, fmt.Errorf("o item %d da última tela não se liga — só cartões (pnn cartas), páginas (pnn paginas), tarefas (pnn dia), livros (pnn livros), decks (pnn decks) e tags (pnn tags)", n)
		}
		return core.Node{Kind: ref.Kind, ID: ref.ID}, nil
	}

	tag := core.NormalizeTag(arg)
	if strings.HasPrefix(strings.TrimSpace(arg), "#") {
		if tag == "" {
			return core.Node{}, fmt.Errorf("tag vazia — informe o nome depois do #")
		}
		return core.Node{Kind: core.NodeTag, ID: tag}, nil
	}
	ids := make([]string, 0, len(g.decks.Decks))
	for id, deck := range g.decks.Decks {
		if deck.Name == arg {
			ids = append(ids, id)
		}
	}
	if len(ids) > 0 {
		slices.SortFunc(ids, func(x, y string) int { // ativo antes de arquivado
			return cmp.Or(compareBool(g.decks.Decks[x].Archived, g.decks.Decks[y].Archived), strings.Compare(x, y))
		})
		return core.Node{Kind: core.NodeDeck, ID: ids[0]}, nil
	}
	if id, err := g.pageByTitle(arg); err != nil {
		return core.Node{}, err
	} else if id != "" {
		return core.Node{Kind: core.NodePage, ID: id}, nil
	}
	if id, err := g.bookByTitle(arg); err != nil {
		return core.Node{}, err
	} else if id != "" {
		return core.Node{Kind: core.NodeBook, ID: id}, nil
	}
	if tag != "" && g.knownTag(tag) {
		return core.Node{Kind: core.NodeTag, ID: tag}, nil
	}
	// Título de livro costuma ser longo: um trecho que case com um livro só
	// basta — depois das tags, para não roubar o nome de uma.
	if id, err := g.bookByWords(arg); err != nil {
		return core.Node{}, err
	} else if id != "" {
		return core.Node{Kind: core.NodeBook, ID: id}, nil
	}
	return core.Node{}, fmt.Errorf("%q não é número da última tela, deck, página, livro nem tag conhecida — tag nova vai com # entre aspas: '#%s'", arg, tag)
}

// bookByWords: o único livro não arquivado com todas as palavras no título.
func (g graph) bookByWords(arg string) (string, error) {
	words := strings.Fields(fold(arg))
	if len(words) == 0 {
		return "", nil
	}
	var ids []string
	for _, id := range g.books.Order {
		if containsAll(fold(g.books.Books[id].Title), words) {
			ids = append(ids, id)
		}
	}
	switch len(ids) {
	case 0:
		return "", nil
	case 1:
		return ids[0], nil
	}
	return "", fmt.Errorf("%d livros casam com %q — use o número de `pnn livros`", len(ids), arg)
}

// bookByTitle: o livro não arquivado com esse título exato, sem caixa nem
// acento — como pageByTitle.
func (g graph) bookByTitle(arg string) (string, error) {
	want := strings.Join(strings.Fields(fold(arg)), " ")
	var ids []string
	for _, id := range g.books.Order {
		if fold(g.books.Books[id].Title) == want {
			ids = append(ids, id)
		}
	}
	switch len(ids) {
	case 0:
		return "", nil
	case 1:
		return ids[0], nil
	}
	return "", fmt.Errorf("%d livros se chamam %q — use o número de `pnn livros`", len(ids), arg)
}

// pageByTitle: a página não arquivada com esse título exato, sem caixa nem
// acento. "" sem erro = nenhuma; duas com o mesmo título é ambíguo — o número
// de `pnn paginas` resolve.
func (g graph) pageByTitle(arg string) (string, error) {
	want := strings.Join(strings.Fields(fold(arg)), " ")
	var ids []string
	for _, id := range g.pages.Order {
		if fold(g.pages.Pages[id].Title) == want {
			ids = append(ids, id)
		}
	}
	if len(ids) > 1 {
		return "", fmt.Errorf("%d páginas se chamam %q — use o número de `pnn paginas`", len(ids), arg)
	}
	if len(ids) == 1 {
		return ids[0], nil
	}
	return "", nil
}

func compareBool(x, y bool) int {
	switch {
	case x == y:
		return 0
	case !x:
		return -1
	}
	return 1
}

// label é o rótulo humano do nó: frente do cartão, "deck Nome", "#tag",
// "página Título", "tarefa Título" ou "livro Título".
func (g graph) label(n core.Node) string {
	switch n.Kind {
	case core.NodeCard:
		card, ok := g.leitner.Cards[n.ID]
		if !ok {
			return n.Key()
		}
		label := `"` + truncate(card.Front, 50) + `"`
		if card.Archived || g.decks.Decks[card.Deck()].Archived {
			label += " (arquivado)"
		}
		return label
	case core.NodeDeck:
		return "deck " + g.decks.Decks[n.ID].Name
	case core.NodePage:
		page, ok := g.pages.Pages[n.ID]
		if !ok {
			return n.Key()
		}
		label := "página " + truncate(page.Title, 50)
		if page.Archived {
			label += " (arquivada)"
		}
		return label
	case core.NodeTask:
		task, ok := g.tasks.Tasks[n.ID]
		if !ok {
			return n.Key()
		}
		label := "tarefa " + truncate(task.Title, 50)
		if task.Done {
			label += " ✔"
		}
		return label
	case core.NodeBook:
		book, ok := g.books.Books[n.ID]
		if !ok {
			return n.Key()
		}
		label := "livro " + truncate(book.Title, 50)
		if book.Archived {
			label += " (arquivado)"
		}
		return label
	}
	return "#" + n.ID
}

func truncate(s string, max int) string {
	if r := []rune(s); len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

// between lista as conexões ativas do par, em qualquer direção.
func (g graph) between(x, y core.Node) []string {
	var ids []string
	for id, link := range g.links.Links {
		if (link.From == x.Key() && link.To == y.Key()) || (link.From == y.Key() && link.To == x.Key()) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

type neighbor struct {
	node      core.Node
	arrow     string // → (x aponta), ← (aponta para x), ↔ (as duas)
	relations []string
}

// neighborhood é o grafo local de x: cada vizinho com a direção e as
// relações das conexões do par.
func (g graph) neighborhood(x core.Node) []neighbor {
	var out []neighbor
	for _, key := range g.links.Neighbors[x.Key()] {
		y, _ := core.ParseNode(key)
		var outgoing, incoming bool
		var relations []string
		for _, id := range g.between(x, y) {
			link := g.links.Links[id]
			if link.From == x.Key() {
				outgoing = true
			} else {
				incoming = true
			}
			if link.Relation != nil && !slices.Contains(relations, *link.Relation) {
				relations = append(relations, *link.Relation)
			}
		}
		arrow := "↔"
		switch {
		case outgoing && !incoming:
			arrow = "→"
		case incoming && !outgoing:
			arrow = "←"
		}
		slices.Sort(relations)
		out = append(out, neighbor{node: y, arrow: arrow, relations: relations})
	}
	return out
}

// linkLines descreve a vizinhança em uma linha por vizinho ("→ #so (prerequisito)").
func (g graph) linkLines(x core.Node) []string {
	var lines []string
	for _, nb := range g.neighborhood(x) {
		line := nb.arrow + " " + g.label(nb.node)
		if len(nb.relations) > 0 {
			line += " (" + strings.Join(nb.relations, ", ") + ")"
		}
		lines = append(lines, line)
	}
	return lines
}

func nodePayload(n core.Node) map[string]any {
	return map[string]any{"kind": n.Kind, "id": n.ID}
}

func resolveAll(a *app.App, g graph, args []string) ([]core.Node, error) {
	nodes := make([]core.Node, len(args))
	for i, arg := range args {
		n, err := g.resolve(a, arg)
		if err != nil {
			return nil, err
		}
		nodes[i] = n
	}
	return nodes, nil
}

// pairKind diz o que ligar/desligar faz com o par (catálogo 1.6, spec §3):
// duas coisas (cartão, página, tarefa, deck) → sinapse; tag e cartão/página →
// a tag classifica o item; deck ↔ tag, tarefa ↔ tag e tag ↔ tag são recusados.
func pairKind(x, y core.Node) (item core.Node, tag string, classify bool, err error) {
	xt, yt := x.Kind == core.NodeTag, y.Kind == core.NodeTag
	switch {
	case !xt && !yt:
		return core.Node{}, "", false, nil
	case xt && yt:
		return core.Node{}, "", false, fmt.Errorf("tags não se ligam: tag classifica — para relacionar assuntos, crie uma página para cada um e ligue as páginas")
	}
	item, tag = x, y.ID
	if xt {
		item, tag = y, x.ID
	}
	if item.Kind == core.NodeDeck {
		return core.Node{}, "", false, fmt.Errorf("deck não ganha tag pelo ligar — as tags do deck são as da criação (`pnn deck \"Nome\" --tag x`)")
	}
	if item.Kind == core.NodeTask {
		return core.Node{}, "", false, fmt.Errorf("tarefa não tem tags — para dar contexto à tarefa, ligue-a a uma página")
	}
	if item.Kind == core.NodeBook {
		return core.Node{}, "", false, fmt.Errorf("livro não tem tags — para dizer do que ele trata, ligue-o a uma página do assunto")
	}
	return item, tag, true, nil
}

// itemTags: as tags próprias do item (o que se grava) e as que ele já tem de
// fato (no cartão, as próprias ∪ as do deck).
func (g graph) itemTags(item core.Node) (own, have []string) {
	if item.Kind == core.NodeCard {
		card := g.leitner.Cards[item.ID]
		return card.Tags, core.EffectiveTags(card, g.decks)
	}
	tags := g.pages.Pages[item.ID].Tags
	return tags, tags
}

// setItemTags grava a lista final de tags do item (LWW por campo).
func setItemTags(a *app.App, item core.Node, tags []string) error {
	var err error
	switch item.Kind {
	case core.NodeCard:
		_, err = a.Store.Append("card.edited", map[string]any{"card_id": item.ID, "tags": tags})
	case core.NodePage:
		_, err = a.Store.Append("page.edited", map[string]any{"page_id": item.ID, "tags": tags})
	}
	return err
}

func newLigarCmd(a *app.App) *cobra.Command {
	var relation string
	cmd := &cobra.Command{
		Use:   "ligar A B [C…]",
		Short: "cria uma sinapse de A para B (e C…); com '#tag', classifica o cartão ou a página (link.created / card.edited, page.edited)",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			g, err := loadGraph(a)
			if err != nil {
				return err
			}
			nodes, err := resolveAll(a, g, args)
			if err != nil {
				return err
			}
			rel := strings.TrimSpace(relation)
			from := nodes[0]

			// Tudo validado antes de gravar: um par recusado não deixa o
			// comando pela metade.
			type tagging struct {
				item core.Node
				tags []string
			}
			var taggings []*tagging
			for _, to := range nodes[1:] {
				item, tag, classify, err := pairKind(from, to)
				if err != nil {
					return err
				}
				if !classify {
					continue
				}
				if rel != "" {
					return fmt.Errorf("--relacao é de sinapse, e tag classifica — para \"%s de um assunto\", crie uma página para o assunto e ligue a ela", rel)
				}
				i := slices.IndexFunc(taggings, func(t *tagging) bool { return t.item == item })
				if i < 0 {
					taggings = append(taggings, &tagging{item: item})
					i = len(taggings) - 1
				}
				taggings[i].tags = append(taggings[i].tags, tag)
			}

			for _, t := range taggings {
				own, have := g.itemTags(t.item)
				var added []string
				for _, tag := range core.NormalizeTags(t.tags) {
					if slices.Contains(have, tag) {
						fmt.Fprintf(a.Out, "· %s já tem #%s\n", g.label(t.item), tag)
					} else {
						added = append(added, tag)
					}
				}
				if len(added) == 0 {
					continue
				}
				if err := setItemTags(a, t.item, core.NormalizeTags(append(slices.Clone(own), added...))); err != nil {
					return err
				}
				fmt.Fprintf(a.Out, "🏷 tag adicionada: %s  %s\n", g.label(t.item), hashTags(added))
			}

			done := map[core.Node]bool{}
			for _, to := range nodes[1:] {
				if _, _, classify, _ := pairKind(from, to); classify {
					continue
				}
				if to == from {
					fmt.Fprintf(a.Out, "· %s não se liga a si mesmo\n", g.label(from))
					continue
				}
				if done[to] || g.linkedAs(from, to, rel) {
					fmt.Fprintf(a.Out, "· já há sinapse: %s ↔ %s\n", g.label(from), g.label(to))
					continue
				}
				payload := map[string]any{"link_id": store.UUIDv7(), "from": nodePayload(from), "to": nodePayload(to)}
				if rel != "" {
					payload["relation"] = rel
				}
				if _, err := a.Store.Append("link.created", payload); err != nil {
					return err
				}
				done[to] = true
				line := fmt.Sprintf("⚡ sinapse: %s → %s", g.label(from), g.label(to))
				if rel != "" {
					line += " (" + rel + ")"
				}
				fmt.Fprintln(a.Out, line)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&relation, "relacao", "", "nome da sinapse, livre (ex.: prerequisito, exemplo, contraste)")
	return cmd
}

// linkedAs: ligar de novo seria redundante — sem relação, o par já tem
// qualquer sinapse; com relação, já tem uma com a mesma (em qualquer direção).
func (g graph) linkedAs(x, y core.Node, rel string) bool {
	return slices.ContainsFunc(g.between(x, y), func(id string) bool {
		have := g.links.Links[id].Relation
		return rel == "" || (have != nil && *have == rel)
	})
}

// removeLinks grava link.removed de todas as conexões do par: duas criadas
// em dispositivos diferentes são arestas distintas (spec §3).
func removeLinks(a *app.App, g graph, x, y core.Node) (int, error) {
	ids := g.between(x, y)
	for _, id := range ids {
		if _, err := a.Store.Append("link.removed", map[string]any{"link_id": id}); err != nil {
			return 0, err
		}
		delete(g.links.Links, id)
	}
	return len(ids), nil
}

func newDesligarCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "desligar A B [C…]",
		Short: "desfaz as sinapses entre A e B (e C…); com '#tag', tira a tag do cartão ou da página",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			g, err := loadGraph(a)
			if err != nil {
				return err
			}
			nodes, err := resolveAll(a, g, args)
			if err != nil {
				return err
			}
			from := nodes[0]
			for _, to := range nodes[1:] {
				// deck ↔ tag e tag ↔ tag não se criam mais, mas uma conexão
				// antiga (1.4) entre eles ainda se desfaz pelo caminho comum.
				item, tag, classify, _ := pairKind(from, to)
				if classify {
					own, have := g.itemTags(item)
					removed, err := removeLinks(a, g, from, to) // conexão antiga com a tag (1.4)
					if err != nil {
						return err
					}
					switch {
					case slices.Contains(own, tag):
						next := slices.DeleteFunc(slices.Clone(own), func(t string) bool { return t == tag })
						if err := setItemTags(a, item, next); err != nil {
							return err
						}
						fmt.Fprintf(a.Out, "🏷 tag removida: %s  #%s\n", g.label(item), tag)
					case slices.Contains(have, tag):
						fmt.Fprintf(a.Out, "· #%s vem do deck de %s — não sai pelo cartão\n", tag, g.label(item))
					case removed > 0:
						fmt.Fprintf(a.Out, "✂ sinapse desfeita: %s ↔ #%s\n", g.label(item), tag)
					default:
						fmt.Fprintf(a.Out, "· %s não tinha #%s\n", g.label(item), tag)
					}
					continue
				}
				removed, err := removeLinks(a, g, from, to)
				if err != nil {
					return err
				}
				if removed == 0 {
					fmt.Fprintf(a.Out, "· não havia sinapse entre %s e %s\n", g.label(from), g.label(to))
					continue
				}
				fmt.Fprintf(a.Out, "✂ sinapse desfeita: %s ↔ %s\n", g.label(from), g.label(to))
			}
			return nil
		},
	}
}

func newSinapsesCmd(a *app.App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "sinapses [nó]",
		Aliases: []string{"conexoes"},
		Short:   "sem argumento, o que tem sinapses; com um nó (N, título de página, deck ou '#tag'), a vizinhança dele",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			g, err := loadGraph(a)
			if err != nil {
				return err
			}
			if asJSON {
				raw, err := json.MarshalIndent(g.links, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(a.Out, string(raw))
				return nil
			}
			th := ui.Theme{On: a.Color}
			if len(args) == 1 {
				x, err := g.resolve(a, args[0])
				if err != nil {
					return err
				}
				return showNeighborhood(a, th, g, x)
			}

			if len(g.links.Nodes) == 0 {
				fmt.Fprintf(a.Out, "%s\n", th.Dim("nenhuma sinapse — ligue duas coisas com `pnn ligar 1 2` (números de `pnn cartas`, `pnn paginas` ou `pnn buscar`)"))
				return a.SaveView(nil)
			}
			// Os mais ligados primeiro (os "hubs" do grafo).
			nodes := slices.Clone(g.links.Nodes)
			slices.SortStableFunc(nodes, func(x, y string) int {
				return len(g.links.Neighbors[y]) - len(g.links.Neighbors[x])
			})
			refs := make([]app.ViewRef, len(nodes))
			for i, key := range nodes {
				n, _ := core.ParseNode(key)
				refs[i] = app.ViewRef{Kind: n.Kind, ID: n.ID}
				fmt.Fprintf(a.Out, " %d %s  %s\n", i+1, g.label(n), th.Dim(plural(len(g.links.Neighbors[key]), "sinapse", "sinapses")))
			}
			fmt.Fprintf(a.Out, "\n%s\n", th.Dim("vizinhança: pnn sinapses N · estudar ao redor: pnn revisar --ao-redor N"))
			return a.SaveView(refs)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emite a projeção de sinapses (conexões) em JSON")
	return cmd
}

// showNeighborhood numera o próprio nó como 1 e os vizinhos a partir de 2,
// para `pnn desligar 1 N` e `pnn revisar --ao-redor 1` sem redigitar nada.
func showNeighborhood(a *app.App, th ui.Theme, g graph, x core.Node) error {
	refs := []app.ViewRef{{Kind: x.Kind, ID: x.ID}}
	fmt.Fprintf(a.Out, " 1 %s\n", th.Bold(g.label(x)))
	neighbors := g.neighborhood(x)
	if len(neighbors) == 0 {
		fmt.Fprintf(a.Out, "   %s\n", th.Dim("sem sinapses — ligue com `pnn ligar 1 N`"))
		return a.SaveView(refs)
	}
	for _, nb := range neighbors {
		refs = append(refs, app.ViewRef{Kind: nb.node.Kind, ID: nb.node.ID})
		line := fmt.Sprintf("   %d %s %s", len(refs), nb.arrow, g.label(nb.node))
		if len(nb.relations) > 0 {
			line += "  " + th.Dim(strings.Join(nb.relations, ", "))
		}
		fmt.Fprintln(a.Out, line)
	}
	fmt.Fprintf(a.Out, "\n%s\n", th.Dim("desligar: pnn desligar 1 N · estudar ao redor: pnn revisar --ao-redor 1"))
	return a.SaveView(refs)
}
