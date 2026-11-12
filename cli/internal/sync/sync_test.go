// O sync é testável sem rede: o bucket vira um mapa em memória e dois
// dispositivos viram dois diretórios temporários. O que precisa ser provado é
// convergência (SPEC §2) e idempotência — rodar de novo não muda nada.
package sync

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/store"
)

// --- bucket de mentira ---

type fakeBucket struct {
	objects map[string][]byte
	puts    int
	gets    int
}

func newFakeBucket() *fakeBucket {
	return &fakeBucket{objects: map[string][]byte{}}
}

func (b *fakeBucket) List(_ context.Context, prefix string) ([]Object, error) {
	var out []Object
	for key, body := range b.objects {
		if strings.HasPrefix(key, prefix) {
			// ETag do S3 é o MD5 do corpo; aqui o tamanho basta para mudar
			// quando o conteúdo muda.
			out = append(out, Object{Key: key, ETag: etagOf(body)})
		}
	}
	return out, nil
}

func (b *fakeBucket) Get(_ context.Context, key string) ([]byte, error) {
	b.gets++
	return b.objects[key], nil
}

func (b *fakeBucket) Put(_ context.Context, key string, body []byte) (string, error) {
	b.puts++
	b.objects[key] = append([]byte(nil), body...)
	return etagOf(body), nil
}

func etagOf(body []byte) string {
	return string(rune(len(body)/7)) + string(rune(len(body)%7)) + string(rune(len(body)))
}

// --- dispositivos ---

