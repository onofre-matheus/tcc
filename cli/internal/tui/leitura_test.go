// Modo de leitura: o livro na tela, um parágrafo sob o cursor, e os comandos
// "/" que anotam sem sair da leitura. O acervo é um fake que guarda as
// chamadas — o TUI só desenha e chama.
package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type annotateCall struct {
	at                BookLoc
	page, quote, text string
}

type fakeRead struct {
	text      BookText
	annotated []annotateCall
	edited    [][2]string
	deleted   []string
	progress  []BookLoc
	pages     []string
	percents  []int
	sessions  []string
	cards     [][3]string // fonte, frente, verso
	notes     []string
}

func (f *fakeRead) Load(string) (BookText, error) { return f.text, nil }
func (f *fakeRead) Annotate(_ string, at BookLoc, page, quote, text string) (BookAnnotation, string, error) {
	f.annotated = append(f.annotated, annotateCall{at, page, quote, text})
	id := fmt.Sprintf("an-%d", len(f.annotated))
	return BookAnnotation{ID: id, At: at, Page: page, Quote: quote, Text: text}, "✎ anotado", nil
}
func (f *fakeRead) EditAnnotation(id, text string) (string, error) {
	f.edited = append(f.edited, [2]string{id, text})
	return "✎ editada", nil
}
func (f *fakeRead) DeleteAnnotation(id string) (string, error) {
	f.deleted = append(f.deleted, id)
	return "✗ apagada", nil
}
func (f *fakeRead) Progress(_ string, at BookLoc, page string, percent int, session string) error {
	f.progress, f.pages, f.percents = append(f.progress, at), append(f.pages, page), append(f.percents, percent)
	f.sessions = append(f.sessions, session)
	return nil
}
func (f *fakeRead) CreateCard(_ string, source, front, back string) (string, error) {
	f.cards = append(f.cards, [3]string{source, front, back})
	return "✔ cartão criado", nil
}
func (f *fakeRead) CaptureNote(text string) (string, error) {
	f.notes = append(f.notes, text)
	return "✔ nota na caixa de entrada", nil
}

// Um livro de três capítulos; o spine começa em 2 (a capa saiu) e pula o 4
// (item não linear) — a posição gravada é a do spine, não o índice.
func sampleBook() BookText {
	long := strings.Repeat("Um parágrafo comprido que ocupa muitas linhas na tela estreita. ", 30)
	return BookText{
		Title: "Mastering Your Adult ADHD", Author: "Safren",
		Chapters: []BookChapter{
			{Spine: 2, Title: "Psicoeducação", Blocks: []BookBlock{
				{Kind: blockHeading, Text: "Psicoeducação"},
				{Kind: blockPara, Text: "We discuss this upfront with clients.", Page: "9"},
				{Kind: blockPara, Text: "CBT is like a ladder. If you're only used to a shovel, the ladder feels strange.", Page: "9"},
				{Kind: blockQuote, Text: "Uma citação de Linehan.", Page: "10"},
			}},
			{Spine: 3, Title: "Organização", Blocks: []BookBlock{
				{Kind: blockHeading, Text: "Organização"},
				{Kind: blockItem, Text: "Calendário único", Page: "12"},
				{Kind: blockPara, Text: long, Page: "12"},
				{Kind: blockPara, Text: "Depois do comprido.", Page: "13"},
			}},
			{Spine: 5, Title: "Distratibilidade", Blocks: []BookBlock{
				{Kind: blockPara, Text: "Adiar a distração: anotar e voltar.", Page: "20"},
			}},
		},
	}
}

func newLeitura(fr *fakeRead, w, h int) Foco {
	m := NewFoco(FocoConfig{
		Standalone: true, Read: fr, Books: []LinkedBook{{ID: "b1", Title: "?"}}, StartReading: true,
		Emit: func(string, map[string]any) error { return nil },
	})
	m.Init()
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(Foco)
}

func hit(m Foco, s string) Foco {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	return next.(Foco)
}

