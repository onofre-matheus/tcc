// Modo de escrita do foco (spec/CLI.md §3, "Página da tarefa"): a página
// ligada à tarefa aberta num editor dentro da própria tela, com o relógio
// numa barra no topo. Ao contrário do $EDITOR (Ctrl+E), que toma o terminal e
// para a tela, aqui o relógio, os check-ins e os avisos seguem vivos.
//
// Os comandos "/" valem na escrita (o ponto de referência é a página) e no
// relógio grande (é a tarefa em foco): criar página e começar a escrever,
// criar subtarefa, concluir, ligar, criar cartão, anotar distração —
// registrar sem trocar de ferramenta no meio do trabalho, a ideia do
// adiamento de distrações (RF06) estendida. Eles só emitem eventos que já
// existem; o TUI desenha e chama, e o acervo fica com a CLI (WriteActions).
package tui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// PageText é a página carregada para a escrita.
type PageText struct {
	Title, Body, Version string
}

// Choice é um item escolhível nos comandos: tarefa, cartão ou página.
type Choice struct {
	Kind, ID, Label string
}

// WriteActions é o que a escrita e os comandos pedem ao acervo. As ações de
// comando devolvem a linha de status. Um Choice também serve de referência a
// um nó (Kind + ID): a página aberta ou a tarefa em foco.
type WriteActions interface {
	Load(pageID string) (PageText, error)
	// Save grava o corpo se ele mudou em relação à versão atual (page.edited
	// com base) e devolve a versão vigente — a base da próxima gravação.
	Save(pageID, body, base string) (string, error)
	Refs(pageID string) ([]string, error) // rótulos ⚡ do que está ligado à página
	// CreatePage cria a página — ligada à tarefa, se taskID não for vazio.
	CreatePage(taskID, title string) (LinkedPage, string, error)
	// CreateTask cria a tarefa: subtarefa de parentID e/ou ligada a pageID
	// (vazios = sem).
	CreateTask(parentID, pageID, title string) (string, error)
	// Tasks: as abertas — a em foco primeiro, depois as ligadas à página,
	// depois as do dia.
	Tasks(pageID, focusID string) ([]Choice, error)
	CompleteTask(taskID string) (string, error)
	Tag(pageID string, add, remove []string) (string, error)
	Search(excludeID, query string) ([]Choice, error)
	Link(from, target Choice) (string, error)
	CreateCard(from Choice, front, back string) (string, error)
	// CaptureNote anota na caixa de entrada — o /distracao fora de uma sessão
	// de foco (pnn escrever), onde não há sessão para a distração pertencer.
	CaptureNote(text string) (string, error)
}

// autosaveEvery: com mudança pendente, grava sozinho a cada 2 min corridos —
// cada gravação é um evento com o texto inteiro, então nunca por tecla.
const autosaveEvery = 120

type writeState struct {
	page    LinkedPage
	editor  textarea.Model
	base    string // versão lida ou gravada por último (base da próxima)
	saved   string // texto da última gravação: mudou = editor ≠ saved
	unsaved int    // segundos corridos com mudança pendente
	refs    []string

	// Desfazer (Ctrl+Z) e refazer (Ctrl+Y): o textarea não tem, então o
	// estado guarda retratos do texto antes de cada grupo de edição — uma
	// palavra digitada, uma sequência de apagar, um Enter, uma colagem.
	undo, redo []snapshot
	lastKind   string
	afterSpace bool
}

type snapshot struct {
	value    string
	row, col int
}

const maxUndo = 200

type palette struct {
	input  textinput.Model
	choose *chooser // depois de /feito ou /ligar: qual item
}

type chooser struct {
	title   string
	options []Choice
	pick    func(*Foco, Choice) (string, error) // pode trocar a página aberta
}

// Onde um comando vale: na escrita (o ponto de referência é a página) ou no
// relógio grande (é a tarefa em foco). help vazio = não vale ali.
type writeCommand struct {
	name, helpPage, helpClock string
}

