// Comandos one-shot do pnn (spec/CLI.md §3): captura em uma linha, tela do dia
// com números efêmeros, --json em toda leitura e o alias-intervenção procrastina.
package cli_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/cli"
	"github.com/onofre-matheus/tcc/cli/store"
)

// App determinístico: réplica em diretório temporário, relógio fixo
// (2026-07-08 12:00 UTC = 09:00 em São Paulo, uma quarta-feira), sem cor.
func newTestApp(t *testing.T) (*app.App, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	buf := &bytes.Buffer{}
	return &app.App{
		Store: s,
		Dir:   dir,
		TZ:    "America/Sao_Paulo",
		Now:   func() time.Time { return time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC) },
		Out:   buf,
	}, buf
}

func run(t *testing.T, a *app.App, args ...string) string {
	t.Helper()
	buf := a.Out.(*bytes.Buffer)
	buf.Reset()
	root := cli.NewRoot(a)
	root.SetArgs(args)
	root.SetOut(a.Out)
	root.SetErr(a.Out)
	if err := root.Execute(); err != nil {
		t.Fatalf("pnn %v: %v\nsaída: %s", args, err, buf.String())
	}
	return buf.String()
}

func eventTypes(t *testing.T, a *app.App) []string {
	t.Helper()
	events, err := a.Store.Events()
	if err != nil {
		t.Fatal(err)
	}
	types := make([]string, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	return types
}

func TestCapturaTarefaComPrioridade(t *testing.T) {
	a, _ := newTestApp(t)
	out := run(t, a, "t", "Escrever seção 4.6", "-p", "A")

	types := eventTypes(t, a)
	if len(types) != 2 || types[0] != "task.created" || types[1] != "task.prioritized" {
		t.Fatalf("eventos esperados [task.created task.prioritized], vieram %v", types)
	}
	if !strings.Contains(out, "Escrever seção 4.6") {
		t.Fatalf("saída deveria confirmar a tarefa: %s", out)
	}
}

func TestCapturaNotaEDepoisCaixa(t *testing.T) {
	a, _ := newTestApp(t)
	run(t, a, "n", "ideia para o capítulo 5")

	out := run(t, a, "caixa")
	if !strings.Contains(out, "ideia para o capítulo 5") {
		t.Fatalf("caixa deveria listar a nota pendente: %s", out)
	}
}

func TestCompromissoEmHorarioLocal(t *testing.T) {
	a, _ := newTestApp(t)
	run(t, a, "c", "Reunião com orientador", "16:00-17:00")

	events, err := a.Store.Events()
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Title    string `json:"title"`
		StartsAt string `json:"starts_at"`
		EndsAt   string `json:"ends_at"`
	}
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	// 16:00 em São Paulo (UTC−3) = 19:00Z do mesmo dia.
	if payload.StartsAt != "2026-07-08T19:00:00.000Z" || payload.EndsAt != "2026-07-08T20:00:00.000Z" {
		t.Fatalf("instantes UTC errados: %+v", payload)
	}
}

func TestCompromissoImportanteMarcaEExibeNaAgenda(t *testing.T) {
	a, _ := newTestApp(t)
	out := run(t, a, "c", "Defesa do TCC", "15:00-17:00", "--dia", "2026-07-15", "--importante")
	if !strings.Contains(out, "⭐") {
		t.Fatalf("a confirmação deveria marcar como importante: %s", out)
	}

	events, err := a.Store.Events()
	if err != nil {
		t.Fatal(err)
	}
	if events[0].V != 2 {
		t.Fatalf("appointment.created importante deveria ser v2, veio v%d", events[0].V)
	}
	var payload struct {
		Importance string `json:"importance"`
	}
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Importance != "important" {
		t.Fatalf("importance = %q, quer \"important\"", payload.Importance)
	}

	// Marca d'água na tela do dia: o próximo importante lembra com antecedência.
	dia := run(t, a, "dia")
	if !strings.Contains(dia, "⭐") || !strings.Contains(dia, "Defesa do TCC") || !strings.Contains(dia, "em 7 dias") {
		t.Fatalf("`pnn dia` deveria destacar o compromisso importante: %s", dia)
	}
}

