// Modo de leitura (catálogo 1.7, spec/CLI.md §3 "Livros"): o livro aberto
// dentro da tela, um parágrafo de cada vez sob o cursor, e os comandos "/"
// para anotar sem trocar de ferramenta — a mesma ideia da escrita
// (escrita.go), com o livro e o parágrafo atual como ponto de referência.
//
// A anotação aponta para (capítulo do spine, parágrafo) e guarda o trecho
// citado; ao abrir, o trecho reancora a anotação se o parágrafo mudou de
// lugar (outra edição, outro extrator). Ao sair, o "onde parei" vira
// book.progressed — só se a posição mudou. O TUI desenha e chama; o acervo
// e o EPUB ficam com a CLI (ReadActions).
//
// Avulso (`pnn ler`), Esc sai. Dentro do foco (a tarefa ligada a um livro,
// Ctrl+O), a leitura é como a escrita: o relógio na barra vermelha, os
// check-ins no rodapé, Esc volta ao relógio grande (gravando onde parou),
// Ctrl+P é a pausa e a /distracao fica na sessão.
package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Tipos de bloco (os mesmos do pacote epub, sem depender dele).
const (
	blockPara    = "para"
	blockHeading = "heading"
	blockQuote   = "quote"
	blockItem    = "item"
	blockPre     = "pre"
)

// BookLoc é a posição no livro como o log a grava: Chapter = posição no
// spine do EPUB; Para = bloco 1-based no capítulo.
type BookLoc struct{ Chapter, Para int }

type BookBlock struct{ Kind, Text, Page string }

type BookChapter struct {
	Spine  int
	Title  string
	Blocks []BookBlock
}

type BookAnnotation struct {
	ID                string
	At                BookLoc
	Page, Quote, Text string // Text vazio = grifo
}

// BookText é o livro carregado para a leitura: o texto vem do EPUB, as
// anotações e o "onde parei" (Start) vêm do log.
type BookText struct {
	Title, Author string
	Chapters      []BookChapter
	Annotations   []BookAnnotation
	Start         BookLoc // zero = começo
}

// LinkedBook é o livro aberto (id no log e título).
type LinkedBook struct{ ID, Title string }

// ReadActions é o que a leitura e os comandos pedem ao acervo. As ações de
// comando devolvem a linha de status.
type ReadActions interface {
	Load(bookID string) (BookText, error)
	Annotate(bookID string, at BookLoc, page, quote, text string) (BookAnnotation, string, error)
	EditAnnotation(id, text string) (string, error)
	DeleteAnnotation(id string) (string, error)
	// Progress grava o "onde parei" (book.progressed); sessionID é a
	// sessão de foco em que se leu ("" no `pnn ler`).
	Progress(bookID string, at BookLoc, page string, percent int, sessionID string) error
	// CreateCard cria o cartão com a fonte (livro, capítulo, página).
	CreateCard(bookID, source, front, back string) (string, error)
	// CaptureNote anota na caixa de entrada — a /distracao fora do foco.
	CaptureNote(text string) (string, error)
}

// maxQuote: a citação automática (o parágrafo inteiro) é cortada aqui — o
// trecho serve para reancorar e para reconhecer a anotação na lista, não
// para copiar o livro para o log.
const maxQuote = 280

// textWidth: a medida confortável de leitura; em terminal estreito, menos.
const textWidth = 76

var (
	yellowAccent = lipgloss.AdaptiveColor{Light: "3", Dark: "11"}
	cursorStyle  = lipgloss.NewStyle().Foreground(blueAccent)
	noteStyle    = lipgloss.NewStyle().Foreground(yellowAccent)
	highlight    = lipgloss.NewStyle().Underline(true).Foreground(yellowAccent)
	headingTxt   = lipgloss.NewStyle().Bold(true)
)

type placedNote struct {
	BookAnnotation
	ch, para int  // onde está agora (índices em Chapters e Blocks)
	lost     bool // o trecho não foi achado nesta edição: fica na posição gravada
}

type readLine struct {
	block int    // -1 = linha entre blocos
	text  string // já com estilo, sem a margem do cursor
}

type readState struct {
	book    LinkedBook
	text    BookText
	ch      int // capítulo atual (índice em text.Chapters)
	para    int // bloco atual (0-based)
	top     int // primeira linha visível do capítulo
	start   BookLoc
	notes   []placedNote
	created int  // anotações criadas nesta leitura
	done    bool // o "onde parei" já foi gravado: a leitura acabou

	before []int // blocos antes de cada capítulo (para o %)
	total  int

	// quebra de linhas do capítulo atual, refeita quando muda o capítulo, a
	// largura ou as anotações
	cacheCh, cacheW int
	cacheOK         bool
	lines           []readLine
	first, last     []int // primeira e última linha de cada bloco
}

// linkedItem é o que está ligado à tarefa e se abre na própria tela: uma
// página (escrita) ou um livro (leitura).
type linkedItem struct {
	page *LinkedPage
	book *LinkedBook
}

func (it linkedItem) label() string {
	if it.book != nil {
		return "📖 " + it.book.Title
	}
	return "página " + it.page.Title
}

// linked: o que o Ctrl+O abre — as páginas (com a escrita ligada) e depois
// os livros (com a leitura ligada).
func (m Foco) linked() []linkedItem {
	var items []linkedItem
	if m.cfg.Write != nil {
		for i := range m.cfg.Pages {
			items = append(items, linkedItem{page: &m.cfg.Pages[i]})
		}
	}
	if m.cfg.Read != nil {
		for i := range m.cfg.Books {
			items = append(items, linkedItem{book: &m.cfg.Books[i]})
		}
	}
	return items
}

