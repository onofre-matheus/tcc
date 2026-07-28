package notify

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSilencioNaoDispara(t *testing.T) {
	t.Setenv("PNN_SILENCIO", "1")

	var chamou atomic.Bool
	fire(func() error { chamou.Store(true); return nil })
	Flush(time.Second)

	if chamou.Load() {
		t.Fatal("com PNN_SILENCIO nada deveria ser disparado")
	}
}

// O disparo não pode segurar o laço da TUI: quem chama volta na hora, e só
// quem está encerrando o programa espera (Flush).
func TestDisparoNaoBloqueiaEFlushEspera(t *testing.T) {
	t.Setenv("PNN_SILENCIO", "")

	var terminou atomic.Bool
	inicio := time.Now()
	fire(func() error {
		time.Sleep(150 * time.Millisecond)
		terminou.Store(true)
		return nil
	})

	if elapsed := time.Since(inicio); elapsed > 50*time.Millisecond {
		t.Fatalf("o disparo deveria voltar na hora, levou %s", elapsed)
	}
	if terminou.Load() {
		t.Fatal("o trabalho deveria estar rodando em segundo plano")
	}

	Flush(2 * time.Second)
	if !terminou.Load() {
		t.Fatal("Flush deveria esperar o disparo pendente")
	}
}

// Um desktop travado não pode segurar a saída do programa para sempre.
func TestFlushDesisteNoPrazo(t *testing.T) {
	t.Setenv("PNN_SILENCIO", "")

	fire(func() error { time.Sleep(3 * time.Second); return nil })

	inicio := time.Now()
	Flush(100 * time.Millisecond)
	if elapsed := time.Since(inicio); elapsed > time.Second {
		t.Fatalf("Flush deveria desistir no prazo, esperou %s", elapsed)
	}
	Flush(5 * time.Second) // não deixa a goroutine vazar para o próximo teste
}

func TestPsQuoteEscapaAspaSimples(t *testing.T) {
	got := psQuote("Você está na tarefa \"a'b\"?\nsegunda linha")

	if strings.Contains(got, "\n") {
		t.Fatalf("a quebra de linha viraria fim de comando no script: %q", got)
	}
	if !strings.Contains(got, "a''b") {
		t.Fatalf("a aspa simples se escapa duplicando no PowerShell: %q", got)
	}
}
