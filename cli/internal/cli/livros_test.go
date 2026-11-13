// Livros (catálogo 1.7) nas telas one-shot: cadastrar (o pnn guarda a
// cópia), listar, restaurar pelo hash numa pasta, anotações numeradas para
// editar e apagar, arquivar. O arquivo fica na pasta de livros do pnn — nunca
// no log.
package cli_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/store"
)

// writeEPUB grava um EPUB3 mínimo: um capítulo por corpo, com título no nav.
func writeEPUB(t *testing.T, dir, name, title, author string, bodies ...string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	add := func(name, content string) {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(content))
	}
	add("META-INF/container.xml", `<container><rootfiles><rootfile full-path="content.opf"/></rootfiles></container>`)
	var manifest, spine, nav strings.Builder
	for i, body := range bodies {
		id := "c" + string(rune('1'+i))
		manifest.WriteString(`<item id="` + id + `" href="` + id + `.xhtml" media-type="application/xhtml+xml"/>`)
		spine.WriteString(`<itemref idref="` + id + `"/>`)
		nav.WriteString(`<li><a href="` + id + `.xhtml">Capítulo ` + string(rune('A'+i)) + `</a></li>`)
		add(id+".xhtml", `<html xmlns:epub="http://www.idpf.org/2007/ops"><head><title/></head><body>`+body+`</body></html>`)
	}
	creator := ""
	if author != "" {
		creator = "<dc:creator>" + author + "</dc:creator>"
	}
	add("content.opf", `<package><metadata><dc:title>`+title+`</dc:title>`+creator+`</metadata><manifest>`+
		`<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>`+manifest.String()+
		`</manifest><spine>`+spine.String()+`</spine></package>`)
	add("nav.xhtml", `<html xmlns:epub="http://www.idpf.org/2007/ops"><body><nav epub:type="toc"><ol>`+nav.String()+`</ol></nav></body></html>`)
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// storedBooks: os EPUBs guardados na pasta de livros do pnn, por id.
func storedBooks(t *testing.T, a *app.App) map[string][]byte {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(a.Dir, "livros", "*.epub"))
	out := map[string][]byte{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		out[strings.TrimSuffix(filepath.Base(f), ".epub")] = raw
	}
	return out
}

// bookIDByTitle: o id do livro pelo título, via `pnn livros --json`.
func bookIDByTitle(t *testing.T, a *app.App, title string) string {
	t.Helper()
	var state struct {
		Books map[string]struct {
			Title string `json:"title"`
		} `json:"books"`
	}
	if err := json.Unmarshal([]byte(run(t, a, "livros", "--json")), &state); err != nil {
		t.Fatal(err)
	}
	for id, b := range state.Books {
		if b.Title == title {
			return id
		}
	}
	t.Fatalf("livro %q não cadastrado", title)
	return ""
}

