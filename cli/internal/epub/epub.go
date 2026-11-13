// Package epub lê um EPUB (2 ou 3) como texto para o modo de leitura do pnn
// (catálogo 1.7, spec/SPEC.md §3 "Livros"): capítulos na ordem do spine, cada
// um como uma sequência de blocos (parágrafo, título, citação, item de
// lista, pré-formatado), com o rótulo da página impressa quando o livro traz
// a paginação da edição em papel (pagebreak ou page-list).
//
// O EPUB é um ZIP com XHTML; nada aqui depende de programa externo. A
// posição de uma anotação é (Spine, índice do bloco): o Spine vem do próprio
// arquivo e é estável; o índice do bloco depende deste extrator — por isso
// a anotação guarda também o trecho citado, que a reancora.
package epub

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Tipos de bloco.
const (
	KindPara    = "para"
	KindHeading = "heading"
	KindQuote   = "quote"
	KindItem    = "item"
	KindPre     = "pre"
)

type Block struct {
	Kind string
	Text string
	Page string // página impressa vigente no começo do bloco ("" = sem paginação)
}

type Chapter struct {
	Spine  int    // posição 1-based no spine (a `chapter` da posição gravada)
	Title  string // do sumário; senão o primeiro título do capítulo; senão ""
	Blocks []Block
}

type Book struct {
	Title, Author string
	// Chapters são só os itens do spine com texto (a capa sai), mas o Spine
	// de cada um segue a numeração do arquivo.
	Chapters []Chapter
	HasPages bool // o livro traz a paginação da edição impressa
}

var (
	ErrNotEPUB = errors.New("não é um EPUB válido")
	ErrDRM     = errors.New("o EPUB tem DRM (conteúdo cifrado) — o pnn só lê EPUB sem DRM")
	ErrNoText  = errors.New("o EPUB não tem texto — só imagens (livro escaneado?); o pnn lê texto")
)

// Open lê o EPUB do caminho.
func Open(name string) (*Book, error) {
	r, err := zip.OpenReader(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotEPUB, err)
	}
	defer r.Close()
	return read(&r.Reader)
}

// Read lê o EPUB de um io.ReaderAt (os testes montam o ZIP em memória).
func Read(ra io.ReaderAt, size int64) (*Book, error) {
	r, err := zip.NewReader(ra, size)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotEPUB, err)
	}
	return read(r)
}

type archive struct {
	files map[string]*zip.File
	lower map[string]*zip.File // fallback: há EPUBs com caixa trocada nos hrefs
}

