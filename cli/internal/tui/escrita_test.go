// Modo de escrita do foco: a página num editor dentro da tela, com o relógio
// vivo, e os comandos "/" que registram sem sair do foco. O acervo é um fake
// que guarda as chamadas — o TUI só desenha e chama.
package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type fakeWrite struct {
	body, version string
	saves         []string // corpos gravados
	bases         []string
	loads         []string
	tasks         []Choice
	created       []string
	parents       []string // parentID de cada CreateTask
	taskPages     []string // pageID de cada CreateTask
	tasksFocus    []string // focusID de cada Tasks
	completed     []string
	tagged        [][2][]string
	linked        []string
	linkedFrom    []string
	refs          []string
	search        []Choice
	cards         [][2]string
	cardsFrom     []string
	notes         []string
	pagesCreated  [][2]string // taskID, título
}

func (f *fakeWrite) Load(id string) (PageText, error) {
	f.loads = append(f.loads, id)
	return PageText{Title: "", Body: f.body, Version: f.version}, nil
}
func (f *fakeWrite) Save(_ string, body, base string) (string, error) {
	f.saves, f.bases = append(f.saves, body), append(f.bases, base)
	f.body, f.version = body, "v"+string(rune('0'+len(f.saves)))
	return f.version, nil
}
func (f *fakeWrite) Refs(string) ([]string, error) { return f.refs, nil }
func (f *fakeWrite) CreatePage(taskID, title string) (LinkedPage, string, error) {
	f.pagesCreated = append(f.pagesCreated, [2]string{taskID, title})
	f.body, f.version = "", "v-nova"
	return LinkedPage{ID: "p-nova", Title: title}, "✔ página criada e ligada à tarefa: " + title, nil
}
func (f *fakeWrite) CreateTask(parentID, pageID, title string) (string, error) {
	f.created, f.parents, f.taskPages = append(f.created, title), append(f.parents, parentID), append(f.taskPages, pageID)
	f.refs = append(f.refs, "tarefa "+title)
	return "✔ tarefa criada e ligada: " + title, nil
}
func (f *fakeWrite) Tasks(_, focusID string) ([]Choice, error) {
	f.tasksFocus = append(f.tasksFocus, focusID)
	return f.tasks, nil
}
func (f *fakeWrite) CompleteTask(id string) (string, error) {
	f.completed = append(f.completed, id)
	return "✔ concluída: " + id, nil
}
func (f *fakeWrite) Tag(_ string, add, remove []string) (string, error) {
	f.tagged = append(f.tagged, [2][]string{add, remove})
	return "🏷 tags atualizadas", nil
}
func (f *fakeWrite) Search(string, string) ([]Choice, error) { return f.search, nil }
func (f *fakeWrite) Link(from, c Choice) (string, error) {
	f.linked, f.linkedFrom = append(f.linked, c.ID), append(f.linkedFrom, from.Kind+":"+from.ID)
	return "⚡ sinapse: " + c.Label, nil
}
func (f *fakeWrite) CreateCard(from Choice, front, back string) (string, error) {
	f.cards, f.cardsFrom = append(f.cards, [2]string{front, back}), append(f.cardsFrom, from.Kind+":"+from.ID)
	return "✔ cartão criado e ligado: " + front, nil
}
func (f *fakeWrite) CaptureNote(text string) (string, error) {
	f.notes = append(f.notes, text)
	return "✔ nota na caixa de entrada", nil
}

func newEscrita(events *[]recorded, emit func(string, map[string]any) error, fw *fakeWrite, pages ...LinkedPage) Foco {
	if len(pages) == 0 {
		pages = []LinkedPage{docsCli}
	}
	m := NewFoco(FocoConfig{
		SessionID: "s1", NewSessionID: func() string { return "s2" },
		TaskID: "t1", TaskTitle: "Terminar a documentação",
		Minutes: 25, PauseMinutes: 5, Emit: emit, Pages: pages,
		Now:   func() time.Time { return time.Date(2026, 11, 10, 9, 30, 0, 0, time.UTC) },
		Write: fw, StartWriting: true,
	})
	m.Init()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return next.(Foco)
}

