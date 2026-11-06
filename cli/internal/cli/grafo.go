// `pnn grafo [nó]` — o acervo como grafo (spec §4.14), em DOT para o Graphviz:
// `pnn grafo | dot -Tsvg > grafo.svg`. É a mesma projeção que a extensão
// desenha em SVG; aqui só muda a renderização. Com um nó, o grafo local
// (estilo Obsidian): o nó e o que está a até --raio arestas dele.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/spf13/cobra"
)

// Cores da identidade visual da extensão (ui/app.css), para o SVG do
// Graphviz conversar com a tela de grafo.
const (
	colorAccent = "#c96442" // conexões
	colorTag    = "#2f855a"
	colorDue    = "#c0392b" // cartão vencido
	colorDeck   = "#b7791f"
	colorCard   = "#4a5568"
	colorMuted  = "#86847c"
)

func newGrafoCmd(a *app.App) *cobra.Command {
	var asJSON, semCartoes bool
	var raio int
	cmd := &cobra.Command{
		Use:   "grafo [nó]",
		Short: "o acervo como grafo em DOT (pnn grafo | dot -Tsvg > grafo.svg); com um nó, o grafo local",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			g, err := loadGraph(a)
			if err != nil {
				return err
			}
			state, err := core.Graph(g.events, a.Params())
			if err != nil {
				return err
			}
			keep := ""
			if len(args) == 1 {
				node, err := g.resolve(a, args[0])
				if err != nil {
					return err
				}
				if keep = node.Key(); !hasNode(state, keep) {
					return fmt.Errorf("%s está fora do grafo (arquivado?)", g.label(node))
				}
				state = localGraph(state, keep, max(raio, 1))
			}
			if semCartoes {
				state = withoutCards(state, keep)
			}
			if asJSON {
				raw, err := json.MarshalIndent(state, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(a.Out, string(raw))
				return nil
			}
			return writeDOT(a.Out, state)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emite a projeção do grafo em JSON")
	cmd.Flags().BoolVar(&semCartoes, "sem-cartoes", false, "só tags e decks (a visão geral do acervo)")
	cmd.Flags().IntVar(&raio, "raio", 1, "com um nó: até quantas arestas de distância entram")
	return cmd
}

func hasNode(state core.GraphState, key string) bool {
	for _, node := range state.Nodes {
		if node.ID == key {
			return true
		}
	}
	return false
}

// localGraph: busca em largura sem direção a partir do nó, até `radius`
// arestas; ficam os nós alcançados e as arestas entre eles.
func localGraph(state core.GraphState, start string, radius int) core.GraphState {
	adjacent := map[string][]string{}
	for _, e := range state.Edges {
		adjacent[e.From] = append(adjacent[e.From], e.To)
		adjacent[e.To] = append(adjacent[e.To], e.From)
	}
	reached := map[string]bool{start: true}
	frontier := []string{start}
	for step := 0; step < radius && len(frontier) > 0; step++ {
		var next []string
		for _, key := range frontier {
			for _, other := range adjacent[key] {
				if !reached[other] {
					reached[other] = true
					next = append(next, other)
				}
			}
		}
		frontier = next
	}
	return filterGraph(state, func(key string) bool { return reached[key] })
}

// withoutCards tira os cartões (menos o nó pedido, se for um).
func withoutCards(state core.GraphState, keep string) core.GraphState {
	return filterGraph(state, func(key string) bool {
		return key == keep || !strings.HasPrefix(key, core.NodeCard+":")
	})
}

func filterGraph(state core.GraphState, in func(string) bool) core.GraphState {
	out := core.GraphState{Nodes: []core.GraphNode{}, Edges: []core.GraphEdge{}}
	for _, node := range state.Nodes {
		if in(node.ID) {
			out.Nodes = append(out.Nodes, node)
		}
	}
	for _, e := range state.Edges {
		if in(e.From) && in(e.To) {
			out.Edges = append(out.Edges, e)
		}
	}
	return out
}

// dotQuote escapa para string DOT: aspas, barra invertida e quebras de linha.
func dotQuote(s string) string {
	s = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\r", "", "\n", `\n`).Replace(s)
	return `"` + s + `"`
}

func writeDOT(w io.Writer, state core.GraphState) error {
	var b strings.Builder
	fmt.Fprintf(&b, "// pnn grafo — %d nós, %d arestas (spec §4.14)\n", len(state.Nodes), len(state.Edges))
	b.WriteString("// renderize: pnn grafo | dot -Tsvg > grafo.svg\n")
	b.WriteString("digraph pnn {\n")
	b.WriteString(`  graph [layout=fdp, overlap=false, splines=true, fontname="Helvetica"];` + "\n")
	b.WriteString(`  node [fontname="Helvetica", fontsize=10];` + "\n")
	fmt.Fprintf(&b, "  edge [arrowsize=0.6, color=%q];\n", colorMuted)
	for _, node := range state.Nodes {
		var attrs string
		switch node.Kind {
		case core.NodeCard:
			color, width := colorCard, 1
			if node.Due {
				color, width = colorDue, 2
			}
			attrs = fmt.Sprintf(`label=%s, shape=box, style=rounded, color=%q, penwidth=%d`, dotQuote(truncate(node.Label, 40)), color, width)
		case core.NodeDeck:
			attrs = fmt.Sprintf(`label=%s, shape=folder, color=%q`, dotQuote(node.Label), colorDeck)
		default:
			attrs = fmt.Sprintf(`label=%s, shape=ellipse, color=%q, fontcolor=%q`, dotQuote("#"+node.Label), colorTag, colorTag)
		}
		fmt.Fprintf(&b, "  %s [%s];\n", dotQuote(node.ID), attrs)
	}
	for _, e := range state.Edges {
		var attrs string
		switch e.Kind {
		case core.EdgeLink:
			attrs = fmt.Sprintf(`color=%q, penwidth=1.4`, colorAccent)
			if e.Relation != nil {
				attrs += fmt.Sprintf(`, label=%s, fontsize=9, fontcolor=%q`, dotQuote(*e.Relation), colorAccent)
			}
		case core.EdgeTag:
			attrs = "style=dashed, arrowhead=none"
		case core.EdgeDeck:
			attrs = "style=dotted, arrowhead=none"
		default: // parent: filho → pai
			attrs = "style=dotted, arrowhead=empty"
		}
		fmt.Fprintf(&b, "  %s -> %s [%s];\n", dotQuote(e.From), dotQuote(e.To), attrs)
	}
	b.WriteString("}\n")
	_, err := io.WriteString(w, b.String())
	return err
}
