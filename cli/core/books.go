// Livros e anotações (catálogo 1.7, spec/SPEC.md §3 "Livros" e §4.16): o
// livro cadastrado é a identidade (título, autor, formato, sha256 do
// arquivo) — o arquivo em si e o caminho dele ficam no dispositivo, fora do
// log. A anotação aponta para uma posição no livro (capítulo do spine +
// parágrafo, e a página impressa quando o EPUB a traz) e guarda o trecho
// citado, que reancora a anotação se a extração mudar. Edição é LWW por
// campo; apagar anotação é tombstone; o "onde parei" é o último na ordem
// total.
package core

import (
	"cmp"
	"encoding/json"
	"math"
	"slices"
	"strings"
)

// Loc é a posição no livro: Chapter é a posição 1-based no spine do EPUB
// (vem do arquivo, é estável); Para, o bloco 1-based no capítulo; Page, o
// rótulo da página impressa, quando o livro o traz.
type Loc struct {
	Chapter int    `json:"chapter"`
	Para    int    `json:"para"`
	Page    string `json:"page,omitempty"`
}

// Before diz se l vem antes de o na ordem de leitura.
func (l Loc) Before(o Loc) bool {
	return cmp.Or(cmp.Compare(l.Chapter, o.Chapter), cmp.Compare(l.Para, o.Para)) < 0
}

type Book struct {
	Title    string `json:"title"`
	Author   string `json:"author"`
	Format   string `json:"format"`
	SHA256   string `json:"sha256"`
	Archived bool   `json:"archived"`
	// Progress é o "onde parei" (book.progressed): o último na ordem total,
	// inteiro — o percent vem junto (nil se o evento não o trouxe).
	Progress        *Loc    `json:"progress"`
	ProgressPercent *int    `json:"progress_percent"`
	ProgressAt      *string `json:"progress_at"`
	CreatedAt       string  `json:"created_at"`
	// UpdatedAt é o ts do último evento, na ordem total, que mexeu no livro
	// ou nas anotações dele; arquivar não conta.
	UpdatedAt string `json:"updated_at"`
	// Annotations em ordem de leitura: (chapter, para, created_at, id).
	Annotations []string `json:"annotations"`
}

type Annotation struct {
	BookID    string `json:"book_id"`
	Loc       Loc    `json:"loc"`
	Quote     string `json:"quote"`
	Text      string `json:"text"` // vazio = grifo
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type BooksState struct {
	Books       map[string]Book       `json:"books"`
	Annotations map[string]Annotation `json:"annotations"`
	Order       []string              `json:"order"` // não arquivados: updated_at ↓, book_id
}

func init() {
	Register("books", func(events []Event, _ Params) (any, error) {
		return Books(events)
	})
}

// ParseLoc lê a posição do payload: chapter e para inteiros ≥ 1; page só se
// for texto não vazio. Inválida = ok false (o evento é ignorado).
func ParseLoc(raw json.RawMessage) (Loc, bool) {
	var m map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &m) != nil || m == nil {
		return Loc{}, false
	}
	chapter, ok1 := positiveInt(m["chapter"])
	para, ok2 := positiveInt(m["para"])
	if !ok1 || !ok2 {
		return Loc{}, false
	}
	loc := Loc{Chapter: chapter, Para: para}
	if page, ok := m["page"].(string); ok {
		loc.Page = strings.TrimSpace(page)
	}
	return loc, true
}

// maxLocIndex limita os índices a um inteiro que os dois núcleos (Go e JS)
// representam igual.
const maxLocIndex = 1_000_000_000

func positiveInt(v any) (int, bool) {
	f, ok := v.(float64)
	if !ok || f < 1 || f > maxLocIndex || f != math.Trunc(f) {
		return 0, false
	}
	return int(f), true
}

