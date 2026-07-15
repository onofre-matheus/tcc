// Testes dos comandos de edição/arquivamento (catálogo v1.1): editar, apagar,
// arquivar sobre números efêmeros, e as telas cartas/agenda que os numeram.
package cli_test

import (
	"strings"
	"testing"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/cli"
)

func TestEditarTarefaTituloEPrazo(t *testing.T) {
	a, _ := newTestApp(t)
	run(t, a, "t", "Ler capitulo 3")
	run(t, a, "dia")
	out := run(t, a, "editar", "1", "Ler capítulo 3 do Sommerville", "--data", "2026-07-20")
	if !strings.Contains(out, "editada: Ler capítulo 3 do Sommerville") {
		t.Fatalf("saída deveria confirmar a edição: %s", out)
	}

	types := eventTypes(t, a)
	if types[len(types)-1] != "task.edited" {
		t.Fatalf("último evento deveria ser task.edited, veio %v", types)
	}
	if out := run(t, a, "dia"); !strings.Contains(out, "Ler capítulo 3 do Sommerville") {
		t.Fatalf("a tela do dia deveria mostrar o título corrigido: %s", out)
	}
}

func TestApagarTarefaSomeDaLista(t *testing.T) {
	a, _ := newTestApp(t)
	run(t, a, "t", "Comprar café")
	run(t, a, "dia")
	out := run(t, a, "apagar", "1")
	if !strings.Contains(out, "apagada: Comprar café") {
		t.Fatalf("saída deveria confirmar o tombstone: %s", out)
	}
	if types := eventTypes(t, a); types[len(types)-1] != "task.deleted" {
		t.Fatalf("último evento deveria ser task.deleted, veio %v", types)
	}
	if out := run(t, a, "dia"); strings.Contains(out, "Comprar café") {
		t.Fatalf("a tarefa apagada não deveria aparecer no dia: %s", out)
	}
}

func TestRenomearEArquivarDeck(t *testing.T) {
	a, _ := newTestApp(t)
	run(t, a, "deck", "Redes")
	run(t, a, "decks")
	out := run(t, a, "editar", "1", "Redes de Computadores")
	if !strings.Contains(out, "Redes → Redes de Computadores") {
		t.Fatalf("saída deveria confirmar o rename: %s", out)
	}
	if out := run(t, a, "decks"); !strings.Contains(out, "1 Redes de Computadores") {
		t.Fatalf("a listagem deveria mostrar o novo nome numerado: %s", out)
	}

	run(t, a, "arquivar", "1")
	if types := eventTypes(t, a); types[len(types)-1] != "deck.archived" {
		t.Fatalf("último evento deveria ser deck.archived, veio %v", types)
	}
	if out := run(t, a, "decks"); !strings.Contains(out, "todos os decks estão arquivados") {
		t.Fatalf("deck arquivado não deveria ser listado: %s", out)
	}
	if out := run(t, a, "decks", "--arquivados"); !strings.Contains(out, "(arquivado)") {
		t.Fatalf("--arquivados deveria mostrar o deck com o selo: %s", out)
	}
}

func TestCartasEditarEArquivar(t *testing.T) {
	a, _ := newTestApp(t)
	run(t, a, "deck", "Redes")
	events, err := a.Store.Events()
	if err != nil {
		t.Fatal(err)
	}
	decks, err := core.Decks(events)
	if err != nil {
		t.Fatal(err)
	}
	var deckID string
	for id := range decks.Decks {
		deckID = id
	}
	if _, err := a.Store.Append("card.created", map[string]any{
		"card_id": "c1", "deck_id": deckID,
		"front": "O que é DNS", "back": "sistema de nomes", "tags": []string{},
	}); err != nil {
		t.Fatal(err)
	}

	if out := run(t, a, "cartas"); !strings.Contains(out, "1  O que é DNS") {
		t.Fatalf("a listagem deveria numerar o cartão: %s", out)
	}
	out := run(t, a, "editar", "1", "--frente", "Defina DNS")
	if !strings.Contains(out, "cartão editado: Defina DNS") {
		t.Fatalf("saída deveria confirmar a edição: %s", out)
	}
	if out := run(t, a, "cartas"); !strings.Contains(out, "Defina DNS") {
		t.Fatalf("a listagem deveria mostrar a frente corrigida: %s", out)
	}

	run(t, a, "arquivar", "1")
	if types := eventTypes(t, a); types[len(types)-1] != "card.archived" {
		t.Fatalf("último evento deveria ser card.archived, veio %v", types)
	}
	if out := run(t, a, "cartas"); !strings.Contains(out, "nenhum cartão") {
		t.Fatalf("cartão arquivado não deveria ser listado: %s", out)
	}
	if out := run(t, a, "cartas", "--arquivados"); !strings.Contains(out, "arquivado") {
		t.Fatalf("--arquivados deveria mostrar o cartão: %s", out)
	}
}

func TestAgendaCancelarCompromisso(t *testing.T) {
	a, _ := newTestApp(t)
	run(t, a, "c", "Reunião com orientador", "16:00-17:00")
	if out := run(t, a, "agenda"); !strings.Contains(out, "1  ") || !strings.Contains(out, "Reunião com orientador") {
		t.Fatalf("a agenda deveria numerar o compromisso: %s", out)
	}
	out := run(t, a, "apagar", "1")
	if !strings.Contains(out, "cancelado: Reunião com orientador") {
		t.Fatalf("saída deveria confirmar o cancelamento: %s", out)
	}
	if types := eventTypes(t, a); types[len(types)-1] != "appointment.cancelled" {
		t.Fatalf("último evento deveria ser appointment.cancelled, veio %v", types)
	}
	if out := run(t, a, "agenda"); !strings.Contains(out, "agenda vazia") {
		t.Fatalf("compromisso cancelado não deveria aparecer: %s", out)
	}
}

func TestArquivarTarefaRecusa(t *testing.T) {
	a, _ := newTestApp(t)
	run(t, a, "t", "Tarefa qualquer")
	run(t, a, "dia")
	root := cli.NewRoot(a)
	root.SetArgs([]string{"arquivar", "1"})
	root.SetOut(a.Out)
	root.SetErr(a.Out)
	if err := root.Execute(); err == nil {
		t.Fatal("arquivar tarefa deveria falhar orientando feito/apagar")
	}
}
