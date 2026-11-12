// Conexões explícitas (spec/SPEC.md §3 "Conexões e renomeação de tag" e
// §4.12): o [[link]] do Obsidian como evento. Uma conexão liga dois nós —
// cartão, deck, tag, página ou tarefa (1.6) — e a vizinhança é não
// direcionada. O id da tag é o próprio nome normalizado, então ligar a uma
// tag não exige que ela "exista".
package core

import (
	"slices"
	"strings"
)

// Tipos de nó conhecidos; kind desconhecido invalida a conexão (um tipo novo
// entra como kind novo, e clientes antigos o ignoram).
const (
	NodeCard = "card"
	NodeDeck = "deck"
	NodeTag  = "tag"
	NodePage = "page" // catálogo 1.6
	NodeTask = "task" // catálogo 1.6
)

type Node struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// Key é a forma do nó na saída das projeções: "kind:id".
func (n Node) Key() string { return n.Kind + ":" + n.ID }

// Clean normaliza o id de tag e diz se o nó é bem-formado (kind conhecido,
// id não vazio). Existência de cartão/deck/página é checada por quem tem o
// estado.
func (n Node) Clean() (Node, bool) {
	switch n.Kind {
	case NodeTag:
		n.ID = NormalizeTag(n.ID)
	case NodeCard, NodeDeck, NodePage, NodeTask:
		n.ID = strings.TrimSpace(n.ID)
	default:
		return n, false
	}
	return n, n.ID != ""
}

// ParseNode lê a chave "kind:id" (o id pode conter ":"; corta no primeiro).
func ParseNode(key string) (Node, bool) {
	kind, id, ok := strings.Cut(key, ":")
	if !ok {
		return Node{}, false
	}
	return Node{Kind: kind, ID: id}.Clean()
}

type Link struct {
	From     string  `json:"from"`
	To       string  `json:"to"`
	Relation *string `json:"relation"` // aberto (ex.: "prerequisito"); nil = conexão simples
}

type LinksState struct {
	Links     map[string]Link     `json:"links"`
	Neighbors map[string][]string `json:"neighbors"` // não direcionada, ordem canônica
	Nodes     []string            `json:"nodes"`     // nós com ao menos uma conexão
}

func init() {
	Register("links", func(events []Event, _ Params) (any, error) {
		return Links(events)
	})
}

func Links(events []Event) (LinksState, error) {
	type edge struct{ from, to Node }
	cards, decks, pages, tasks := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	edges := map[string]*edge{}
	relations := map[string]*string{}
	removed := map[string]bool{}

	for _, event := range Normalize(events) {
		switch event.Type {
		case "card.created":
			pl, err := decodePayload[struct {
				CardID string `json:"card_id"`
			}](event)
			if err != nil {
				return LinksState{}, err
			}
			cards[pl.CardID] = true

		case "deck.created":
			pl, err := decodePayload[struct {
				DeckID string `json:"deck_id"`
			}](event)
			if err != nil {
				return LinksState{}, err
			}
			decks[pl.DeckID] = true

		case "page.created":
			// a mesma validação da projeção de páginas (§3, Páginas): sem
			// título, a página não existe e não se conecta
			pl, err := decodePayload[struct {
				PageID string `json:"page_id"`
				Title  string `json:"title"`
			}](event)
			if err != nil {
				return LinksState{}, err
			}
			if pl.PageID != "" && strings.TrimSpace(pl.Title) != "" {
				pages[pl.PageID] = true
			}

		case "task.created":
			pl, err := decodePayload[struct {
				TaskID string `json:"task_id"`
			}](event)
			if err != nil {
				return LinksState{}, err
			}
			if pl.TaskID != "" {
				tasks[pl.TaskID] = true
			}

		case "task.deleted":
			// apagar é tombstone: a tarefa sai do estado, e as sinapses dela
			// junto (ao contrário de arquivar cartão, deck ou página)
			pl, err := decodePayload[struct {
				TaskID string `json:"task_id"`
			}](event)
			if err != nil {
				return LinksState{}, err
			}
			delete(tasks, pl.TaskID)

		case "link.created":
			pl, err := decodePayload[struct {
				LinkID   string `json:"link_id"`
				From     Node   `json:"from"`
				To       Node   `json:"to"`
				Relation string `json:"relation"`
			}](event)
			if err != nil {
				return LinksState{}, err
			}
			if _, dup := edges[pl.LinkID]; dup || removed[pl.LinkID] || pl.LinkID == "" {
				continue // o primeiro link_id vence; o removido não volta
			}
			exists := func(n Node) bool {
				switch n.Kind {
				case NodeCard:
					return cards[n.ID]
				case NodeDeck:
					return decks[n.ID]
				case NodePage:
					return pages[n.ID]
				case NodeTask:
					return tasks[n.ID]
				}
				return true // tag sempre existe
			}
			from, okFrom := pl.From.Clean()
			to, okTo := pl.To.Clean()
			if !okFrom || !okTo || !exists(from) || !exists(to) || from == to {
				continue
			}
			edges[pl.LinkID] = &edge{from: from, to: to}
			if rel := strings.TrimSpace(pl.Relation); rel != "" {
				relations[pl.LinkID] = &rel
			}

		case "link.removed":
			pl, err := decodePayload[struct {
				LinkID string `json:"link_id"`
			}](event)
			if err != nil {
				return LinksState{}, err
			}
			if _, ok := edges[pl.LinkID]; ok {
				delete(edges, pl.LinkID)
				removed[pl.LinkID] = true
			}

		case "tag.renamed":
			from, to, ok, err := decodeRename(event)
			if err != nil {
				return LinksState{}, err
			}
			if !ok {
				continue
			}
			for _, e := range edges {
				for _, n := range []*Node{&e.from, &e.to} {
					if n.Kind == NodeTag {
						n.ID = RenameTag(n.ID, from, to)
					}
				}
			}
		}
	}

	state := LinksState{Links: map[string]Link{}, Neighbors: map[string][]string{}, Nodes: []string{}}
	for id, e := range edges {
		if e.from == e.to {
			continue // virou laço por renomeação: deixa de valer
		}
		if (e.from.Kind == NodeTask && !tasks[e.from.ID]) || (e.to.Kind == NodeTask && !tasks[e.to.ID]) {
			continue // a tarefa foi apagada depois da sinapse
		}
		from, to := e.from.Key(), e.to.Key()
		state.Links[id] = Link{From: from, To: to, Relation: relations[id]}
		for _, pair := range [][2]string{{from, to}, {to, from}} {
			if !slices.Contains(state.Neighbors[pair[0]], pair[1]) {
				state.Neighbors[pair[0]] = append(state.Neighbors[pair[0]], pair[1])
			}
		}
	}
	for node, neighbors := range state.Neighbors {
		slices.Sort(neighbors)
		state.Nodes = append(state.Nodes, node)
	}
	slices.Sort(state.Nodes)
	return state, nil
}