func command(m Foco, line string) Foco {
	m = typeText(m, "/"+line)
	return press(m, tea.KeyEnter)
}

func TestLeituraAbreNoOndeParei(t *testing.T) {
	fr := &fakeRead{text: sampleBook()}
	fr.text.Start = BookLoc{Chapter: 3, Para: 2}
	m := newLeitura(fr, 100, 30)
	if !m.reading || m.read.ch != 1 || m.read.para != 1 {
		t.Fatalf("abre no capítulo do spine 3, parágrafo 2: ch=%d para=%d", m.read.ch, m.read.para)
	}
	view := m.View()
	for _, want := range []string{"📖 Mastering Your Adult ADHD — Safren", "cap. 2/3", "p. 12", "Organização", "• Calendário único"} {
		if !strings.Contains(view, want) {
			t.Fatalf("a tela deveria ter %q:\n%s", want, view)
		}
	}
	// sem mexer, sair não grava progresso
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Foco)
	if cmd == nil || len(fr.progress) != 0 {
		t.Fatalf("Esc sai sem gravar o mesmo lugar: %v", fr.progress)
	}
	if !strings.Contains(m.Status(), "parou em cap. 2/3") {
		t.Fatalf("o resumo diz onde parou: %q", m.Status())
	}
}

func TestLeituraNavegaEGravaOndeParei(t *testing.T) {
	fr := &fakeRead{text: sampleBook()}
	m := newLeitura(fr, 100, 30)
	m = press(m, tea.KeyDown)
	m = hit(m, "j")
	if m.read.ch != 0 || m.read.para != 2 {
		t.Fatalf("↓ e j andam por parágrafo: %d/%d", m.read.ch, m.read.para)
	}
	m = press(m, tea.KeyDown)
	m = press(m, tea.KeyDown) // passa do fim do capítulo
	if m.read.ch != 1 || m.read.para != 0 {
		t.Fatalf("depois do último parágrafo vem o próximo capítulo: %d/%d", m.read.ch, m.read.para)
	}
	m = press(m, tea.KeyUp)
	if m.read.ch != 0 || m.read.para != 3 {
		t.Fatalf("↑ no começo volta ao fim do anterior: %d/%d", m.read.ch, m.read.para)
	}
	m = press(m, tea.KeyRight)
	m = press(m, tea.KeyRight)
	if m.read.ch != 2 {
		t.Fatalf("→ avança capítulo: %d", m.read.ch)
	}
	m = press(m, tea.KeyRight)
	if m.read.ch != 2 || !strings.Contains(m.View(), "último capítulo") {
		t.Fatal("→ no último capítulo avisa e fica")
	}
	m = hit(m, "q")
	if len(fr.progress) != 1 || fr.progress[0] != (BookLoc{Chapter: 5, Para: 1}) || fr.pages[0] != "20" || fr.percents[0] != 88 {
		t.Fatalf("grava o spine, o parágrafo, a página e o %%: %v %v %v", fr.progress, fr.pages, fr.percents)
	}
}

// Parágrafo maior que a tela: ↓ rola por linha até o fim dele, e ↑ volta.
func TestLeituraParagrafoComprido(t *testing.T) {
	fr := &fakeRead{text: sampleBook()}
	fr.text.Start = BookLoc{Chapter: 3, Para: 3}
	m := newLeitura(fr, 60, 14)
	top := m.read.top
	m = press(m, tea.KeyDown)
	if m.read.para != 2 || m.read.top != top+1 {
		t.Fatalf("rola uma linha dentro do parágrafo: para=%d top=%d→%d", m.read.para, top, m.read.top)
	}
	for i := 0; i < 200 && m.read.para == 2; i++ {
		m = press(m, tea.KeyDown)
	}
	if m.read.para != 3 {
		t.Fatalf("no fim do parágrafo, passa ao próximo: %d", m.read.para)
	}
	m = press(m, tea.KeyUp)
	if m.read.para != 2 || m.read.last[2] != m.read.top+m.bodyHeight()-1 {
		t.Fatalf("subindo para o comprido, mostra o fim dele: top=%d last=%d", m.read.top, m.read.last[2])
	}
}

