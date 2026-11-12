// Páginas (catálogo 1.6): o editor abre só o corpo; título, tags e conexões
// são metadado (comando) e a página os mostra como referência numerada. O
// editor é trocado por uma função que reescreve o arquivo, como faria o
// usuário.
package cli_test

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/app"
)

type fakeEditor struct {
	got, path string // o que o editor recebeu e onde
	calls     int
}

func useEditor(a *app.App, edit func(string) string) *fakeEditor {
	fe := &fakeEditor{}
	a.Editor = func(path string) error {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fe.got, fe.path = string(raw), path
		fe.calls++
		return os.WriteFile(path, []byte(edit(string(raw))), 0o600)
	}
	return fe
}

func noEditor(t *testing.T, a *app.App) {
	a.Editor = func(string) error { t.Fatal("o editor não deveria abrir"); return nil }
}

func appendPage(t *testing.T, a *app.App, id, title, body string, tags ...string) core.Event {
	t.Helper()
	if tags == nil {
		tags = []string{}
	}
	e, err := a.Store.Append("page.created", map[string]any{"page_id": id, "title": title, "body": body, "tags": tags})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func countType(t *testing.T, a *app.App, eventType string) int {
	t.Helper()
	n := 0
	for _, typ := range eventTypes(t, a) {
		if typ == eventType {
			n++
		}
	}
	return n
}

func TestPCriaPaginaNoEditor(t *testing.T) {
	a, _ := newTestApp(t)
	fe := useEditor(a, func(string) string {
		return "# Portal\n\nAdicionar o **console** nas explicações.\n"
	})

	out := run(t, a, "p", "Portal reference", "--tag", "Trabalho")

	if fe.got != "" {
		t.Fatalf("o editor abre só com o corpo — vazio numa página nova: %q", fe.got)
	}
	if !strings.Contains(out, "✔ página criada: Portal reference  #trabalho") {
		t.Fatalf("confirma título e tags: %s", out)
	}
	p := lastPayload(t, a, "page.created")
	if p["title"] != "Portal reference" || p["body"] != "# Portal\n\nAdicionar o **console** nas explicações." {
		t.Fatalf("page.created leva título e corpo: %v", p)
	}
	if tags := p["tags"].([]any); len(tags) != 1 || tags[0] != "trabalho" {
		t.Fatalf("as tags vêm do --tag, normalizadas: %v", p["tags"])
	}
	if _, err := os.Stat(fe.path); !os.IsNotExist(err) {
		t.Fatalf("o temporário é apagado depois de gravar: %v", err)
	}

	out = run(t, a, "pagina", "1")
	if !strings.Contains(out, "Portal reference") || !strings.Contains(out, "console") {
		t.Fatalf("`pnn pagina 1` mostra a página recém-criada: %s", out)
	}
}

func TestPSemTituloNaoAbreOEditor(t *testing.T) {
	a, _ := newTestApp(t)
	noEditor(t, a)

	if err := runErr(t, a, "p", "   "); err == nil || !strings.Contains(err.Error(), "informe o título") {
		t.Fatalf("título vazio é recusado antes do editor: %v", err)
	}
	if err := runErr(t, a, "p"); err == nil {
		t.Fatal("sem título não há página")
	}
	if countType(t, a, "page.created") != 0 {
		t.Fatal("nada é gravado")
	}
}

func TestPaginasFixadasPrimeiroEFiltroPorTag(t *testing.T) {
	a, _ := newTestApp(t)
	appendPage(t, a, "p-a", "Alfa", "", "pessoal")
	appendPage(t, a, "p-b", "Beta", "", "trabalho/portal")

	run(t, a, "pagina", "alfa")
	if out := run(t, a, "fixar", "1"); !strings.Contains(out, "📌 fixada: Alfa") {
		t.Fatalf("fixar N fixa a página: %s", out)
	}
	out := run(t, a, "paginas")
	if !strings.Contains(out, " 1  📌 Alfa  #pessoal") || !strings.Contains(out, " 2  Beta  #trabalho/portal") {
		t.Fatalf("fixadas primeiro, com 📌: %s", out)
	}
	if out := run(t, a, "paginas", "--tag", "trabalho"); strings.Contains(out, "Alfa") || !strings.Contains(out, " 1  Beta") {
		t.Fatalf("--tag filtra pela tag e descendentes: %s", out)
	}
	run(t, a, "paginas")
	if out := run(t, a, "fixar", "1"); !strings.Contains(out, "desafixada: Alfa") {
		t.Fatalf("fixar de novo desafixa: %s", out)
	}
	if p := lastPayload(t, a, "page.edited"); p["pinned"] != false || len(p) != 2 {
		t.Fatalf("fixar grava só pinned: %v", p)
	}

	if out := run(t, a, "paginas", "--json"); !strings.Contains(out, `"order"`) {
		t.Fatalf("--json expõe a projeção: %s", out)
	}
}

func TestPaginaPeloTituloSemCaixaNemAcento(t *testing.T) {
	a, _ := newTestApp(t)
	appendPage(t, a, "p1", "Manutenção do cluster", "kubectl drain antes de tudo")
	appendPage(t, a, "p2", "Manutenção da casa", "trocar o filtro")
	appendPage(t, a, "p3", "Leituras 2026", "- O Mito de Sísifo")

	if out := run(t, a, "pagina", "MANUTENCAO", "cluster"); !strings.Contains(out, "Manutenção do cluster") || !strings.Contains(out, "kubectl drain") {
		t.Fatalf("acha pelo título sem caixa nem acento, com várias palavras: %s", out)
	}

	out := run(t, a, "pagina", "manutencao")
	if !strings.Contains(out, `2 páginas casam com "manutencao"`) || !strings.Contains(out, "Manutenção da casa") {
		t.Fatalf("várias candidatas viram uma lista numerada: %s", out)
	}
	if out := run(t, a, "pagina", "1"); !strings.Contains(out, "Manutenção") {
		t.Fatalf("a lista de candidatas vira a última tela: %s", out)
	}

	if err := runErr(t, a, "pagina", "inexistente"); err == nil || !strings.Contains(err.Error(), "pnn buscar") {
		t.Fatalf("sem candidata, sugere a busca no texto: %v", err)
	}
	if out := run(t, a, "pagina", "leituras", "--cru"); out != "- O Mito de Sísifo\n" {
		t.Fatalf("--cru imprime o markdown sem formatar: %q", out)
	}
}

func TestEditarPaginaGravaSoOCorpoComBase(t *testing.T) {
	a, _ := newTestApp(t)
	created := appendPage(t, a, "p1", "Notas", "linha 1", "tcc")
	run(t, a, "pagina", "notas")

	fe := useEditor(a, func(s string) string { return s + "linha 2\n" })
	if out := run(t, a, "editar", "1"); !strings.Contains(out, "✔ página salva: Notas") {
		t.Fatalf("editar N abre o editor: %s", out)
	}
	if fe.got != "linha 1\n" {
		t.Fatalf("o editor recebe só o corpo — sem título nem tags: %q", fe.got)
	}
	p := lastPayload(t, a, "page.edited")
	if p["body"] != "linha 1\nlinha 2" || p["base"] != created.ID || len(p) != 3 {
		t.Fatalf("page.edited leva só o corpo, com base = versão lida: %v", p)
	}

	useEditor(a, func(s string) string { return s })
	if out := run(t, a, "editar", "1"); !strings.Contains(out, "nada mudou: Notas") || countType(t, a, "page.edited") != 1 {
		t.Fatalf("sem mudança, nada é gravado: %s", out)
	}

	// o que parece cabeçalho é texto: título e tags não mudam pelo corpo
	useEditor(a, func(string) string { return "---\ntitulo: Outro\ntags: #x\n---\ncorpo\n" })
	run(t, a, "editar", "1")
	page := lastPayload(t, a, "page.edited")
	if page["body"] != "---\ntitulo: Outro\ntags: #x\n---\ncorpo" {
		t.Fatalf("o corpo é guardado como está: %v", page)
	}
	if out := run(t, a, "pagina", "1"); !strings.Contains(out, "Notas") || !strings.Contains(out, "#tcc") {
		t.Fatalf("título e tags são metadado — o texto não os muda: %s", out)
	}

	useEditor(a, func(string) string { return "" })
	if out := run(t, a, "editar", "1"); !strings.Contains(out, "ficou vazia") {
		t.Fatalf("esvaziar a página avisa que a versão anterior está no log: %s", out)
	}
}

func TestEditarPaginaTituloETagsPorComando(t *testing.T) {
	a, _ := newTestApp(t)
	appendPage(t, a, "p1", "Notas", "texto", "velha")
	noEditor(t, a)
	run(t, a, "pagina", "notas")

	if out := run(t, a, "editar", "1", "Notas novas"); !strings.Contains(out, "✔ página editada: Notas novas") {
		t.Fatalf("editar N \"título\" renomeia: %s", out)
	}
	if out := run(t, a, "editar", "1", "--tag", "nova", "--untag", "velha"); !strings.Contains(out, "#nova") {
		t.Fatalf("--tag/--untag editam as tags da página: %s", out)
	}
	if p := lastPayload(t, a, "page.edited"); p["tags"].([]any)[0] != "nova" || len(p["tags"].([]any)) != 1 {
		t.Fatalf("a lista final de tags vai inteira: %v", p)
	}
	if err := runErr(t, a, "editar", "1", "--frente", "x"); err == nil || !strings.Contains(err.Error(), "página se edita no editor") {
		t.Fatalf("--frente não vale para página: %v", err)
	}
}

func TestEditorComErroGuardaOTexto(t *testing.T) {
	a, _ := newTestApp(t)
	appendPage(t, a, "p1", "Notas", "texto")
	run(t, a, "pagina", "notas")

	var path string
	a.Editor = func(p string) error { path = p; return errors.New("saiu com :cq") }
	if err := runErr(t, a, "editar", "1"); err == nil || !strings.Contains(err.Error(), "o editor saiu com erro") || !strings.Contains(err.Error(), path) {
		t.Fatalf("editor com erro não grava e diz onde está o texto: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("o texto fica no temporário: %v", err)
	}
	os.Remove(path)
	if countType(t, a, "page.edited") != 0 {
		t.Fatal("nada foi gravado")
	}
}

func TestLigarPaginaEMostrarReferencias(t *testing.T) {
	a, _ := newTestApp(t)
	appendPage(t, a, "p1", "Documentação do portal", "texto", "trabalho")
	appendPage(t, a, "p2", "Diário", "")
	appendCard(t, a, map[string]any{"card_id": "c1", "front": "O que é um Pod?", "back": "a menor unidade", "tags": []string{}})

	run(t, a, "cartas") // 1 = c1
	if out := run(t, a, "ligar", "documentacao do portal", "1"); !strings.Contains(out, `⚡ sinapse: página Documentação do portal → "O que é um Pod?"`) {
		t.Fatalf("a página entra no ligar pelo título, sem caixa nem acento: %s", out)
	}
	if out := run(t, a, "ligar", "Documentação do portal", "#Kubernetes"); !strings.Contains(out, "🏷 tag adicionada: página Documentação do portal  #kubernetes") {
		t.Fatalf("ligar a página a uma tag classifica a página: %s", out)
	}
	run(t, a, "ligar", "diario", "Documentação do portal")

	out := run(t, a, "pagina", "portal")
	for _, want := range []string{"#trabalho #kubernetes", `⚡ 2 → "O que é um Pod?"  3 ← página Diário`} {
		if !strings.Contains(out, want) {
			t.Fatalf("faltou %q — tags na meta, sinapses numeradas:\n%s", want, out)
		}
	}
	if strings.Contains(out, "→ #kubernetes") {
		t.Fatalf("a tag não aparece como sinapse: %s", out)
	}
	if out := run(t, a, "pagina", "3"); !strings.Contains(out, "Diário") || !strings.Contains(out, "⚡ 2 → página Documentação do portal") {
		t.Fatalf("o número da referência abre a página ligada: %s", out)
	}

	run(t, a, "pagina", "portal")
	if out := run(t, a, "desligar", "1", "2"); !strings.Contains(out, `✂ sinapse desfeita: página Documentação do portal ↔ "O que é um Pod?"`) {
		t.Fatalf("desligar usa os números da página: %s", out)
	}
	if out := run(t, a, "paginas"); !strings.Contains(out, "Documentação do portal  #trabalho #kubernetes · editada hoje · ⚡1") {
		t.Fatalf("a lista mostra quantas sinapses a página tem: %s", out)
	}
	if out := run(t, a, "sinapses"); !strings.Contains(out, "página Documentação do portal") {
		t.Fatalf("sinapses rotula a página: %s", out)
	}

	appendCard(t, a, map[string]any{"card_id": "c2", "front": "Pods", "back": "b", "tags": []string{"kubernetes"}})
	run(t, a, "pagina", "portal")
	// c2 entra pela tag da página (ao redor da página = a vizinhança dela + a
	// de cada tag dela); c1 foi desligado. (Criado com o relógio real, c2
	// ainda não venceu no "hoje" do teste — conta como treino.)
	if out := run(t, a, "sessao", "Portal", "--ao-redor", "1"); !strings.Contains(out, "0 vencidos · 1 para treinar") || !strings.Contains(out, "ao redor de página Documentação do portal") {
		t.Fatalf("estudar ao redor da página reúne os cartões da tag ligada: %s", out)
	}

	appendPage(t, a, "p3", "Diário", "")
	if err := runErr(t, a, "ligar", "diario", "#x"); err == nil || !strings.Contains(err.Error(), "2 páginas se chamam") {
		t.Fatalf("título repetido é ambíguo: %v", err)
	}
	if !slices.Contains(eventTypes(t, a), "link.removed") {
		t.Fatal("link.removed gravado")
	}
}

func TestBuscarPaginasECartoes(t *testing.T) {
	a, _ := newTestApp(t)
	appendPage(t, a, "p1", "Diário", "segunda\nhoje estudei Kubernetes e redes")
	appendPage(t, a, "p2", "Kubernetes na prática", "pods, deployments")
	appendCard(t, a, map[string]any{"card_id": "c1", "front": "O que é um Pod no Kubernetes?", "back": "A menor unidade do kubernetes", "tags": []string{}})

	out := run(t, a, "buscar", "KUBERNETES")
	for _, want := range []string{" Páginas", " 1  Kubernetes na prática", " 2  Diário", "hoje estudei Kubernetes e redes", " Cartões", " 3  O que é um Pod no Kubernetes?"} {
		if !strings.Contains(out, want) {
			t.Fatalf("faltou %q na busca (título primeiro, trecho do texto, cartões):\n%s", want, out)
		}
	}
	if strings.Contains(out, "segunda") {
		t.Fatalf("o trecho é a linha que casa, não a primeira: %s", out)
	}
	if out := run(t, a, "ligar", "2", "3"); !strings.Contains(out, "⚡ sinapse: página Diário →") {
		t.Fatalf("a busca põe página e cartão na mesma tela, para ligar: %s", out)
	}

	if out := run(t, a, "buscar", "kubernetes", "redes"); !strings.Contains(out, "Diário") || strings.Contains(out, "na prática") || strings.Contains(out, "Cartões") {
		t.Fatalf("várias palavras: todas precisam aparecer: %s", out)
	}
	if out := run(t, a, "buscar", "inexistente"); !strings.Contains(out, "nada encontrado") {
		t.Fatalf("busca vazia avisa: %s", out)
	}
}

func TestArquivarPaginaEApagarRecusa(t *testing.T) {
	a, _ := newTestApp(t)
	appendPage(t, a, "p1", "Velha", "texto")
	run(t, a, "pagina", "velha")

	if err := runErr(t, a, "apagar", "1"); err == nil || !strings.Contains(err.Error(), "pnn arquivar 1") {
		t.Fatalf("página não se apaga: %v", err)
	}
	if out := run(t, a, "arquivar", "1"); !strings.Contains(out, "✔ página arquivada: Velha") {
		t.Fatalf("arquivar N arquiva a página: %s", out)
	}
	if out := run(t, a, "paginas"); !strings.Contains(out, "nenhuma página") {
		t.Fatalf("arquivada sai da lista: %s", out)
	}
	if out := run(t, a, "paginas", "--arquivadas"); !strings.Contains(out, " 1  Velha  arquivada") {
		t.Fatalf("--arquivadas mostra: %s", out)
	}
}

func TestFixarSoPaginas(t *testing.T) {
	a, _ := newTestApp(t)
	seedTasks(t, a)
	run(t, a, "dia")
	if err := runErr(t, a, "fixar", "1"); err == nil || !strings.Contains(err.Error(), "só páginas") {
		t.Fatalf("fixar recusa o que não é página: %v", err)
	}
}

func TestLigarTarefaAPaginaEMostrarNoDia(t *testing.T) {
	a, _ := newTestApp(t)
	appendPage(t, a, "p1", "docs cli", "contexto da tarefa")
	run(t, a, "t", "Terminar a documentação do portal")
	run(t, a, "t", "Rascunho descartável")
	run(t, a, "dia") // 1 = Terminar…, 2 = Rascunho…

	if out := run(t, a, "ligar", "docs cli", "1", "2"); !strings.Contains(out, "⚡ sinapse: página docs cli → tarefa Terminar a documentação do portal") {
		t.Fatalf("a página se liga à tarefa do `pnn dia`: %s", out)
	}
	if out := run(t, a, "dia"); !strings.Contains(out, "Terminar a documentação do portal  ⚡1") {
		t.Fatalf("o dia mostra as sinapses da tarefa: %s", out)
	}
	if err := runErr(t, a, "ligar", "1", "#portal"); err == nil || !strings.Contains(err.Error(), "tarefa não tem tags") {
		t.Fatalf("tarefa não se classifica por tag: %v", err)
	}

	out := run(t, a, "pagina", "docs")
	// a ordem é a canônica dos ids (UUIDv7 do mesmo milissegundo: qualquer uma)
	for _, want := range []string{"⚡ 2 → tarefa ", "→ tarefa Terminar a documentação do portal", "→ tarefa Rascunho descartável"} {
		if !strings.Contains(out, want) {
			t.Fatalf("faltou %q — a página mostra as tarefas ligadas: %s", want, out)
		}
	}

	run(t, a, "dia")
	run(t, a, "feito", "1")
	run(t, a, "dia") // depois do feito, o 1 do dia é o Rascunho
	run(t, a, "apagar", "1")
	out = run(t, a, "pagina", "docs")
	if !strings.Contains(out, "⚡ 2 → tarefa Terminar a documentação do portal ✔") || strings.Contains(out, "Rascunho") {
		t.Fatalf("concluída mantém a sinapse (✔); apagada some: %s", out)
	}
}

// linhas numeradas de uma listagem (" 3  Título …" ou indentadas no --por-tag)
func numberedLines(out string) []string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) > 0 && strings.Trim(f[0], "0123456789") == "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestPaginasListaCurtaPorPadrao(t *testing.T) {
	a, _ := newTestApp(t)
	for i := range 20 {
		appendPage(t, a, fmt.Sprintf("p%02d", i), fmt.Sprintf("Página %02d", i), "")
	}
	run(t, a, "pagina", "pagina 07")
	run(t, a, "fixar", "1")

	out := run(t, a, "paginas")
	lines := numberedLines(out)
	if len(lines) != 16 || !strings.Contains(lines[0], "📌 Página 07") {
		t.Fatalf("a fixada e as 15 mais recentes (%d linhas):\n%s", len(lines), out)
	}
	if !strings.Contains(out, "… e mais 4 — `pnn paginas --todas`") {
		t.Fatalf("o rodapé diz quantas ficaram de fora e como chegar nelas:\n%s", out)
	}
	out = run(t, a, "paginas", "--todas")
	if len(numberedLines(out)) != 20 || strings.Contains(out, "e mais") {
		t.Fatalf("--todas lista todas, sem rodapé:\n%s", out)
	}
}

