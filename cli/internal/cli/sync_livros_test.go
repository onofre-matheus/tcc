package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"strings"
	stdsync "sync"
	"testing"

	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/sync"
)

// memBucket é um bucket em memória com o contrato do sync (List/Get/Put).
type memBucket struct {
	mu      stdsync.Mutex
	objects map[string][]byte
}

func (b *memBucket) List(_ context.Context, prefix string) ([]sync.Object, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []sync.Object
	for key, body := range b.objects {
		if strings.HasPrefix(key, prefix) {
			out = append(out, sync.Object{Key: key, ETag: string(rune(len(body)))})
		}
	}
	return out, nil
}

func (b *memBucket) Get(_ context.Context, key string) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.objects[key]), nil
}

func (b *memBucket) Put(_ context.Context, key string, body []byte) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.objects[key] = bytes.Clone(body)
	return "etag", nil
}

func cmdRun(t *testing.T, a *app.App, args ...string) string {
	t.Helper()
	buf := a.Out.(*bytes.Buffer)
	buf.Reset()
	root := NewRoot(a)
	root.SetArgs(args)
	root.SetOut(a.Out)
	root.SetErr(a.Out)
	if err := root.Execute(); err != nil {
		t.Fatalf("pnn %v: %v\n%s", args, err, buf.String())
	}
	return buf.String()
}

func syncWith(t *testing.T, a *app.App, bucket sync.Bucket, passphrase string) string {
	t.Helper()
	buf := a.Out.(*bytes.Buffer)
	buf.Reset()
	if err := runSync(context.Background(), a, bucket, sync.Config{Prefix: "pnn/", BookPassphrase: passphrase}); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// O cadastro feito num dispositivo vira livro legível no outro só pelo
// `pnn sync`: o evento vai pelo log, o EPUB vai cifrado com a senha dos
// livros, e quem recebe confere o sha256 e guarda na pasta do pnn.
func TestSyncLevaOLivroCifradoAOutroDispositivo(t *testing.T) {
	bucket := &memBucket{objects: map[string][]byte{}}
	a, b := internalApp(t), internalApp(t)
	dir := t.TempDir()
	epubPath := dir + "/a.epub"
	writeTestEPUB(t, epubPath, "Livro A")
	cmdRun(t, a, "livro", epubPath)

	out := syncWith(t, a, bucket, "")
	if !strings.Contains(out, "▲ 1 enviado(s)") || !strings.Contains(out, "1 livro espera") {
		t.Fatalf("sem a senha, o evento vai e o livro espera:\n%s", out)
	}
	for key := range bucket.objects {
		if strings.Contains(key, "livros/") {
			t.Fatal("sem senha, nenhum livro no bucket")
		}
	}

	out = syncWith(t, a, bucket, "s3nha")
	if !strings.Contains(out, "▲ 📖 Livro A") || !strings.Contains(out, "cifrado") {
		t.Fatalf("com a senha, o livro sobe:\n%s", out)
	}
	original, _ := os.ReadFile(epubPath)
	for key, body := range bucket.objects {
		if strings.Contains(key, "livros/") && (!strings.HasSuffix(key, ".pnnlivro") || bytes.Contains(body, original[:40])) {
			t.Fatalf("no bucket, só o objeto cifrado: %s", key)
		}
	}

	out = syncWith(t, b, bucket, "senha errada")
	if !strings.Contains(out, "▼ 1 recebido(s)") || !strings.Contains(out, "⚠ 📖 Livro A: não abriu") {
		t.Fatalf("senha errada: o evento chega, o livro não abre:\n%s", out)
	}
	if out := cmdRun(t, b, "livros"); !strings.Contains(out, "ainda não baixado") {
		t.Fatalf("B ainda sem o arquivo:\n%s", out)
	}

	out = syncWith(t, b, bucket, "s3nha")
	if !strings.Contains(out, "▼ 📖 Livro A") {
		t.Fatalf("com a senha certa, desce:\n%s", out)
	}
	state, _ := loadBooks(b)
	path, err := bookFile(b, state.Order[0], "1")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, original) {
		t.Fatal("B guardou o EPUB original")
	}
	if _, err := openEPUB(path); err != nil {
		t.Fatalf("e ele abre no leitor: %v", err)
	}
	if out := cmdRun(t, b, "livros"); strings.Contains(out, "ainda não baixado") {
		t.Fatalf("B tem o arquivo:\n%s", out)
	}
	if out := syncWith(t, a, bucket, "s3nha"); strings.Contains(out, "📖") {
		t.Fatalf("de novo em A, nada a levar:\n%s", out)
	}
}

// writeTestEPUB grava um EPUB3 mínimo de um capítulo.
func writeTestEPUB(t *testing.T, path, title string) {
	t.Helper()
	var buf bytes.Buffer
	z := newZip(&buf)
	z.add("META-INF/container.xml", `<container><rootfiles><rootfile full-path="content.opf"/></rootfiles></container>`)
	z.add("content.opf", `<package><metadata><dc:title>`+title+`</dc:title></metadata><manifest>`+
		`<item id="c1" href="c1.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="c1"/></spine></package>`)
	z.add("c1.xhtml", `<html><body><p>Um parágrafo de `+title+` com texto suficiente para não aparecer inteiro no começo do arquivo.</p></body></html>`)
	z.close(t)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

type testZip struct{ w *zip.Writer }

func newZip(buf *bytes.Buffer) testZip { return testZip{zip.NewWriter(buf)} }

func (z testZip) add(name, content string) {
	w, _ := z.w.Create(name)
	w.Write([]byte(content))
}

func (z testZip) close(t *testing.T) {
	t.Helper()
	if err := z.w.Close(); err != nil {
		t.Fatal(err)
	}
}