func typeText(m Foco, s string) Foco {
	for _, r := range s {
		var msg tea.KeyMsg
		switch r {
		case '\n':
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		}
		next, _ := m.Update(msg)
		m = next.(Foco)
	}
	return m
}

func press(m Foco, k tea.KeyType) Foco {
	next, _ := m.Update(tea.KeyMsg{Type: k})
	return next.(Foco)
}

func TestEscritaAbreAPaginaComORelogioNaBarra(t *testing.T) {
	events, emit := recorder()
	fw := &fakeWrite{body: "# Portal\nprimeira linha", version: "v0", refs: []string{`"O que é um Pod?"`}}
	m := newEscrita(events, emit, fw)

	view := m.View()
	for _, want := range []string{"● FOCO ⏱ 25:00 · Terminar a documentação", "página docs cli", "primeira linha", `⚡ "O que é um Pod?"`, "/ comandos"} {
		if !strings.Contains(view, want) {
			t.Fatalf("faltou %q na tela de escrita:\n%s", want, view)
		}
	}
	next, _ := m.Update(tickMsg{})
	if !strings.Contains(next.(Foco).View(), "⏱ 24:59") {
		t.Fatal("o relógio segue correndo enquanto se escreve")
	}
}

func TestEscritaSalvaComCtrlSUsandoABase(t *testing.T) {
	events, emit := recorder()
	fw := &fakeWrite{body: "a", version: "v0"}
	m := newEscrita(events, emit, fw)

	m = press(m, tea.KeyCtrlEnd)
	m = typeText(m, "\nb")
	if !strings.Contains(m.View(), "● não salvo") {
		t.Fatalf("mudança pendente aparece:\n%s", m.View())
	}
	m = press(m, tea.KeyCtrlS)
	if len(fw.saves) != 1 || fw.saves[0] != "a\nb" || fw.bases[0] != "v0" {
		t.Fatalf("Ctrl+S grava o texto com a base lida: %v %v", fw.saves, fw.bases)
	}
	if !strings.Contains(m.View(), "✔ salvo 09:30") {
		t.Fatalf("o status mostra a gravação:\n%s", m.View())
	}
	m = typeText(m, "c")
	m = press(m, tea.KeyCtrlS)
	if len(fw.bases) != 2 || fw.bases[1] != "v1" {
		t.Fatalf("a próxima gravação usa a versão gravada como base: %v", fw.bases)
	}
	if m = press(m, tea.KeyCtrlS); len(fw.saves) != 2 {
		t.Fatal("sem mudança, Ctrl+S não grava")
	}
}

func TestEscritaBarraNoMeioDaLinhaEhTexto(t *testing.T) {
	events, emit := recorder()
	fw := &fakeWrite{body: "", version: "v0"}
	m := newEscrita(events, emit, fw)

	m = typeText(m, "e/ou http://x")
	if m.palette != nil || m.write.editor.Value() != "e/ou http://x" {
		t.Fatalf("colada a uma palavra, / é só uma barra: %q", m.write.editor.Value())
	}
}

// O caso do relato: a página abre com texto e o "/" precisa abrir os
// comandos sem exigir uma linha vazia — no fim do texto, depois de um espaço
// ou no começo da linha.
func TestBarraAbreComandosNoComecoOuDepoisDeEspaco(t *testing.T) {
	events, emit := recorder()
	fw := &fakeWrite{body: "# Portal\ntexto existente", version: "v0"}
	m := newEscrita(events, emit, fw)

	if m.write.editor.Line() != 1 {
		t.Fatalf("a página abre no fim do texto, onde se continua escrevendo: linha %d", m.write.editor.Line())
	}
	m = typeText(m, " /")
	if m.palette == nil {
		t.Fatalf("depois de um espaço, / abre os comandos: %q", m.write.editor.Value())
	}
	m = press(m, tea.KeyEsc)
	m = press(m, tea.KeyHome)
	m = typeText(m, "/")
	if m.palette == nil {
		t.Fatal("no começo de uma linha com texto, / abre os comandos")
	}
	m = typeText(m, "/") // "//" escreve a barra
	if m.palette != nil || !strings.HasPrefix(strings.Split(m.write.editor.Value(), "\n")[1], "/texto") {
		t.Fatalf("// escreve uma barra onde ela abriria os comandos: %q", m.write.editor.Value())
	}
}

