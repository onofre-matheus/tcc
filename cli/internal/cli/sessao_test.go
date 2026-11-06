// Sessões salvas (catálogo 1.5) e grafo (spec §4.14) nas telas one-shot:
// sessao, sessoes, revisar --sessao, editar/apagar de sessão e grafo em DOT.
package cli_test

import (
	"strings"
	"testing"
)

func TestSessaoSalvaListaEAtualizaPeloNome(t *testing.T) {
	a, _ := newTestApp(t)
	seedTags(t, a)

	out := run(t, a, "sessao", "Véspera P2", "--tag", "#Prova-P2", "--treino")
	if !strings.Contains(out, "✔ sessão salva: Véspera P2 — 1 vencido · 0 para treinar") || !strings.Contains(out, "#prova-p2 · treino") {
		t.Fatalf("salvar mostra o que a sessão reúne hoje e o filtro: %s", out)
	}
	p := lastPayload(t, a, "study.saved")
	if p["practice"] != true || p["tags"].([]any)[0] != "prova-p2" {
		t.Fatalf("study.saved leva o filtro inteiro: %v", p)
	}
	firstID := p["study_id"]

	out = run(t, a, "sessao", "Véspera P2", "SO", "--tag", "sistemas")
	if !strings.Contains(out, "✔ sessão atualizada: Véspera P2") || !strings.Contains(out, "deck SO · #sistemas") {
		t.Fatalf("salvar de novo com o mesmo nome substitui o filtro: %s", out)
	}
	if p := lastPayload(t, a, "study.saved"); p["study_id"] != firstID || p["practice"] != false {
		t.Fatalf("o mesmo nome reaproveita o study_id e troca o filtro inteiro: %v", p)
	}

	run(t, a, "sessao", "Redes", "--ao-redor", "#redes")
	out = run(t, a, "sessoes")
	for _, want := range []string{" 1 Redes  1 vencido · 0 para treinar", "ao redor de #redes", " 2 Véspera P2  1 vencido"} {
		if !strings.Contains(out, want) {
			t.Fatalf("faltou %q em sessoes:\n%s", want, out)
		}
	}

	if err := runErr(t, a, "sessao", "Vazia"); err == nil || !strings.Contains(err.Error(), "sem filtro") {
		t.Fatalf("sessão sem filtro é recusada: %v", err)
	}
	if err := runErr(t, a, "sessao", "X", "Inexistente"); err == nil || !strings.Contains(err.Error(), "não encontrado") {
		t.Fatalf("deck desconhecido é recusado: %v", err)
	}
}

func TestSessaoRenomeiaApagaERevisa(t *testing.T) {
	a, _ := newTestApp(t)
	seedTags(t, a)
	run(t, a, "sessao", "Tudo de SO", "--tag", "so")
	run(t, a, "sessoes")

	if out := run(t, a, "editar", "1", "SO inteiro"); !strings.Contains(out, "✔ sessão renomeada: Tudo de SO → SO inteiro") {
		t.Fatalf("editar N renomeia a sessão: %s", out)
	}
	if p := lastPayload(t, a, "study.saved"); p["tags"].([]any)[0] != "so" {
		t.Fatalf("renomear mantém o filtro (study.saved substitui a sessão inteira): %v", p)
	}

	// c1 e c2 (so/…) revisados: a sessão não tem vencidos, mas tem treino
	for _, id := range []string{"c1", "c2"} {
		if _, err := a.Store.Append("card.reviewed", map[string]any{"card_id": id, "result": "correct"}); err != nil {
			t.Fatal(err)
		}
	}
	if out := run(t, a, "revisar", "--sessao", "SO inteiro"); !strings.Contains(out, "2 cartões para treinar") {
		t.Fatalf("revisar --sessao usa o filtro salvo: %s", out)
	}
	if err := runErr(t, a, "revisar", "--sessao", "SO inteiro", "--tag", "redes"); err == nil || !strings.Contains(err.Error(), "não os dois") {
		t.Fatalf("sessão e filtro avulso juntos são recusados: %v", err)
	}

	run(t, a, "sessoes")
	if out := run(t, a, "apagar", "1"); !strings.Contains(out, "✗ sessão apagada: SO inteiro") {
		t.Fatalf("apagar N apaga a sessão: %s", out)
	}
	if out := run(t, a, "sessoes"); !strings.Contains(out, "nenhuma sessão salva") {
		t.Fatalf("a sessão apagada some: %s", out)
	}
}

func TestSessaoAcompanhaRenomeacaoDeTag(t *testing.T) {
	a, _ := newTestApp(t)
	seedTags(t, a)
	run(t, a, "sessao", "Concorrência", "--tag", "so/concorrencia")
	run(t, a, "tags") // 4 = #so
	run(t, a, "editar", "4", "sistemas-operacionais")
	if out := run(t, a, "sessoes"); !strings.Contains(out, "#sistemas-operacionais/concorrencia") || !strings.Contains(out, "1 vencido") {
		t.Fatalf("a sessão acompanha tag.renamed e segue reunindo o cartão: %s", out)
	}
}

func TestGrafoEmDOTComGrafoLocal(t *testing.T) {
	a, _ := newTestApp(t)
	seedTags(t, a)
	run(t, a, "cartas")
	run(t, a, "ligar", "1", "2", "--relacao", `pré "requisito"`)

	out := run(t, a, "grafo")
	for _, want := range []string{
		"digraph pnn {",
		`"card:c1" [label="O que é um processo", shape=box`,
		`"tag:so" [label="#so", shape=ellipse`,
		`"deck:`,
		`"card:c1" -> "card:c2" [color="#c96442", penwidth=1.4, label="pré \"requisito\""`,
		`"tag:so/processos" -> "tag:so" [style=dotted, arrowhead=empty]`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("faltou %q no DOT:\n%s", want, out)
		}
	}

	local := run(t, a, "grafo", "#redes")
	if !strings.Contains(local, `"card:c3"`) || strings.Contains(local, `"card:c1"`) {
		t.Fatalf("o grafo local de #redes tem o Modelo OSI e não o processo:\n%s", local)
	}
	if out := run(t, a, "grafo", "--sem-cartoes"); strings.Contains(out, `"card:`) || !strings.Contains(out, `"tag:so"`) {
		t.Fatalf("--sem-cartoes deixa só tags e decks:\n%s", out)
	}
	run(t, a, "cartas") // 1 = processo
	if _, err := a.Store.Append("card.archived", map[string]any{"card_id": "c1"}); err != nil {
		t.Fatal(err)
	}
	if err := runErr(t, a, "grafo", "1"); err == nil || !strings.Contains(err.Error(), "fora do grafo") {
		t.Fatalf("cartão arquivado não está no grafo: %v", err)
	}
}