func TestLeituraNotaEGrifo(t *testing.T) {
	fr := &fakeRead{text: sampleBook()}
	m := newLeitura(fr, 100, 30)
	m = press(m, tea.KeyDown)
	m = press(m, tea.KeyDown) // "CBT is like a ladder…"

	m = command(m, "nota metáfora da escada")
	c := fr.annotated[0]
	if c.at != (BookLoc{Chapter: 2, Para: 3}) || c.page != "9" || c.text != "metáfora da escada" ||
		c.quote != "CBT is like a ladder. If you're only used to a shovel, the ladder feels strange." {
		t.Fatalf("a nota cita o parágrafo inteiro: %+v", c)
	}
	view := m.View()
	if !strings.Contains(view, "✎") || !strings.Contains(view, "✎ metáfora da escada") {
		t.Fatalf("o parágrafo ganha a marca e o rodapé mostra a nota:\n%s", view)
	}

	// grifo de um trecho, digitado sem caixa: a citação sai com a grafia do livro
	m = command(m, "grifo like a LADDER")
	if c := fr.annotated[1]; c.quote != "like a ladder" || c.text != "" {
		t.Fatalf("grifo do trecho: %+v", c)
	}
	// nota sobre um trecho
	m = command(m, "nota the ladder feels | a escada estranha")
	if c := fr.annotated[2]; c.quote != "the ladder feels" || c.text != "a escada estranha" {
		t.Fatalf("nota do trecho: %+v", c)
	}
	// trecho que não está no parágrafo: aviso, nada gravado
	m = command(m, "grifo pá")
	if len(fr.annotated) != 3 || !strings.Contains(m.View(), "não está neste parágrafo") {
		t.Fatalf("trecho ausente não grava: %d", len(fr.annotated))
	}
	m = press(m, tea.KeyUp) // qualquer tecla tira o aviso; voltando, o rodapé lista as anotações
	m = press(m, tea.KeyDown)
	if !strings.Contains(m.View(), "✎ metáfora da escada · ▍ grifo: like a ladder · ✎ a escada estranha") {
		t.Fatalf("o rodapé lista as anotações do parágrafo:\n%s", m.View())
	}

	// /editar e /apagar com três anotações no parágrafo: escolhe pelo número
	m = command(m, "editar escada de Linehan")
	if !strings.Contains(m.View(), "Editar qual anotação?") {
		t.Fatalf("várias anotações: pergunta qual\n%s", m.View())
	}
	m = hit(m, "1")
	if len(fr.edited) != 1 || fr.edited[0] != [2]string{"an-1", "escada de Linehan"} {
		t.Fatalf("editou a primeira: %v", fr.edited)
	}
	m = command(m, "apagar")
	m = hit(m, "2")
	if len(fr.deleted) != 1 || fr.deleted[0] != "an-2" || len(m.read.here()) != 2 {
		t.Fatalf("apagou o grifo: %v (ficaram %d)", fr.deleted, len(m.read.here()))
	}

	m = hit(m, "q")
	if !strings.Contains(m.Status(), "3 anotações novas") {
		t.Fatalf("o resumo conta as anotações: %q", m.Status())
	}
}

