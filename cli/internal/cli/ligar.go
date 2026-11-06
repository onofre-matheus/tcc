// Conexões explícitas (catálogo 1.4, spec §3 e §4.12), estilo Obsidian:
// `pnn ligar A B` conecta dois nós — cartão, deck ou tag —, `pnn desligar A
// B` desfaz e `pnn conexoes [A]` mostra o grafo local. Um nó é o número da
// última tela (cartas, decks, tags, conexoes), uma "#tag" ou o nome exato de
// um deck; sem "#", um nome que não é deck vale como tag se ela já existir.
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
	g.tags, err = core.Tags(g.events, a.Params())
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
		case core.NodeTag:
		default:
			return core.Node{}, fmt.Errorf("o item %d da última tela não se conecta — só cartões (pnn cartas), decks (pnn decks) e tags (pnn tags)", n)
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
	if tag != "" && g.knownTag(tag) {
		return core.Node{Kind: core.NodeTag, ID: tag}, nil
	}
	return core.Node{}, fmt.Errorf("%q não é número da última tela, deck nem tag conhecida — tag nova vai com # entre aspas: '#%s'", arg, tag)
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

// label é o rótulo humano do nó: frente do cartão, "deck Nome" ou "#tag".
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

func newLigarCmd(a *app.App) *cobra.Command {
	var relation string
	cmd := &cobra.Command{
		Use:   "ligar A B [C…]",
		Short: "conecta A a B (e a C…): número da última tela, '#tag' ou nome de deck (link.created)",
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
			done := map[core.Node]bool{}
			for _, to := range nodes[1:] {
				if to == from {
					fmt.Fprintf(a.Out, "· %s não se liga a si mesmo\n", g.label(from))
					continue
				}
				if done[to] || g.linkedAs(from, to, rel) {
					fmt.Fprintf(a.Out, "· já ligados: %s ↔ %s\n", g.label(from), g.label(to))
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
				line := fmt.Sprintf("🔗 ligado: %s → %s", g.label(from), g.label(to))
				if rel != "" {
					line += " (" + rel + ")"
				}
				fmt.Fprintln(a.Out, line)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&relation, "relacao", "", "nome da relação, livre (ex.: prerequisito, exemplo, contraste)")
	return cmd
}

// linkedAs: ligar de novo seria redundante — sem relação, o par já tem
// qualquer conexão; com relação, já tem uma com a mesma (em qualquer direção).
func (g graph) linkedAs(x, y core.Node, rel string) bool {
	return slices.ContainsFunc(g.between(x, y), func(id string) bool {
		have := g.links.Links[id].Relation
		return rel == "" || (have != nil && *have == rel)
	})
}

func newDesligarCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "desligar A B [C…]",
		Short: "desfaz as conexões entre A e B (e C…), em qualquer direção (link.removed)",
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
				ids := g.between(from, to)
				if len(ids) == 0 {
					fmt.Fprintf(a.Out, "· não estavam ligados: %s e %s\n", g.label(from), g.label(to))
					continue
				}
				// Todas as do par: duas conexões criadas em dispositivos
				// diferentes são arestas distintas (spec §3).
				for _, id := range ids {
					if _, err := a.Store.Append("link.removed", map[string]any{"link_id": id}); err != nil {
						return err
					}
					delete(g.links.Links, id)
				}
				fmt.Fprintf(a.Out, "✂ desligado: %s ↔ %s\n", g.label(from), g.label(to))
			}
			return nil
		},
	}
}

func newConexoesCmd(a *app.App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "conexoes [nó]",
		Short: "sem argumento, os nós conectados; com um nó (N, '#tag' ou deck), a vizinhança dele",
		Args:  cobra.MaximumNArgs(1),
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
				fmt.Fprintf(a.Out, "%s\n", th.Dim("nenhuma conexão — ligue com `pnn ligar 1 2` (números de `pnn cartas`/`pnn tags`) ou `pnn ligar 1 '#tag'`"))
				return a.SaveView(nil)
			}
			// Os mais conectados primeiro (os "hubs" do grafo).
			nodes := slices.Clone(g.links.Nodes)
			slices.SortStableFunc(nodes, func(x, y string) int {
				return len(g.links.Neighbors[y]) - len(g.links.Neighbors[x])
			})
			refs := make([]app.ViewRef, len(nodes))
			for i, key := range nodes {
				n, _ := core.ParseNode(key)
				refs[i] = app.ViewRef{Kind: n.Kind, ID: n.ID}
				fmt.Fprintf(a.Out, " %d %s  %s\n", i+1, g.label(n), th.Dim(plural(len(g.links.Neighbors[key]), "conexão", "conexões")))
			}
			fmt.Fprintf(a.Out, "\n%s\n", th.Dim("vizinhança: pnn conexoes N · estudar ao redor: pnn revisar --ao-redor N"))
			return a.SaveView(refs)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emite a projeção de conexões em JSON")
	return cmd
}

// showNeighborhood numera o próprio nó como 1 e os vizinhos a partir de 2,
// para `pnn desligar 1 N` e `pnn revisar --ao-redor 1` sem redigitar nada.
func showNeighborhood(a *app.App, th ui.Theme, g graph, x core.Node) error {
	refs := []app.ViewRef{{Kind: x.Kind, ID: x.ID}}
	fmt.Fprintf(a.Out, " 1 %s\n", th.Bold(g.label(x)))
	neighbors := g.neighborhood(x)
	if len(neighbors) == 0 {
		fmt.Fprintf(a.Out, "   %s\n", th.Dim("sem conexões — ligue com `pnn ligar 1 N`"))
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