// A ordem importa: Enter executa o primeiro que casa (/tar → /tarefa).
var writeCommands = []writeCommand{
	{"pagina", "troca de página, ou cria outra ligada à tarefa — /pagina Título", "cria uma página ligada à tarefa e começa a escrever — /pagina Título"},
	{"tarefa", "cria uma tarefa ligada à página (subtarefa da em foco) — /tarefa Título", "cria uma subtarefa da tarefa em foco — /tarefa Título"},
	{"tarefas", "lista as tarefas abertas — o número conclui", "lista as tarefas abertas — o número conclui"},
	{"feito", "conclui uma tarefa (a em foco e as da página primeiro)", "conclui uma tarefa (a em foco primeiro)"},
	{"cartao", "cria um cartão ligado à página — /cartao frente | verso", "cria um cartão ligado à tarefa — /cartao frente | verso"},
	{"tag", "classifica a página — /tag cli docs", ""},
	{"untag", "tira tags da página — /untag docs", ""},
	{"ligar", "liga a página a um cartão ou página — /ligar pod", "liga a tarefa a uma página ou cartão — /ligar pod"},
	{"distracao", "anota uma distração sem sair do foco", "anota uma distração (o mesmo que a linha de baixo)"},
	{"salvar", "grava agora (Ctrl+S)", ""},
	{"sair", "volta ao relógio grande (Esc)", ""},
}

func (c writeCommand) help(writing bool) string {
	if writing {
		return c.helpPage
	}
	return c.helpClock
}

func newEditor() textarea.Model {
	ed := textarea.New()
	ed.ShowLineNumbers = false
	ed.Prompt = " "
	ed.MaxHeight = 0 // o padrão (99) limitaria as LINHAS da página, não só a altura
	ed.CharLimit = 0
	ed.Placeholder = "escreva — / no começo de uma linha abre os comandos"
	ed.FocusedStyle.CursorLine = lipgloss.NewStyle()
	// Ctrl+P (pausa) e Ctrl+E ($EDITOR) são do foco; as setas e o End bastam.
	ed.KeyMap.LinePrevious = key.NewBinding(key.WithKeys("up"))
	ed.KeyMap.LineEnd = key.NewBinding(key.WithKeys("end"))
	return ed
}

// enterWrite abre a página no modo de escrita.
func (m *Foco) enterWrite(p LinkedPage) tea.Cmd {
	if m.cfg.Write == nil {
		return nil
	}
	text, err := m.cfg.Write.Load(p.ID)
	if err != nil {
		m.status = "⚠ " + err.Error()
		return nil
	}
	if text.Title != "" {
		p.Title = text.Title
	}
	w := &writeState{page: p, editor: newEditor(), base: text.Version, saved: text.Body}
	w.editor.SetValue(text.Body) // o cursor fica no fim: no foco, continua-se de onde parou
	m.write = w
	m.writing = true
	m.resizeEditor()
	m.refreshRefs()
	return w.editor.Focus()
}

// leaveWrite grava e volta ao relógio grande; a página continua carregada.
func (m *Foco) leaveWrite() {
	m.saveWrite(false)
	m.writing = false
}

// reloadWrite relê a página depois do $EDITOR, que pode tê-la mudado.
func (m *Foco) reloadWrite() {
	text, err := m.cfg.Write.Load(m.write.page.ID)
	if err != nil {
		m.status = "⚠ " + err.Error()
		return
	}
	m.write.editor.SetValue(text.Body)
	m.write.base, m.write.saved, m.write.unsaved = text.Version, text.Body, 0
	m.write.undo, m.write.redo, m.write.lastKind = nil, nil, "" // o $EDITOR reescreveu
}

func trimText(s string) string { return strings.TrimRight(s, " \t\r\n") }

func (m Foco) dirty() bool {
	return m.write != nil && trimText(m.write.editor.Value()) != trimText(m.write.saved)
}

// saveWrite grava se houver mudança; auto marca as gravações que o usuário
// não pediu (autosave, fim de bloco, saída).
func (m *Foco) saveWrite(auto bool) {
	w := m.write
	if w == nil || !m.dirty() {
		return
	}
	body := w.editor.Value()
	version, err := m.cfg.Write.Save(w.page.ID, body, w.base)
	if err != nil {
		m.status = "⚠ não salvou: " + err.Error()
		return
	}
	w.base, w.saved, w.unsaved = version, body, 0
	m.status = "✔ salvo " + m.cfg.Now().Format("15:04")
	if auto {
		m.status += " (automático)"
	}
}

