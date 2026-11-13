package cli

import (
	"strings"
	"testing"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/epub"
	"github.com/onofre-matheus/tcc/cli/internal/tui"
)

// O acervo do modo de leitura, sobre o log de verdade: anotar, editar,
// apagar, o "onde parei" e o cartão com a fonte viram os eventos do
// catálogo; Load junta o texto do EPUB com o que o log sabe.
func TestReadActionsSobreOLog(t *testing.T) {
	a := internalApp(t)
	appendOrFail(t, a, "book.registered", map[string]any{"book_id": "b1", "title": "Livro", "author": "Autora", "format": "epub", "sha256": "x"})
	book := &epub.Book{Title: "Título do EPUB", Chapters: []epub.Chapter{
		{Spine: 2, Title: "Um", Blocks: []epub.Block{{Kind: epub.KindHeading, Text: "Um"}, {Kind: epub.KindPara, Text: "CBT is like a ladder.", Page: "9"}}},
		{Spine: 4, Title: "Dois", Blocks: []epub.Block{{Kind: epub.KindPara, Text: "fim"}}},
	}}
	r := readActions{a: a, opened: map[string]*epub.Book{"b1": book}}

	an, status, err := r.Annotate("b1", tui.BookLoc{Chapter: 2, Para: 2}, "9", "CBT is like a ladder.", "escada")
	if err != nil || status != "✎ anotado (p. 9)" || an.ID == "" {
		t.Fatalf("Annotate: %+v %q %v", an, status, err)
	}
	if _, status, _ := r.Annotate("b1", tui.BookLoc{Chapter: 4, Para: 1}, "", "fim", ""); status != "▍ grifado" {
		t.Fatalf("grifo: %q", status)
	}
	created := lastOf(t, writeActions{a}, "annotation.created")
	if _, hasText := created["text"]; hasText || created["loc"].(map[string]any)["page"] != nil {
		t.Fatalf("grifo sem text e sem page vazia: %v", created)
	}
	if _, err := r.EditAnnotation(an.ID, " escada de Linehan "); err != nil {
		t.Fatal(err)
	}
	if err := r.Progress("b1", tui.BookLoc{Chapter: 4, Para: 1}, "", 66, "s1"); err != nil {
		t.Fatal(err)
	}
	if p := lastOf(t, writeActions{a}, "book.progressed"); p["session_id"] != "s1" || p["percent"] != float64(66) {
		t.Fatalf("o progresso leva a sessão e o %%: %v", p)
	}
	if status, err := r.CreateCard("b1", "Livro, cap. 1 (Um), p. 9", "Metáfora?", "escada"); err != nil || status == "" {
		t.Fatal(err)
	}
	card := lastOf(t, writeActions{a}, "card.created")
	if card["source_title"] != "Livro, cap. 1 (Um), p. 9" || card["deck_id"] != nil {
		t.Fatalf("cartão solto com a fonte: %v", card)
	}
	link := lastOf(t, writeActions{a}, "link.created")
	if from := link["from"].(map[string]any); from["kind"] != "book" || from["id"] != "b1" || link["to"].(map[string]any)["id"] != card["card_id"] {
		t.Fatalf("o cartão fica ligado ao livro: %v", link)
	}

	text, err := r.Load("b1")
	if err != nil {
		t.Fatal(err)
	}
	if text.Title != "Livro" || text.Author != "Autora" || len(text.Chapters) != 2 || text.Chapters[1].Spine != 4 {
		t.Fatalf("título do log, texto do EPUB: %+v", text)
	}
	if text.Start != (tui.BookLoc{Chapter: 4, Para: 1}) {
		t.Fatalf("abre no onde parei: %+v", text.Start)
	}
	if len(text.Annotations) != 2 || text.Annotations[0].Text != "escada de Linehan" || text.Annotations[0].Page != "9" {
		t.Fatalf("anotações em ordem de leitura: %+v", text.Annotations)
	}

	if _, err := r.DeleteAnnotation(an.ID); err != nil {
		t.Fatal(err)
	}
	events, _ := a.Store.Events()
	state, err := core.Books(events)
	if err != nil {
		t.Fatal(err)
	}
	b := state.Books["b1"]
	if len(b.Annotations) != 1 || b.ProgressPercent == nil || *b.ProgressPercent != 66 {
		t.Fatalf("projeção depois de apagar: %+v", b)
	}
}

// O foco de uma tarefa recebe os livros ligados a ela (não os arquivados), e
// o /ligar do foco acha livros pelo título.
func TestTarefaLigadaALivroNoFoco(t *testing.T) {
	a := internalApp(t)
	appendOrFail(t, a, "task.created", map[string]any{"task_id": "t1", "title": "Ler a sessão 1"})
	for _, b := range []struct{ id, title string }{{"b1", "Mastering Your Adult ADHD"}, {"b2", "Livro arquivado ADHD"}} {
		appendOrFail(t, a, "book.registered", map[string]any{"book_id": b.id, "title": b.title, "format": "epub", "sha256": b.id})
		appendOrFail(t, a, "link.created", map[string]any{"link_id": "l-" + b.id,
			"from": map[string]any{"kind": "task", "id": "t1"}, "to": map[string]any{"kind": "book", "id": b.id}})
	}
	appendOrFail(t, a, "book.archived", map[string]any{"book_id": "b2"})

	pages, books, err := taskLinked(a, "t1")
	if err != nil || len(pages) != 0 || len(books) != 1 || books[0] != (tui.LinkedBook{ID: "b1", Title: "Mastering Your Adult ADHD"}) {
		t.Fatalf("só o livro ativo: %v %v %v", pages, books, err)
	}
	found, err := writeActions{a}.Search("t1", "adhd")
	if err != nil || len(found) != 1 || found[0].Kind != "book" || found[0].Label != "livro Mastering Your Adult ADHD" {
		t.Fatalf("/ligar acha o livro: %v %v", found, err)
	}

	// no foco, o livro abre na hora do Ctrl+O; sem arquivo aqui, o erro diz
	// como religar
	if _, err := newReadActions(a).Load("b1"); err == nil || !strings.Contains(err.Error(), "pnn sync") {
		t.Fatalf("livro sem arquivo: %v", err)
	}
}
