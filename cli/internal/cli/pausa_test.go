// Testes de pausas (catálogo v1.2) e da retrospectiva `pnn semana`.
package cli_test

import (
	"strings"
	"testing"

	"github.com/onofre-matheus/tcc/cli/internal/cli"
)

func TestPausaEVolta(t *testing.T) {
	a, _ := newTestApp(t)
	out := run(t, a, "pausa", "10")
	if !strings.Contains(out, "pausa de 10 min") {
		t.Fatalf("saída deveria confirmar a pausa: %s", out)
	}
	if types := eventTypes(t, a); types[len(types)-1] != "pause.started" {
		t.Fatalf("último evento deveria ser pause.started, veio %v", types)
	}

	out = run(t, a, "volta")
	if !strings.Contains(out, "pausa encerrada") {
		t.Fatalf("saída deveria confirmar o fim: %s", out)
	}
	if types := eventTypes(t, a); types[len(types)-1] != "pause.ended" {
		t.Fatalf("último evento deveria ser pause.ended, veio %v", types)
	}

	// volta sem pausa aberta é erro orientado
	root := cli.NewRoot(a)
	root.SetArgs([]string{"volta"})
	root.SetOut(a.Out)
	root.SetErr(a.Out)
	if err := root.Execute(); err == nil {
		t.Fatal("volta sem pausa aberta deveria falhar")
	}
}

func TestPausaAbertaImpedeOutra(t *testing.T) {
	a, _ := newTestApp(t)
	run(t, a, "pausa")
	root := cli.NewRoot(a)
	root.SetArgs([]string{"pausa", "5"})
	root.SetOut(a.Out)
	root.SetErr(a.Out)
	if err := root.Execute(); err == nil {
		t.Fatal("segunda pausa com uma aberta deveria falhar orientando `pnn volta`")
	}
}

func TestPausaRetroativaApareceNaSemana(t *testing.T) {
	a, _ := newTestApp(t)
	out := run(t, a, "pausa", "--das", "09:30", "--ate", "09:45")
	if !strings.Contains(out, "pausa registrada") {
		t.Fatalf("saída deveria confirmar o lançamento retroativo: %s", out)
	}
	if types := eventTypes(t, a); types[len(types)-1] != "pause.logged" {
		t.Fatalf("último evento deveria ser pause.logged, veio %v", types)
	}

	out = run(t, a, "semana")
	if !strings.Contains(out, "15 min de pausa") {
		t.Fatalf("a retrospectiva deveria contar os 15 min retroativos: %s", out)
	}
}

func TestSemanaRetroativaNavegaPeloNow(t *testing.T) {
	a, _ := newTestApp(t)
	// relógio fixo dos testes: quarta 2026-07-08 → semana 06/07–12/07
	out := run(t, a, "semana")
	if !strings.Contains(out, "06/07") || !strings.Contains(out, "12/07") {
		t.Fatalf("semana atual deveria ser 06/07–12/07: %s", out)
	}
	out = run(t, a, "semana", "1")
	if !strings.Contains(out, "29/06") || !strings.Contains(out, "05/07") {
		t.Fatalf("`semana 1` deveria mostrar 29/06–05/07: %s", out)
	}
	out = run(t, a, "semana", "--de", "2026-06-10")
	if !strings.Contains(out, "08/06") || !strings.Contains(out, "14/06") {
		t.Fatalf("`--de 2026-06-10` deveria mostrar 08/06–14/06: %s", out)
	}
}