func Books(events []Event) (BooksState, error) {
	books := map[string]*Book{}
	notes := map[string]*Annotation{}
	deleted := map[string]bool{}

	for _, event := range Normalize(events) {
		switch event.Type {
		case "book.registered":
			pl, err := decodePayload[struct {
				BookID string `json:"book_id"`
				Title  string `json:"title"`
				Author string `json:"author"`
				Format string `json:"format"`
				SHA256 string `json:"sha256"`
			}](event)
			if err != nil {
				return BooksState{}, err
			}
			title := strings.TrimSpace(pl.Title)
			if _, exists := books[pl.BookID]; pl.BookID == "" || title == "" || exists {
				continue
			}
			books[pl.BookID] = &Book{
				Title:       title,
				Author:      strings.TrimSpace(pl.Author),
				Format:      strings.TrimSpace(pl.Format),
				SHA256:      strings.TrimSpace(pl.SHA256),
				CreatedAt:   event.TS,
				UpdatedAt:   event.TS,
				Annotations: []string{},
			}

		case "book.edited":
			pl, err := decodePayload[struct {
				BookID string  `json:"book_id"`
				Title  *string `json:"title"`
				Author *string `json:"author"`
			}](event)
			if err != nil {
				return BooksState{}, err
			}
			book, ok := books[pl.BookID]
			if !ok {
				continue
			}
			edited := false
			if pl.Title != nil {
				if title := strings.TrimSpace(*pl.Title); title != "" {
					book.Title = title
					edited = true
				}
			}
			if pl.Author != nil {
				book.Author = strings.TrimSpace(*pl.Author)
				edited = true
			}
			if edited {
				book.UpdatedAt = event.TS
			}

		case "book.archived":
			pl, err := decodePayload[struct {
				BookID string `json:"book_id"`
			}](event)
			if err != nil {
				return BooksState{}, err
			}
			if book, ok := books[pl.BookID]; ok {
				book.Archived = true
			}

		case "book.progressed":
			pl, err := decodePayload[struct {
				BookID  string          `json:"book_id"`
				Loc     json.RawMessage `json:"loc"`
				Percent any             `json:"percent"`
			}](event)
			if err != nil {
				return BooksState{}, err
			}
			book, ok := books[pl.BookID]
			loc, valid := ParseLoc(pl.Loc)
			if !ok || !valid {
				continue
			}
			ts := event.TS
			book.Progress, book.ProgressAt, book.ProgressPercent = &loc, &ts, nil
			if f, ok := pl.Percent.(float64); ok && f >= 0 && f <= 100 && f == math.Trunc(f) {
				percent := int(f)
				book.ProgressPercent = &percent
			}
			book.UpdatedAt = event.TS

		case "annotation.created":
			pl, err := decodePayload[struct {
				AnnotationID string          `json:"annotation_id"`
				BookID       string          `json:"book_id"`
				Loc          json.RawMessage `json:"loc"`
				Quote        string          `json:"quote"`
				Text         string          `json:"text"`
			}](event)
			if err != nil {
				return BooksState{}, err
			}
			book, ok := books[pl.BookID]
			loc, valid := ParseLoc(pl.Loc)
			quote := strings.TrimSpace(pl.Quote)
			_, exists := notes[pl.AnnotationID]
			if pl.AnnotationID == "" || exists || deleted[pl.AnnotationID] || !ok || !valid || quote == "" {
				continue
			}
			notes[pl.AnnotationID] = &Annotation{
				BookID:    pl.BookID,
				Loc:       loc,
				Quote:     quote,
				Text:      strings.TrimSpace(pl.Text),
				CreatedAt: event.TS,
				UpdatedAt: event.TS,
			}
			book.UpdatedAt = event.TS

		case "annotation.edited":
			pl, err := decodePayload[struct {
				AnnotationID string  `json:"annotation_id"`
				Text         *string `json:"text"`
			}](event)
			if err != nil {
				return BooksState{}, err
			}
			note, ok := notes[pl.AnnotationID]
			if !ok || pl.Text == nil {
				continue
			}
			note.Text = strings.TrimSpace(*pl.Text)
			note.UpdatedAt = event.TS
			books[note.BookID].UpdatedAt = event.TS

		case "annotation.deleted":
			pl, err := decodePayload[struct {
				AnnotationID string `json:"annotation_id"`
			}](event)
			if err != nil {
				return BooksState{}, err
			}
			note, ok := notes[pl.AnnotationID]
			if !ok {
				continue
			}
			delete(notes, pl.AnnotationID)
			deleted[pl.AnnotationID] = true
			books[note.BookID].UpdatedAt = event.TS
		}
	}

	state := BooksState{
		Books:       make(map[string]Book, len(books)),
		Annotations: make(map[string]Annotation, len(notes)),
		Order:       []string{},
	}
	for id, note := range notes {
		state.Annotations[id] = *note
		book := books[note.BookID]
		book.Annotations = append(book.Annotations, id)
	}
	for id, book := range books {
		slices.SortFunc(book.Annotations, func(a, b string) int {
			na, nb := state.Annotations[a], state.Annotations[b]
			return cmp.Or(
				cmp.Compare(na.Loc.Chapter, nb.Loc.Chapter),
				cmp.Compare(na.Loc.Para, nb.Loc.Para),
				strings.Compare(na.CreatedAt, nb.CreatedAt),
				strings.Compare(a, b),
			)
		})
		state.Books[id] = *book
		if !book.Archived {
			state.Order = append(state.Order, id)
		}
	}
	slices.SortFunc(state.Order, func(a, b string) int {
		return cmp.Or(
			strings.Compare(state.Books[b].UpdatedAt, state.Books[a].UpdatedAt), // mais recente primeiro
			strings.Compare(a, b),
		)
	})
	return state, nil
}