// linkedAll: tudo o que está ligado, para a linha ⚡ do relógio grande.
func (m Foco) linkedAll() []linkedItem {
	var items []linkedItem
	for i := range m.cfg.Pages {
		items = append(items, linkedItem{page: &m.cfg.Pages[i]})
	}
	for i := range m.cfg.Books {
		items = append(items, linkedItem{book: &m.cfg.Books[i]})
	}
	return items
}

// openLinked abre a página na escrita ou o livro na leitura — retomando o
// que já estava carregado, se for o mesmo.
func (m *Foco) openLinked(it linkedItem) tea.Cmd {
	if it.book != nil {
		if m.writing {
			m.leaveWrite()
		}
		if m.read != nil && m.read.book.ID == it.book.ID {
			m.reading = true
			m.show(true)
			return nil
		}
		if err := m.enterRead(*it.book); err != nil {
			m.status = "⚠ " + err.Error()
		}
		return nil
	}
	if m.reading {
		m.leaveRead()
	}
	if m.write != nil && m.write.page.ID == it.page.ID {
		m.writing = true
		m.resizeEditor()
		return m.write.editor.Focus()
	}
	return m.enterWrite(*it.page)
}

// enterRead abre o livro no modo de leitura, no "onde parei".
func (m *Foco) enterRead(b LinkedBook) error {
	if m.cfg.Read == nil {
		return nil
	}
	text, err := m.cfg.Read.Load(b.ID)
	if err != nil {
		return err
	}
	if len(text.Chapters) == 0 {
		return fmt.Errorf("o livro não tem texto")
	}
	if text.Title != "" {
		b.Title = text.Title
	}
	r := &readState{book: b, text: text}
	for _, ch := range text.Chapters {
		r.before = append(r.before, r.total)
		r.total += len(ch.Blocks)
	}
	r.ch, r.para = r.locate(text.Start)
	r.start = r.loc()
	for _, an := range text.Annotations {
		r.place(an)
	}
	m.read, m.reading = r, true
	m.relayoutRead()
	r.top = r.first[r.para] // abre com o parágrafo do "onde parei" no topo
	m.clampTop()
	return nil
}

// saveProgress grava o "onde parei" se a posição mudou desde a última
// gravação (ou desde a abertura).
func (m *Foco) saveProgress() error {
	r := m.read
	if r == nil {
		return nil
	}
	at := r.loc()
	if at == r.start {
		return nil
	}
	if err := m.cfg.Read.Progress(r.book.ID, at, r.block().Page, r.percent(), m.sessionID); err != nil {
		return err
	}
	r.start = at
	return nil
}

// leaveRead volta ao relógio grande (no foco), gravando onde parou; o livro
// continua carregado para o próximo Ctrl+O.
func (m *Foco) leaveRead() {
	if err := m.saveProgress(); err != nil {
		m.status = "⚠ não gravou onde parou: " + err.Error()
	} else {
		m.status = "📖 " + m.read.book.Title + " — parou em " + m.where()
	}
	m.reading = false
	m.closePalette()
}

// locate traduz a posição gravada para índices; fora do livro, o mais
// próximo (capítulo sumido: o começo do seguinte).
func (r *readState) locate(at BookLoc) (int, int) {
	if at.Chapter <= 0 {
		return 0, 0
	}
	for i, ch := range r.text.Chapters {
		if ch.Spine == at.Chapter {
			return i, min(max(at.Para-1, 0), len(ch.Blocks)-1)
		}
		if ch.Spine > at.Chapter {
			return i, 0
		}
	}
	last := len(r.text.Chapters) - 1
	return last, len(r.text.Chapters[last].Blocks) - 1
}

func (r *readState) loc() BookLoc {
	return BookLoc{Chapter: r.text.Chapters[r.ch].Spine, Para: r.para + 1}
}

func (r *readState) block() BookBlock { return r.text.Chapters[r.ch].Blocks[r.para] }

func (r *readState) percent() int {
	if r.total == 0 {
		return 0
	}
	return (r.before[r.ch] + r.para) * 100 / r.total
}

// --- anotações ---

// flat: o <br> do parágrafo vira espaço — é assim que a citação o guarda.
func flat(s string) string { return strings.ReplaceAll(s, "\n", " ") }

// quoteMatch: o trecho (sem o "…" do corte) está no parágrafo, sem caixa nem
// acento.
func quoteMatch(text, quote string) bool {
	q := strings.TrimSuffix(quote, "…")
	return q != "" && strings.Contains(foldASCII(flat(text)), foldASCII(flat(q)))
}

// place reancora a anotação: na posição gravada, se o parágrafo ainda tem o
// trecho; senão, no mesmo capítulo; senão, no livro; senão, fica na posição
// gravada, marcada como perdida.
func (r *readState) place(an BookAnnotation) {
	ch, para := r.locate(an.At)
	n := placedNote{BookAnnotation: an, ch: ch, para: para}
	chapters := r.text.Chapters
	if chapters[ch].Spine == an.At.Chapter && an.At.Para-1 == para && quoteMatch(chapters[ch].Blocks[para].Text, an.Quote) {
		r.notes = append(r.notes, n)
		return
	}
	if i, ok := r.find(ch, func(b BookBlock) bool { return quoteMatch(b.Text, an.Quote) }); ok {
		n.para = i
		r.notes = append(r.notes, n)
		return
	}
	for c := range chapters {
		if i, ok := r.find(c, func(b BookBlock) bool { return quoteMatch(b.Text, an.Quote) }); ok {
			n.ch, n.para = c, i
			r.notes = append(r.notes, n)
			return
		}
	}
	n.lost = true
	r.notes = append(r.notes, n)
}