func (a archive) open(name string) ([]byte, error) {
	f, ok := a.files[name]
	if !ok {
		if f, ok = a.lower[strings.ToLower(name)]; !ok {
			return nil, fmt.Errorf("%w: falta %s", ErrNotEPUB, name)
		}
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func (a archive) has(name string) bool {
	_, ok := a.files[name]
	return ok
}

// --- pacote (OPF) ---

type container struct {
	Rootfiles []struct {
		FullPath string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

type opf struct {
	Titles   []string `xml:"metadata>title"`
	Creators []string `xml:"metadata>creator"`
	Manifest []struct {
		ID         string `xml:"id,attr"`
		Href       string `xml:"href,attr"`
		MediaType  string `xml:"media-type,attr"`
		Properties string `xml:"properties,attr"`
	} `xml:"manifest>item"`
	Spine struct {
		Toc   string `xml:"toc,attr"`
		Items []struct {
			IDRef  string `xml:"idref,attr"`
			Linear string `xml:"linear,attr"`
		} `xml:"itemref"`
	} `xml:"spine"`
}

type encryption struct {
	Data []struct {
		Method struct {
			Algorithm string `xml:"Algorithm,attr"`
		} `xml:"EncryptionMethod"`
	} `xml:"EncryptedData"`
}

// Algoritmos que só ofuscam fontes embutidas — não são DRM do texto.
var fontObfuscation = map[string]bool{
	"http://www.idpf.org/2008/embedding": true,
	"http://ns.adobe.com/pdf/enc#RC":     true,
}

func read(r *zip.Reader) (*Book, error) {
	a := archive{files: map[string]*zip.File{}, lower: map[string]*zip.File{}}
	for _, f := range r.File {
		a.files[f.Name] = f
		a.lower[strings.ToLower(f.Name)] = f
	}

	if a.has("META-INF/rights.xml") {
		return nil, ErrDRM
	}
	if a.has("META-INF/encryption.xml") {
		raw, err := a.open("META-INF/encryption.xml")
		if err != nil {
			return nil, err
		}
		var enc encryption
		if xml.Unmarshal(raw, &enc) == nil {
			for _, d := range enc.Data {
				if !fontObfuscation[d.Method.Algorithm] {
					return nil, ErrDRM
				}
			}
		}
	}

	raw, err := a.open("META-INF/container.xml")
	if err != nil {
		return nil, err
	}
	var c container
	if err := xml.Unmarshal(raw, &c); err != nil || len(c.Rootfiles) == 0 {
		return nil, fmt.Errorf("%w: container.xml sem rootfile", ErrNotEPUB)
	}
	opfPath := c.Rootfiles[0].FullPath
	if raw, err = a.open(opfPath); err != nil {
		return nil, err
	}
	var pkg opf
	if err := xml.Unmarshal(raw, &pkg); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrNotEPUB, opfPath, err)
	}
	base := path.Dir(opfPath)

	type item struct{ href, mediaType, properties string }
	manifest := map[string]item{}
	navHref, ncxHref := "", ""
	for _, it := range pkg.Manifest {
		href := resolve(base, it.Href)
		manifest[it.ID] = item{href, it.MediaType, it.Properties}
		if hasToken(it.Properties, "nav") {
			navHref = href
		}
	}
	if it, ok := manifest[pkg.Spine.Toc]; ok {
		ncxHref = it.href
	}

	book := &Book{Title: firstNonEmpty(pkg.Titles), Author: joinNonEmpty(pkg.Creators)}
	nav := navigation{titles: map[string]string{}, pages: map[string]map[string]string{}}
	if navHref != "" {
		if raw, err := a.open(navHref); err == nil {
			nav.readNav(raw, path.Dir(navHref))
		}
	}
	if len(nav.titles) == 0 && ncxHref != "" {
		if raw, err := a.open(ncxHref); err == nil {
			nav.readNCX(raw, path.Dir(ncxHref))
		}
	}

	for i, ref := range pkg.Spine.Items {
		it, ok := manifest[ref.IDRef]
		if !ok || ref.Linear == "no" || !isHTML(it.mediaType) {
			continue
		}
		raw, err := a.open(it.href)
		if err != nil {
			continue // item do spine que falta no ZIP: segue sem ele
		}
		doc, err := parseXHTML(raw)
		if err != nil {
			continue
		}
		ex := extractor{pageIDs: nav.pages[it.href]}
		ex.walk(doc, KindPara)
		ex.flush(KindPara)
		if len(ex.blocks) == 0 {
			continue // capa, página só com imagem
		}
		ch := Chapter{Spine: i + 1, Title: nav.titles[it.href], Blocks: ex.blocks}
		if ch.Title == "" {
			for _, b := range ch.Blocks {
				if b.Kind == KindHeading {
					ch.Title = b.Text
					break
				}
			}
		}
		book.HasPages = book.HasPages || ex.sawPage
		book.Chapters = append(book.Chapters, ch)
	}
	if len(book.Chapters) == 0 {
		return nil, ErrNoText
	}
	return book, nil
}

func isHTML(mediaType string) bool {
	return mediaType == "application/xhtml+xml" || mediaType == "text/html"
}

func hasToken(list, token string) bool {
	return strings.Contains(" "+list+" ", " "+token+" ")
}

// resolve junta o href (relativo a dir, com %xx e talvez #fragmento) ao
// caminho dentro do ZIP, sem o fragmento.
func resolve(dir, href string) string {
	href, _, _ = strings.Cut(href, "#")
	if u, err := url.PathUnescape(href); err == nil {
		href = u
	}
	if dir == "." {
		return path.Clean(href)
	}
	return path.Join(dir, href)
}

func splitHref(dir, href string) (file, fragment string) {
	_, fragment, _ = strings.Cut(href, "#")
	return resolve(dir, href), fragment
}

func firstNonEmpty(xs []string) string {
	for _, x := range xs {
		if x = collapse(x); x != "" {
			return x
		}
	}
	return ""
}

func joinNonEmpty(xs []string) string {
	var out []string
	for _, x := range xs {
		if x = collapse(x); x != "" {
			out = append(out, x)
		}
	}
	return strings.Join(out, ", ")
}

// --- sumário e paginação (nav do EPUB3, NCX do EPUB2) ---

type navigation struct {
	titles map[string]string            // arquivo → primeiro rótulo do sumário que aponta para ele
	pages  map[string]map[string]string // arquivo → id do alvo → rótulo da página impressa
}

func (n *navigation) addTitle(file, label string) {
	if label = collapse(label); label != "" && n.titles[file] == "" {
		n.titles[file] = label
	}
}

func (n *navigation) addPage(file, id, label string) {
	if label = collapse(label); label == "" || id == "" {
		return
	}
	if n.pages[file] == nil {
		n.pages[file] = map[string]string{}
	}
	n.pages[file][id] = label
}

// readNav lê o nav.xhtml: o <nav epub:type="toc"> dá os títulos; o
// <nav epub:type="page-list">, os alvos das páginas impressas.
func (n *navigation) readNav(raw []byte, dir string) {
	doc, err := parseXHTML(raw)
	if err != nil {
		return
	}
	var visit func(*html.Node, string)
	visit = func(node *html.Node, kind string) {
		if node.Type == html.ElementNode && node.DataAtom == atom.Nav {
			t := attr(node, "epub:type") + " " + attr(node, "type") + " " + attr(node, "role")
			switch {
			case strings.Contains(t, "page-list") || strings.Contains(t, "doc-pagelist"):
				kind = "pages"
			case strings.Contains(t, "toc"):
				kind = "toc"
			default:
				kind = ""
			}
		}
		if node.Type == html.ElementNode && node.DataAtom == atom.A && kind != "" {
			file, frag := splitHref(dir, attr(node, "href"))
			if kind == "toc" {
				n.addTitle(file, textOf(node))
			} else {
				n.addPage(file, frag, textOf(node))
			}
			return
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			visit(c, kind)
		}
	}
	visit(doc, "")
}

type ncx struct {
	NavPoints []ncxPoint `xml:"navMap>navPoint"`
	Pages     []struct {
		Label   string `xml:"navLabel>text"`
		Content struct {
			Src string `xml:"src,attr"`
		} `xml:"content"`
	} `xml:"pageList>pageTarget"`
}

type ncxPoint struct {
	Label   string `xml:"navLabel>text"`
	Content struct {
		Src string `xml:"src,attr"`
	} `xml:"content"`
	Children []ncxPoint `xml:"navPoint"`
}

func (n *navigation) readNCX(raw []byte, dir string) {
	var doc ncx
	if xml.Unmarshal(raw, &doc) != nil {
		return
	}
	var visit func([]ncxPoint)
	visit = func(points []ncxPoint) {
		for _, p := range points {
			file, _ := splitHref(dir, p.Content.Src)
			n.addTitle(file, p.Label)
			visit(p.Children)
		}
	}
	visit(doc.NavPoints)
	for _, p := range doc.Pages {
		file, frag := splitHref(dir, p.Content.Src)
		n.addPage(file, frag, p.Label)
	}
}

// --- conteúdo ---

// extractor percorre o XHTML acumulando o texto corrido e fechando um bloco
// a cada fronteira de bloco — o texto solto entre dois <p> de um <div> vira
// o próprio parágrafo.
type extractor struct {
	blocks  []Block
	buf     strings.Builder
	page    string            // página impressa vigente
	start   string            // página vigente quando o bloco atual começou
	hasText bool              // o bloco atual já tem texto (não só espaço)
	pageIDs map[string]string // ids que a page-list aponta neste arquivo
	sawPage bool
}

var blockKind = map[atom.Atom]string{
	atom.P: KindPara, atom.Div: KindPara, atom.Section: KindPara, atom.Article: KindPara,
	atom.Aside: KindPara, atom.Header: KindPara, atom.Footer: KindPara, atom.Main: KindPara,
	atom.Figure: KindPara, atom.Figcaption: KindPara, atom.Table: KindPara, atom.Tr: KindPara,
	atom.Ul: KindPara, atom.Ol: KindPara, atom.Dl: KindPara, atom.Nav: KindPara, atom.Body: KindPara,
	atom.Hr: KindPara,

	atom.H1: KindHeading, atom.H2: KindHeading, atom.H3: KindHeading,
	atom.H4: KindHeading, atom.H5: KindHeading, atom.H6: KindHeading,

	atom.Blockquote: KindQuote,

	atom.Li: KindItem, atom.Dt: KindItem, atom.Dd: KindItem,

	atom.Pre: KindPre,
}

var skip = map[atom.Atom]bool{
	atom.Head: true, atom.Script: true, atom.Style: true, atom.Svg: true, atom.Template: true, atom.Title: true,
}

// inner: o tipo de um bloco dentro de outro — pré-formatado e título
// prevalecem; dentro de citação, tudo é citação; dentro de item, o
// parágrafo é item.
func inner(outer, tag string) string {
	switch {
	case outer == KindPre || tag == KindPre:
		return KindPre
	case tag == KindHeading:
		return KindHeading
	case outer == KindQuote || tag == KindQuote:
		return KindQuote
	case outer == KindItem || tag == KindItem:
		return KindItem
	}
	return KindPara
}

func (e *extractor) isPageBreak(n *html.Node) (string, bool) {
	t := attr(n, "epub:type") + " " + attr(n, "role")
	if strings.Contains(t, "pagebreak") {
		label := cmpOr(attr(n, "title"), attr(n, "aria-label"), textOf(n), strings.TrimLeft(attr(n, "id"), "pagePAGE_-"))
		return collapse(label), true
	}
	if id := attr(n, "id"); id != "" {
		if label, ok := e.pageIDs[id]; ok {
			return label, true
		}
	}
	return "", false
}

func (e *extractor) walk(n *html.Node, kind string) {
	switch n.Type {
	case html.TextNode:
		if !e.hasText && strings.TrimSpace(cleanRunes(n.Data)) != "" {
			e.start, e.hasText = e.page, true
		}
		e.buf.WriteString(n.Data) // o espaço é colapsado no flush (menos no pre)
		return
	case html.ElementNode:
		if skip[n.DataAtom] || n.Data == "svg" {
			return
		}
		if label, ok := e.isPageBreak(n); ok {
			if label != "" {
				e.page, e.sawPage = label, true
			}
			if n.FirstChild == nil || attr(n, "epub:type") != "" || attr(n, "role") != "" {
				return // o marcador da página não é texto
			}
		}
		if n.DataAtom == atom.Br {
			if kind == KindPre {
				e.buf.WriteString("\n")
			} else {
				e.buf.WriteRune(lineBreak)
			}
			return
		}
		if tag, ok := blockKind[n.DataAtom]; ok {
			e.flush(kind)
			k := inner(kind, tag)
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				e.walk(c, k)
			}
			e.flush(k)
			return
		}
		if n.DataAtom == atom.Td || n.DataAtom == atom.Th {
			if e.buf.Len() > 0 {
				e.buf.WriteString(" · ")
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		e.walk(c, kind)
	}
}

func (e *extractor) flush(kind string) {
	raw := e.buf.String()
	e.buf.Reset()
	e.hasText = false
	var text string
	if kind == KindPre {
		text = strings.Trim(cleanRunes(raw), "\n")
	} else {
		text = collapseKeepBreaks(raw)
	}
	if strings.TrimSpace(text) == "" {
		return
	}
	e.blocks = append(e.blocks, Block{Kind: kind, Text: text, Page: e.start})
}

// --- texto ---

// cleanRunes tira o hífen suave e os caracteres de largura zero, e troca o
// espaço inquebrável por espaço.
func cleanRunes(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\u00ad', '\u200b', '\u200c', '\u200d', '\ufeff':
			return -1
		case '\u00a0', '\u202f', '\u2007':
			return ' '
		}
		return r
	}, s)
}