// Ao abrir, a anotação reancora pelo trecho: o parágrafo mudou de lugar
// (outra edição); trecho sumido fica na posição gravada, marcado.
func TestLeituraReancoraPeloTrecho(t *testing.T) {
	fr := &fakeRead{text: sampleBook()}
	fr.text.Annotations = []BookAnnotation{
		{ID: "a1", At: BookLoc{Chapter: 2, Para: 2}, Quote: "CBT is like a ladder", Text: "mudou de parágrafo"},
		{ID: "a2", At: BookLoc{Chapter: 2, Para: 1}, Quote: "Adiar a distração", Text: "mudou de capítulo"},
		{ID: "a3", At: BookLoc{Chapter: 3, Para: 2}, Quote: "trecho que não existe mais", Text: "perdida"},
		{ID: "a4", At: BookLoc{Chapter: 9, Para: 1}, Quote: "Depois do comprido."},
	}
	m := newLeitura(fr, 100, 30)
	got := map[string][3]any{}
	for _, n := range m.read.notes {
		got[n.ID] = [3]any{n.ch, n.para, n.lost}
	}
	want := map[string][3]any{
		"a1": {0, 2, false},
		"a2": {2, 0, false},
		"a3": {1, 1, true},
		"a4": {1, 3, false},
	}
	for id, w := range want {
		if got[id] != w {
			t.Fatalf("%s: esperado %v, veio %v", id, w, got[id])
		}
	}
	m = hit(m, "n")
	m = hit(m, "n")
	if m.read.ch != 1 || m.read.para != 1 || !strings.Contains(m.View(), "trecho não encontrado nesta edição") {
		t.Fatalf("n anda até a anotação perdida e a marca:\n%s", m.View())
	}
	m = hit(m, "N")
	if m.read.ch != 0 || m.read.para != 2 {
		t.Fatalf("N volta: %d/%d", m.read.ch, m.read.para)
	}
}

func TestLeituraIrBuscarNotasECartao(t *testing.T) {
	fr := &fakeRead{text: sampleBook()}
	fr.text.Annotations = []BookAnnotation{
		{ID: "a1", At: BookLoc{Chapter: 5, Para: 1}, Quote: "Adiar a distração: anotar e voltar.", Text: "distractibility delay"},
	}
	m := newLeitura(fr, 100, 30)

	m = command(m, "ir 3")
	if m.read.ch != 2 {
		t.Fatalf("/ir 3 vai ao 3º capítulo: %d", m.read.ch)
	}
	m = command(m, "ir p12")
	if m.read.ch != 1 || m.read.para != 1 {
		t.Fatalf("/ir p12 vai à página impressa: %d/%d", m.read.ch, m.read.para)
	}
	m = command(m, "ir 0%")
	if m.read.ch != 0 || m.read.para != 0 {
		t.Fatalf("/ir 0%% vai ao começo: %d/%d", m.read.ch, m.read.para)
	}
	m = command(m, "ir organizacao")
	if m.read.ch != 1 {
		t.Fatalf("/ir pelo título do capítulo, sem acento: %d", m.read.ch)
	}
	m = command(m, "ir p99")
	if !strings.Contains(m.View(), "página 99 não encontrada") {
		t.Fatalf("página que não existe avisa:\n%s", m.View())
	}

	m = command(m, "buscar LADDER")
	if !strings.Contains(m.View(), "1 ocorrência") || m.read.ch != 0 || m.read.para != 2 {
		t.Fatalf("/buscar acha sem caixa e vai até lá: %d/%d", m.read.ch, m.read.para)
	}
	m = command(m, "buscar comprido")
	if !strings.Contains(m.View(), "2 ocorrências") {
		t.Fatalf("várias ocorrências viram lista:\n%s", m.View())
	}
	m = press(m, tea.KeyEsc)

	m = command(m, "notas escada")
	if !strings.Contains(m.View(), "nenhuma anotação com") {
		t.Fatal("/notas filtra")
	}
	m = command(m, "notas distractibility")
	m = hit(m, "1")
	if m.read.ch != 2 || m.read.para != 0 {
		t.Fatalf("/notas: o número vai até a anotação: %d/%d", m.read.ch, m.read.para)
	}

	m = command(m, "cartao O que é distractibility delay? | anotar e voltar depois")
	if len(fr.cards) != 1 || fr.cards[0][0] != "Mastering Your Adult ADHD, cap. 3 (Distratibilidade), p. 20" {
		t.Fatalf("o cartão leva a fonte: %v", fr.cards)
	}
	m = command(m, "distracao ver o e-mail")
	if len(fr.notes) != 1 || fr.notes[0] != "ver o e-mail" {
		t.Fatalf("/distracao vai para a caixa de entrada: %v", fr.notes)
	}
	m = command(m, "tarefa nada")
	if !strings.Contains(m.View(), "comando desconhecido aqui: /tarefa") {
		t.Fatal("comandos da escrita não valem na leitura")
	}
	m = command(m, "sair")
	if len(fr.progress) != 1 || fr.progress[0] != (BookLoc{Chapter: 5, Para: 1}) || !strings.Contains(m.Status(), "parou em cap. 3/3") {
		t.Fatalf("/sair grava onde parou: %v %q", fr.progress, m.Status())
	}
}

