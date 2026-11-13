package epub

import (
	"archive/zip"
	"bytes"
	"errors"
	"slices"
	"testing"
)

// build monta um EPUB em memória: mimetype primeiro e sem compressão, como
// manda o OCF; o resto na ordem dada.
func build(t *testing.T, files ...string) *Book {
	t.Helper()
	b, err := buildErr(t, files...)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func buildErr(t *testing.T, files ...string) (*Book, error) {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	w, err := z.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("application/epub+zip"))
	for i := 0; i+1 < len(files); i += 2 {
		w, err := z.Create(files[i])
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(files[i+1]))
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return Read(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
}

const containerXML = `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`

func xhtml(body string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<head><title/><link rel="stylesheet" href="s.css"/></head>
<body>` + body + `</body></html>`
}

func texts(blocks []Block) []string {
	var out []string
	for _, b := range blocks {
		out = append(out, b.Kind+":"+b.Text+"@"+b.Page)
	}
	return out
}

// EPUB3: sumário e page-list no nav, pagebreak autofechado no meio do texto,
// capa sem texto (sai da navegação, mas a numeração segue o spine), item
// não linear fora, e o texto limpo (hífen suave, espaço inquebrável, <br>).
func TestEPUB3ComNavEPaginacao(t *testing.T) {
	opf := `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title> Mastering Your
      Adult ADHD </dc:title>
    <dc:creator>Steven A. Safren</dc:creator>
    <dc:creator>Susan E. Sprich</dc:creator>
  </metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="capa" href="capa.xhtml" media-type="application/xhtml+xml"/>
    <item id="c1" href="Text/Cap%C3%ADtulo%201.xhtml" media-type="application/xhtml+xml"/>
    <item id="notas" href="notas.xhtml" media-type="application/xhtml+xml"/>
    <item id="c2" href="Text/cap2.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine>
    <itemref idref="capa"/>
    <itemref idref="c1"/>
    <itemref idref="notas" linear="no"/>
    <itemref idref="c2"/>
  </spine>
</package>`
	nav := xhtml(`<nav epub:type="toc"><ol>
      <li><a href="Text/Cap%C3%ADtulo%201.xhtml">Sessão 1: Psicoeducação</a></li>
      <li><a href="Text/Cap%C3%ADtulo%201.xhtml#parte2">Parte 2</a></li>
    </ol></nav>
    <nav epub:type="page-list"><ol><li><a href="Text/cap2.xhtml#pg12">12</a></li></ol></nav>`)
	cap1 := xhtml(`<h1>Psicoeducação</h1>
    <span epub:type="pagebreak" id="page9" title="9"/>
    <p>We discuss this up&#173;front with clients&nbsp;so that they can agree.</p>
    <div>Texto solto no div
      <p>CBT is like a <em>ladder</em>.</p>
      e mais texto solto</div>
    <ul><li>Ask the client <p>to give you permission</p></li><li>Say to the client</li></ul>
    <blockquote><p>Uma citação</p></blockquote>
    <p>Poema<br/>em duas linhas</p>
    <p>Quebra do código-fonte
       é só espaço</p>
    <p>Começa na 9 <span epub:type="pagebreak" title="10"></span>e termina na 10.</p>
    <p>Já na 10.</p>
    <pre>  código<br/>
    indentado</pre>
    <table><tr><th>A</th><td>B</td></tr></table>
    <script>nada()</script>`)
	cap2 := xhtml(`<h2>Sem título no sumário</h2><p>antes</p><a id="pg12"/><p>Na página 12.</p>`)
	book := build(t,
		"META-INF/container.xml", containerXML,
		"OEBPS/content.opf", opf,
		"OEBPS/nav.xhtml", nav,
		"OEBPS/capa.xhtml", xhtml(`<img src="capa.jpg"/>`),
		"OEBPS/Text/Capítulo 1.xhtml", cap1,
		"OEBPS/notas.xhtml", xhtml(`<p>nota de rodapé</p>`),
		"OEBPS/Text/cap2.xhtml", cap2,
	)

	if book.Title != "Mastering Your Adult ADHD" || book.Author != "Steven A. Safren, Susan E. Sprich" {
		t.Fatalf("metadados: %q / %q", book.Title, book.Author)
	}
	if !book.HasPages {
		t.Fatal("o livro tem paginação impressa")
	}
	if len(book.Chapters) != 2 {
		t.Fatalf("capa e item não linear ficam fora: %d capítulos", len(book.Chapters))
	}
	c1, c2 := book.Chapters[0], book.Chapters[1]
	if c1.Spine != 2 || c2.Spine != 4 {
		t.Fatalf("a numeração segue o spine: %d, %d", c1.Spine, c2.Spine)
	}
	if c1.Title != "Sessão 1: Psicoeducação" || c2.Title != "Sem título no sumário" {
		t.Fatalf("títulos: %q, %q", c1.Title, c2.Title)
	}
	want := []string{
		"heading:Psicoeducação@",
		"para:We discuss this upfront with clients so that they can agree.@9",
		"para:Texto solto no div@9",
		"para:CBT is like a ladder.@9",
		"para:e mais texto solto@9",
		"item:Ask the client@9",
		"item:to give you permission@9",
		"item:Say to the client@9",
		"quote:Uma citação@9",
		"para:Poema\nem duas linhas@9",
		"para:Quebra do código-fonte é só espaço@9",
		"para:Começa na 9 e termina na 10.@9",
		"para:Já na 10.@10",
		"pre:  código\n\n    indentado@10",
		"para:A · B@10",
	}
	if got := texts(c1.Blocks); !slices.Equal(got, want) {
		t.Fatalf("blocos do capítulo 1:\n%q\nesperado:\n%q", got, want)
	}
	want2 := []string{"heading:Sem título no sumário@", "para:antes@", "para:Na página 12.@12"}
	if got := texts(c2.Blocks); !slices.Equal(got, want2) {
		t.Fatalf("a page-list aponta um id sem marca de pagebreak:\n%q", got)
	}
}

// EPUB2: títulos pelo NCX (spine@toc), pageList do NCX, OPF na raiz.
func TestEPUB2ComNCX(t *testing.T) {
	opf := `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Livro dois</dc:title></metadata>
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="a" href="a.html" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx"><itemref idref="a"/></spine>
</package>`
	ncx := `<?xml version="1.0"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
  <navMap><navPoint id="n1"><navLabel><text>Capítulo Um</text></navLabel><content src="a.html"/>
    <navPoint id="n2"><navLabel><text>Seção</text></navLabel><content src="a.html#s"/></navPoint>
  </navPoint></navMap>
  <pageList><pageTarget type="normal"><navLabel><text>iii</text></navLabel><content src="a.html#p3"/></pageTarget></pageList>
</ncx>`
	book := build(t,
		"META-INF/container.xml", `<container><rootfiles><rootfile full-path="content.opf"/></rootfiles></container>`,
		"content.opf", opf,
		"toc.ncx", ncx,
		"a.html", xhtml(`<p>um</p><span id="p3"></span><p>dois</p>`),
	)
	if book.Title != "Livro dois" || book.Author != "" || len(book.Chapters) != 1 {
		t.Fatalf("livro: %+v", book)
	}
	ch := book.Chapters[0]
	if ch.Title != "Capítulo Um" || ch.Spine != 1 {
		t.Fatalf("capítulo: %q %d", ch.Title, ch.Spine)
	}
	if got := texts(ch.Blocks); !slices.Equal(got, []string{"para:um@", "para:dois@iii"}) {
		t.Fatalf("blocos: %q", got)
	}
}

func TestDRMRecusado(t *testing.T) {
	_, err := buildErr(t,
		"META-INF/container.xml", containerXML,
		"META-INF/encryption.xml", `<encryption><EncryptedData><EncryptionMethod Algorithm="http://www.w3.org/2001/04/xmlenc#aes128-cbc"/></EncryptedData></encryption>`,
	)
	if !errors.Is(err, ErrDRM) {
		t.Fatalf("esperava ErrDRM, veio %v", err)
	}
}

// Ofuscação de fonte não é DRM do texto.
func TestFonteOfuscadaNaoEDRM(t *testing.T) {
	book := build(t,
		"META-INF/container.xml", containerXML,
		"META-INF/encryption.xml", `<encryption><EncryptedData><EncryptionMethod Algorithm="http://www.idpf.org/2008/embedding"/></EncryptedData></encryption>`,
		"OEBPS/content.opf", `<package><metadata><title>T</title></metadata><manifest><item id="a" href="a.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="a"/></spine></package>`,
		"OEBPS/a.xhtml", xhtml(`<p>texto</p>`),
	)
	if book.Title != "T" || book.Chapters[0].Title != "" || book.HasPages {
		t.Fatalf("livro: %+v", book)
	}
}

func TestNaoEPUB(t *testing.T) {
	if _, err := Read(bytes.NewReader([]byte("%PDF-1.7")), 8); !errors.Is(err, ErrNotEPUB) {
		t.Fatalf("esperava ErrNotEPUB, veio %v", err)
	}
	if _, err := buildErr(t, "META-INF/container.xml", containerXML); !errors.Is(err, ErrNotEPUB) {
		t.Fatalf("sem OPF: %v", err)
	}
	// livro escaneado: o spine só tem páginas com imagem
	_, err := buildErr(t,
		"META-INF/container.xml", containerXML,
		"OEBPS/content.opf", `<package><manifest><item id="a" href="a.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="a"/></spine></package>`,
		"OEBPS/a.xhtml", xhtml(`<div><img src="0_0.png"/></div>`),
	)
	if !errors.Is(err, ErrNoText) {
		t.Fatalf("só imagens: %v", err)
	}
}
