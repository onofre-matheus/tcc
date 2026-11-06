// Conexões explícitas e renomeação de tag (catálogo 1.4) nas telas one-shot:
// ligar, desligar, conexoes, editar N "novo-nome" numa tag e revisar --ao-redor.
package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/cli"
)

func lastPayload(t *testing.T, a *app.App, eventType string) map[string]any {
	t.Helper()
	events, err := a.Store.Events()
	if err != nil {
		t.Fatal(err)
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == eventType {
			var p map[string]any
			if err := json.Unmarshal(events[i].Payload, &p); err != nil {
				t.Fatal(err)
			}
			return p
		}
	}
	t.Fatalf("nenhum %s no log", eventType)
	return nil
}

func linksState(t *testing.T, a *app.App) core.LinksState {
	t.Helper()
	events, err := a.Store.Events()
	if err != nil {
		t.Fatal(err)
	}
	state, err := core.Links(events)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func runErr(t *testing.T, a *app.App, args ...string) error {
	t.Helper()
	root := cli.NewRoot(a)
	root.SetArgs(args)
	root.SetOut(a.Out)
	root.SetErr(a.Out)
	return root.Execute()
}

func TestLigarCartoesETagEMostraConexoes(t *testing.T) {
	a, _ := newTestApp(t)
	seedTags(t, a)
	run(t, a, "cartas") // SO: 1 processo · (sem deck): 2 deadlock, 3 OSI

	out := run(t, a, "ligar", "1", "2", "#Escalonamento", "--relacao", "prerequisito")
	if !strings.Contains(out, `🔗 ligado: "O que é um processo" → "O que é deadlock" (prerequisito)`) ||
		!strings.Contains(out, `→ #escalonamento (prerequisito)`) {
		t.Fatalf("ligar conecta o primeiro a cada um dos demais: %s", out)
	}
	p := lastPayload(t, a, "link.created")
	if p["relation"] != "prerequisito" || p["to"].(map[string]any)["id"] != "escalonamento" {
		t.Fatalf("link.created leva a relação e a tag normalizada: %v", p)
	}

	if out := run(t, a, "ligar", "2", "1", "--relacao", "prerequisito"); !strings.Contains(out, "já ligados") {
		t.Fatalf("o mesmo par com a mesma relação não duplica (em qualquer direção): %s", out)
	}
	// sem "#", um nome que não é deck vale como tag se ela já existe
	run(t, a, "ligar", "3", "redes")
	if got := linksState(t, a).Neighbors["card:c3"]; len(got) != 1 || got[0] != "tag:redes" {
		t.Fatalf("ligar 3 redes → tag existente: %v", got)
	}
	if err := runErr(t, a, "ligar", "3", "inexistente"); err == nil || !strings.Contains(err.Error(), "'#inexistente'") {
		t.Fatalf("nome desconhecido sem # é recusado com a dica do #: %v", err)
	}

	out = run(t, a, "conexoes")
	if !strings.Contains(out, ` 1 "O que é um processo"  2 conexões`) {
		t.Fatalf("conexoes lista os nós, os mais conectados primeiro: %s", out)
	}
	out = run(t, a, "conexoes", "1")
	for _, want := range []string{` 1 "O que é um processo"`, `   2 → "O que é deadlock"  prerequisito`, `   3 → #escalonamento  prerequisito`} {
		if !strings.Contains(out, want) {
			t.Fatalf("faltou %q na vizinhança:\n%s", want, out)
		}
	}

	// a vizinhança numera o próprio nó como 1: desligar 1 2 sem redigitar
	if out := run(t, a, "desligar", "1", "2"); !strings.Contains(out, `✂ desligado: "O que é um processo" ↔ "O que é deadlock"`) {
		t.Fatalf("desligar remove a conexão do par: %s", out)
	}
	if got := linksState(t, a).Neighbors["card:c1"]; len(got) != 1 || got[0] != "tag:escalonamento" {
		t.Fatalf("sobra só a conexão com a tag: %v", got)
	}
	run(t, a, "conexoes", "1")
	if out := run(t, a, "desligar", "1", "2"); !strings.Contains(out, "✂ desligado") {
		t.Fatalf("depois da nova tela, 2 é a tag: %s", out)
	}
	if out := run(t, a, "desligar", "1", "#escalonamento"); !strings.Contains(out, "não estavam ligados") {
		t.Fatalf("desligar o que não está ligado avisa: %s", out)
	}
}

func TestLigarRecusaItemQueNaoSeConecta(t *testing.T) {
	a, _ := newTestApp(t)
	run(t, a, "t", "Tarefa qualquer")
	run(t, a, "dia")
	if err := runErr(t, a, "ligar", "1", "#so"); err == nil || !strings.Contains(err.Error(), "não se conecta") {
		t.Fatalf("tarefa não é nó do grafo: %v", err)
	}
}

func TestEditarTagRenomeiaEmCartoesDecksEConexoes(t *testing.T) {
	a, _ := newTestApp(t)
	seedTags(t, a)
	run(t, a, "cartas", "--tag", "redes")
	run(t, a, "ligar", "1", "#so/concorrencia")

	out := run(t, a, "tags")
	if !strings.Contains(out, " 4 #so  2 cartões") || !strings.Contains(out, " 5   concorrencia  1 cartão  1 vencido  🔗1") {
		t.Fatalf("tags numera os nós da árvore e mostra as conexões:\n%s", out)
	}
	// 4 = #so (ancestral implícito): renomeia so/concorrencia e so/processos
	out = run(t, a, "editar", "4", "#Sistemas Operacionais")
	if !strings.Contains(out, "✔ tag renomeada: #so → #sistemas-operacionais (2 cartões, 1 conexão)") {
		t.Fatalf("renomear diz o alcance: %s", out)
	}
	if p := lastPayload(t, a, "tag.renamed"); p["from"] != "so" || p["to"] != "sistemas-operacionais" {
		t.Fatalf("tag.renamed leva os dois nomes normalizados: %v", p)
	}
	out = run(t, a, "tags")
	if strings.Contains(out, "#so ") || !strings.Contains(out, "#sistemas-operacionais  2 cartões") {
		t.Fatalf("a árvore reflete o nome novo:\n%s", out)
	}
	if got := linksState(t, a).Neighbors["card:c3"]; len(got) != 1 || got[0] != "tag:sistemas-operacionais/concorrencia" {
		t.Fatalf("a conexão acompanha a renomeação: %v", got)
	}

	run(t, a, "tags") // 1 #prova-p2 · 2 #redes · 3 #sistemas · 4 #sistemas-operacionais …
	if out := run(t, a, "editar", "3", "sistemas-operacionais"); !strings.Contains(out, "#sistemas → #sistemas-operacionais (1 deck) — fundida com a #sistemas-operacionais") {
		t.Fatalf("renomear para uma tag existente avisa a fusão: %s", out)
	}
	if err := runErr(t, a, "apagar", "1"); err == nil || !strings.Contains(err.Error(), "tag não se apaga") {
		t.Fatalf("apagar uma tag explica a alternativa: %v", err)
	}
}

func TestRevisarAoRedorFiltraPelaVizinhanca(t *testing.T) {
	a, _ := newTestApp(t)
	seedTags(t, a)
	// c3 (Modelo OSI) revisado hoje: deixa de estar vencido
	if _, err := a.Store.Append("card.reviewed", map[string]any{"card_id": "c3", "result": "correct"}); err != nil {
		t.Fatal(err)
	}
	run(t, a, "cartas", "--tag", "redes")
	run(t, a, "ligar", "1", "#isolada")

	out := run(t, a, "revisar", "--ao-redor", "#isolada")
	if !strings.Contains(out, "nenhum cartão vencido 🐘 — 1 cartão para treinar") {
		t.Fatalf("a vizinhança de #isolada é só o Modelo OSI, não vencido: %s", out)
	}
	if out := run(t, a, "revisar", "--ao-redor", "#ninguem"); !strings.Contains(out, "volte amanhã") {
		t.Fatalf("vizinhança vazia não abre a tela: %s", out)
	}
}

func TestLigarSemRelacaoNaoDuplicaParJaLigado(t *testing.T) {
	a, _ := newTestApp(t)
	seedTags(t, a)
	run(t, a, "cartas")
	run(t, a, "ligar", "1", "2", "--relacao", "prerequisito")
	if out := run(t, a, "ligar", "1", "2"); !strings.Contains(out, "já ligados") {
		t.Fatalf("sem relação, o par já ligado (de qualquer forma) não ganha outra conexão: %s", out)
	}
	if out := run(t, a, "ligar", "2", "1", "--relacao", "exemplo"); !strings.Contains(out, "🔗 ligado") {
		t.Fatalf("uma relação nova no mesmo par é outra conexão: %s", out)
	}
	if got := len(linksState(t, a).Links); got != 2 {
		t.Fatalf("esperava 2 conexões (prerequisito e exemplo), veio %d", got)
	}
	out := run(t, a, "conexoes", "1")
	if !strings.Contains(out, `↔ "O que é deadlock"  exemplo, prerequisito`) {
		t.Fatalf("as duas direções viram ↔ e as relações se juntam: %s", out)
	}
}
