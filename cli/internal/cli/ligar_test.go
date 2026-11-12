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

func TestLigarCriaSinapseETagClassifica(t *testing.T) {
	a, _ := newTestApp(t)
	seedTags(t, a)
	run(t, a, "cartas") // SO: 1 processo · (sem deck): 2 deadlock, 3 OSI

	out := run(t, a, "ligar", "1", "2", "--relacao", "prerequisito")
	if !strings.Contains(out, `⚡ sinapse: "O que é um processo" → "O que é deadlock" (prerequisito)`) {
		t.Fatalf("ligar duas coisas cria uma sinapse: %s", out)
	}
	if p := lastPayload(t, a, "link.created"); p["relation"] != "prerequisito" {
		t.Fatalf("link.created leva a relação: %v", p)
	}
	if out := run(t, a, "ligar", "2", "1", "--relacao", "prerequisito"); !strings.Contains(out, "já há sinapse") {
		t.Fatalf("o mesmo par com a mesma relação não duplica (em qualquer direção): %s", out)
	}

	// com uma tag de um lado, ligar classifica: a tag vai para o cartão, sem
	// sinapse; sem "#", um nome que não é deck vale como tag se ela já existe
	out = run(t, a, "ligar", "3", "#Escalonamento", "redes")
	if !strings.Contains(out, `🏷 tag adicionada: "Modelo OSI"  #escalonamento`) || !strings.Contains(out, `· "Modelo OSI" já tem #redes`) {
		t.Fatalf("ligar a uma tag classifica o cartão: %s", out)
	}
	if p := lastPayload(t, a, "card.edited"); len(p["tags"].([]any)) != 2 || p["tags"].([]any)[1] != "escalonamento" {
		t.Fatalf("card.edited leva a lista final das tags próprias: %v", p)
	}
	if got := linksState(t, a).Neighbors["card:c3"]; len(got) != 0 {
		t.Fatalf("tag não é ponta de sinapse: %v", got)
	}
	out = run(t, a, "ligar", "1", "3", "#Revisar", "#sistemas")
	for _, want := range []string{`🏷 tag adicionada: "O que é um processo"  #revisar`, `· "O que é um processo" já tem #sistemas`, `⚡ sinapse: "O que é um processo" → "Modelo OSI"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("faltou %q (sinapse e tags no mesmo comando; a do deck já conta):\n%s", want, out)
		}
	}

	// recusas não gravam nada, nem a parte que seria válida
	before := len(eventTypes(t, a))
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"ligar", "1", "2", "#y", "--relacao", "exemplo"}, "--relacao é de sinapse"},
		{[]string{"ligar", "#a", "#b"}, "tags não se ligam"},
		{[]string{"ligar", "SO", "#x"}, "deck não ganha tag"},
		{[]string{"ligar", "3", "inexistente"}, "'#inexistente'"},
	} {
		if err := runErr(t, a, c.args...); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("pnn %v: esperava erro com %q, veio %v", c.args, c.want, err)
		}
	}
	if got := len(eventTypes(t, a)); got != before {
		t.Fatalf("recusa não grava nada: %d eventos novos", got-before)
	}

	if out := run(t, a, "sinapses"); !strings.Contains(out, ` 1 "O que é um processo"  2 sinapses`) {
		t.Fatalf("sinapses lista as coisas, as mais ligadas primeiro: %s", out)
	}
	out = run(t, a, "conexoes", "1") // o nome antigo continua valendo
	for _, want := range []string{` 1 "O que é um processo"`, `   2 → "O que é deadlock"  prerequisito`, `   3 → "Modelo OSI"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("faltou %q na vizinhança:\n%s", want, out)
		}
	}

	// a vizinhança numera o próprio nó como 1: desligar 1 2 sem redigitar
	if out := run(t, a, "desligar", "1", "2"); !strings.Contains(out, `✂ sinapse desfeita: "O que é um processo" ↔ "O que é deadlock"`) {
		t.Fatalf("desligar desfaz a sinapse do par: %s", out)
	}
	if out := run(t, a, "desligar", "1", "#revisar"); !strings.Contains(out, `🏷 tag removida: "O que é um processo"  #revisar`) {
		t.Fatalf("desligar de uma tag tira a tag: %s", out)
	}
	if p := lastPayload(t, a, "card.edited"); len(p["tags"].([]any)) != 1 || p["tags"].([]any)[0] != "so/processos" {
		t.Fatalf("sobram as tags próprias: %v", p)
	}
	for args, want := range map[[2]string]string{
		{"1", "#sistemas"}: "vem do deck",
		{"1", "#nada"}:     "não tinha #nada",
		{"3", "2"}:         "não havia sinapse",
	} {
		if out := run(t, a, "desligar", args[0], args[1]); !strings.Contains(out, want) {
			t.Fatalf("desligar %v: esperava %q: %s", args, want, out)
		}
	}
}

