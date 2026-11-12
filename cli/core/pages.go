// Páginas (catálogo 1.6, spec/SPEC.md §3 "Páginas" e §4.15): o texto longo
// de consulta e de uso constante — a anotação que fica, ao contrário da nota
// capturada (passagem para a triagem) e do cartão (estudo). O corpo é
// markdown opaco: a projeção não o interpreta. Edição é LWW por campo; as
// tags acompanham tag.renamed; `version` é o que o cliente envia como `base`.
package core

import (
	"cmp"
	"slices"
	"strings"
)

type Page struct {
	Title    string   `json:"title"`
	Body     string   `json:"body"`
	Tags     []string `json:"tags"`
	Pinned   bool     `json:"pinned"`
	Archived bool     `json:"archived"`
	// Version é o id do último evento que gravou o corpo — a `base` da
	// próxima edição.
	Version   string `json:"version"`
	CreatedAt string `json:"created_at"`
	// UpdatedAt é o ts do último evento que trouxe título, corpo ou tags;
	// fixar e arquivar não contam como edição.
	UpdatedAt string `json:"updated_at"`
}

type PagesState struct {
	Pages map[string]Page `json:"pages"`
	Order []string        `json:"order"` // não arquivadas: fixadas, updated_at ↓, page_id
}

func init() {
	Register("pages", func(events []Event, _ Params) (any, error) {
		return Pages(events)
	})
}

func Pages(events []Event) (PagesState, error) {
	pages := map[string]*Page{}

	for _, event := range Normalize(events) {
		switch event.Type {
		case "page.created":
			pl, err := decodePayload[struct {
				PageID string   `json:"page_id"`
				Title  string   `json:"title"`
				Body   string   `json:"body"`
				Tags   []string `json:"tags"`
			}](event)
			if err != nil {
				return PagesState{}, err
			}
			title := strings.TrimSpace(pl.Title)
			if _, exists := pages[pl.PageID]; pl.PageID == "" || title == "" || exists {
				continue
			}
			pages[pl.PageID] = &Page{
				Title:     title,
				Body:      pl.Body,
				Tags:      NormalizeTags(pl.Tags),
				Version:   event.ID,
				CreatedAt: event.TS,
				UpdatedAt: event.TS,
			}

		case "page.edited":
			pl, err := decodePayload[struct {
				PageID string    `json:"page_id"`
				Title  *string   `json:"title"`
				Body   *string   `json:"body"`
				Tags   *[]string `json:"tags"`
				Pinned *bool     `json:"pinned"`
			}](event)
			if err != nil {
				return PagesState{}, err
			}
			page, ok := pages[pl.PageID]
			if !ok {
				continue
			}
			// last-writer-wins por campo; `base` só será lido pela detecção
			// de conflito — no 1.6 o corpo é LWW como os demais campos.
			edited := false
			if pl.Title != nil {
				if title := strings.TrimSpace(*pl.Title); title != "" {
					page.Title = title
					edited = true
				}
			}
			if pl.Body != nil {
				page.Body = *pl.Body
				page.Version = event.ID
				edited = true
			}
			if pl.Tags != nil {
				page.Tags = NormalizeTags(*pl.Tags)
				edited = true
			}
			if pl.Pinned != nil {
				page.Pinned = *pl.Pinned
			}
			if edited {
				page.UpdatedAt = event.TS
			}

		case "page.archived":
			pl, err := decodePayload[struct {
				PageID string `json:"page_id"`
			}](event)
			if err != nil {
				return PagesState{}, err
			}
			if page, ok := pages[pl.PageID]; ok {
				page.Archived = true
			}

		case "tag.renamed":
			// vale na posição do evento: só as tags já gravadas mudam
			from, to, ok, err := decodeRename(event)
			if err != nil {
				return PagesState{}, err
			}
			if !ok {
				continue
			}
			for _, page := range pages {
				page.Tags = renameTags(page.Tags, from, to)
			}
		}
	}

	state := PagesState{Pages: make(map[string]Page, len(pages)), Order: []string{}}
	for id, page := range pages {
		state.Pages[id] = *page
		if !page.Archived {
			state.Order = append(state.Order, id)
		}
	}
	slices.SortFunc(state.Order, func(a, b string) int {
		pa, pb := state.Pages[a], state.Pages[b]
		return cmp.Or(
			compareBool(pb.Pinned, pa.Pinned),           // fixadas primeiro
			strings.Compare(pb.UpdatedAt, pa.UpdatedAt), // mais recente primeiro
			strings.Compare(a, b),
		)
	})
	return state, nil
}

func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case !a:
		return -1
	}
	return 1
}