func TestPaginasPorTag(t *testing.T) {
	a, _ := newTestApp(t)
	appendPage(t, a, "a", "Portal", "", "trabalho/portal")
	appendPage(t, a, "b", "Portal e TCC", "", "trabalho/portal", "tcc")
	appendPage(t, a, "c", "Cronograma", "", "tcc")
	appendPage(t, a, "d", "Diário", "")
	for i := range 7 {
		appendPage(t, a, fmt.Sprintf("l%d", i), fmt.Sprintf("Livro %d", i), "", "pessoal")
	}

	out := run(t, a, "paginas", "--por-tag")
	for _, want := range []string{" #pessoal  7 páginas", "… e mais 2 — `pnn paginas --tag pessoal`", " #tcc  2 páginas", " #trabalho  2 páginas", "   portal  2 páginas", " (sem tag)  1 página"} {
		if !strings.Contains(out, want) {
			t.Fatalf("faltou %q na árvore de páginas:\n%s", want, out)
		}
	}
	// a página com duas tags aparece nas duas, com o mesmo número
	var nums []string
	for _, line := range numberedLines(out) {
		if strings.Contains(line, "Portal e TCC") {
			nums = append(nums, strings.Fields(line)[0])
		}
	}
	if len(nums) != 2 || nums[0] != nums[1] {
		t.Fatalf("Portal e TCC sob #tcc e #trabalho/portal, mesmo número: %v\n%s", nums, out)
	}
	if out := run(t, a, "pagina", nums[0]); !strings.Contains(out, "Portal e TCC") {
		t.Fatalf("os números do --por-tag valem para os outros comandos: %s", out)
	}

	out = run(t, a, "paginas", "--por-tag", "--tag", "trabalho")
	if strings.Contains(out, "#tcc") || strings.Contains(out, "(sem tag)") || !strings.Contains(out, "   portal  2 páginas") {
		t.Fatalf("--tag recorta a árvore na subárvore filtrada:\n%s", out)
	}
	if out := run(t, a, "paginas", "--por-tag", "--todas"); strings.Contains(out, "e mais") {
		t.Fatalf("--todas tira o limite por tag:\n%s", out)
	}
}