func TestLivroCadastraGuardaACopiaEOCaminhoNaoVaiParaOLog(t *testing.T) {
	a, _ := newTestApp(t)
	dir := t.TempDir()
	path := writeEPUB(t, dir, "safren.epub", "Mastering Your Adult ADHD", "Steven A. Safren",
		`<h1>Psicoeducação</h1><span epub:type="pagebreak" title="9"/><p>CBT is like a ladder.</p>`,
		`<p>Calendário único.</p>`)

	t.Setenv("PNN_LIVROS_SENHA", "")
	out := run(t, a, "livro", path)
	if !strings.Contains(out, "📖 cadastrado: Mastering Your Adult ADHD — Steven A. Safren") ||
		!strings.Contains(out, "2 capítulos, com a paginação impressa") || !strings.Contains(out, "pnn ler 1") ||
		!strings.Contains(out, "guardado no pnn") || !strings.Contains(out, "defina PNN_LIVROS_SENHA") {
		t.Fatalf("saída do cadastro: %s", out)
	}
	if types := eventTypes(t, a); len(types) != 1 || types[0] != "book.registered" {
		t.Fatalf("um book.registered: %v", types)
	}
	original, _ := os.ReadFile(path)
	stored := storedBooks(t, a)
	if len(stored) != 1 {
		t.Fatalf("uma cópia guardada: %v", stored)
	}
	for id, body := range stored {
		if string(body) != string(original) {
			t.Fatal("a cópia é o EPUB original")
		}
		if info, _ := os.Stat(filepath.Join(a.Dir, "livros", id+".epub")); info.Mode().Perm() != 0o600 {
			t.Fatalf("cópia 0600: %v", info.Mode())
		}
	}
	// o original pode sumir: o pnn usa a cópia
	os.Remove(path)
	run(t, a, "livros")
	if out := run(t, a, "anotacoes", "1"); !strings.Contains(out, "nenhuma anotação") {
		t.Fatalf("o livro segue utilizável sem o original: %s", out)
	}
	os.WriteFile(path, original, 0o644)
	raw, err := os.ReadFile(filepath.Join(a.Dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), dir) || strings.Contains(string(raw), "safren.epub") {
		t.Fatalf("o caminho do arquivo não pode ir para o log: %s", raw)
	}

	// o mesmo arquivo (pelo conteúdo), noutro lugar: não cadastra de novo
	moved := filepath.Join(t.TempDir(), "outro-nome.epub")
	os.WriteFile(moved, original, 0o644)
	out = run(t, a, "livro", moved)
	if !strings.Contains(out, "já cadastrado") || !strings.Contains(out, "já está guardado") || len(eventTypes(t, a)) != 1 {
		t.Fatalf("mesmo arquivo não cadastra de novo: %s %v", out, eventTypes(t, a))
	}

	out = run(t, a, "livros")
	if !strings.Contains(out, " 1  Mastering Your Adult ADHD — Steven A. Safren  não começado") {
		t.Fatalf("lista: %s", out)
	}
	out = run(t, a, "livro", writeEPUB(t, dir, "sem-autor.epub", "Outro livro", "", `<p>texto</p>`), "--titulo", "Título meu")
	if !strings.Contains(out, "📖 cadastrado: Título meu (1 capítulos)") {
		t.Fatalf("--titulo vence o do EPUB: %s", out)
	}
}

func TestLivroRecusaMOBIEPDFComASaida(t *testing.T) {
	a, _ := newTestApp(t)
	dir := t.TempDir()
	for name, want := range map[string]string{
		"livro.mobi": "ebook-convert",
		"livro.azw3": "Calibre",
		"livro.pdf":  "não PDF",
	} {
		path := filepath.Join(dir, name)
		os.WriteFile(path, []byte("x"), 0o644)
		err := runErr(t, a, "livro", path)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: esperava erro com %q, veio %v", name, want, err)
		}
	}
	path := filepath.Join(dir, "quebrado.epub")
	os.WriteFile(path, []byte("não é zip"), 0o644)
	if err := runErr(t, a, "livro", path); err == nil || !strings.Contains(err.Error(), "não é um EPUB válido") {
		t.Fatalf("EPUB quebrado: %v", err)
	}
	if len(eventTypes(t, a)) != 0 {
		t.Fatal("nada é gravado quando o arquivo não serve")
	}
}