func TestComandoTarefaCriaSemSujarOTexto(t *testing.T) {
	events, emit := recorder()
	fw := &fakeWrite{body: "", version: "v0"}
	m := newEscrita(events, emit, fw)

	m = typeText(m, "/")
	if m.palette == nil || !strings.Contains(m.View(), "/tarefa") {
		t.Fatalf("/ numa linha vazia abre os comandos:\n%s", m.View())
	}
	m = typeText(m, "tar Revisar o console")
	m = press(m, tea.KeyEnter)

	if len(fw.created) != 1 || fw.created[0] != "Revisar o console" {
		t.Fatalf("/tar (prefixo único) cria a tarefa: %v", fw.created)
	}
	if m.write.editor.Value() != "" || m.palette != nil {
		t.Fatalf("o comando não fica no texto: %q", m.write.editor.Value())
	}
	view := m.View()
	if !strings.Contains(view, "✔ tarefa criada e ligada: Revisar o console") || !strings.Contains(view, "⚡ tarefa Revisar o console") {
		t.Fatalf("o resultado e a sinapse nova aparecem na hora:\n%s", view)
	}
}

func TestComandoFeitoEscolheDaLista(t *testing.T) {
	events, emit := recorder()
	fw := &fakeWrite{tasks: []Choice{{Kind: "task", ID: "t1", Label: "Revisar o console ⚡"}, {Kind: "task", ID: "t2", Label: "Testar o mgc auth"}}}
	m := newEscrita(events, emit, fw)

	m = typeText(m, "/feito")
	m = press(m, tea.KeyEnter)
	if !strings.Contains(m.View(), "Concluir qual?") || !strings.Contains(m.View(), "[2] Testar o mgc auth") {
		t.Fatalf("/feito lista as tarefas abertas:\n%s", m.View())
	}
	m = typeText(m, "2")
	if len(fw.completed) != 1 || fw.completed[0] != "t2" || m.palette != nil {
		t.Fatalf("o número conclui a tarefa: %v", fw.completed)
	}

	m = typeText(m, "/feito console")
	m = press(m, tea.KeyEnter)
	if len(fw.completed) != 2 || fw.completed[1] != "t1" {
		t.Fatalf("/feito com texto que casa uma só conclui direto: %v", fw.completed)
	}
}

func TestComandosTagLigarEDistracao(t *testing.T) {
	events, emit := recorder()
	fw := &fakeWrite{search: []Choice{{Kind: "card", ID: "c1", Label: `"O que é um Pod?"`}, {Kind: "page", ID: "p2", Label: "página Kubernetes"}}}
	m := newEscrita(events, emit, fw)

	m = press(m, tea.KeyEnter) // garante linha vazia
	m = typeText(m, "/tag cli docs")
	m = press(m, tea.KeyEnter)
	m = typeText(m, "/untag docs")
	m = press(m, tea.KeyEnter)
	if len(fw.tagged) != 2 || fw.tagged[0][0][1] != "docs" || fw.tagged[1][1][0] != "docs" {
		t.Fatalf("/tag e /untag mexem nas tags da página: %v", fw.tagged)
	}

	m = typeText(m, "/ligar kube")
	m = press(m, tea.KeyEnter)
	m = typeText(m, "1")
	if len(fw.linked) != 1 || fw.linked[0] != "c1" {
		t.Fatalf("/ligar escolhe o resultado e liga: %v", fw.linked)
	}

	m = typeText(m, "/distração lembrar de pagar a conta")
	m = press(m, tea.KeyEnter)
	last := (*events)[len(*events)-1]
	if last.eventType != "distraction.captured" || last.payload["text"] != "lembrar de pagar a conta" || m.Distractions != 1 {
		t.Fatalf("/distração (com acento) anota sem sair do foco: %v", last)
	}
	if strings.Contains(m.write.editor.Value(), "/") {
		t.Fatalf("nenhum comando ficou no texto: %q", m.write.editor.Value())
	}
}

