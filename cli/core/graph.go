// Grafo do acervo (spec/SPEC.md §4.14), estilo Obsidian: cartões ativos,
// decks e tags como nós; conexões explícitas, tags, pertença a deck e
// hierarquia (/ e ::) como arestas. É o contrato que a extensão desenha em
// SVG e a CLI em DOT — as duas, a partir da mesma projeção.
package core

import (
	"cmp"
	"slices"
	"strings"
)

// Tipos de aresta.
const (
	EdgeLink   = "link"
	EdgeTag    = "tag"
	EdgeDeck   = "deck"
	EdgeParent = "parent"
)

type GraphNode struct {
	ID    string `json:"id"` // "kind:id"
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Due   bool   `json:"due"` // cartão na fila Leitner
}

type GraphEdge struct {
	From     string  `json:"from"`
	To       string  `json:"to"`
	Kind     string  `json:"kind"`
	Relation *string `json:"relation"` // só em link
}

type GraphState struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

func init() {
	Register("graph", func(events []Event, p Params) (any, error) {
		return Graph(events, p)
	})
}

// deckPath: o nome do deck com os segmentos "::" aparados e sem vazios (§4.5).
func deckPath(name string) []string {
	var segments []string
	for _, seg := range strings.Split(name, "::") {
		if seg = strings.TrimSpace(seg); seg != "" {
			segments = append(segments, seg)
		}
	}
	return segments
}

func Graph(events []Event, p Params) (GraphState, error) {
	leitner, err := Leitner(events, p)
	if err != nil {
		return GraphState{}, err
	}
	decks, err := Decks(events)
	if err != nil {
		return GraphState{}, err
	}
	links, err := Links(events)
	if err != nil {
		return GraphState{}, err
	}

	nodes := map[string]GraphNode{}
	addTag := func(tag string) { // a tag e os ancestrais implícitos
		for {
			nodes[NodeTag+":"+tag] = GraphNode{ID: NodeTag + ":" + tag, Kind: NodeTag, Label: tag}
			i := strings.LastIndex(tag, "/")
			if i < 0 {
				return
			}
			tag = tag[:i]
		}
	}
	due := map[string]bool{}
	for _, id := range leitner.Queue {
		due[id] = true
	}
	for id, card := range leitner.Cards {
		if !activeCard(card, decks) {
			continue
		}
		key := NodeCard + ":" + id
		nodes[key] = GraphNode{ID: key, Kind: NodeCard, Label: card.Front, Due: due[id]}
		for _, tag := range card.Tags {
			addTag(tag)
		}
	}
	for id, deck := range decks.Decks {
		if deck.Archived {
			continue
		}
		key := NodeDeck + ":" + id
		nodes[key] = GraphNode{ID: key, Kind: NodeDeck, Label: deck.Name}
		for _, tag := range deck.Tags {
			addTag(tag)
		}
	}
	for _, link := range links.Links {
		for _, end := range []string{link.From, link.To} {
			if node, ok := ParseNode(end); ok && node.Kind == NodeTag {
				addTag(node.ID)
			}
		}
	}

	var edges []GraphEdge
	add := func(from, to, kind string, relation *string) {
		_, okFrom := nodes[from]
		_, okTo := nodes[to]
		if okFrom && okTo {
			edges = append(edges, GraphEdge{From: from, To: to, Kind: kind, Relation: relation})
		}
	}
	for _, link := range links.Links {
		add(link.From, link.To, EdgeLink, link.Relation)
	}
	// Deck ativo por caminho normalizado, para achar o pai "::" (menor id
	// entre homônimos).
	byPath := map[string]string{}
	for id, deck := range decks.Decks {
		path := strings.Join(deckPath(deck.Name), "::")
		if have, ok := byPath[path]; !deck.Archived && (!ok || id < have) {
			byPath[path] = id
		}
	}
	for key, node := range nodes {
		switch node.Kind {
		case NodeCard:
			card := leitner.Cards[strings.TrimPrefix(key, NodeCard+":")]
			for _, tag := range card.Tags {
				add(key, NodeTag+":"+tag, EdgeTag, nil)
			}
			if card.DeckID != nil {
				add(key, NodeDeck+":"+*card.DeckID, EdgeDeck, nil)
			}
		case NodeDeck:
			deck := decks.Decks[strings.TrimPrefix(key, NodeDeck+":")]
			for _, tag := range deck.Tags {
				add(key, NodeTag+":"+tag, EdgeTag, nil)
			}
			segments := deckPath(deck.Name)
			for i := len(segments) - 1; i >= 1; i-- {
				if parent, ok := byPath[strings.Join(segments[:i], "::")]; ok {
					add(key, NodeDeck+":"+parent, EdgeParent, nil)
					break
				}
			}
		case NodeTag:
			if i := strings.LastIndex(node.Label, "/"); i >= 0 {
				add(key, NodeTag+":"+node.Label[:i], EdgeParent, nil)
			}
		}
	}

	state := GraphState{Nodes: []GraphNode{}, Edges: []GraphEdge{}}
	for _, node := range nodes {
		state.Nodes = append(state.Nodes, node)
	}
	slices.SortFunc(state.Nodes, func(a, b GraphNode) int { return strings.Compare(a.ID, b.ID) })
	relation := func(r *string) string { // null antes de qualquer relação
		if r == nil {
			return ""
		}
		return "\x01" + *r
	}
	slices.SortFunc(edges, func(a, b GraphEdge) int {
		return cmp.Or(strings.Compare(a.From, b.From), strings.Compare(a.To, b.To),
			strings.Compare(a.Kind, b.Kind), strings.Compare(relation(a.Relation), relation(b.Relation)))
	})
	for _, edge := range edges {
		if n := len(state.Edges); n > 0 {
			last := state.Edges[n-1]
			if last.From == edge.From && last.To == edge.To && last.Kind == edge.Kind && relation(last.Relation) == relation(edge.Relation) {
				continue // aresta idêntica aparece uma vez
			}
		}
		state.Edges = append(state.Edges, edge)
	}
	return state, nil
}