func (m *Foco) refreshRefs() {
	if refs, err := m.cfg.Write.Refs(m.write.page.ID); err == nil {
		m.write.refs = refs
	}
}

func (m Foco) paletteLines() int {
	if m.palette == nil {
		return 0
	}
	if c := m.palette.choose; c != nil {
		return 2 + min(len(c.options), 9)
	}
	return 1 + min(len(matchCommands(m.palette.input.Value(), m.writing)), 6)
}

// resizeEditor encaixa o editor entre o cabeçalho (2 linhas) e o rodapé (3),
// mais o que os comandos abertos ocupam.
func (m *Foco) resizeEditor() {
	if m.write == nil {
		return
	}
	width, height := m.width, m.height
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	m.write.editor.SetWidth(max(20, width-2))
	m.write.editor.SetHeight(max(3, height-5-m.paletteLines()))
}

// slashOpensCommands: o "/" abre os comandos no começo da linha ou depois de
// um espaço, como no Notion; colado a uma palavra é só uma barra ("e/ou",
// "http://"). Para escrever uma barra onde ela abriria os comandos: "//".
func (m Foco) slashOpensCommands() bool {
	lines := strings.Split(m.write.editor.Value(), "\n")
	row := m.write.editor.Line()
	if row >= len(lines) {
		return true
	}
	info := m.write.editor.LineInfo()
	col := info.StartColumn + info.ColumnOffset
	line := []rune(lines[row])
	return col == 0 || col > len(line) || unicode.IsSpace(line[col-1])
}

func (m Foco) updateWriting(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	w := m.write
	if m.palette != nil {
		return m.updatePalette(msg)
	}
	switch msg.Type {
	case tea.KeyCtrlS:
		m.saveWrite(false)
		if !m.dirty() && !strings.HasPrefix(m.status, "✔ salvo") && !strings.HasPrefix(m.status, "⚠") {
			m.status = "nada mudou desde a última gravação"
		}
		return m, nil
	case tea.KeyCtrlZ:
		m.status = w.step(&w.undo, &w.redo, "nada a desfazer")
		return m, nil
	case tea.KeyCtrlY:
		m.status = w.step(&w.redo, &w.undo, "nada a refazer")
		return m, nil
	case tea.KeyEsc:
		if m.cfg.Standalone { // fora do foco, Esc é sair
			m.saveWrite(true)
			return m, tea.Quit
		}
		m.leaveWrite()
		return m, nil
	case tea.KeyCtrlC:
		m.saveWrite(true)
		if !m.cfg.Standalone {
			if err := m.emitEnded("interrupted", ""); err != nil {
				m.Err = err
			}
		}
		return m, tea.Quit
	case tea.KeyCtrlP:
		if m.cfg.Standalone {
			return m, nil
		}
		m.saveWrite(true)
		m.phase = focoBreak
		return m, nil
	case tea.KeyCtrlE:
		// O vim, para edição pesada: grava antes, relê na volta (afterEdit).
		m.saveWrite(true)
		return m.openPage(w.page)
	case tea.KeyRunes:
		if !msg.Paste && string(msg.Runes) == "/" && m.slashOpensCommands() {
			m.openPalette("")
			return m, nil
		}
	}
	w.record(msg)
	var cmd tea.Cmd
	w.editor, cmd = w.editor.Update(msg)
	return m, cmd
}

// --- desfazer ---

func (w *writeState) snapshot() snapshot {
	info := w.editor.LineInfo()
	return snapshot{value: w.editor.Value(), row: w.editor.Line(), col: info.StartColumn + info.ColumnOffset}
}

// restore põe o texto e o cursor de volta: SetValue deixa o cursor no fim,
// então ele sobe até a linha e vai à coluna.
func (w *writeState) restore(s snapshot) {
	w.editor.SetValue(s.value)
	for i := 0; i < w.editor.LineCount()*4 && w.editor.Line() > s.row; i++ {
		w.editor.CursorUp()
	}
	w.editor.SetCursor(s.col)
}

