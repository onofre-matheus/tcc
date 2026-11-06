// Sessões salvas (spec/SPEC.md §3 "Sessões salvas" e §4.13): o filtro da
// sessão personalizada com um nome, como os decks filtrados do Anki — guarda
// o filtro, não a lista de cartões. study.saved cria ou substitui a sessão
// inteira; study.deleted é tombstone; as tags acompanham tag.renamed.
package core

import (
	"cmp"
	"slices"
	"strings"
)

type SavedStudy struct {
	Name     string   `json:"name"`
	Decks    []string `json:"decks"`
	Tags     []string `json:"tags"`
	Around   []string `json:"around"`
	Practice bool     `json:"practice"` // abre no modo treino por padrão
}

// Filter devolve o filtro da sessão personalizada (§4.11).
func (s SavedStudy) Filter() StudyFilter {
	return StudyFilter{Decks: s.Decks, Tags: s.Tags, Around: s.Around}
}

type StudiesState struct {
	Studies map[string]SavedStudy `json:"studies"`
	Order   []string              `json:"order"` // (name, study_id)
}

func init() {
	Register("studies", func(events []Event, _ Params) (any, error) {
		return Studies(events)
	})
}

// normalizeAround normaliza as chaves válidas; a inválida fica como veio (e
// não casa nada, §4.11). Sem repetição; nunca nil.
func normalizeAround(raw []string) []string {
	out := []string{}
	for _, key := range raw {
		if node, ok := ParseNode(key); ok {
			key = node.Key()
		}
		if !slices.Contains(out, key) {
			out = append(out, key)
		}
	}
	return out
}

func cleanIDs(raw []string) []string {
	out := []string{}
	for _, id := range raw {
		if id = strings.TrimSpace(id); id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

func Studies(events []Event) (StudiesState, error) {
	state := StudiesState{Studies: map[string]SavedStudy{}, Order: []string{}}
	deleted := map[string]bool{}

	for _, event := range Normalize(events) {
		switch event.Type {
		case "study.saved":
			pl, err := decodePayload[struct {
				StudyID  string   `json:"study_id"`
				Name     string   `json:"name"`
				Decks    []string `json:"decks"`
				Tags     []string `json:"tags"`
				Around   []string `json:"around"`
				Practice bool     `json:"practice"`
			}](event)
			if err != nil {
				return StudiesState{}, err
			}
			name := strings.TrimSpace(pl.Name)
			if pl.StudyID == "" || name == "" || deleted[pl.StudyID] {
				continue
			}
			state.Studies[pl.StudyID] = SavedStudy{
				Name:     name,
				Decks:    cleanIDs(pl.Decks),
				Tags:     NormalizeTags(pl.Tags),
				Around:   normalizeAround(pl.Around),
				Practice: pl.Practice,
			}

		case "study.deleted":
			pl, err := decodePayload[struct {
				StudyID string `json:"study_id"`
			}](event)
			if err != nil {
				return StudiesState{}, err
			}
			if _, ok := state.Studies[pl.StudyID]; ok {
				delete(state.Studies, pl.StudyID)
				deleted[pl.StudyID] = true
			}

		case "tag.renamed":
			from, to, ok, err := decodeRename(event)
			if err != nil {
				return StudiesState{}, err
			}
			if !ok {
				continue
			}
			for id, study := range state.Studies {
				study.Tags = renameTags(study.Tags, from, to)
				around := make([]string, len(study.Around))
				for i, key := range study.Around {
					if node, ok := ParseNode(key); ok && node.Kind == NodeTag {
						node.ID = RenameTag(node.ID, from, to)
						key = node.Key()
					}
					around[i] = key
				}
				study.Around = normalizeAround(around)
				state.Studies[id] = study
			}
		}
	}

	for id := range state.Studies {
		state.Order = append(state.Order, id)
	}
	slices.SortFunc(state.Order, func(a, b string) int {
		return cmp.Or(strings.Compare(state.Studies[a].Name, state.Studies[b].Name), strings.Compare(a, b))
	})
	return state, nil
}