// Outra máquina sem o arquivo (o log chegou, o livro ainda não): a lista
// avisa, o ler explica, e `procurar` guarda de novo pelo conteúdo — sem
// bucket; --cadastrar cadastra o resto da pasta.
func TestLivrosProcurarGuardaDeNovoPeloHash(t *testing.T) {
	a, _ := newTestApp(t)
	src := t.TempDir()
	path := writeEPUB(t, src, "a.epub", "Livro A", "", `<p>a</p>`)
	run(t, a, "livro", path)
	os.RemoveAll(filepath.Join(a.Dir, "livros"))

	out := run(t, a, "livros")
	if !strings.Contains(out, "⚠ ainda não baixado — pnn sync") {
		t.Fatalf("sem o arquivo neste dispositivo: %s", out)
	}
	if err := runErr(t, a, "ler", "1"); err == nil || !strings.Contains(err.Error(), "pnn sync") {
		t.Fatalf("ler sem arquivo explica como trazer: %v", err)
	}

	lib := t.TempDir()
	content, _ := os.ReadFile(path)
	os.MkdirAll(filepath.Join(lib, "sub"), 0o755)
	os.WriteFile(filepath.Join(lib, "sub", "A renomeado.EPUB"), content, 0o644)
	writeEPUB(t, lib, "b.epub", "Livro B", "", `<p>b</p>`)

	out = run(t, a, "livros", "procurar", lib)
	if !strings.Contains(out, "2 EPUB(s)") || !strings.Contains(out, "✔ guardado de novo: Livro A") || !strings.Contains(out, "1 não cadastrado(s)") || !strings.Contains(out, "b.epub") {
		t.Fatalf("procurar: %s", out)
	}
	if len(eventTypes(t, a)) != 1 {
		t.Fatal("procurar sem --cadastrar não grava evento")
	}
	out = run(t, a, "livros", "procurar", lib, "--cadastrar")
	if !strings.Contains(out, "1 já guardado(s)") || !strings.Contains(out, "📖 cadastrado: Livro B") {
		t.Fatalf("procurar --cadastrar: %s", out)
	}
	if stored := storedBooks(t, a); len(stored) != 2 {
		t.Fatalf("duas cópias guardadas: %d", len(stored))
	}
}

func TestAnotacoesEditarApagarArquivar(t *testing.T) {
	a, _ := newTestApp(t)
	path := writeEPUB(t, t.TempDir(), "l.epub", "Livro", "Autora",
		`<p>primeiro</p><p>segundo</p>`, `<p>terceiro</p>`)
	run(t, a, "livro", path)
	bookID := bookIDByTitle(t, a, "Livro")
	run(t, a, "livros")
	appendEvent := func(eventType string, payload map[string]any) {
		if _, err := a.Store.Append(eventType, payload); err != nil {
			t.Fatal(err)
		}
	}
	appendEvent("annotation.created", map[string]any{"annotation_id": "n2", "book_id": bookID,
		"loc": map[string]any{"chapter": 2, "para": 1, "page": "7"}, "quote": "terceiro"})
	appendEvent("annotation.created", map[string]any{"annotation_id": "n1", "book_id": bookID,
		"loc": map[string]any{"chapter": 1, "para": 2}, "quote": "segundo", "text": "comentário"})

	out := run(t, a, "anotacoes", "livro")
	for _, want := range []string{"📖 Livro — Autora · 2 anotações", "cap. 1 · Capítulo A", " 1  ✎ \"segundo\"", "      comentário", "cap. 2 · Capítulo B", ` 2  ▍ "terceiro" (p. 7)`} {
		if !strings.Contains(out, want) {
			t.Fatalf("anotações em ordem de leitura — falta %q:\n%s", want, out)
		}
	}
	run(t, a, "editar", "2", "virou nota")
	run(t, a, "apagar", "1")
	if err := runErr(t, a, "anotacoes", "1"); err == nil {
		t.Fatal("a última tela agora é de anotações, não de livros")
	}
	run(t, a, "livros")
	out = run(t, a, "anotacoes", "1")
	if strings.Contains(out, "segundo") || !strings.Contains(out, "virou nota") || !strings.Contains(out, "1 anotação") {
		t.Fatalf("editar e apagar pela lista numerada:\n%s", out)
	}

	run(t, a, "livros")
	run(t, a, "editar", "1", "Livro renomeado")
	if err := runErr(t, a, "apagar", "1"); err == nil || !strings.Contains(err.Error(), "arquivar") {
		t.Fatalf("livro não se apaga: %v", err)
	}
	run(t, a, "arquivar", "1")
	if out := run(t, a, "livros"); !strings.Contains(out, "nenhum livro") {
		t.Fatalf("arquivado sai da lista: %s", out)
	}
	if out := run(t, a, "livros", "--arquivados"); !strings.Contains(out, "Livro renomeado — Autora  arquivado · 1 anotação") {
		t.Fatalf("--arquivados: %s", out)
	}
	raw := run(t, a, "livros", "--json")
	var state struct {
		Books map[string]struct {
			Title    string `json:"title"`
			Archived bool   `json:"archived"`
		} `json:"books"`
	}
	if err := json.Unmarshal([]byte(raw), &state); err != nil || state.Books[bookID].Title != "Livro renomeado" || !state.Books[bookID].Archived {
		t.Fatalf("--json emite a projeção: %v %s", err, raw)
	}
}