func push(stack *[]snapshot, s snapshot) {
	if n := len(*stack); n > 0 && (*stack)[n-1].value == s.value {
		return
	}
	*stack = append(*stack, s)
	if len(*stack) > maxUndo {
		*stack = (*stack)[1:]
	}
}

// editKind classifica a tecla para agrupar o desfazer; "" = não edita.
func editKind(msg tea.KeyMsg) string {
	switch msg.Type {
	case tea.KeyRunes, tea.KeySpace:
		if msg.Paste {
			return "paste"
		}
		return "type"
	case tea.KeyEnter, tea.KeyTab:
		return "break"
	case tea.KeyBackspace, tea.KeyDelete, tea.KeyCtrlW, tea.KeyCtrlK, tea.KeyCtrlU, tea.KeyCtrlD, tea.KeyCtrlH:
		return "delete"
	}
	return ""
}

// record guarda o retrato de antes da tecla quando começa um grupo novo de
// edição: cada palavra digitada, cada sequência de apagar, cada Enter ou
// colagem vira um passo do Ctrl+Z. Editar de novo apaga o que havia para
// refazer.
func (w *writeState) record(msg tea.KeyMsg) {
	kind := editKind(msg)
	if kind == "" {
		w.lastKind = "move" // andar com o cursor encerra o grupo
		return
	}
	space := msg.Type == tea.KeySpace || (msg.Type == tea.KeyRunes && string(msg.Runes) == " ")
	newWord := kind == "type" && !space && w.afterSpace
	if kind != w.lastKind || kind == "break" || kind == "paste" || newWord {
		push(&w.undo, w.snapshot())
	}
	w.redo = nil
	w.lastKind, w.afterSpace = kind, space
}

// step desfaz (from = undo) ou refaz (from = redo): o estado atual vai para a
// outra pilha e o retrato do topo volta.
func (w *writeState) step(from, to *[]snapshot, empty string) string {
	if len(*from) == 0 {
		return empty
	}
	top := (*from)[len(*from)-1]
	*from = (*from)[:len(*from)-1]
	push(to, w.snapshot())
	w.restore(top)
	w.lastKind = "move"
	return ""
}

// --- comandos ---

// openPalette abre os comandos — na escrita ("/" no começo da linha ou depois
// de um espaço) ou no relógio grande ("/" como primeiro caractere da linha de
// distração). prefill já deixa um comando escrito (Ctrl+O sem página).
func (m *Foco) openPalette(prefill string) {
	in := textinput.New()
	in.Prompt = " /"
	in.Placeholder = "comando — Tab completa · Esc fecha · // escreve a barra"
	in.Focus()
	in.SetValue(prefill)
	in.CursorEnd()
	m.palette = &palette{input: in}
	m.resizeEditor()
}

func (m *Foco) closePalette() {
	m.palette = nil
	m.resizeEditor()
}

// source é o ponto de referência dos comandos: a página aberta na escrita, a
// tarefa em foco no relógio grande; ok = false no foco livre (sem tarefa).
func (m Foco) source() (Choice, bool) {
	if m.writing && m.write != nil {
		return Choice{Kind: "page", ID: m.write.page.ID, Label: "página " + m.write.page.Title}, true
	}
	if m.cfg.TaskID != "" {
		return Choice{Kind: "task", ID: m.cfg.TaskID, Label: "tarefa " + m.cfg.TaskTitle}, true
	}
	return Choice{}, false
}

func (m Foco) pageID() string {
	if m.writing && m.write != nil {
		return m.write.page.ID
	}
	return ""
}

