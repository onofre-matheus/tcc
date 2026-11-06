// Tags livres (spec/SPEC.md §3 "Normalização de tag" e §4.10). A identidade
// da tag é o próprio nome normalizado — não existe tag.created nem tag_id —,
// então dois dispositivos que escrevem "#SO" e "so" convergem para a mesma tag
// sem regra de merge. Hierarquia por "/" (so/escalonamento), estilo Obsidian;
// o "::" continua sendo dos decks.
package core

import (
	"slices"
	"strings"
)

// NormalizeTag aplica a regra da spec: apara, tira os "#" iniciais, passa a
// minúsculas, troca espaço interno por "-" e descarta segmentos "/" vazios.
// Tag que sobra vazia devolve "".
func NormalizeTag(raw string) string {
	s := strings.ToLower(strings.TrimLeft(strings.TrimSpace(raw), "#"))
	var segments []string
	for _, seg := range strings.Split(s, "/") {
		if seg = strings.Join(strings.Fields(seg), "-"); seg != "" {
			segments = append(segments, seg)
		}
	}
	return strings.Join(segments, "/")
}

// NormalizeTags normaliza uma lista, descarta vazias e deduplica mantendo a
// primeira ocorrência. Nunca devolve nil (no JSON, lista é sempre `[]`).
func NormalizeTags(raw []string) []string {
	out := []string{}
	for _, tag := range raw {
		if tag = NormalizeTag(tag); tag != "" && !slices.Contains(out, tag) {
			out = append(out, tag)
		}
	}
	return out
}

// ParseTags lê a entrada livre da UI: tags separadas por espaço ou vírgula,
// com ou sem "#" ("#SO #Prova-P2, redes" → so, prova-p2, redes).
func ParseTags(input string) []string {
	return NormalizeTags(strings.FieldsFunc(input, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	}))
}

// EffectiveTags = tags do cartão ∪ tags do próprio deck (as "transversais").
func EffectiveTags(card LeitnerCard, decks DecksState) []string {
	tags := slices.Clone(card.Tags)
	if card.DeckID != nil {
		tags = append(tags, decks.Decks[*card.DeckID].Tags...)
	}
	return NormalizeTags(tags)
}

// TagUnder diz se a tag está sob o prefixo: a própria ou uma descendente
// ("so" ⊇ "so/concorrencia", mas não "sobre").
func TagUnder(tag, prefix string) bool {
	return tag == prefix || strings.HasPrefix(tag, prefix+"/")
}

// activeCard: não arquivado e de deck não arquivado (cartão sem deck nunca
// está em deck arquivado). É o universo das projeções de tags e de estudo.
func activeCard(card LeitnerCard, decks DecksState) bool {
	return !card.Archived && (card.DeckID == nil || !decks.Decks[*card.DeckID].Archived)
}

type TagInfo struct {
	Cards int `json:"cards"` // cartões ativos com a tag exata entre as efetivas
	Due   int `json:"due"`   // quantos desses estão na fila Leitner
}

type TagsState struct {
	Tags  map[string]TagInfo `json:"tags"`
	Names []string           `json:"names"` // ordem canônica (byte a byte)
}

func init() {
	Register("tags", func(events []Event, p Params) (any, error) {
		return Tags(events, p)
	})
}

// Tags projeta o índice de tags (spec §4.10). A soma pela hierarquia ("so"
// incluindo "so/x") é feita só na exibição, como a dos decks.
func Tags(events []Event, p Params) (TagsState, error) {
	leitner, err := Leitner(events, p)
	if err != nil {
		return TagsState{}, err
	}
	decks, err := Decks(events)
	if err != nil {
		return TagsState{}, err
	}

	state := TagsState{Tags: map[string]TagInfo{}, Names: []string{}}
	for _, deck := range decks.Decks {
		if deck.Archived {
			continue
		}
		for _, tag := range deck.Tags {
			state.Tags[tag] = state.Tags[tag] // tag de deck ativo aparece mesmo sem cartões
		}
	}
	due := make(map[string]bool, len(leitner.Queue))
	for _, id := range leitner.Queue {
		due[id] = true
	}
	for id, card := range leitner.Cards {
		if !activeCard(card, decks) {
			continue
		}
		for _, tag := range EffectiveTags(card, decks) {
			info := state.Tags[tag]
			info.Cards++
			if due[id] {
				info.Due++
			}
			state.Tags[tag] = info
		}
	}
	for tag := range state.Tags {
		state.Names = append(state.Names, tag)
	}
	slices.Sort(state.Names)
	return state, nil
}