func (r *readState) find(ch int, ok func(BookBlock) bool) (int, bool) {
	for i, b := range r.text.Chapters[ch].Blocks {
		if ok(b) {
			return i, true
		}
	}
	return 0, false
}

// here são as anotações do parágrafo atual (índices em notes).
func (r *readState) here() []int {
	var out []int
	for i, n := range r.notes {
		if n.ch == r.ch && n.para == r.para {
			out = append(out, i)
		}
	}
	return out
}

// highlights: os trechos grifados do bloco, em runas. Nota sobre o
// parágrafo inteiro não pinta o texto (a marca ✎ basta); grifo e nota sobre
// um trecho, sim.
func (r *readState) highlights(ch, para int) [][2]int {
	text := r.text.Chapters[ch].Blocks[para].Text
	folded := []rune(foldASCII(flat(text)))
	auto := paragraphQuote(text)
	var out [][2]int
	for _, n := range r.notes {
		if n.ch != ch || n.para != para || n.lost {
			continue
		}
		if n.Quote == auto { // o parágrafo inteiro
			if n.Text == "" {
				out = append(out, [2]int{0, len(folded)})
			}
			continue
		}
		q := []rune(foldASCII(flat(n.Quote)))
		i := runeIndex(folded, q)
		if i < 0 { // citação cortada de outra edição
			q = []rune(strings.TrimSuffix(string(q), "…"))
			i = runeIndex(folded, q)
		}
		if i >= 0 {
			out = append(out, [2]int{i, i + len(q)})
		}
	}
	return out
}