func TestLigarRecusaItemQueNaoSeLiga(t *testing.T) {
	a, _ := newTestApp(t)
	run(t, a, "c", "Reunião", "14:00-15:00", "--dia", "2026-07-09")
	run(t, a, "agenda")
	if err := runErr(t, a, "ligar", "1", "#so"); err == nil || !strings.Contains(err.Error(), "não se liga") {
		t.Fatalf("compromisso não é ponta de sinapse: %v", err)
	}
}

func TestEditarTagRenomeiaEmCartoesPaginasDecksESinapses(t *testing.T) {
	a, _ := newTestApp(t)
	seedTags(t, a)
	appendPage(t, a, "p1", "Escalonamento", "", "so/escalonamento")
	// conexão com tag gravada antes do 1.6: o log é imutável, então ela
	// continua valendo, aparece e acompanha a renomeação
	if _, err := a.Store.Append("link.created", map[string]any{
		"link_id": "l-antigo", "from": map[string]any{"kind": "card", "id": "c3"}, "to": map[string]any{"kind": "tag", "id": "so/concorrencia"},
	}); err != nil {
		t.Fatal(err)
	}

	out := run(t, a, "tags")
	for _, want := range []string{" 4 #so  2 cartões  1 página", " 5   concorrencia  1 cartão  1 vencido  ⚡1", " 6   escalonamento  0 cartões  1 página"} {
		if !strings.Contains(out, want) {
			t.Fatalf("faltou %q — tags numera a árvore com cartões, páginas e sinapses:\n%s", want, out)
		}
	}
	// 4 = #so (ancestral implícito): renomeia so/concorrencia, so/processos e so/escalonamento
	out = run(t, a, "editar", "4", "#Sistemas Operacionais")
	if !strings.Contains(out, "✔ tag renomeada: #so → #sistemas-operacionais (2 cartões, 1 página, 1 sinapse)") {
		t.Fatalf("renomear diz o alcance: %s", out)
	}
	if p := lastPayload(t, a, "tag.renamed"); p["from"] != "so" || p["to"] != "sistemas-operacionais" {
		t.Fatalf("tag.renamed leva os dois nomes normalizados: %v", p)
	}
	out = run(t, a, "tags")
	if strings.Contains(out, "#so ") || !strings.Contains(out, "#sistemas-operacionais  2 cartões  1 página") {
		t.Fatalf("a árvore reflete o nome novo:\n%s", out)
	}
	if got := linksState(t, a).Neighbors["card:c3"]; len(got) != 1 || got[0] != "tag:sistemas-operacionais/concorrencia" {
		t.Fatalf("a conexão antiga acompanha a renomeação: %v", got)
	}
	if out := run(t, a, "pagina", "escalonamento"); !strings.Contains(out, "#sistemas-operacionais/escalonamento") {
		t.Fatalf("a tag da página acompanha a renomeação: %s", out)
	}

	run(t, a, "cartas", "--tag", "redes") // 1 = Modelo OSI
	if out := run(t, a, "desligar", "1", "#sistemas-operacionais/concorrencia"); !strings.Contains(out, `✂ sinapse desfeita: "Modelo OSI" ↔ #sistemas-operacionais/concorrencia`) {
		t.Fatalf("a conexão antiga com tag se desfaz pelo desligar: %s", out)
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
	run(t, a, "ligar", "1", "#isolada") // classifica: o Modelo OSI ganha #isolada

	out := run(t, a, "revisar", "--ao-redor", "#isolada")
	if !strings.Contains(out, "nenhum cartão vencido 🐘 — 1 cartão para treinar") {
		t.Fatalf("ao redor de #isolada está só o Modelo OSI, não vencido: %s", out)
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
	if out := run(t, a, "ligar", "1", "2"); !strings.Contains(out, "já há sinapse") {
		t.Fatalf("sem relação, o par já ligado (de qualquer forma) não ganha outra sinapse: %s", out)
	}
	if out := run(t, a, "ligar", "2", "1", "--relacao", "exemplo"); !strings.Contains(out, "⚡ sinapse") {
		t.Fatalf("uma relação nova no mesmo par é outra sinapse: %s", out)
	}
	if got := len(linksState(t, a).Links); got != 2 {
		t.Fatalf("esperava 2 sinapses (prerequisito e exemplo), veio %d", got)
	}
	out := run(t, a, "sinapses", "1")
	if !strings.Contains(out, `↔ "O que é deadlock"  exemplo, prerequisito`) {
		t.Fatalf("as duas direções viram ↔ e as relações se juntam: %s", out)
	}
}