// foldASCII tira acento e caixa — "/distração" e "/Distracao" são o mesmo.
func foldASCII(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch r {
		case 'á', 'à', 'â', 'ã':
			r = 'a'
		case 'é', 'ê':
			r = 'e'
		case 'í':
			r = 'i'
		case 'ó', 'ô', 'õ':
			r = 'o'
		case 'ú', 'ü':
			r = 'u'
		case 'ç':
			r = 'c'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// matchCommands: os comandos que valem aqui e cujo nome começa com a primeira
// palavra digitada.
func matchCommands(input string, writing bool) []writeCommand {
	word := foldASCII(strings.TrimSpace(input))
	if i := strings.IndexFunc(word, unicode.IsSpace); i >= 0 {
		word = word[:i]
	}
	var out []writeCommand
	for _, c := range writeCommands {
		if c.help(writing) != "" && strings.HasPrefix(c.name, word) {
			out = append(out, c)
		}
	}
	return out
}

// interrupt é o Ctrl+C de dentro dos comandos: o mesmo da tela por baixo.
func (m Foco) interrupt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.closePalette()
	if m.writing {
		return m.updateWriting(msg)
	}
	return m.updateRunning(msg)
}

func (m Foco) updatePalette(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.palette
	if p.choose != nil {
		return m.updateChooser(msg)
	}
	switch msg.Type {
	case tea.KeyEsc:
		m.closePalette()
		return m, nil
	case tea.KeyCtrlC:
		return m.interrupt(msg)
	case tea.KeyBackspace:
		if p.input.Value() == "" {
			m.closePalette()
			return m, nil
		}
	case tea.KeyRunes:
		if string(msg.Runes) == "/" && p.input.Value() == "" { // "//" escreve a barra
			m.closePalette()
			if m.writing {
				m.write.record(msg)
				m.write.editor.InsertString("/")
			} else {
				m.input.SetValue("/")
				m.input.CursorEnd()
			}
			return m, nil
		}
	case tea.KeyTab:
		if found := matchCommands(p.input.Value(), m.writing); len(found) > 0 {
			_, args, _ := strings.Cut(strings.TrimSpace(p.input.Value()), " ")
			p.input.SetValue(strings.TrimSpace(found[0].name+" "+args) + " ")
			p.input.CursorEnd()
		}
		return m, nil
	case tea.KeyEnter:
		return m.runCommand(p.input.Value())
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	m.resizeEditor() // a lista de comandos que casam muda de tamanho
	return m, cmd
}

func (m *Foco) result(status string, err error) {
	if err != nil {
		m.status = "⚠ " + err.Error()
		return
	}
	m.status = status
}

// switchPage passa a escrever na página p (gravando a atual antes).
func (m *Foco) switchPage(p LinkedPage) tea.Cmd {
	if m.writing {
		m.saveWrite(true)
	}
	return m.enterWrite(p)
}

func (m *Foco) refreshAfter() {
	if m.writing && m.write != nil {
		m.refreshRefs()
	}
}

// runCommand executa a linha "/comando args". Um nome incompleto executa o
// primeiro comando que casa (o marcado com ▸).
func (m Foco) runCommand(line string) (tea.Model, tea.Cmd) {
	name, args, _ := strings.Cut(strings.TrimSpace(line), " ")
	args = strings.TrimSpace(args)
	found := matchCommands(name, m.writing)
	for _, c := range found {
		if c.name == foldASCII(name) {
			found = []writeCommand{c}
			break
		}
	}
	if name == "" || len(found) == 0 {
		m.closePalette()
		m.status = fmt.Sprintf("⚠ comando desconhecido aqui: /%s — / e Tab mostram os comandos", name)
		return m, nil
	}
	act := m.cfg.Write
	src, hasSrc := m.source()
	need := func(hint string) (tea.Model, tea.Cmd) {
		m.closePalette()
		m.status = "⚠ " + hint
		return m, nil
	}
	complete := func(_ *Foco, c Choice) (string, error) { return act.CompleteTask(c.ID) }

	switch found[0].name {
	case "pagina":
		if args == "" {
			var others []Choice
			for _, pg := range m.cfg.Pages {
				if pg.ID != m.pageID() {
					others = append(others, Choice{Kind: "page", ID: pg.ID, Label: "página " + pg.Title})
				}
			}
			if len(others) > 0 { // trocar de página
				m.choose("Escrever em qual página?", others, func(f *Foco, c Choice) (string, error) {
					f.switchPage(LinkedPage{ID: c.ID, Title: strings.TrimPrefix(c.Label, "página ")})
					return "", nil
				})
				return m, nil
			}
			if args = m.cfg.TaskTitle; args == "" {
				return need("/pagina Título da página")
			}
		}
		m.closePalette()
		page, status, err := act.CreatePage(m.cfg.TaskID, args)
		if err != nil {
			m.result("", err)
			return m, nil
		}
		m.cfg.Pages = append(m.cfg.Pages, page)
		cmd := m.switchPage(page)
		m.status = status
		return m, cmd

	case "tarefa":
		if args == "" {
			return need("/tarefa Título da tarefa")
		}
		m.closePalette()
		m.result(act.CreateTask(m.cfg.TaskID, m.pageID(), args))
		m.refreshAfter()

	case "tarefas":
		tasks, err := act.Tasks(m.pageID(), m.cfg.TaskID)
		if err != nil {
			m.closePalette()
			m.result("", err)
			return m, nil
		}
		if len(tasks) == 0 {
			return need("nenhuma tarefa aberta — /tarefa cria uma")
		}
		title := "Tarefas abertas — o número conclui"
		if len(tasks) > 9 {
			title += fmt.Sprintf(" · as 9 primeiras de %d (pnn dia mostra todas)", len(tasks))
		}
		m.choose(title, tasks, complete)

	case "feito":
		tasks, err := act.Tasks(m.pageID(), m.cfg.TaskID)
		if err != nil {
			m.closePalette()
			m.result("", err)
			return m, nil
		}
		tasks = filterChoices(tasks, args)
		switch {
		case len(tasks) == 0:
			return need("nenhuma tarefa aberta casa com isso")
		case len(tasks) == 1 && args != "":
			m.closePalette()
			m.result(act.CompleteTask(tasks[0].ID))
			m.refreshAfter()
		default:
			m.choose("Concluir qual?", tasks, complete)
		}

	case "cartao":
		front, back, ok := strings.Cut(args, "|")
		front, back = strings.TrimSpace(front), strings.TrimSpace(back)
		if !ok || front == "" || back == "" {
			return need("/cartao frente | verso")
		}
		if !hasSrc {
			return need("foco livre: sem tarefa para ligar o cartão — crie com `pnn carta`")
		}
		m.closePalette()
		m.result(act.CreateCard(src, front, back))
		m.refreshAfter()

	case "tag", "untag":
		tags := strings.Fields(args)
		if len(tags) == 0 {
			return need("/" + found[0].name + " nome-da-tag")
		}
		m.closePalette()
		if found[0].name == "tag" {
			m.result(act.Tag(m.pageID(), tags, nil))
		} else {
			m.result(act.Tag(m.pageID(), nil, tags))
		}

	case "ligar":
		if args == "" {
			return need("/ligar palavras da página ou do cartão")
		}
		if !hasSrc {
			return need("foco livre: sem tarefa para ligar")
		}
		results, err := act.Search(src.ID, args)
		if err != nil {
			m.closePalette()
			m.result("", err)
			return m, nil
		}
		link := func(f *Foco, c Choice) (string, error) {
			status, err := act.Link(src, c)
			if err == nil && c.Kind == "page" && !f.writing { // página ligada à tarefa: entra no Ctrl+O
				f.cfg.Pages = append(f.cfg.Pages, LinkedPage{ID: c.ID, Title: strings.TrimPrefix(c.Label, "página ")})
			}
			return status, err
		}
		switch len(results) {
		case 0:
			return need(fmt.Sprintf("nada encontrado para %q", args))
		case 1:
			m.closePalette()
			m.result(link(&m, results[0]))
			m.refreshAfter()
		default:
			m.choose("Ligar a qual?", results, link)
		}

	case "distracao":
		if args == "" {
			return need("/distracao o que passou pela cabeça")
		}
		m.closePalette()
		if m.cfg.Standalone { // sem sessão de foco, o pensamento solto vai para a caixa
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
		m.status = "✔ distração anotada — triagem depois; de volta ao trabalho"

	case "salvar":
		m.closePalette()
		m.saveWrite(false)

	case "sair":
		m.closePalette()
		m.leaveWrite()
	}
	return m, nil
}

// filterChoices: as que têm todas as palavras de query no rótulo.
func filterChoices(choices []Choice, query string) []Choice {
	words := strings.Fields(foldASCII(query))
	if len(words) == 0 {
		return choices
	}
	var out []Choice
	for _, c := range choices {
		if containsWords(foldASCII(c.Label), words) {
			out = append(out, c)
		}
	}
	return out
}

func containsWords(hay string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

func (m *Foco) choose(title string, options []Choice, pick func(*Foco, Choice) (string, error)) {
	m.palette.choose = &chooser{title: title, options: options, pick: pick}
	m.resizeEditor()
}

func (m Foco) updateChooser(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	c := m.palette.choose
	switch msg.Type {
	case tea.KeyEsc:
		m.closePalette()
		return m, nil
	case tea.KeyCtrlC:
		return m.interrupt(msg)
	}
	if k := msg.String(); len(k) == 1 && k >= "1" && k <= "9" {
		if i := int(k[0] - '1'); i < len(c.options) {
			m.closePalette()
			status, err := c.pick(&m, c.options[i])
			if status != "" || err != nil {
				m.result(status, err)
			}
			m.refreshAfter()
			if m.writing && m.write != nil {
				return m, m.write.editor.Focus()
			}
		}
	}
	return m, nil
}

// paletteView são as linhas dos comandos abertos — as mesmas na escrita e no
// relógio grande.
func (m Foco) paletteView() []string {
	if m.palette == nil {
		return nil
	}
	if c := m.palette.choose; c != nil {
		lines := []string{" " + c.title}
		for i, opt := range c.options {
			if i == 9 {
				break
			}
			lines = append(lines, fmt.Sprintf("   [%d] %s", i+1, opt.Label))
		}
		return append(lines, " "+dim.Render("número escolhe · Esc cancela"))
	}
	lines := []string{m.palette.input.View()}
	for i, c := range matchCommands(m.palette.input.Value(), m.writing) {
		if i == 6 {
			break
		}
		marker := "   "
		if i == 0 {
			marker = " ▸ " // o que o Enter executa
		}
		lines = append(lines, marker+blueText.Render("/"+c.name)+"  "+dim.Render(c.help(m.writing)))
	}
	return lines
}

// --- tela ---

func (m Foco) writeView() string {
	w := m.write
	width := m.width
	if width <= 0 {
		width = 80
	}
	// O relógio vem primeiro, para nenhum título comprido empurrá-lo para fora.
	title := "● FOCO ⏱ " + m.timer.clock()
	if m.cfg.TaskTitle != "" {
		title += " · " + m.cfg.TaskTitle
	}
	head, right := redTitle.Render(title), "página "+w.page.Title
	if m.cfg.Standalone {
		head, right = blueTitle.Render("✎ "+w.page.Title), "sem sessão de foco"
	}
	if gap := width - 1 - lipgloss.Width(head) - lipgloss.Width(right); gap >= 2 {
		head += strings.Repeat(" ", gap) + dim.Render(right)
	}
	rule := dim.Render(strings.Repeat("─", width-1))

	lines := []string{head, rule}
	lines = append(lines, strings.Split(w.editor.View(), "\n")...)
	lines = append(lines, rule)

	refs := "⚡ " + strings.Join(w.refs, " · ")
	if len(w.refs) == 0 {
		refs = "⚡ nada ligado à página — /ligar"
	}
	status := m.status
	if status == "" && m.dirty() {
		status = "● não salvo"
	}
	foot := " " + dim.Render(refs)
	if status != "" {
		// o status é o que acabou de acontecer: vem antes das referências
		foot = " " + dim.Render(status) + "   " + dim.Render(refs)
	}
	lines = append(lines, foot)

	switch {
	case m.phase == focoCheckin:
		lines = append(lines, " 🔔 "+m.checkinQuestion()+" "+dim.Render("[s] sim · [n] distraí · Esc pula"))
	case m.palette != nil:
		lines = append(lines, m.paletteView()...)
	case m.cfg.Standalone:
		lines = append(lines, " "+dim.Render("Ctrl+S salva · / comandos · Ctrl+Z desfaz · Ctrl+E vim · Esc sai"))
	default:
		lines = append(lines, " "+dim.Render("Ctrl+S salva · / comandos · Ctrl+Z desfaz · Esc relógio · Ctrl+E vim · Ctrl+P pausa"))
	}

	// Cada linha cabe na largura (sobra uma coluna para emoji largo), e a tela
	// NÃO termina em "\n": o bubbletea conta a linha vazia do fim e, passando
	// da altura, descarta as de CIMA — a do relógio.
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width-1, "…")
	}
	return strings.Join(lines, "\n")
}