// Etapa 2: livro como nó de sinapse. A tarefa se liga ao livro por um trecho
// do título (títulos de livro são longos); livro não ganha tag; a lista mostra
// as sinapses; a semana mostra a leitura do dia.
func TestLigarTarefaALivroESemana(t *testing.T) {
	dir := t.TempDir()
	clock := func() time.Time { return time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC) }
	s, err := store.Open(dir, store.WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	a := &app.App{Store: s, Dir: dir, TZ: "America/Sao_Paulo", Now: clock, Out: &bytes.Buffer{}}
	run(t, a, "livro", writeEPUB(t, t.TempDir(), "s.epub", "Mastering Your Adult ADHD", "Safren", `<p>CBT is like a ladder.</p>`))
	run(t, a, "livro", writeEPUB(t, t.TempDir(), "k.epub", "Understanding the Linux Kernel", "", `<p>kernel</p>`))
	run(t, a, "t", "Ler a sessão 1")
	run(t, a, "dia")

	out := run(t, a, "ligar", "1", "adult adhd")
	if !strings.Contains(out, "⚡ sinapse: tarefa Ler a sessão 1 → livro Mastering Your Adult ADHD") {
		t.Fatalf("ligar pelo trecho do título: %s", out)
	}
	if err := runErr(t, a, "ligar", "1", "u"); err == nil || !strings.Contains(err.Error(), "2 livros casam") {
		t.Fatalf("trecho ambíguo pede o número: %v", err)
	}
	out = run(t, a, "livros")
	if !strings.Contains(out, "Mastering Your Adult ADHD — Safren  não começado · ⚡1") {
		t.Fatalf("a lista mostra a sinapse: %s", out)
	}
	if err := runErr(t, a, "ligar", "1", "#tdah"); err == nil || !strings.Contains(err.Error(), "livro não tem tags") {
		t.Fatalf("livro não ganha tag: %v", err)
	}
	out = run(t, a, "sinapses")
	if !strings.Contains(out, "livro Mastering Your Adult ADHD") {
		t.Fatalf("sinapses rotulam o livro: %s", out)
	}

	bookID := bookIDByTitle(t, a, "Mastering Your Adult ADHD")
	a.Store.Append("annotation.created", map[string]any{"annotation_id": "a1", "book_id": bookID,
		"loc": map[string]any{"chapter": 1, "para": 1}, "quote": "CBT is like a ladder."})
	a.Store.Append("book.progressed", map[string]any{"book_id": bookID, "loc": map[string]any{"chapter": 1, "para": 1}, "percent": 0})
	out = run(t, a, "semana")
	if !strings.Contains(out, "📖 1 livro, 1 anotação") || !strings.Contains(out, "leitura: 1 livro · 1 anotação") {
		t.Fatalf("a semana mostra a leitura:\n%s", out)
	}
	if out := run(t, a, "dia"); !strings.Contains(out, "1 dia") {
		t.Fatalf("ler conta como dia estudado no streak:\n%s", out)
	}
}