// collapse: um espaço entre palavras, sem pontas.
func collapse(s string) string {
	return strings.Join(strings.Fields(cleanRunes(s)), " ")
}

// lineBreak marca no texto acumulado a quebra que veio de um <br>: a quebra
// de linha do código-fonte é só espaço e se colapsa.
const lineBreak = '\x00'

// collapseKeepBreaks colapsa o espaço do código-fonte mas mantém as quebras
// de linha que vieram de <br> (poesia, endereços).
func collapseKeepBreaks(s string) string {
	lines := strings.Split(s, string(lineBreak))
	out := lines[:0]
	for _, line := range lines {
		if line = collapse(line); line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(n)
	return collapse(b.String())
}

// attr lê o atributo pelo nome com prefixo (epub:type) ou sem.
func attr(n *html.Node, name string) string {
	ns, local, hasNS := strings.Cut(name, ":")
	for _, a := range n.Attr {
		if hasNS && (a.Key == name || (a.Namespace == ns && a.Key == local)) {
			return a.Val
		}
		if !hasNS && a.Key == name && a.Namespace == "" {
			return a.Val
		}
	}
	return ""
}

func cmpOr(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

// selfClosing casa a tag autofechada do XHTML (<span …/>), com atributos
// entre aspas que podem conter > ou /.
var selfClosing = regexp.MustCompile(`<([A-Za-z][A-Za-z0-9:_.-]*)((?:\s+[^\s=/>]+(?:\s*=\s*(?:"[^"]*"|'[^']*'|[^\s"'>]+))?)*)\s*/>`)

var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true,
}

// parseXHTML: o parser de HTML é tolerante a tudo, mas ignora o "/>" das
// tags que não são vazias — <span epub:type="pagebreak"/> abriria um span
// que engole o resto do capítulo. A tag autofechada vira abre-e-fecha antes.
func parseXHTML(raw []byte) (*html.Node, error) {
	fixed := selfClosing.ReplaceAllStringFunc(string(raw), func(tag string) string {
		m := selfClosing.FindStringSubmatch(tag)
		if voidElements[strings.ToLower(m[1])] {
			return tag
		}
		return "<" + m[1] + m[2] + "></" + m[1] + ">"
	})
	return html.Parse(strings.NewReader(fixed))
}