func TestComandoDesconhecidoEEscFecham(t *testing.T) {
	events, emit := recorder()
	m := newEscrita(events, emit, &fakeWrite{})

	m = typeText(m, "/xyz")
	m = press(m, tea.KeyEnter)
	if m.palette != nil || !strings.Contains(m.View(), "⚠ comando desconhecido aqui: /xyz") {
		t.Fatalf("comando desconhecido avisa:\n%s", m.View())
	}
	m = typeText(m, "/tar")
	m = press(m, tea.KeyEsc)
	if m.palette != nil || !m.writing {
		t.Fatal("Esc com os comandos abertos só fecha os comandos")
	}
}

func TestEscSalvaEVoltaAoRelogioGrandeECtrlOVolta(t *testing.T) {
	events, emit := recorder()
	fw := &fakeWrite{body: "x", version: "v0"}
	m := newEscrita(events, emit, fw)

	m = typeText(m, "y")
	m = press(m, tea.KeyEsc)
	if m.writing || len(fw.saves) != 1 {
		t.Fatalf("Esc grava e sai da escrita: %v", fw.saves)
	}
	if view := m.View(); !strings.Contains(view, "Distração?") || !strings.Contains(view, "Ctrl+O escreve") {
		t.Fatalf("volta ao relógio grande, com a dica do Ctrl+O:\n%s", view)
	}
	for _, e := range *events {
		if e.eventType == "session.ended" {
			t.Fatal("sair da escrita não encerra o bloco")
		}
	}
	m = press(m, tea.KeyCtrlO)
	if !m.writing || m.write.editor.Value() != "yx" && m.write.editor.Value() != "xy" {
		t.Fatalf("Ctrl+O volta à escrita com o texto: %q", m.write.editor.Value())
	}
}

