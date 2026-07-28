// Sincronização via bucket S3 (spec/SPEC.md §5, adaptado).
//
// A spec previa um `POST /sync` com sequência global no servidor. Aqui o
// servidor é um bucket burro — o que combina ainda melhor com o espírito da
// spec, já que ele continua sem ler `payload` e sem regra de negócio nenhuma.
//
// A decisão que faz isso funcionar sem servidor é o layout:
//
//	<prefixo>/<device>.jsonl — um objeto por dispositivo, e cada dispositivo
//	                           escreve SOMENTE o seu
//
// Como dois clientes nunca gravam a mesma chave, não existe escrita perdida —
// o problema clássico do "push do arquivo inteiro", em que o último a subir
// apaga em silêncio o que o outro emitiu. Não é preciso lock, ETag
// condicional nem retry: baixar tudo e unir é sempre seguro, porque evento é
// fato imutável com id único e a união de dois logs é o log completo. A ordem
// total e o dedup ficam onde já estavam, em core.Normalize (SPEC §2).
package sync

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/store"
)

// Object é a entrada do bucket que o sync enxerga: chave e versão.
type Object struct {
	Key  string
	ETag string
}

// Bucket é o mínimo que o sync precisa de um S3 — três chamadas. A interface
// existe para os testes rodarem sem rede (ver fakeBucket).
type Bucket interface {
	List(ctx context.Context, prefix string) ([]Object, error)
	Get(ctx context.Context, key string) ([]byte, error)
	Put(ctx context.Context, key string, body []byte) (etag string, err error)
}

// Result é o que `pnn sync` imprime: ▲ enviados · ▼ recebidos.
type Result struct {
	Sent     int
	Received int
}

// cursor guarda o que já foi visto, só para poupar download e upload. Nunca é
// fonte de verdade: perdido ou errado, o pior que acontece é baixar de novo o
// que já se tem, e o dedup por id absorve.
type cursor struct {
	ETags  map[string]string `json:"etags"`  // chave do objeto → ETag baixado
	Pushed int               `json:"pushed"` // eventos meus já confirmados no bucket
}

// Run executa um ciclo completo: baixa o que mudou, funde, sobe o que é meu.
func Run(ctx context.Context, s *store.Store, bucket Bucket, prefix string) (Result, error) {
	device, err := s.DeviceID()
	if err != nil {
		return Result{}, err
	}
	local, err := s.Events()
	if err != nil {
		return Result{}, err
	}
	cur := loadCursor(s, len(local))

	objects, err := bucket.List(ctx, prefix)
	if err != nil {
		return Result{}, fmt.Errorf("listar o bucket: %w", err)
	}

	// ▼ Baixa o que mudou desde a última vez (inclusive o próprio objeto: é o
	// que permite restaurar a réplica inteira numa instalação nova).
	var incoming []core.Event
	etags := map[string]string{}
	for _, obj := range objects {
		if !strings.HasSuffix(obj.Key, ".jsonl") {
			continue // o bucket pode ser compartilhado com outra coisa
		}
		if cur.ETags[obj.Key] == obj.ETag && obj.ETag != "" {
			etags[obj.Key] = obj.ETag
			continue
		}
		body, err := bucket.Get(ctx, obj.Key)
		if err != nil {
			return Result{}, fmt.Errorf("baixar %s: %w", obj.Key, err)
		}
		incoming = append(incoming, parseLog(body)...)
		etags[obj.Key] = obj.ETag
	}

	received, err := s.Merge(incoming)
	if err != nil {
		return Result{}, err
	}

	// ▲ Sobe só os eventos que este dispositivo autorou: o objeto de cada um é
	// o registro do que ele emitiu, sem cópias do que veio dos outros.
	merged, err := s.Events()
	if err != nil {
		return Result{}, err
	}
	mine := filterDevice(merged, device)

	result := Result{Received: received, Sent: len(mine) - cur.Pushed}
	if result.Sent < 0 {
		result.Sent = 0 // cursor à frente do log; nada a enviar
	}
	// Sobe quando há evento novo meu — ou quando meu objeto sumiu do bucket
	// (apagado à mão), caso em que o cursor mente ao dizer que já foi enviado.
	key := prefix + device + ".jsonl"
	_, inBucket := etags[key]
	if len(mine) > 0 && (result.Sent > 0 || !inBucket) {
		etag, err := bucket.Put(ctx, key, encodeLog(mine))
		if err != nil {
			return Result{}, fmt.Errorf("enviar %s: %w", key, err)
		}
		etags[key] = etag
		cur.Pushed = len(mine)
	}

	saveCursor(s, cursor{ETags: etags, Pushed: cur.Pushed})
	return result, nil
}

// loadCursor lê o cursor gravado. Com o log local vazio ele é descartado: o
// dispositivo perdeu (ou nunca teve) a réplica e precisa baixar tudo de novo —
// caso contrário o bucket ficaria "sem novidades" e a réplica, vazia para
// sempre.
func loadCursor(s *store.Store, localEvents int) cursor {
	empty := cursor{ETags: map[string]string{}}
	if localEvents == 0 {
		return empty
	}
	raw, err := s.Cursor()
	if err != nil || len(raw) == 0 {
		return empty
	}
	var cur cursor
	if err := json.Unmarshal(raw, &cur); err != nil {
		return empty
	}
	if cur.ETags == nil {
		cur.ETags = map[string]string{}
	}
	return cur
}

// saveCursor grava a otimização; falhar aqui não invalida o sync que acabou de
// acontecer — na próxima vez baixa-se de novo o que já se tem.
func saveCursor(s *store.Store, cur cursor) {
	if raw, err := json.Marshal(cur); err == nil {
		_ = s.SetCursor(raw)
	}
}

// parseLog lê um objeto .jsonl. Linha ilegível é pulada, não aborta o sync:
// um objeto truncado por um upload interrompido não pode impedir o
// dispositivo de receber os outros.
func parseLog(body []byte) []core.Event {
	var events []core.Event
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event core.Event
		if err := json.Unmarshal(line, &event); err != nil || event.ID == "" {
			continue
		}
		events = append(events, event)
	}
	return events
}

func encodeLog(events []core.Event) []byte {
	var buf bytes.Buffer
	for _, event := range events {
		line, err := json.Marshal(event)
		if err != nil {
			continue
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

func filterDevice(events []core.Event, device string) []core.Event {
	var mine []core.Event
	for _, event := range events {
		if event.Device == device {
			mine = append(mine, event)
		}
	}
	return mine
}