func newDevice(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func syncNow(t *testing.T, s *store.Store, b *fakeBucket) Result {
	t.Helper()
	result, err := Run(context.Background(), s, b, "pnn/")
	if err != nil {
		t.Fatalf("sync falhou: %v", err)
	}
	return result
}

func types(t *testing.T, s *store.Store) []string {
	t.Helper()
	events, err := s.Events()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range core.Normalize(events) {
		out = append(out, e.Type)
	}
	return out
}

// --- testes ---

// O caso que motiva o desenho: A e B emitem eventos sem se ver e, depois de
// sincronizar, os dois têm o log completo. Nenhum apaga o outro.
func TestDoisDispositivosConvergem(t *testing.T) {
	bucket := newFakeBucket()
	a, b := newDevice(t), newDevice(t)

	if _, err := a.Append("task.created", map[string]any{"task_id": "t1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Append("card.created", map[string]any{"card_id": "c1"}); err != nil {
		t.Fatal(err)
	}

	// A sobe o seu; B sobe o seu e desce o de A; A desce o de B.
	if got := syncNow(t, a, bucket); got.Sent != 1 || got.Received != 0 {
		t.Fatalf("A no primeiro sync: %+v", got)
	}
	if got := syncNow(t, b, bucket); got.Sent != 1 || got.Received != 1 {
		t.Fatalf("B deveria enviar 1 e receber o evento de A: %+v", got)
	}
	if got := syncNow(t, a, bucket); got.Received != 1 {
		t.Fatalf("A deveria receber o evento de B: %+v", got)
	}

	want := []string{"task.created", "card.created"}
	for name, s := range map[string]*store.Store{"A": a, "B": b} {
		got := types(t, s)
		if len(got) != 2 {
			t.Fatalf("%s deveria ter os dois eventos, tem %v", name, got)
		}
		for _, tipo := range want {
			if !slicesContains(got, tipo) {
				t.Fatalf("%s perdeu %s: %v", name, tipo, got)
			}
		}
	}
}

// Rodar de novo sem novidade não envia, não recebe e não reescreve o bucket.
func TestSyncRepetidoNaoFazNada(t *testing.T) {
	bucket := newFakeBucket()
	a := newDevice(t)
	if _, err := a.Append("task.created", map[string]any{"task_id": "t1"}); err != nil {
		t.Fatal(err)
	}

	syncNow(t, a, bucket)
	putsDepoisDoPrimeiro := bucket.puts

	for i := 0; i < 3; i++ {
		if got := syncNow(t, a, bucket); got.Sent != 0 || got.Received != 0 {
			t.Fatalf("sync %d deveria ser no-op, veio %+v", i, got)
		}
	}
	if bucket.puts != putsDepoisDoPrimeiro {
		t.Fatalf("sync sem novidade não deveria reescrever o objeto (%d puts extras)",
			bucket.puts-putsDepoisDoPrimeiro)
	}
}

// Máquina nova (log vazio) baixa tudo — é o caminho de restauração.
func TestReplicaNovaBaixaTudo(t *testing.T) {
	bucket := newFakeBucket()
	a := newDevice(t)
	for _, id := range []string{"t1", "t2", "t3"} {
		if _, err := a.Append("task.created", map[string]any{"task_id": id}); err != nil {
			t.Fatal(err)
		}
	}
	syncNow(t, a, bucket)

	nova := newDevice(t)
	if got := syncNow(t, nova, bucket); got.Received != 3 {
		t.Fatalf("réplica nova deveria receber os 3 eventos, veio %+v", got)
	}
	if got := types(t, nova); len(got) != 3 {
		t.Fatalf("réplica nova ficou com %v", got)
	}
}

// Cada dispositivo escreve só a própria chave — é isso que elimina a escrita
// perdida. Depois de A e B sincronizarem, o objeto de A não pode ter engordado
// com os eventos de B.
func TestCadaDispositivoEscreveSomenteASuaChave(t *testing.T) {
	bucket := newFakeBucket()
	a, b := newDevice(t), newDevice(t)
	if _, err := a.Append("task.created", map[string]any{"task_id": "t1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Append("card.created", map[string]any{"card_id": "c1"}); err != nil {
		t.Fatal(err)
	}
	syncNow(t, a, bucket)
	syncNow(t, b, bucket)
	syncNow(t, a, bucket) // A já viu B; não pode reescrever o objeto de B
	syncNow(t, a, bucket)

	if len(bucket.objects) != 2 {
		t.Fatalf("esperava um objeto por dispositivo, veio %d", len(bucket.objects))
	}
	for key, body := range bucket.objects {
		events := parseLog(decodeObject(body))
		if len(events) != 1 {
			t.Fatalf("%s deveria conter só os eventos do seu dono, tem %d", key, len(events))
		}
		if !strings.Contains(key, events[0].Device) {
			t.Fatalf("%s contém evento do dispositivo %s", key, events[0].Device)
		}
	}
}

// Objeto truncado (upload interrompido) não pode impedir o resto de chegar.
func TestLinhaCorrompidaNoBucketNaoDerrubaOSync(t *testing.T) {
	bucket := newFakeBucket()
	a := newDevice(t)
	if _, err := a.Append("task.created", map[string]any{"task_id": "t1"}); err != nil {
		t.Fatal(err)
	}
	syncNow(t, a, bucket)
	bucket.objects["pnn/dev-corrompido.jsonl"] = []byte("{\"id\":\"x\",\"type\":\"tas")

	nova := newDevice(t)
	if got := syncNow(t, nova, bucket); got.Received != 1 {
		t.Fatalf("o evento íntegro deveria chegar mesmo assim, veio %+v", got)
	}
}

// O cursor é otimização, não verdade: perdido, o sync se refaz sozinho.
func TestCursorPerdidoSeRecupera(t *testing.T) {
	bucket := newFakeBucket()
	a, b := newDevice(t), newDevice(t)
	if _, err := b.Append("card.created", map[string]any{"card_id": "c1"}); err != nil {
		t.Fatal(err)
	}
	syncNow(t, b, bucket)
	syncNow(t, a, bucket)

	if err := a.SetCursor(nil); err != nil {
		t.Fatal(err)
	}
	// Sem cursor, A rebaixa tudo — e o dedup por id garante que nada duplica.
	syncNow(t, a, bucket)
	if got := types(t, a); len(got) != 1 {
		t.Fatalf("rebaixar não pode duplicar: %v", got)
	}
}

func slicesContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// --- compressão (SPEC §5) ---

// O objeto sobe comprimido, na chave .jsonl.zst, e volta igual.
func TestObjetoSobeComprimidoComZstd(t *testing.T) {
	bucket := newFakeBucket()
	a := newDevice(t)
	for i := range 50 {
		if _, err := a.Append("page.edited", map[string]any{"page_id": "p1", "body": strings.Repeat("texto da página, versão inteira. ", 60) + fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	syncNow(t, a, bucket)

	device, _ := a.DeviceID()
	body, ok := bucket.objects["pnn/"+device+".jsonl.zst"]
	if !ok || !bytes.HasPrefix(body, zstdMagic) {
		t.Fatalf("o objeto do dispositivo sobe em .jsonl.zst, comprimido: %v", keys(bucket))
	}
	events, _ := a.Events()
	if raw := encodeLog(events); len(body)*10 > len(raw) {
		t.Fatalf("versões inteiras da mesma página comprimem muito: %d → %d bytes", len(raw), len(body))
	}
	if got := len(parseLog(decodeObject(body))); got != 50 {
		t.Fatalf("o objeto descomprime para os mesmos eventos: %d", got)
	}
}

// Quem lê aceita os dois formatos — pelo conteúdo, não pelo nome: o .jsonl de
// quem não comprime (a extensão) e o .zst gerado pelo encoder da CLI
// (spec/sync/objeto_cli.jsonl.zst, o mesmo que o teste da extensão lê).
func TestLeObjetoPuroEComprimido(t *testing.T) {
	bucket := newFakeBucket()
	plain, err := os.ReadFile(filepath.Join("..", "..", "..", "spec", "sync", "objeto_cli.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	zst, err := os.ReadFile(filepath.Join("..", "..", "..", "spec", "sync", "objeto_cli.jsonl.zst"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decodeObject(zst), plain) {
		t.Fatal("o objeto de referência descomprime para o JSONL de referência")
	}
	bucket.objects["pnn/dev-extensao.jsonl"] = []byte(`{"id":"ext-1","type":"note.captured","v":1,"lc":1,"ts":"2026-07-27T22:00:00.000Z","device":"dev-extensao","payload":{"note_id":"n1","text":"oi"}}` + "\n")
	bucket.objects["pnn/dev-cli-go.jsonl.zst"] = zst
	bucket.objects["pnn/outra-coisa.txt"] = []byte("não é log")

	if got := syncNow(t, newDevice(t), bucket); got.Received != 4 {
		t.Fatalf("1 evento do .jsonl + 3 do .jsonl.zst: %+v", got)
	}
}

// Quem vinha do .jsonl migra sozinho: sem evento novo, o primeiro sync sobe
// o .zst; o .jsonl antigo fica (a política não deixa apagar) e quem baixa os
// dois não duplica nada.
func TestMigraDoJsonlParaZstSemDuplicar(t *testing.T) {
	bucket := newFakeBucket()
	a := newDevice(t)
	for range 3 {
		if _, err := a.Append("task.created", map[string]any{"task_id": "t"}); err != nil {
			t.Fatal(err)
		}
	}
	device, _ := a.DeviceID()
	events, _ := a.Events()
	bucket.objects["pnn/"+device+".jsonl"] = encodeLog(events) // o que a versão anterior subiu
	if err := a.SetCursor([]byte(`{"etags":{},"pushed":3}`)); err != nil {
		t.Fatal(err)
	}

	if got := syncNow(t, a, bucket); got.Sent != 0 {
		t.Fatalf("nenhum evento novo: %+v", got)
	}
	if _, ok := bucket.objects["pnn/"+device+".jsonl.zst"]; !ok {
		t.Fatalf("o .zst sobe mesmo sem evento novo: %v", keys(bucket))
	}
	nova := newDevice(t)
	if got := syncNow(t, nova, bucket); got.Received != 3 {
		t.Fatalf("os dois objetos do mesmo dispositivo não duplicam: %+v", got)
	}
}

// Frame zstd cortado no meio (o PUT do S3 é atômico, mas um objeto pode ser
// copiado à mão): não derruba o sync, e os outros objetos chegam. Do cortado,
// só sai o que fecha bloco inteiro (até 128 KB) — num objeto pequeno, nada.
func TestZstTruncadoNaoDerrubaOSync(t *testing.T) {
	bucket := newFakeBucket()
	var lines []core.Event
	for i := range 200 {
		lines = append(lines, core.Event{ID: fmt.Sprintf("e%03d", i), Type: "task.created", V: 1, LC: int64(i + 1),
			TS: "2026-07-27T22:00:00.000Z", Device: "dev-x", Payload: []byte(fmt.Sprintf(`{"task_id":"t%d","title":"%s"}`, i, strings.Repeat("x", 200)))})
	}
	body, err := encodeObject(lines)
	if err != nil {
		t.Fatal(err)
	}
	bucket.objects["pnn/dev-x.jsonl.zst"] = body[:len(body)*2/3]
	bucket.objects["pnn/dev-y.jsonl"] = []byte(`{"id":"y1","type":"task.created","v":1,"lc":1,"ts":"2026-07-27T22:00:00.000Z","device":"dev-y","payload":{"task_id":"y"}}` + "\n")

	if got := syncNow(t, newDevice(t), bucket); got.Received < 1 {
		t.Fatalf("o objeto íntegro chega mesmo com o outro cortado: %+v", got)
	}
}

func keys(b *fakeBucket) []string {
	var out []string
	for key := range b.objects {
		out = append(out, key)
	}
	return out
}
