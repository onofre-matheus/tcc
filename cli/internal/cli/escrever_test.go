package cli

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/tui"
)

func lastOf(t *testing.T, w writeActions, eventType string) map[string]any {
	t.Helper()
	events, err := w.a.Store.Events()
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
	return nil
}

func countEvents(t *testing.T, w writeActions) int {
	t.Helper()
	events, err := w.a.Store.Events()
	if err != nil {
		t.Fatal(err)
	}
	return len(events)
}

// O acervo do modo de escrita, sobre o log de verdade: cada comando vira o
// evento de sempre.
func TestWriteActionsSobreOLog(t *testing.T) {
	a := internalApp(t)
	w := writeActions{a}
	appendOrFail(t, a, "page.created", map[string]any{"page_id": "p1", "title": "docs cli", "body": "v1", "tags": []string{}})
	appendOrFail(t, a, "page.created", map[string]any{"page_id": "p2", "title": "Kubernetes", "body": "pods", "tags": []string{}})
	appendOrFail(t, a, "task.created", map[string]any{"task_id": "t-dia", "title": "Testar o mgc auth"})
	appendOrFail(t, a, "card.created", map[string]any{"card_id": "c1", "front": "O que é um Pod?", "back": "a menor unidade", "tags": []string{}})

	text, err := w.Load("p1")
	if err != nil || text.Body != "v1" || text.Title != "docs cli" {
		t.Fatalf("Load: %+v %v", text, err)
	}

	// Save: sem mudança não grava; com mudança, page.edited com a base
	n := countEvents(t, w)
	if v, err := w.Save("p1", "v1\n", text.Version); err != nil || v != text.Version || countEvents(t, w) != n {
		t.Fatalf("sem mudança, nada é gravado: %q %v", v, err)
	}
	v2, err := w.Save("p1", "v2", text.Version)
	if p := lastOf(t, w, "page.edited"); err != nil || p["body"] != "v2" || p["base"] != text.Version || v2 == text.Version {
		t.Fatalf("Save grava com a base lida e devolve a versão nova: %v %v", p, err)
	}

	// /tarefa: a tarefa nasce ligada à página
	status, err := w.CreateTask("", "p1", "Revisar o console")
	if err != nil || status != "✔ tarefa criada e ligada à página: Revisar o console" {
		t.Fatalf("CreateTask: %q %v", status, err)
	}
	if p := lastOf(t, w, "link.created"); p["from"].(map[string]any)["id"] != "p1" || p["to"].(map[string]any)["kind"] != "task" {
		t.Fatalf("a tarefa nova é ligada à página: %v", p)
	}
	refs, _ := w.Refs("p1")
	if !slices.Contains(refs, "tarefa Revisar o console") {
		t.Fatalf("a sinapse aparece nas referências: %v", refs)
	}

	// /feito: as ligadas à página primeiro
	tasks, _ := w.Tasks("p1", "")
	if len(tasks) != 2 || tasks[0].Label != "[C] Revisar o console ⚡" || tasks[1].Label != "[C] Testar o mgc auth" {
		t.Fatalf("as tarefas da página primeiro, depois as do dia: %+v", tasks)
	}
	if status, err := w.CompleteTask(tasks[0].ID); err != nil || status != "✔ concluída: Revisar o console" {
		t.Fatalf("CompleteTask: %q %v", status, err)
	}
	if focused, _ := w.Tasks("p1", "t-dia"); len(focused) != 1 || focused[0].Label != "[C] Testar o mgc auth (em foco)" {
		t.Fatalf("a tarefa em foco vem primeiro: %+v", focused)
	}
	if tasks, _ = w.Tasks("p1", ""); len(tasks) != 1 {
		t.Fatalf("a concluída sai da lista: %+v", tasks)
	}
	refs, _ = w.Refs("p1")
	if !slices.Contains(refs, "tarefa Revisar o console ✔") {
		t.Fatalf("a concluída segue ligada, com ✔: %v", refs)
	}

	// /tag e /untag
	if status, _ := w.Tag("p1", []string{"#CLI", "docs"}, nil); status != "🏷 tags da página: #cli #docs" {
		t.Fatalf("Tag acrescenta, normalizando: %q", status)
	}
	if status, _ := w.Tag("p1", nil, []string{"docs"}); status != "🏷 tags da página: #cli" {
		t.Fatalf("Tag tira: %q", status)
	}
	if status, _ := w.Tag("p1", []string{"cli"}, nil); status != "· nada mudou nas tags" {
		t.Fatalf("sem mudança, nada é gravado: %q", status)
	}

	// /ligar: busca sem a própria página, liga, e não duplica
	found, _ := w.Search("p1", "pod")
	if len(found) != 2 || found[0].Label != "página Kubernetes" || found[1].Kind != core.NodeCard {
		t.Fatalf("Search acha página (pelo texto) e cartão: %+v", found)
	}
	if self, _ := w.Search("p1", "docs"); len(self) != 0 {
		t.Fatalf("a própria página não aparece: %+v", self)
	}
	pagina := tui.Choice{Kind: core.NodePage, ID: "p1"}
	if status, _ := w.Link(pagina, found[1]); status != `⚡ sinapse: página docs cli → "O que é um Pod?"` {
		t.Fatalf("Link: %q", status)
	}
	if status, _ := w.Link(pagina, found[1]); !strings.HasPrefix(status, "· já há sinapse") {
		t.Fatalf("ligar de novo não duplica: %q", status)
	}

	// /cartao: cartão solto (v2) ligado à página
	if status, err := w.CreateCard(pagina, "O que é um Service?", "um nome estável para pods"); err != nil || status != "✔ cartão criado e ligado à página: O que é um Service?" {
		t.Fatalf("CreateCard: %q %v", status, err)
	}
	events, _ := w.a.Store.Events()
	var card core.Event
	for _, e := range events {
		if e.Type == "card.created" {
			card = e
		}
	}
	if card.V != 2 || strings.Contains(string(card.Payload), "deck_id") {
		t.Fatalf("cartão sem deck é card.created v2: %+v", card)
	}
	if refs, _ := w.Refs("p1"); !slices.Contains(refs, `"O que é um Service?"`) {
		t.Fatalf("o cartão novo aparece ligado à página: %v", refs)
	}

	// /distracao fora do foco vira nota
	if status, _ := w.CaptureNote("ligar para o banco"); !strings.Contains(status, "caixa de entrada") {
		t.Fatalf("CaptureNote: %q", status)
	}
	if p := lastOf(t, w, "note.captured"); p["text"] != "ligar para o banco" {
		t.Fatalf("a nota vai para a caixa de entrada: %v", p)
	}

	// no relógio grande, o ponto de referência é a tarefa: /pagina cria a
	// página ligada a ela, /tarefa cria subtarefa
	page, status, err := w.CreatePage("t-dia", "Notas do auth")
	if err != nil || status != "✔ página criada e ligada à tarefa: Notas do auth" || page.Title != "Notas do auth" {
		t.Fatalf("CreatePage: %+v %q %v", page, status, err)
	}
	if p := lastOf(t, w, "link.created"); p["from"].(map[string]any)["id"] != "t-dia" || p["to"].(map[string]any)["id"] != page.ID {
		t.Fatalf("a página nova nasce ligada à tarefa: %v", p)
	}
	if status, _ := w.CreateTask("t-dia", "", "Ler a doc do OAuth"); status != "✔ subtarefa criada: Ler a doc do OAuth" {
		t.Fatalf("CreateTask com pai: %q", status)
	}
	if p := lastOf(t, w, "task.created"); p["parent_id"] != "t-dia" {
		t.Fatalf("a subtarefa leva parent_id: %v", p)
	}
	if status, _ := w.CreateCard(tui.Choice{Kind: core.NodeTask, ID: "t-dia"}, "f", "v"); !strings.Contains(status, "ligado à tarefa") {
		t.Fatalf("cartão a partir da tarefa: %q", status)
	}
	var _ tui.WriteActions = w
}