func TestBlocoAcabaEscrevendoGravaAntesDaPausa(t *testing.T) {
	events, emit := recorder()
	fw := &fakeWrite{body: "", version: "v0"}
	m := newEscrita(events, emit, fw)

	m = typeText(m, "texto do bloco")
	m.timer.remaining = 1
	next, _ := m.Update(tickMsg{})
	m = next.(Foco)

	if len(fw.saves) != 1 || fw.saves[0] != "texto do bloco" {
		t.Fatalf("o texto é gravado antes da pausa: %v", fw.saves)
	}
	if (*events)[len(*events)-1].eventType != "pause.started" || !strings.Contains(m.View(), "05:00") {
		t.Fatalf("o bloco termina e a pausa começa:\n%s", m.View())
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(next.(Foco).View(), "⏱ 25:00") {
		t.Fatalf("o próximo bloco volta à escrita:\n%s", next.(Foco).View())
	}
}

func TestEscritaGravaSozinhaACada2Min(t *testing.T) {
	events, emit := recorder()
	fw := &fakeWrite{body: "", version: "v0"}
	m := newEscrita(events, emit, fw)

	m = typeText(m, "rascunho")
	for range autosaveEvery - 1 {
		next, _ := m.Update(tickMsg{})
		m = next.(Foco)
	}
	if len(fw.saves) != 0 {
		t.Fatal("antes dos 2 min, nada")
	}
	next, _ := m.Update(tickMsg{})
	if len(fw.saves) != 1 || !strings.Contains(next.(Foco).View(), "(automático)") {
		t.Fatalf("com mudança pendente, grava sozinha aos 2 min: %v", fw.saves)
	}
}

func TestCheckinNaEscritaNaoViraTexto(t *testing.T) {
	events, emit := recorder()
	fw := &fakeWrite{}
	m := newEscrita(events, emit, fw)
	m.cfg.CheckinEvery = 1
	for range 60 {
		next, _ := m.Update(tickMsg{})
		m = next.(Foco)
	}
	if !strings.Contains(m.View(), "🔔") || !strings.Contains(m.View(), "⏱") {
		t.Fatalf("o check-in aparece na própria tela de escrita:\n%s", m.View())
	}
	m = typeText(m, "s")
	if m.write.editor.Value() != "" || m.Checkins != 1 || !m.writing {
		t.Fatalf("o s responde o check-in, não vai para o texto: %q", m.write.editor.Value())
	}
}

func TestCtrlCNaEscritaGravaEEncerra(t *testing.T) {
	events, emit := recorder()
	fw := &fakeWrite{}
	m := newEscrita(events, emit, fw)
	m = typeText(m, "não perder isto")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if len(fw.saves) != 1 || !isQuit(t, cmd) {
		t.Fatalf("Ctrl+C grava antes de sair: %v", fw.saves)
	}
	if last := (*events)[len(*events)-1]; last.eventType != "session.ended" || last.payload["outcome"] != "interrupted" {
		t.Fatalf("e encerra a sessão como interrompida: %v", last)
	}
}

func TestVariasPaginasPerguntaOndeEscrever(t *testing.T) {
	events, emit := recorder()
	fw := &fakeWrite{body: "corpo"}
	m := newEscrita(events, emit, fw, docsCli, LinkedPage{ID: "p2", Title: "Kubernetes"})
	if m.writing || !strings.Contains(m.View(), "Em qual página escrever?") {
		t.Fatalf("com várias páginas, pergunta onde escrever:\n%s", m.View())
	}
	m = typeText(m, "2")
	if !m.writing || m.write.page.ID != "p2" {
		t.Fatal("o número abre a página escolhida na escrita")
	}
}

func TestPaginaLongaPassaDe99Linhas(t *testing.T) {
	events, emit := recorder()
	long := strings.Repeat("linha\n", 150) + "fim"
	m := newEscrita(events, emit, &fakeWrite{body: long})
	if got := m.write.editor.Value(); got != long {
		t.Fatalf("a página inteira cabe no editor (%d de %d linhas)", strings.Count(got, "\n")+1, 151)
	}
}

func TestDesfazPorPalavraERefaz(t *testing.T) {
	events, emit := recorder()
	m := newEscrita(events, emit, &fakeWrite{})

	m = typeText(m, "uma frase curta")
	m = press(m, tea.KeyCtrlZ)
	if got := m.write.editor.Value(); got != "uma frase " {
		t.Fatalf("Ctrl+Z desfaz a última palavra: %q", got)
	}
	m = press(m, tea.KeyCtrlZ)
	m = press(m, tea.KeyCtrlZ)
	if got := m.write.editor.Value(); got != "" {
		t.Fatalf("e volta palavra por palavra até o começo: %q", got)
	}
	if m = press(m, tea.KeyCtrlZ); !strings.Contains(m.View(), "nada a desfazer") {
		t.Fatal("sem mais o que desfazer, avisa")
	}
	m = press(m, tea.KeyCtrlY)
	m = press(m, tea.KeyCtrlY)
	if got := m.write.editor.Value(); got != "uma frase " {
		t.Fatalf("Ctrl+Y refaz: %q", got)
	}
	m = typeText(m, "x")
	if m = press(m, tea.KeyCtrlY); m.write.editor.Value() != "uma frase x" {
		t.Fatalf("editar de novo apaga o que havia para refazer: %q", m.write.editor.Value())
	}

	m = press(m, tea.KeyBackspace)
	m = press(m, tea.KeyBackspace)
	m = press(m, tea.KeyCtrlZ)
	if got := m.write.editor.Value(); got != "uma frase x" {
		t.Fatalf("uma sequência de apagar é um passo só: %q", got)
	}
}

func TestDesfazVoltaOCursorParaOLugar(t *testing.T) {
	events, emit := recorder()
	m := newEscrita(events, emit, &fakeWrite{body: "primeira\nsegunda\nterceira"})

	m = press(m, tea.KeyUp) // a página abre no fim; sobe para a linha 2 (segunda)
	m = press(m, tea.KeyHome)
	m = typeText(m, "XX ")
	m = press(m, tea.KeyCtrlZ)
	if got := m.write.editor.Value(); got != "primeira\nsegunda\nterceira" {
		t.Fatalf("o texto volta: %q", got)
	}
	if m.write.editor.Line() != 1 {
		t.Fatalf("o cursor volta para a linha da edição, não para o fim: linha %d", m.write.editor.Line())
	}
}

func TestComandoTarefasListaEConclui(t *testing.T) {
	events, emit := recorder()
	fw := &fakeWrite{tasks: []Choice{{ID: "t1", Label: "[A] Revisar o console ⚡"}, {ID: "t2", Label: "[C] Testar o mgc auth"}}}
	m := newEscrita(events, emit, fw)

	m = typeText(m, "/tarefas")
	m = press(m, tea.KeyEnter)
	view := m.View()
	if !strings.Contains(view, "Tarefas abertas") || !strings.Contains(view, "[1] [A] Revisar o console ⚡") {
		t.Fatalf("/tarefas mostra as abertas, com prioridade:\n%s", view)
	}
	m = typeText(m, "1")
	if len(fw.completed) != 1 || fw.completed[0] != "t1" {
		t.Fatalf("o número conclui: %v", fw.completed)
	}
}

func TestPrefixoAmbiguoExecutaOPrimeiro(t *testing.T) {
	events, emit := recorder()
	fw := &fakeWrite{}
	m := newEscrita(events, emit, fw)
	m = typeText(m, "/tar")
	if !strings.Contains(m.View(), "▸ /tarefa") {
		t.Fatalf("o primeiro que casa vem marcado:\n%s", m.View())
	}
	m = typeText(m, " Revisar")
	m = press(m, tea.KeyEnter)
	if len(fw.created) != 1 || fw.created[0] != "Revisar" {
		t.Fatalf("/tar (tarefa ou tarefas) executa o primeiro, /tarefa: %v", fw.created)
	}
}

func TestComandoCartao(t *testing.T) {
	events, emit := recorder()
	fw := &fakeWrite{}
	m := newEscrita(events, emit, fw)

	m = typeText(m, "/cartão O que é um Pod? | a menor unidade do kubernetes")
	m = press(m, tea.KeyEnter)
	if len(fw.cards) != 1 || fw.cards[0] != [2]string{"O que é um Pod?", "a menor unidade do kubernetes"} {
		t.Fatalf("/cartão frente | verso cria o cartão: %v", fw.cards)
	}
	m = typeText(m, "/cartao sem verso")
	m = press(m, tea.KeyEnter)
	if len(fw.cards) != 1 || !strings.Contains(m.View(), "/cartao frente | verso") {
		t.Fatalf("sem a barra, mostra o formato: %v", fw.cards)
	}
}

func TestEscreverForaDoFoco(t *testing.T) {
	events, emit := recorder()
	fw := &fakeWrite{body: "texto", version: "v0"}
	m := NewFoco(FocoConfig{
		Standalone: true, StartWriting: true, Write: fw, Emit: emit,
		Pages: []LinkedPage{docsCli},
		Now:   func() time.Time { return time.Date(2026, 11, 10, 9, 30, 0, 0, time.UTC) },
	})
	if cmd := m.Init(); cmd == nil || len(*events) != 0 {
		t.Fatalf("fora do foco, nenhuma sessão começa: %v", *events)
	}
	view := m.View()
	if !strings.Contains(view, "✎ docs cli") || strings.Contains(view, "⏱") || !strings.Contains(view, "Esc sai") {
		t.Fatalf("o cabeçalho é a página, sem relógio:\n%s", view)
	}

	m = press(m, tea.KeyEnter) // a página abre no fim do texto
	m = typeText(m, "/distracao ligar para o banco")
	m = press(m, tea.KeyEnter)
	if len(fw.notes) != 1 || len(*events) != 0 {
		t.Fatalf("sem sessão, /distracao vira nota na caixa de entrada: %v %v", fw.notes, *events)
	}

	m = typeText(m, "mais ")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !isQuit(t, cmd) || len(fw.saves) != 1 || len(*events) != 0 {
		t.Fatalf("Esc grava e sai, sem encerrar sessão nenhuma: %v %v", fw.saves, *events)
	}
}

// --- comandos no relógio grande (o ponto de referência é a tarefa) ---

func newRelogio(events *[]recorded, emit func(string, map[string]any) error, fw *fakeWrite, pages ...LinkedPage) Foco {
	m := NewFoco(FocoConfig{
		SessionID: "s1", NewSessionID: func() string { return "s2" },
		TaskID: "t1", TaskTitle: "Terminar a documentação",
		Minutes: 25, PauseMinutes: 5, Emit: emit, Pages: pages,
		Now:   func() time.Time { return time.Date(2026, 11, 10, 9, 30, 0, 0, time.UTC) },
		Write: fw, StartWriting: true,
	})
	m.Init()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return next.(Foco)
}

func TestBarraNoRelogioGrandeAbreOsComandosDaTarefa(t *testing.T) {
	events, emit := recorder()
	fw := &fakeWrite{}
	m := newRelogio(events, emit, fw)

	if m.writing || !strings.Contains(m.View(), "/ comandos") {
		t.Fatalf("sem página, abre no relógio grande com a dica dos comandos:\n%s", m.View())
	}
	m = typeText(m, "/")
	view := m.View()
	if m.palette == nil || !strings.Contains(view, "/pagina") || strings.Contains(view, "/tag ") {
		t.Fatalf("o / na linha de distração abre os comandos da tarefa (sem /tag, que é de página):\n%s", view)
	}
	m = typeText(m, "tarefa Separar em partes")
	m = press(m, tea.KeyEnter)
	if len(fw.created) != 1 || fw.parents[0] != "t1" || fw.taskPages[0] != "" {
		t.Fatalf("/tarefa no relógio cria uma subtarefa da em foco: %v %v %v", fw.created, fw.parents, fw.taskPages)
	}
	if m.input.Value() != "" {
		t.Fatalf("o comando não vira distração: %q", m.input.Value())
	}
}

func TestPaginaPeloRelogioCriaEComecaAEscrever(t *testing.T) {
	events, emit := recorder()
	fw := &fakeWrite{}
	m := newRelogio(events, emit, fw)

	m = typeText(m, "/pagina")
	m = press(m, tea.KeyEnter)
	if len(fw.pagesCreated) != 1 || fw.pagesCreated[0] != [2]string{"t1", "Terminar a documentação"} {
		t.Fatalf("/pagina sem título cria a página da tarefa, com o título dela: %v", fw.pagesCreated)
	}
	if !m.writing || m.write.page.ID != "p-nova" || !strings.Contains(m.View(), "página Terminar a documentação") {
		t.Fatalf("e já começa a escrever nela:\n%s", m.View())
	}
	m = typeText(m, "primeira ideia")
	m = press(m, tea.KeyEsc)
	if len(fw.saves) != 1 || fw.saves[0] != "primeira ideia" {
		t.Fatalf("o texto da página nova é gravado: %v", fw.saves)
	}
	if !strings.Contains(m.View(), "⚡ página Terminar a documentação") {
		t.Fatalf("a página nova entra nas ligadas, sob o relógio:\n%s", m.View())
	}
}

func TestCtrlOSemPaginaJaSugereAPagina(t *testing.T) {
	events, emit := recorder()
	fw := &fakeWrite{}
	m := newRelogio(events, emit, fw)

	m = press(m, tea.KeyCtrlO)
	if m.palette == nil || m.palette.input.Value() != "pagina Terminar a documentação" {
		t.Fatalf("Ctrl+O sem página abre os comandos com /pagina e o título da tarefa: %+v", m.palette)
	}
	m = press(m, tea.KeyEnter)
	if len(fw.pagesCreated) != 1 || !m.writing {
		t.Fatal("um Enter cria a página e começa a escrever")
	}
}

func TestPaginaNaEscritaTrocaDePagina(t *testing.T) {
	events, emit := recorder()
	fw := &fakeWrite{body: "a"}
	m := newEscrita(events, emit, fw, docsCli, LinkedPage{ID: "p2", Title: "Kubernetes"})
	m = typeText(m, "1") // começa por docs cli
	m = typeText(m, " mais")

	m = typeText(m, " /pagina")
	m = press(m, tea.KeyEnter)
	if !strings.Contains(m.View(), "Escrever em qual página?") || !strings.Contains(m.View(), "[1] página Kubernetes") {
		t.Fatalf("/pagina sem título troca entre as páginas ligadas:\n%s", m.View())
	}
	m = typeText(m, "1")
	if m.write.page.ID != "p2" || fw.loads[len(fw.loads)-1] != "p2" {
		t.Fatalf("a escolhida abre na escrita: %v", fw.loads)
	}
	if len(fw.saves) != 1 || strings.TrimSpace(fw.saves[0]) != "a mais" {
		t.Fatalf("a anterior é gravada antes de trocar: %v", fw.saves)
	}
}

func TestLigarECartaoNoRelogioPartemDaTarefa(t *testing.T) {
	events, emit := recorder()
	fw := &fakeWrite{search: []Choice{{Kind: "page", ID: "p9", Label: "página Kubernetes"}}}
	m := newRelogio(events, emit, fw)

	m = typeText(m, "/ligar kube")
	m = press(m, tea.KeyEnter)
	if len(fw.linkedFrom) != 1 || fw.linkedFrom[0] != "task:t1" {
		t.Fatalf("no relógio, /ligar liga a tarefa: %v", fw.linkedFrom)
	}
	if !strings.Contains(m.View(), "página Kubernetes") {
		t.Fatalf("a página ligada aparece sob o relógio:\n%s", m.View())
	}
	m = typeText(m, "/cartao frente | verso")
	m = press(m, tea.KeyEnter)
	if len(fw.cardsFrom) != 1 || fw.cardsFrom[0] != "task:t1" {
		t.Fatalf("no relógio, /cartao liga o cartão à tarefa: %v", fw.cardsFrom)
	}
}

func TestDuasBarrasNaLinhaDeDistracaoEscrevemABarra(t *testing.T) {
	events, emit := recorder()
	m := newRelogio(events, emit, &fakeWrite{})
	m = typeText(m, "//")
	if m.palette != nil || m.input.Value() != "/" {
		t.Fatalf("// escreve a barra na linha de distração: %q", m.input.Value())
	}
}

func TestFeitoPassaATarefaEmFoco(t *testing.T) {
	events, emit := recorder()
	fw := &fakeWrite{tasks: []Choice{{ID: "t1", Label: "[C] Terminar a documentação (em foco)"}}}
	m := newRelogio(events, emit, fw)
	m = typeText(m, "/feito")
	m = press(m, tea.KeyEnter)
	if len(fw.tasksFocus) != 1 || fw.tasksFocus[0] != "t1" || !strings.Contains(m.View(), "(em foco)") {
		t.Fatalf("/feito sabe qual é a tarefa em foco, que vem primeiro: %v", fw.tasksFocus)
	}
}

// No relógio grande, a dica dos comandos cabe em 80 colunas — o bubbletea
// corta o fim da linha, e o fim era justamente o "/ comandos".
func TestDicaDoRelogioCabeEm80Colunas(t *testing.T) {
	events, emit := recorder()
	m := newRelogio(events, emit, &fakeWrite{}, docsCli)
	m = press(m, tea.KeyEsc) // sai da escrita para o relógio grande
	for _, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, "Ctrl+P pausa") && ansi.StringWidth(line) > 79 {
			t.Fatalf("a dica passa de 80 colunas (%d): %q", ansi.StringWidth(line), line)
		}
	}
	if view := m.View(); !strings.Contains(view, "/ comandos") || !strings.Contains(view, "/ abre os comandos") {
		t.Fatalf("o / aparece na dica e no próprio campo:\n%s", view)
	}
}