// Cenário compartilhado: pai A com subtarefa A, mais uma tarefa B.
func seedTasks(t *testing.T, a *app.App) {
	t.Helper()
	run(t, a, "t", "Escrever seção 4.6", "-p", "A")
	run(t, a, "dia")
	run(t, a, "t", "Rascunhar requisitos", "-p", "A", "--sub", "1")
	run(t, a, "t", "Ler capítulo 3", "-p", "B")
}

func TestDiaNumeraArvoreDeTarefas(t *testing.T) {
	a, _ := newTestApp(t)
	seedTasks(t, a)

	out := run(t, a, "dia")
	plain := out
	for _, want := range []string{
		"quarta, 8 de julho",
		"1 [A] Escrever seção 4.6",
		"2 [A]  ↳ Rascunhar requisitos",
		"3 [B] Ler capítulo 3",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("dia deveria conter %q:\n%s", want, plain)
		}
	}
}

func TestFeitoResolveNumeroEfemero(t *testing.T) {
	a, _ := newTestApp(t)
	seedTasks(t, a)
	run(t, a, "dia")

	run(t, a, "feito", "2")

	out := run(t, a, "dia")
	if strings.Contains(out, "Rascunhar requisitos") {
		t.Fatalf("subtarefa concluída não deveria aparecer no dia:\n%s", out)
	}
	if !strings.Contains(out, "Escrever seção 4.6") {
		t.Fatalf("as demais tarefas deveriam permanecer:\n%s", out)
	}
}

func TestPriRepriorizaPorNumero(t *testing.T) {
	a, _ := newTestApp(t)
	seedTasks(t, a)
	run(t, a, "dia")

	run(t, a, "pri", "3", "A")

	out := run(t, a, "dia")
	if !strings.Contains(out, "[A] Ler capítulo 3") {
		t.Fatalf("tarefa 3 deveria ter virado A:\n%s", out)
	}
}

func TestDiaJSONExpoeAsProjecoes(t *testing.T) {
	a, _ := newTestApp(t)
	seedTasks(t, a)

	out := run(t, a, "dia", "--json")
	var view struct {
		Tasks struct {
			DayList []string `json:"day_list"`
		} `json:"tasks"`
		Stats struct {
			Streak int `json:"streak"`
		} `json:"stats"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("--json deveria emitir JSON válido: %v\n%s", err, out)
	}
	if len(view.Tasks.DayList) != 3 {
		t.Fatalf("day_list esperada com 3 tarefas, veio %v", view.Tasks.DayList)
	}
}

func TestProcrastinaSugereAMenorSubtarefaA(t *testing.T) {
	a, _ := newTestApp(t)
	seedTasks(t, a)

	out := run(t, a, "procrastina")
	if !strings.Contains(out, "Não.") {
		t.Fatalf("procrastina deveria começar com a intervenção:\n%s", out)
	}
	if !strings.Contains(out, "Rascunhar requisitos") || !strings.Contains(out, "pnn foco 2") {
		t.Fatalf("procrastina deveria apontar a menor subtarefa A com o comando pronto:\n%s", out)
	}
}

func TestProcrastinaAceitaOTypoComum(t *testing.T) {
	a, _ := newTestApp(t)
	for _, alias := range []string{"procastina", "procrastinar", "procastinar"} {
		out := run(t, a, alias)
		if !strings.Contains(out, "Não.") {
			t.Fatalf("a intervenção não pode falhar por erro de digitação (%s):\n%s", alias, out)
		}
	}
}

func TestProcrastinaSemTarefasElogia(t *testing.T) {
	a, _ := newTestApp(t)
	out := run(t, a, "procrastina")
	if !strings.Contains(out, "Não.") {
		t.Fatalf("a intervenção nunca falta:\n%s", out)
	}
	if strings.Contains(out, "pnn foco") {
		t.Fatalf("sem pendências não há foco a sugerir:\n%s", out)
	}
}

func TestDecksCriacaoEListagem(t *testing.T) {
	a, _ := newTestApp(t)
	run(t, a, "deck", "Cálculo::Limites")

	out := run(t, a, "decks")
	if !strings.Contains(out, "Cálculo::Limites") {
		t.Fatalf("decks deveria listar o deck criado:\n%s", out)
	}
}
