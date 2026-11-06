// Tags livres, cartão solto e sessão personalizada (catálogo v1.3) nas telas
// one-shot: cartas, decks, tags, editar --tag e o filtro do revisar.
package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/cli"
	"github.com/onofre-matheus/tcc/cli/store"
)

func appendCard(t *testing.T, a *app.App, payload map[string]any) {
	t.Helper()
	v := 1
	if _, hasDeck := payload["deck_id"]; !hasDeck {
		v = 2 // cartão solto
	}
	if _, err := a.Store.AppendV("card.created", v, payload); err != nil {
		t.Fatal(err)
	}
}

func deckIDByName(t *testing.T, a *app.App, name string) string {
	t.Helper()
	events, err := a.Store.Events()
	if err != nil {
		t.Fatal(err)
	}
	decks, err := core.Decks(events)
	if err != nil {
		t.Fatal(err)
	}
	for id, deck := range decks.Decks {
		if deck.Name == name {
			return id
		}
	}
	t.Fatalf("deck %q não existe", name)
	return ""
}

// acervo: um deck com tag transversal e dois cartões soltos com tags. O store
// é reaberto com o relógio do app, para os cartões nascerem vencidos "hoje".
func seedTags(t *testing.T, a *app.App) {
	t.Helper()
	s, err := store.Open(a.Dir, store.WithClock(a.Now))
	if err != nil {
		t.Fatal(err)
	}
	a.Store = s
	run(t, a, "deck", "SO", "--tag", "#Sistemas")
	appendCard(t, a, map[string]any{
		"card_id": "c1", "deck_id": deckIDByName(t, a, "SO"),
		"front": "O que é um processo", "back": "programa em execução", "tags": []string{"so/processos"},
	})
	appendCard(t, a, map[string]any{
		"card_id": "c2", "front": "O que é deadlock", "back": "espera circular",
		"tags": []string{"SO/Concorrencia", "prova-p2"},
	})
	appendCard(t, a, map[string]any{
		"card_id": "c3", "front": "Modelo OSI", "back": "7 camadas", "tags": []string{"redes"},
	})
}

func TestCartasAgrupaSoltosEFiltraPorTag(t *testing.T) {
	a, _ := newTestApp(t)
	seedTags(t, a)

	out := run(t, a, "cartas")
	if !strings.Contains(out, "(sem deck)") || !strings.Contains(out, "#so/concorrencia #prova-p2") {
		t.Fatalf("cartões soltos vão em (sem deck), com as tags normalizadas: %s", out)
	}
	if strings.Index(out, "SO") > strings.Index(out, "(sem deck)") {
		t.Fatalf("o grupo (sem deck) vem por último: %s", out)
	}
	if !strings.Contains(out, "#so/processos #sistemas") {
		t.Fatalf("a listagem mostra as tags efetivas (cartão ∪ deck): %s", out)
	}

	out = run(t, a, "cartas", "--tag", "#SO")
	if !strings.Contains(out, "O que é deadlock") || !strings.Contains(out, "O que é um processo") || strings.Contains(out, "Modelo OSI") {
		t.Fatalf("--tag so reúne so/… de qualquer deck ou solto, e só eles: %s", out)
	}
	out = run(t, a, "cartas", "SO", "--tag", "prova-p2")
	if !strings.Contains(out, "nenhum cartão") {
		t.Fatalf("deck e tag combinam com E (o cartão prova-p2 é solto): %s", out)
	}
	out = run(t, a, "cartas", "--tag", "sistemas")
	if !strings.Contains(out, "O que é um processo") || strings.Contains(out, "deadlock") {
		t.Fatalf("a tag do deck é herdada pelos cartões dele: %s", out)
	}
}

func TestDecksMostraCartoesSoltos(t *testing.T) {
	a, _ := newTestApp(t)
	seedTags(t, a)
	out := run(t, a, "decks")
	if !strings.Contains(out, "(sem deck) 2 cartões") || !strings.Contains(out, "2 vencidos") {
		t.Fatalf("decks deveria resumir os cartões soltos: %s", out)
	}
}