func runeIndex(hay, needle []rune) int {
	if len(needle) == 0 {
		return -1
	}
	for i := 0; i+len(needle) <= len(hay); i++ {
		match := true
		for j := range needle {
			if hay[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// paragraphQuote: o parágrafo inteiro como citação, cortado em maxQuote.
func paragraphQuote(text string) string {
	rs := []rune(flat(text))
	if len(rs) <= maxQuote {
		return string(rs)
	}
	return strings.TrimRight(string(rs[:maxQuote]), " ") + "…"
}

// excerpt acha o trecho digitado no parágrafo (sem caixa nem acento) e o
// devolve com a grafia do livro.
func excerpt(text, typed string) (string, bool) {
	rs := []rune(flat(text))
	q := []rune(foldASCII(strings.Join(strings.Fields(typed), " ")))
	i := runeIndex([]rune(foldASCII(flat(text))), q)
	if i < 0 {
		return "", false
	}
	return string(rs[i : i+len(q)]), true
}

// --- quebra de linhas ---

func runeWidth(r rune) int { return ansi.StringWidth(string(r)) }

// wrapSpans quebra o texto em linhas de até width colunas, nas fronteiras de
// palavra, e devolve o intervalo [ini, fim) em runas de cada linha — o
// intervalo permite pintar o trecho grifado depois de quebrar. "\n" (o <br>)
// é quebra forçada.
func wrapSpans(rs []rune, width int) [][2]int {
	var out [][2]int
	start := 0
	for i := 0; i <= len(rs); i++ {
		if i == len(rs) || rs[i] == '\n' {
			out = append(out, wrapSegment(rs, start, i, width)...)
			start = i + 1
		}
	}
	return out
}

func wrapSegment(rs []rune, s, e, width int) [][2]int {
	var out [][2]int
	for s < e {
		for s < e && rs[s] == ' ' {
			s++
		}
		if s >= e {
			break
		}
		w, end, lastSpace := 0, s, -1
		for end < e {
			rw := runeWidth(rs[end])
			if w+rw > width {
				break
			}
			if rs[end] == ' ' {
				lastSpace = end
			}
			w += rw
			end++
		}
		if end < e && rs[end] != ' ' && lastSpace > s {
			end = lastSpace
		}
		if end == s {
			end = s + 1 // runa mais larga que a linha
		}
		lineEnd := end
		for lineEnd > s && rs[lineEnd-1] == ' ' {
			lineEnd--
		}
		out = append(out, [2]int{s, lineEnd})
		s = end
	}
	if len(out) == 0 {
		out = append(out, [2]int{s, s})
	}
	return out
}

// styleSpan pinta a linha [s, e) do texto com os trechos grifados.
func styleSpan(rs []rune, s, e int, hl [][2]int, base lipgloss.Style, plain bool) string {
	var b strings.Builder
	render := func(from, to int, marked bool) {
		if from >= to {
			return
		}
		seg := string(rs[from:to])
		switch {
		case marked:
			b.WriteString(highlight.Render(seg))
		case plain:
			b.WriteString(seg)
		default:
			b.WriteString(base.Render(seg))
		}
	}
	pos := s
	for pos < e {
		next, marked := e, false
		for _, h := range hl {
			if h[0] <= pos && pos < h[1] {
				marked, next = true, min(next, h[1])
				break
			}
			if h[0] > pos {
				next = min(next, h[0])
			}
		}
		render(pos, next, marked)
		pos = next
	}
	return b.String()
}

func (m Foco) readWidth() int {
	width := m.width
	if width <= 0 {
		width = 80
	}
	return max(20, min(textWidth, width-5))
}

func (m Foco) screenHeight() int {
	if m.height <= 0 {
		return 24
	}
	return m.height
}

// paletteRoom: as linhas da ajuda ou dos comandos abertos — em terminal
// baixo, a lista é cortada para sobrarem 3 linhas de texto.
func (m Foco) paletteRoom() int {
	return max(1, min(max(1, m.paletteLines()), m.screenHeight()-4-3))
}

// bodyHeight: o que sobra para o texto entre o cabeçalho (2 linhas) e o
// rodapé (régua, status e ajuda — ou os comandos abertos).
func (m Foco) bodyHeight() int {
	return max(3, m.screenHeight()-4-m.paletteRoom())
}

// relayoutRead refaz a quebra de linhas do capítulo atual quando algo mudou.
func (m *Foco) relayoutRead() {
	r := m.read
	if r == nil {
		return
	}
	width := m.readWidth()
	if r.cacheOK && r.cacheCh == r.ch && r.cacheW == width {
		return
	}
	r.cacheCh, r.cacheW, r.cacheOK = r.ch, width, true
	r.lines = r.lines[:0]
	blocks := r.text.Chapters[r.ch].Blocks
	r.first, r.last = make([]int, len(blocks)), make([]int, len(blocks))
	for i, b := range blocks {
		if i > 0 {
			r.lines = append(r.lines, readLine{block: -1})
		}
		r.first[i] = len(r.lines)
		rs := []rune(b.Text)
		hl := r.highlights(r.ch, i)
		indent, first, cont := 0, "", ""
		style, plain := lipgloss.NewStyle(), true
		switch b.Kind {
		case blockHeading:
			style, plain = headingTxt, false
		case blockQuote:
			indent, first, cont = 2, dim.Render("│ "), dim.Render("│ ")
		case blockItem:
			indent, first, cont = 2, "• ", "  "
		}
		var spans [][2]int
		if b.Kind == blockPre {
			for s, i := 0, 0; i <= len(rs); i++ {
				if i == len(rs) || rs[i] == '\n' {
					spans = append(spans, [2]int{s, i})
					s = i + 1
				}
			}
		} else {
			spans = wrapSpans(rs, width-indent)
		}
		for j, sp := range spans {
			prefix := cont
			if j == 0 {
				prefix = first
			}
			r.lines = append(r.lines, readLine{block: i, text: prefix + styleSpan(rs, sp[0], sp[1], hl, style, plain)})
		}
		r.last[i] = len(r.lines) - 1
	}
}

// invalidate força refazer a quebra (anotação nova pinta o texto).
func (r *readState) invalidate() { r.cacheOK = false }

// --- navegação ---

func (m *Foco) clampTop() {
	r := m.read
	h := m.bodyHeight()
	r.top = max(0, min(r.top, len(r.lines)-h))
}

// show põe o bloco atual na tela; down diz de onde se veio — descendo, o
// começo do bloco; subindo para um bloco comprido, o fim dele.
func (m *Foco) show(fromAbove bool) {
	m.relayoutRead()
	r := m.read
	h := m.bodyHeight()
	first, last := r.first[r.para], r.last[r.para]
	switch {
	case first < r.top:
		r.top = first
		if !fromAbove && last-first+1 > h {
			r.top = last - h + 1
		}
	case last > r.top+h-1:
		r.top = min(first, last-h+1)
	}
	m.clampTop()
}

func (m *Foco) goTo(ch, para int) {
	r := m.read
	r.ch, r.para = ch, para
	m.relayoutRead()
	r.top = r.first[para]
	m.clampTop()
}

func (m *Foco) down() {
	r := m.read
	m.relayoutRead()
	h := m.bodyHeight()
	if r.last[r.para] > r.top+h-1 { // parágrafo maior que a tela: rola por linha
		r.top++
		return
	}
	switch {
	case r.para+1 < len(r.text.Chapters[r.ch].Blocks):
		r.para++
		m.show(true)
	case r.ch+1 < len(r.text.Chapters):
		m.goTo(r.ch+1, 0)
	default:
		m.status = "fim do livro"
	}
}

func (m *Foco) up() {
	r := m.read
	m.relayoutRead()
	if r.first[r.para] < r.top {
		r.top--
		return
	}
	switch {
	case r.para > 0:
		r.para--
		m.show(false)
	case r.ch > 0:
		r.ch--
		r.para = len(r.text.Chapters[r.ch].Blocks) - 1
		m.relayoutRead()
		r.top = len(r.lines)
		m.show(false)
	default:
		m.status = "começo do livro"
	}
}

// pageDown rola uma tela; o cursor vai ao primeiro bloco que começa nela.
// No fim do capítulo, passa ao próximo.
func (m *Foco) pageDown() {
	r := m.read
	m.relayoutRead()
	h := m.bodyHeight()
	maxTop := max(0, len(r.lines)-h)
	if r.top >= maxTop {
		if r.ch+1 < len(r.text.Chapters) {
			m.goTo(r.ch+1, 0)
		} else {
			r.para = len(r.text.Chapters[r.ch].Blocks) - 1
			m.status = "fim do livro"
		}
		return
	}
	r.top = min(r.top+h-1, maxTop)
	r.para = m.firstVisible()
}

func (m *Foco) pageUp() {
	r := m.read
	m.relayoutRead()
	h := m.bodyHeight()
	if r.top == 0 {
		if r.ch > 0 {
			r.ch--
			m.relayoutRead()
			r.para = len(r.text.Chapters[r.ch].Blocks) - 1
			r.top = len(r.lines)
			m.clampTop()
			r.para = m.firstVisible()
		} else {
			r.para = 0
			m.status = "começo do livro"
		}
		return
	}
	r.top = max(0, r.top-h+1)
	r.para = m.firstVisible()
}

// firstVisible: o primeiro bloco que começa na tela (ou o que a ocupa toda).
func (m Foco) firstVisible() int {
	r := m.read
	for i, f := range r.first {
		if f >= r.top {
			return i
		}
	}
	for i := len(r.last) - 1; i >= 0; i-- {
		if r.first[i] <= r.top {
			return i
		}
	}
	return 0
}

// nextNote vai à próxima (dir = 1) ou anterior (-1) anotação na ordem de
// leitura, a partir do parágrafo atual.
func (m *Foco) nextNote(dir int) {
	r := m.read
	best, found := [2]int{}, false
	cur := [2]int{r.ch, r.para}
	less := func(a, b [2]int) bool { return a[0] < b[0] || a[0] == b[0] && a[1] < b[1] }
	for _, n := range r.notes {
		p := [2]int{n.ch, n.para}
		if dir > 0 && less(cur, p) && (!found || less(p, best)) || dir < 0 && less(p, cur) && (!found || less(best, p)) {
			best, found = p, true
		}
	}
	if !found {
		if len(r.notes) == 0 {
			m.status = "nenhuma anotação ainda — /nota ou /grifo"
		} else if dir > 0 {
			m.status = "não há anotação depois daqui"
		} else {
			m.status = "não há anotação antes daqui"
		}
		return
	}
	m.goTo(best[0], best[1])
}

func (m Foco) updateReading(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.read.done {
		return m, nil
	}
	if m.palette != nil {
		return m.updatePalette(msg)
	}
	if msg.Type == tea.KeyRunes && msg.Paste {
		return m, nil // colar só vale dentro dos comandos
	}
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 {
		return m.eachRune(msg)
	}
	m.status = ""
	switch msg.Type {
	case tea.KeyEsc:
		if !m.cfg.Standalone { // no foco, Esc volta ao relógio grande
			m.leaveRead()
			return m, nil
		}
		m.finishReading()
		return m, tea.Quit
	case tea.KeyCtrlC:
		if !m.cfg.Standalone {
			m.saveProgress()
			if err := m.emitEnded("interrupted", ""); err != nil {
				m.Err = err
			}
			return m, tea.Quit
		}
		m.finishReading()
		return m, tea.Quit
	case tea.KeyCtrlP:
		if !m.cfg.Standalone {
			m.saveProgress()
			m.phase = focoBreak
		}
		return m, nil
	case tea.KeyDown:
		m.down()
	case tea.KeyUp:
		m.up()
	case tea.KeyPgDown, tea.KeySpace:
		m.pageDown()
	case tea.KeyPgUp:
		m.pageUp()
	case tea.KeyRight:
		m.chapter(1)
	case tea.KeyLeft:
		m.chapter(-1)
	case tea.KeyHome:
		m.goTo(m.read.ch, 0)
	case tea.KeyEnd:
		m.goTo(m.read.ch, len(m.read.text.Chapters[m.read.ch].Blocks)-1)
	case tea.KeyRunes:
		switch string(msg.Runes) {
		case "/":
			m.openPalette("")
		case "j":
			m.down()
		case "k":
			m.up()
		case "f", " ":
			m.pageDown()
		case "b":
			m.pageUp()
		case "l":
			m.chapter(1)
		case "h":
			m.chapter(-1)
		case "g":
			m.goTo(m.read.ch, 0)
		case "G":
			m.goTo(m.read.ch, len(m.read.text.Chapters[m.read.ch].Blocks)-1)
		case "n":
			m.nextNote(1)
		case "N":
			m.nextNote(-1)
		case "q":
			if !m.cfg.Standalone {
				m.leaveRead()
				return m, nil
			}
			m.finishReading()
			return m, tea.Quit
		}
	}
	return m, nil
}

// eachRune: teclas que chegam juntas (tecla segurada, digitação rápida)
// vêm num só KeyRunes — "jjj", ou "/nota texto" inteiro. Cada runa é uma
// tecla; a que abre os comandos manda as seguintes para eles.
func (m Foco) eachRune(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var model tea.Model = m
	var cmds []tea.Cmd
	for _, r := range msg.Runes {
		next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: msg.Alt})
		model = next
		cmds = append(cmds, cmd)
		if f := next.(Foco); f.read == nil || f.read.done {
			break
		}
	}
	return model, tea.Batch(cmds...)
}

func (m *Foco) chapter(dir int) {
	r := m.read
	next := r.ch + dir
	if next < 0 || next >= len(r.text.Chapters) {
		if dir > 0 {
			m.status = "este é o último capítulo"
		} else {
			m.status = "este é o primeiro capítulo"
		}
		return
	}
	m.goTo(next, 0)
}

// finishReading grava o "onde parei" (só se a posição mudou) e deixa o
// resumo na linha de status, que o `pnn ler` imprime ao sair.
func (m *Foco) finishReading() {
	r := m.read
	if r == nil || r.done {
		return
	}
	r.done = true
	if err := m.saveProgress(); err != nil {
		m.Err = err
		return
	}
	summary := "📖 " + r.book.Title + " — parou em " + m.where()
	if r.created > 0 {
		summary += fmt.Sprintf(" · %d anotaç%s nova%s", r.created, plural(r.created, "ão", "ões"), plural(r.created, "", "s"))
	}
	m.status = summary
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// where: "cap. 3/24 · 42% · p. 40".
func (m Foco) where() string {
	r := m.read
	s := fmt.Sprintf("cap. %d/%d · %d%%", r.ch+1, len(r.text.Chapters), r.percent())
	if page := r.block().Page; page != "" {
		s += " · p. " + page
	}
	return s
}

// citation: "Título, cap. 3 (Sessão 1), p. 40" — a fonte do cartão.
func (m Foco) citation() string {
	r := m.read
	ch := r.text.Chapters[r.ch]
	s := fmt.Sprintf("%s, cap. %d", r.book.Title, r.ch+1)
	if ch.Title != "" {
		s += " (" + ch.Title + ")"
	}
	if page := r.block().Page; page != "" {
		s += ", p. " + page
	}
	return s
}

// --- comandos ---

func (m Foco) runReadCommand(name, args string) (tea.Model, tea.Cmd) {
	r := m.read
	act := m.cfg.Read
	need := func(hint string) (tea.Model, tea.Cmd) {
		m.closePalette()
		m.status = "⚠ " + hint
		return m, nil
	}
	annotate := func(quote, text string) {
		an, status, err := act.Annotate(r.book.ID, r.loc(), r.block().Page, quote, text)
		if err != nil {
			m.result("", err)
			return
		}
		an.At = r.loc()
		r.notes = append(r.notes, placedNote{BookAnnotation: an, ch: r.ch, para: r.para})
		r.created++
		r.invalidate()
		m.status = status
	}
	pickNote := func(title string, run func(*Foco, int) (string, error)) (tea.Model, tea.Cmd) {
		here := r.here()
		switch len(here) {
		case 0:
			return need("nenhuma anotação neste parágrafo — /nota ou /grifo cria; n e N andam entre elas")
		case 1:
			m.closePalette()
			m.result(run(&m, here[0]))
			return m, nil
		}
		var options []Choice
		for _, i := range here {
			options = append(options, Choice{Kind: "annotation", ID: strconv.Itoa(i), Label: noteLabel(r.notes[i])})
		}
		m.choose(title, options, func(f *Foco, c Choice) (string, error) {
			i, _ := strconv.Atoi(c.ID)
			return run(f, i)
		})
		return m, nil
	}

	switch name {
	case "nota":
		if args == "" {
			return need("/nota comentário — ou /nota trecho | comentário")
		}
		quote, text := paragraphQuote(r.block().Text), args
		if typed, comment, ok := strings.Cut(args, "|"); ok {
			found, inText := excerpt(r.block().Text, typed)
			if !inText {
				return need(fmt.Sprintf("%q não está neste parágrafo", strings.TrimSpace(typed)))
			}
			quote, text = found, strings.TrimSpace(comment)
			if text == "" {
				return need("/nota trecho | comentário — falta o comentário (só grifar: /grifo trecho)")
			}
		}
		m.closePalette()
		annotate(quote, text)

	case "grifo":
		quote := paragraphQuote(r.block().Text)
		if args != "" {
			found, ok := excerpt(r.block().Text, args)
			if !ok {
				return need(fmt.Sprintf("%q não está neste parágrafo — /grifo sem nada grifa o parágrafo inteiro", args))
			}
			quote = found
		}
		m.closePalette()
		annotate(quote, "")

	case "notas":
		words := strings.Fields(foldASCII(args))
		var options []Choice
		for i, n := range r.notes {
			if containsWords(foldASCII(n.Quote+" "+n.Text), words) {
				options = append(options, Choice{Kind: "annotation", ID: strconv.Itoa(i), Label: noteLabel(n)})
			}
		}
		if len(options) == 0 {
			if len(r.notes) == 0 {
				return need("nenhuma anotação ainda — /nota ou /grifo")
			}
			return need(fmt.Sprintf("nenhuma anotação com %q", args))
		}
		sortByReading(options, r.notes)
		title := "Anotações — o número vai até ela"
		if len(options) > 9 {
			options = nearest(options, r)
			title = fmt.Sprintf("Anotações a partir daqui — 9 de %d (/notas palavra filtra; pnn anotacoes lista todas)", len(r.notes))
		}
		m.choose(title, options, func(f *Foco, c Choice) (string, error) {
			i, _ := strconv.Atoi(c.ID)
			n := f.read.notes[i]
			f.goTo(n.ch, n.para)
			return "", nil
		})

	case "editar":
		if args == "" {
			return need("/editar novo comentário (vazio não; para virar grifo, apague e grife)")
		}
		return pickNote("Editar qual anotação?", func(f *Foco, i int) (string, error) {
			status, err := act.EditAnnotation(f.read.notes[i].ID, args)
			if err == nil {
				f.read.notes[i].Text = strings.TrimSpace(args)
				f.read.invalidate()
			}
			return status, err
		})

	case "apagar":
		return pickNote("Apagar qual anotação?", func(f *Foco, i int) (string, error) {
			status, err := act.DeleteAnnotation(f.read.notes[i].ID)
			if err == nil {
				f.read.notes = append(f.read.notes[:i:i], f.read.notes[i+1:]...)
				f.read.invalidate()
			}
			return status, err
		})

	case "ir":
		return m.goCommand(args)

	case "buscar":
		if args == "" {
			return need("/buscar termo")
		}
		hits := m.search(args)
		switch len(hits) {
		case 0:
			return need(fmt.Sprintf("nada encontrado para %q", args))
		case 1:
			m.closePalette()
			m.goTo(hits[0][0], hits[0][1])
			m.status = fmt.Sprintf("1 ocorrência de %q", args)
			return m, nil
		}
		var options []Choice
		for _, h := range hits[:min(len(hits), 9)] {
			options = append(options, Choice{Kind: "pos", ID: fmt.Sprintf("%d:%d", h[0], h[1]), Label: m.hitLabel(h, args)})
		}
		title := fmt.Sprintf("%d ocorrências de %q a partir daqui", len(hits), args)
		if len(hits) > 9 {
			title += " — as 9 primeiras"
		}
		m.choose(title, options, jumpTo)

	case "cartao":
		front, back, ok := strings.Cut(args, "|")
		front, back = strings.TrimSpace(front), strings.TrimSpace(back)
		if !ok || front == "" || back == "" {
			return need("/cartao frente | verso")
		}
		m.closePalette()
		m.result(act.CreateCard(r.book.ID, m.citation(), front, back))

	case "distracao":
		if args == "" {
			return need("/distracao o que passou pela cabeça")
		}
		m.closePalette()
		if m.cfg.Standalone { // sem sessão, o pensamento solto vai para a caixa
			m.result(act.CaptureNote(args))
			return m, nil
		}
		if err := m.cfg.Emit("distraction.captured", map[string]any{
			"distraction_id": m.cfg.GenID(), "session_id": m.sessionID, "text": args,
		}); err != nil {
			m.result("", err)
			return m, nil
		}
		m.Distractions++
		m.status = "✔ distração anotada — triagem depois; de volta à leitura"

	case "sair":
		m.closePalette()
		if !m.cfg.Standalone {
			m.leaveRead()
			return m, nil
		}
		m.finishReading()
		return m, tea.Quit
	}
	return m, nil
}

func jumpTo(f *Foco, c Choice) (string, error) {
	a, b, _ := strings.Cut(c.ID, ":")
	ch, _ := strconv.Atoi(a)
	para, _ := strconv.Atoi(b)
	f.goTo(ch, para)
	return "", nil
}

func noteLabel(n placedNote) string {
	quote := []rune(strings.TrimSuffix(n.Quote, "…"))
	if len(quote) > 50 {
		quote = append(quote[:50], '…')
	}
	label := fmt.Sprintf("cap. %d · %q", n.ch+1, string(quote))
	if n.Page != "" {
		label = fmt.Sprintf("cap. %d · p. %s · %q", n.ch+1, n.Page, string(quote))
	}
	if n.Text != "" {
		label += " — " + n.Text
	}
	if n.lost {
		label += " (trecho não encontrado nesta edição)"
	}
	return label
}

func sortByReading(options []Choice, notes []placedNote) {
	key := func(c Choice) [2]int {
		i, _ := strconv.Atoi(c.ID)
		return [2]int{notes[i].ch, notes[i].para}
	}
	for i := 1; i < len(options); i++ { // inserção: estável e as listas são curtas
		for j := i; j > 0; j-- {
			a, b := key(options[j-1]), key(options[j])
			if a[0] < b[0] || a[0] == b[0] && a[1] <= b[1] {
				break
			}
			options[j-1], options[j] = options[j], options[j-1]
		}
	}
}

// nearest: as 9 anotações a partir do parágrafo atual (dando a volta no fim).
func nearest(options []Choice, r *readState) []Choice {
	start := len(options)
	for i, c := range options {
		n, _ := strconv.Atoi(c.ID)
		if r.notes[n].ch > r.ch || r.notes[n].ch == r.ch && r.notes[n].para >= r.para {
			start = i
			break
		}
	}
	out := make([]Choice, 0, 9)
	for i := 0; i < len(options) && len(out) < 9; i++ {
		out = append(out, options[(start+i)%len(options)])
	}
	return out
}

// goCommand: /ir 3 (capítulo), /ir 50%, /ir p40 (página impressa), /ir
// palavras do título do capítulo.
func (m Foco) goCommand(args string) (tea.Model, tea.Cmd) {
	r := m.read
	need := func(hint string) (tea.Model, tea.Cmd) {
		m.closePalette()
		m.status = "⚠ " + hint
		return m, nil
	}
	if args == "" {
		return need("/ir 3 · /ir 50% · /ir p40 · /ir palavras do título")
	}
	if pct, ok := strings.CutSuffix(args, "%"); ok {
		n, err := strconv.Atoi(strings.TrimSpace(pct))
		if err != nil || n < 0 || n > 100 {
			return need("/ir 50% — um número de 0 a 100")
		}
		target := min(n*r.total/100, r.total-1)
		for i := len(r.before) - 1; i >= 0; i-- {
			if r.before[i] <= target {
				m.closePalette()
				m.goTo(i, target-r.before[i])
				return m, nil
			}
		}
	}
	if n, err := strconv.Atoi(args); err == nil {
		if n < 1 || n > len(r.text.Chapters) {
			return need(fmt.Sprintf("o livro tem %d capítulos", len(r.text.Chapters)))
		}
		m.closePalette()
		m.goTo(n-1, 0)
		return m, nil
	}
	lower := strings.ToLower(args)
	if page, ok := strings.CutPrefix(lower, "p"); ok {
		page = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(page, "."), "ág"))
		if page != "" && !strings.Contains(page, " ") {
			for c, ch := range r.text.Chapters {
				for i, b := range ch.Blocks {
					if strings.EqualFold(b.Page, page) {
						m.closePalette()
						m.goTo(c, i)
						return m, nil
					}
				}
			}
			if _, err := strconv.Atoi(page); err == nil {
				return need(fmt.Sprintf("página %s não encontrada — este EPUB não traz a paginação impressa; use /ir 50%%", page))
			}
		}
	}
	words := strings.Fields(foldASCII(args))
	var options []Choice
	for c, ch := range r.text.Chapters {
		if ch.Title != "" && containsWords(foldASCII(ch.Title), words) {
			options = append(options, Choice{Kind: "pos", ID: fmt.Sprintf("%d:0", c), Label: fmt.Sprintf("cap. %d · %s", c+1, ch.Title)})
		}
	}
	switch len(options) {
	case 0:
		return need(fmt.Sprintf("nenhum capítulo com %q no título — /buscar procura no texto", args))
	case 1:
		m.closePalette()
		jumpTo(&m, options[0])
		return m, nil
	}
	m.choose("Ir para qual capítulo?", options[:min(len(options), 9)], jumpTo)
	return m, nil
}

// search: os blocos com todas as palavras, a partir do atual (dando a volta).
func (m Foco) search(query string) [][2]int {
	r := m.read
	words := strings.Fields(foldASCII(query))
	var hits [][2]int
	n := r.total
	pos := r.before[r.ch] + r.para + 1
	for k := 0; k < n; k++ {
		g := (pos + k) % n
		c := 0
		for c+1 < len(r.before) && r.before[c+1] <= g {
			c++
		}
		if containsWords(foldASCII(r.text.Chapters[c].Blocks[g-r.before[c]].Text), words) {
			hits = append(hits, [2]int{c, g - r.before[c]})
		}
	}
	return hits
}

// hitLabel: "cap. 3 · …o trecho com o termo…".
func (m Foco) hitLabel(h [2]int, query string) string {
	text := []rune(strings.ReplaceAll(m.read.text.Chapters[h[0]].Blocks[h[1]].Text, "\n", " "))
	words := strings.Fields(foldASCII(query))
	at := 0
	if len(words) > 0 {
		at = max(0, runeIndex([]rune(foldASCII(string(text))), []rune(words[0])))
	}
	from, to := max(0, at-20), min(len(text), at+40)
	snippet := string(text[from:to])
	if from > 0 {
		snippet = "…" + snippet
	}
	if to < len(text) {
		snippet += "…"
	}
	return fmt.Sprintf("cap. %d · %s", h[0]+1, snippet)
}

// --- tela ---

func (m Foco) readView() string {
	r := m.read
	width := m.width
	if width <= 0 {
		width = 80
	}
	(&m).relayoutRead()
	h := m.bodyHeight()

	var head string
	right := m.where()
	chTitle := r.text.Chapters[r.ch].Title
	if chTitle == "" {
		chTitle = fmt.Sprintf("Capítulo %d", r.ch+1)
	}
	if m.cfg.Standalone {
		title := "📖 " + r.book.Title
		if r.text.Author != "" {
			title += " — " + r.text.Author
		}
		head = blueTitle.Render(title)
		if gap := width - 1 - lipgloss.Width(head) - lipgloss.Width(right); gap < 2 {
			head = blueTitle.Render("📖 " + r.book.Title)
		}
	} else {
		// No foco, o relógio vem primeiro (como na escrita) e o livro desce
		// para a régua.
		title := "● FOCO ⏱ " + m.timer.clock()
		if m.cfg.TaskTitle != "" {
			title += " · " + m.cfg.TaskTitle
		}
		head = redTitle.Render(title)
		chTitle = "📖 " + r.book.Title + " · " + chTitle
	}
	if gap := width - 1 - lipgloss.Width(head) - lipgloss.Width(right); gap >= 2 {
		head += strings.Repeat(" ", gap) + dim.Render(right)
	}
	rule := dim.Render("── " + chTitle + " " + strings.Repeat("─", max(0, width-5-ansi.StringWidth(chTitle))))
	lines := []string{head, rule}

	annotated := map[int]bool{}
	for _, n := range r.notes {
		if n.ch == r.ch {
			annotated[n.para] = true
		}
	}
	for i := r.top; i < r.top+h; i++ {
		if i >= len(r.lines) {
			if i == len(r.lines) {
				end := "· fim do capítulo — → passa ao próximo ·"
				if r.ch+1 == len(r.text.Chapters) {
					end = "· fim do livro ·"
				}
				lines = append(lines, "   "+dim.Render(end))
			} else {
				lines = append(lines, "")
			}
			continue
		}
		line := r.lines[i]
		gutter := "  "
		if line.block >= 0 {
			bar, mark := " ", " "
			if line.block == r.para {
				bar = cursorStyle.Render("▌")
			}
			if annotated[line.block] && i == r.first[line.block] {
				mark = noteStyle.Render("✎")
			}
			gutter = bar + mark
		}
		lines = append(lines, gutter+" "+line.text)
	}
	lines = append(lines, dim.Render(strings.Repeat("─", width-1)))

	var info []string
	for _, i := range r.here() {
		n := r.notes[i]
		switch {
		case n.Text != "":
			info = append(info, "✎ "+n.Text)
		default:
			info = append(info, "▍ grifo: "+strings.TrimSuffix(n.Quote, "…"))
		}
		if n.lost {
			info[len(info)-1] += " (trecho não encontrado nesta edição)"
		}
	}
	foot := strings.Join(info, " · ")
	if m.status != "" {
		foot = m.status + "   " + foot
	}
	lines = append(lines, " "+dim.Render(foot))

	switch {
	case m.phase == focoCheckin:
		lines = append(lines, " 🔔 "+m.checkinQuestion()+" "+dim.Render("[s] sim · [n] distraí · Esc pula"))
	case m.palette != nil:
		palette := m.paletteView()
		lines = append(lines, palette[:min(len(palette), m.paletteRoom())]...)
	case m.cfg.Standalone:
		lines = append(lines, " "+dim.Render("↑↓ parágrafo · Espaço/b tela · ←→ capítulo · n/N anotação · / comandos · Esc sai"))
	default:
		lines = append(lines, " "+dim.Render("↑↓ parágrafo · Espaço/b tela · ←→ capítulo · / comandos · Esc relógio · Ctrl+P pausa"))
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width-1, "…")
	}
	return strings.Join(lines, "\n")
}
