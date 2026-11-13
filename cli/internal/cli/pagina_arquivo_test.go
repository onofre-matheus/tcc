package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/store"
)

func internalApp(t *testing.T) *app.App {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &app.App{Store: s, Dir: dir, TZ: "America/Sao_Paulo", Out: &bytes.Buffer{},
		Now: func() time.Time { return time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC) }}
}

func appendOrFail(t *testing.T, a *app.App, eventType string, payload map[string]any) {
	t.Helper()
	if _, err := a.Store.Append(eventType, payload); err != nil {
		t.Fatal(err)
	}
}

// O foco recebe só as páginas ativas ligadas à tarefa, e a edição de dentro
// dele devolve a linha de status em vez de imprimir por cima da tela.
func TestTaskPagesEFocusEditPage(t *testing.T) {
	a := internalApp(t)
	appendOrFail(t, a, "task.created", map[string]any{"task_id": "t1", "title": "Documentar"})
	for _, p := range []struct{ id, title string }{{"p1", "docs cli"}, {"p2", "Arquivada"}} {
		appendOrFail(t, a, "page.created", map[string]any{"page_id": p.id, "title": p.title, "body": "v1", "tags": []string{}})
		appendOrFail(t, a, "link.created", map[string]any{"link_id": "l-" + p.id,
			"from": map[string]any{"kind": "task", "id": "t1"}, "to": map[string]any{"kind": "page", "id": p.id}})
	}
	appendOrFail(t, a, "page.archived", map[string]any{"page_id": "p2"})

	pages, books, err := taskLinked(a, "t1")
	if err != nil || len(pages) != 1 || pages[0].ID != "p1" || pages[0].Title != "docs cli" || len(books) != 0 {
		t.Fatalf("só a página ativa ligada à tarefa: %v %v %v", pages, books, err)
	}

	a.Editor = func(path string) error { return os.WriteFile(path, []byte("v2\n"), 0o600) }
	status, err := focusEditPage(a)("p1")
	if err != nil || status != "✔ página salva: docs cli" {
		t.Fatalf("a edição devolve a linha de status: %q %v", status, err)
	}
	if out := a.Out.(*bytes.Buffer).String(); strings.TrimSpace(out) != "" {
		t.Fatalf("nada é impresso por cima da tela de foco: %q", out)
	}
}

func TestCleanBody(t *testing.T) {
	for in, want := range map[string]string{
		"\n\n  código\r\nfim\r\n\n":  "  código\nfim",              // CRLF, linhas em branco no começo, indentação da 1ª linha
		"\ufefftexto\n":              "texto",                      // BOM do editor
		"---\ntitulo: x\n---\ncorpo": "---\ntitulo: x\n---\ncorpo", // corpo que começa com --- fica como está
		" \n\t\n":                    "",
	} {
		if got := cleanBody(in); got != want {
			t.Fatalf("cleanBody(%q) = %q, esperado %q", in, got, want)
		}
	}
	if got := bodyFile("texto"); got != "texto\n" {
		t.Fatalf("o temporário termina com \\n: %q", got)
	}
	if got := bodyFile(""); got != "" {
		t.Fatalf("página vazia abre o editor vazio: %q", got)
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Manutenção do Cluster!": "manutencao-do-cluster",
		"":                       "pagina",
		"???":                    "pagina",
	} {
		if got := slug(in); got != want {
			t.Fatalf("slug(%q) = %q, esperado %q", in, got, want)
		}
	}
}
