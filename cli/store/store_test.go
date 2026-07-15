// Espelho de extension/test/log.test.js sobre a réplica em arquivos:
// append monta envelopes válidos, o estado sobrevive à reabertura, merge é
// idempotente e avança o Lamport, e appends concorrentes não se perdem (flock).
package store_test

import (
	"fmt"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/store"
)

// fixture determinístico: ids sequenciais e relógio de parede fixo.
func fixtureStore(t *testing.T, dir string) *store.Store {
	t.Helper()
	n := 0
	s, err := store.Open(dir,
		store.WithGenID(func() string {
			n++
			return fmt.Sprintf("id-%03d", n)
		}),
		store.WithClock(func() time.Time {
			return time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustAppend(t *testing.T, s *store.Store, eventType string, payload any) core.Event {
	t.Helper()
	event, err := s.Append(eventType, payload)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func mustEvents(t *testing.T, s *store.Store) []core.Event {
	t.Helper()
	events, err := s.Events()
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestAppendMontaEnvelopesValidos(t *testing.T) {
	s := fixtureStore(t, t.TempDir())

	a := mustAppend(t, s, "task.created", map[string]any{"task_id": "t1", "title": "A"})
	b := mustAppend(t, s, "task.created", map[string]any{"task_id": "t2", "title": "B"})

	if a.LC != 1 || b.LC != 2 {
		t.Fatalf("lc incremental esperado (1, 2), veio (%d, %d)", a.LC, b.LC)
	}
	if a.V != 1 {
		t.Fatalf("v esperado 1, veio %d", a.V)
	}
	if a.Device == "" || a.Device != b.Device {
		t.Fatalf("device deve ser estável: %q vs %q", a.Device, b.Device)
	}
	if a.Type != "task.created" {
		t.Fatalf("type esperado task.created, veio %q", a.Type)
	}
	if got := string(a.Payload); got != `{"task_id":"t1","title":"A"}` {
		t.Fatalf("payload inesperado: %s", got)
	}
	if events := mustEvents(t, s); len(events) != 2 {
		t.Fatalf("esperados 2 eventos no log, vieram %d", len(events))
	}
}

func TestReaberturaRecuperaLogELamport(t *testing.T) {
	dir := t.TempDir()
	first := fixtureStore(t, dir)
	mustAppend(t, first, "task.created", map[string]any{"task_id": "t1", "title": "A"})

	reopened, err := store.Open(dir, store.WithGenID(func() string { return "id-x" }))
	if err != nil {
		t.Fatal(err)
	}
	next := mustAppend(t, reopened, "task.created", map[string]any{"task_id": "t2", "title": "B"})
	if next.LC != 2 {
		t.Fatalf("contador deveria sobreviver ao restart: lc esperado 2, veio %d", next.LC)
	}
	if events := mustEvents(t, reopened); len(events) != 2 {
		t.Fatalf("esperados 2 eventos, vieram %d", len(events))
	}
	if events := mustEvents(t, reopened); events[0].Device != next.Device {
		t.Fatal("device id deveria sobreviver ao restart")
	}
}

func TestLogAlimentaProjecoesPuras(t *testing.T) {
	s := fixtureStore(t, t.TempDir())
	mustAppend(t, s, "task.created", map[string]any{"task_id": "t1", "title": "A"})
	mustAppend(t, s, "task.prioritized", map[string]any{"task_id": "t1", "priority": "A"})
	mustAppend(t, s, "task.created", map[string]any{"task_id": "t2", "title": "B"})

	state, err := core.Tasks(mustEvents(t, s))
	if err != nil {
		t.Fatal(err)
	}
	if len(state.DayList) != 2 || state.DayList[0] != "t1" || state.DayList[1] != "t2" {
		t.Fatalf("day_list esperada [t1 t2], veio %v", state.DayList)
	}
	if p := state.Tasks["t1"].Priority; p == nil || *p != "A" {
		t.Fatalf("t1 deveria ter prioridade A, veio %v", p)
	}
}

func remoteEvent(id string, lc int64) core.Event {
	return core.Event{
		ID: id, Type: "task.created", V: 1, LC: lc,
		TS: "2026-06-01T00:00:00.000Z", Device: "dev-remote",
		Payload: []byte(fmt.Sprintf(`{"task_id":%q,"title":%q}`, id, id)),
	}
}

func TestMergeDeduplicaEAvancaLamport(t *testing.T) {
	s := fixtureStore(t, t.TempDir())
	mustAppend(t, s, "task.created", map[string]any{"task_id": "t1", "title": "A"}) // lc=1

	added, err := s.Merge([]core.Event{remoteEvent("r1", 5), remoteEvent("r2", 9)})
	if err != nil {
		t.Fatal(err)
	}
	if added != 2 {
		t.Fatalf("esperados 2 adicionados, vieram %d", added)
	}

	next := mustAppend(t, s, "task.created", map[string]any{"task_id": "t3", "title": "C"})
	if next.LC != 10 {
		t.Fatalf("lc esperado max(1, 9) + 1 = 10, veio %d", next.LC)
	}
}

func TestMergeIdempotente(t *testing.T) {
	s := fixtureStore(t, t.TempDir())
	if _, err := s.Merge([]core.Event{remoteEvent("r1", 5), remoteEvent("r2", 9)}); err != nil {
		t.Fatal(err)
	}
	before := mustEvents(t, s)

	added, err := s.Merge([]core.Event{remoteEvent("r1", 5), remoteEvent("r2", 9)})
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 {
		t.Fatalf("reaplicar os mesmos eventos deveria adicionar 0, veio %d", added)
	}
	after := mustEvents(t, s)
	if len(after) != len(before) {
		t.Fatalf("log mudou de %d para %d eventos", len(before), len(after))
	}
}

func TestCursorPersistido(t *testing.T) {
	dir := t.TempDir()
	s := fixtureStore(t, dir)

	cursor, err := s.Cursor()
	if err != nil {
		t.Fatal(err)
	}
	if cursor != nil {
		t.Fatalf("cursor inicial deveria ser null, veio %s", cursor)
	}

	if err := s.SetCursor([]byte(`42`)); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err = reopened.Cursor()
	if err != nil {
		t.Fatal(err)
	}
	if string(cursor) != `42` {
		t.Fatalf("cursor esperado 42, veio %s", cursor)
	}
}

// Duas instâncias (como dois processos: captura durante uma sessão de foco)
// gravando ao mesmo tempo: nenhum evento se perde e os lc não colidem.
func TestAppendsConcorrentesNaoSePerdem(t *testing.T) {
	dir := t.TempDir()
	a, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	const perWriter = 20
	var wg sync.WaitGroup
	for _, s := range []*store.Store{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				if _, err := s.Append("note.captured", map[string]any{"note_id": store.UUIDv7(), "text": "x"}); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()

	events := mustEvents(t, a)
	if len(events) != 2*perWriter {
		t.Fatalf("esperados %d eventos, vieram %d", 2*perWriter, len(events))
	}
	seenLC := map[int64]bool{}
	seenID := map[string]bool{}
	for _, e := range events {
		if seenLC[e.LC] {
			t.Fatalf("lc duplicado: %d", e.LC)
		}
		if seenID[e.ID] {
			t.Fatalf("id duplicado: %s", e.ID)
		}
		seenLC[e.LC] = true
		seenID[e.ID] = true
	}
}

func TestUUIDv7FormatoEUnicidade(t *testing.T) {
	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	a, b := store.UUIDv7(), store.UUIDv7()
	if !pattern.MatchString(a) {
		t.Fatalf("uuid fora do formato v7: %s", a)
	}
	if a == b {
		t.Fatal("dois uuids consecutivos não deveriam colidir")
	}
}