// A tela de leitura cabe no terminal, com e sem os comandos abertos.
func TestLeituraCabeNoTerminal(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {100, 30}, {60, 20}, {40, 12}, {200, 50}} {
		for _, start := range []BookLoc{{}, {Chapter: 3, Para: 3}, {Chapter: 5, Para: 1}} {
			fr := &fakeRead{text: sampleBook()}
			fr.text.Start = start
			fr.text.Annotations = []BookAnnotation{{ID: "a", At: BookLoc{Chapter: 3, Para: 3}, Quote: "Um parágrafo comprido", Text: strings.Repeat("nota longa ", 20)}}
			m := newLeitura(fr, size[0], size[1])
			for _, palette := range []bool{false, true} {
				if palette {
					m = typeText(m, "/")
				}
				lines := strings.Split(m.View(), "\n")
				name := fmt.Sprintf("%dx%d início=%v paleta=%v", size[0], size[1], start, palette)
				if len(lines) > size[1] {
					t.Errorf("%s: %d linhas num terminal de %d", name, len(lines), size[1])
				}
				if !strings.Contains(lines[0], "📖") {
					t.Errorf("%s: a 1ª linha não tem o livro: %q", name, lines[0])
				}
				if !strings.Contains(m.View(), "▌") {
					t.Errorf("%s: o parágrafo atual sumiu da tela", name)
				}
				for i, l := range lines {
					if w := ansi.StringWidth(l); w > size[0]-1 {
						t.Errorf("%s: linha %d com %d colunas: %q", name, i, w, l)
						break
					}
				}
			}
		}
	}
}

// Teclas que chegam juntas (tecla segurada, digitação rápida): "jj" anda
// dois parágrafos; "/nota texto" num bloco só abre os comandos e escreve
// neles; nada depois do q vale.
func TestLeituraTeclasJuntas(t *testing.T) {
	fr := &fakeRead{text: sampleBook()}
	m := newLeitura(fr, 100, 30)
	burst := func(m Foco, s string) Foco {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
		return next.(Foco)
	}
	m = burst(m, "jj")
	if m.read.para != 2 {
		t.Fatalf("jj anda dois: %d", m.read.para)
	}
	m = burst(m, "/nota junto")
	m = press(m, tea.KeyEnter)
	if len(fr.annotated) != 1 || fr.annotated[0].text != "junto" {
		t.Fatalf("/nota num bloco só: %+v", fr.annotated)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x"), Paste: true})
	m = next.(Foco)
	m = burst(m, "jqj")
	if len(fr.progress) != 1 || fr.progress[0] != (BookLoc{Chapter: 2, Para: 4}) {
		t.Fatalf("o q encerra; o j depois dele não anda: %v", fr.progress)
	}
	m = burst(m, "j")
	if len(fr.progress) != 1 || m.read.para != 3 {
		t.Fatal("depois de encerrar, nada mais vale")
	}
}

func TestWrapSpansGuardaAsPosicoes(t *testing.T) {
	text := []rune("abc defgh ij\nklmnopqrstu v")
	got := wrapSpans(text, 5)
	var lines []string
	for _, sp := range got {
		lines = append(lines, string(text[sp[0]:sp[1]]))
	}
	want := []string{"abc", "defgh", "ij", "klmno", "pqrst", "u v"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("quebra: %q", lines)
	}
}
