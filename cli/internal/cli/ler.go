// O acervo por trás do modo de leitura (tui/leitura.go): o texto vem do EPUB
// deste dispositivo; as anotações e o "onde parei", do log. Os comandos
// viram os eventos do catálogo 1.7 (annotation.created/edited/deleted,
// book.progressed) e os que já existem (card.created v2 com a fonte e
// link.created livro → cartão, note.captured). O TUI só desenha e chama. É
// o mesmo acervo no `pnn ler` e no `pnn foco` de uma tarefa ligada a livros.
package cli

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/epub"
	"github.com/onofre-matheus/tcc/cli/internal/tui"
	"github.com/onofre-matheus/tcc/cli/store"
)

type readActions struct {
	a *app.App
	// opened: os EPUBs já abertos, por livro. O `pnn ler` abre o seu antes
	// de tomar a tela (o erro sai no terminal); no foco, cada livro abre no
	// primeiro Ctrl+O, e o erro vira a linha de status.
	opened map[string]*epub.Book
}

var _ tui.ReadActions = readActions{}

func newReadActions(a *app.App) readActions {
	return readActions{a: a, opened: map[string]*epub.Book{}}
}

// open abre o EPUB do livro neste dispositivo (uma vez por execução).
func (r readActions) open(bookID string) (*epub.Book, error) {
	if book, ok := r.opened[bookID]; ok {
		return book, nil
	}
	path, err := bookFile(r.a, bookID, "N")
	if err != nil {
		return nil, err
	}
	book, err := openEPUB(path)
	if err != nil {
		return nil, err
	}
	r.opened[bookID] = book
	return book, nil
}

func (r readActions) Load(bookID string) (tui.BookText, error) {
	state, err := loadBooks(r.a)
	if err != nil {
		return tui.BookText{}, err
	}
	b, ok := state.Books[bookID]
	if !ok {
		return tui.BookText{}, fmt.Errorf("o livro não existe mais — rode `pnn livros`")
	}
	book, err := r.open(bookID)
	if err != nil {
		return tui.BookText{}, err
	}
	text := tui.BookText{Title: b.Title, Author: b.Author}
	for _, ch := range book.Chapters {
		out := tui.BookChapter{Spine: ch.Spine, Title: ch.Title}
		for _, block := range ch.Blocks {
			out.Blocks = append(out.Blocks, tui.BookBlock{Kind: block.Kind, Text: block.Text, Page: block.Page})
		}
		text.Chapters = append(text.Chapters, out)
	}
	if b.Progress != nil {
		text.Start = tui.BookLoc{Chapter: b.Progress.Chapter, Para: b.Progress.Para}
	}
	for _, id := range b.Annotations {
		an := state.Annotations[id]
		text.Annotations = append(text.Annotations, tui.BookAnnotation{
			ID: id, At: tui.BookLoc{Chapter: an.Loc.Chapter, Para: an.Loc.Para},
			Page: an.Loc.Page, Quote: an.Quote, Text: an.Text,
		})
	}
	return text, nil
}

func locPayload(at tui.BookLoc, page string) map[string]any {
	loc := map[string]any{"chapter": at.Chapter, "para": at.Para}
	if page != "" {
		loc["page"] = page
	}
	return loc
}

func (r readActions) Annotate(bookID string, at tui.BookLoc, page, quote, text string) (tui.BookAnnotation, string, error) {
	id := store.UUIDv7()
	payload := map[string]any{"annotation_id": id, "book_id": bookID, "loc": locPayload(at, page), "quote": quote}
	if text != "" {
		payload["text"] = text
	}
	if _, err := r.a.Store.Append("annotation.created", payload); err != nil {
		return tui.BookAnnotation{}, "", err
	}
	status := "✎ anotado"
	if text == "" {
		status = "▍ grifado"
	}
	if page != "" {
		status += " (p. " + page + ")"
	}
	return tui.BookAnnotation{ID: id, At: at, Page: page, Quote: quote, Text: text}, status, nil
}

func (r readActions) EditAnnotation(id, text string) (string, error) {
	if _, err := r.a.Store.Append("annotation.edited", map[string]any{"annotation_id": id, "text": strings.TrimSpace(text)}); err != nil {
		return "", err
	}
	return "✎ anotação editada", nil
}

func (r readActions) DeleteAnnotation(id string) (string, error) {
	if _, err := r.a.Store.Append("annotation.deleted", map[string]any{"annotation_id": id}); err != nil {
		return "", err
	}
	return "✗ anotação apagada", nil
}

func (r readActions) Progress(bookID string, at tui.BookLoc, page string, percent int, sessionID string) error {
	payload := map[string]any{"book_id": bookID, "loc": locPayload(at, page), "percent": percent}
	if sessionID != "" {
		payload["session_id"] = sessionID
	}
	_, err := r.a.Store.Append("book.progressed", payload)
	return err
}

// CreateCard: cartão solto (card.created v2, sem deck) com a fonte no
// source_title — o mesmo campo que a extensão preenche com a página da web —
// e ligado ao livro por sinapse: estudar ao redor do livro o traz.
func (r readActions) CreateCard(bookID, source, front, back string) (string, error) {
	cardID := store.UUIDv7()
	if _, err := r.a.Store.AppendV("card.created", 2, map[string]any{
		"card_id": cardID, "front": front, "back": back, "source_title": source, "tags": []string{},
	}); err != nil {
		return "", err
	}
	if _, err := r.a.Store.Append("link.created", map[string]any{
		"link_id": store.UUIDv7(), "from": nodePayload(core.Node{Kind: core.NodeBook, ID: bookID}),
		"to": nodePayload(core.Node{Kind: core.NodeCard, ID: cardID}),
	}); err != nil {
		return "", err
	}
	return "✔ cartão criado e ligado ao livro (fonte: " + source + "): " + front, nil
}

func (r readActions) CaptureNote(text string) (string, error) {
	return writeActions{r.a}.CaptureNote(text)
}

func newLerCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "ler N|título",
		Short: "abre o livro no leitor do pnn, de onde parou, com os comandos / para anotar",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			state, err := loadBooks(a)
			if err != nil {
				return err
			}
			query := strings.Join(args, " ")
			id, err := findBook(a, state, query)
			if err != nil || id == "" {
				return err
			}
			if _, err := bookFile(a, id, query); err != nil {
				return err // com a dica de como religar, antes de tomar a tela
			}
			actions := newReadActions(a)
			if _, err := actions.open(id); err != nil {
				return err
			}
			model := tui.NewFoco(tui.FocoConfig{
				Standalone:   true,
				Read:         actions,
				Books:        []tui.LinkedBook{{ID: id, Title: state.Books[id].Title}},
				StartReading: true,
				Emit:         emitTo(a),
			})
			final, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
			if err != nil {
				return err
			}
			foco := final.(tui.Foco)
			if foco.Err != nil {
				return foco.Err
			}
			if status := foco.Status(); status != "" {
				fmt.Fprintln(a.Out, status)
			}
			return nil
		},
	}
}