func TestTagsArvoreComContagemHierarquica(t *testing.T) {
	a, _ := newTestApp(t)
	seedTags(t, a)

	out := run(t, a, "tags")
	for _, want := range []string{
		"#so  2 cartões  2 vencidos",
		"   concorrencia  1 cartão",
		"   processos  1 cartão",
		"#prova-p2  1 cartão",
		"#sistemas  1 cartão",
		"#redes  1 cartão",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("faltou %q na árvore de tags:\n%s", want, out)
		}
	}

	var state core.TagsState
	if err := json.Unmarshal([]byte(run(t, a, "tags", "--json")), &state); err != nil {
		t.Fatal(err)
	}
	if strings.Join(state.Names, ",") != "prova-p2,redes,sistemas,so/concorrencia,so/processos" {
		t.Fatalf("--json expõe a projeção (sem ancestrais implícitos), veio %v", state.Names)
	}
}

func TestEditarTagsDoCartao(t *testing.T) {
	a, _ := newTestApp(t)
	seedTags(t, a)
	run(t, a, "cartas", "--tag", "redes") // 1 = Modelo OSI

	lastTags := func() []any {
		events, err := a.Store.Events()
		if err != nil {
			t.Fatal(err)
		}
		last := events[len(events)-1]
		if last.Type != "card.edited" {
			t.Fatalf("último evento deveria ser card.edited, veio %s", last.Type)
		}
		var p map[string]any
		if err := json.Unmarshal(last.Payload, &p); err != nil {
			t.Fatal(err)
		}
		return p["tags"].([]any)
	}

	out := run(t, a, "editar", "1", "--tag", "#Prova-P2", "--tag", "redes")
	if !strings.Contains(out, "#redes #prova-p2") {
		t.Fatalf("--tag acrescenta (normalizada, sem duplicar): %s", out)
	}
	if got := lastTags(); len(got) != 2 {
		t.Fatalf("card.edited leva a lista final inteira, veio %v", got)
	}

	out = run(t, a, "editar", "1", "--untag", "redes")
	if got := lastTags(); len(got) != 1 || got[0] != "prova-p2" || !strings.Contains(out, "#prova-p2") {
		t.Fatalf("--untag remove, veio %v (%s)", got, out)
	}

	before := len(eventTypes(t, a))
	if out := run(t, a, "editar", "1", "--tag", "prova-p2"); !strings.Contains(out, "nada mudou") {
		t.Fatalf("tag que já existe não gera evento: %s", out)
	}
	if after := len(eventTypes(t, a)); after != before {
		t.Fatalf("nada mudou, nada gravado (%d → %d eventos)", before, after)
	}

	if out := run(t, a, "editar", "1", "--tags", ""); !strings.Contains(out, "(sem tags)") {
		t.Fatalf(`--tags "" limpa a lista: %s`, out)
	}
	if got := lastTags(); len(got) != 0 {
		t.Fatalf("lista limpa, veio %v", got)
	}
}

func TestEditarTagsRecusaEmTarefa(t *testing.T) {
	a, _ := newTestApp(t)
	run(t, a, "t", "Tarefa qualquer")
	run(t, a, "dia")
	root := cli.NewRoot(a)
	root.SetArgs([]string{"editar", "1", "--tag", "so"})
	root.SetOut(a.Out)
	root.SetErr(a.Out)
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "valem para cartões") {
		t.Fatalf("tags em tarefa deveriam ser recusadas, veio %v", err)
	}
}

func TestRevisarPorTagSemCartoesNaoAbreTela(t *testing.T) {
	a, _ := newTestApp(t)
	seedTags(t, a)
	if out := run(t, a, "revisar", "--tag", "inexistente"); !strings.Contains(out, "nenhum cartão vencido") {
		t.Fatalf("filtro sem cartões avisa em vez de abrir a revisão: %s", out)
	}
	if out := run(t, a, "revisar", "--tag", "inexistente", "--treino"); !strings.Contains(out, "nenhum cartão ativo casa com o filtro") {
		t.Fatalf("treino sem cartões também avisa: %s", out)
	}
}
