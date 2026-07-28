// Notificações de desktop das sessões (spec/CLI.md §4): o timer do foco, o fim
// da pausa e o check-in de atenção têm de alcançar o usuário mesmo com o
// terminal atrás de outra janela — é o mesmo papel que a extensão cumpre no
// navegador, agora na CLI (paridade).
//
// Regra de ouro: notificação é enfeite. Nenhuma falha aqui — sem daemon, sem
// D-Bus, sem alto-falante — pode derrubar a sessão, e nenhuma espera pode
// travá-la: todo disparo sai em segundo plano e o erro morre no caminho, já
// que não há nada que o usuário no meio de um bloco de foco possa fazer a
// respeito. Quem está encerrando o programa chama Flush para não cortar o
// aviso na saída.
package notify

import (
	"os"
	"sync"
	"time"

	"github.com/gen2brain/beeep"
)

func init() { beeep.AppName = "Procrastina Não" }

var pending sync.WaitGroup

// Send é a notificação comum (check-in de atenção): aparece, apita e some.
func Send(title, body string) {
	fire(func() error {
		return orWindows(beeep.Notify(title, body, ""), title, body, false)
	})
}

// Alarm é o despertador: o timer tocou. Vai como urgente — não some sozinha
// enquanto o usuário estiver longe do terminal, que é justamente o caso.
func Alarm(title, body string) {
	fire(func() error {
		return orWindows(beeep.Alert(title, body, ""), title, body, true)
	})
}

// Flush espera os disparos em curso, para que o último aviso não seja cortado
// pelo fim do programa. Passado o prazo, desiste em silêncio.
func Flush(timeout time.Duration) {
	done := make(chan struct{})
	go func() { pending.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// Silenced desliga som e notificação (PNN_SILENCIO=1) — sessão em biblioteca,
// gravação de tela, ou simplesmente preferência.
func Silenced() bool { return os.Getenv("PNN_SILENCIO") != "" }

func fire(fn func() error) {
	if Silenced() {
		return
	}
	pending.Add(1)
	go func() {
		defer pending.Done()
		_ = fn() // ver a regra de ouro no topo do arquivo
	}()
}
