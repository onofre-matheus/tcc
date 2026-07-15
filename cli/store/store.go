// Réplica local do log de eventos (spec/SPEC.md §1, §2, §5; spec/CLI.md §6).
// Fonte de verdade da CLI: o estado é sempre uma projeção deste log.
//
// Layout do diretório (por padrão ~/.pnn):
//
//	events.jsonl — log append-only, um envelope JSON por linha
//	device       — id estável da instalação
//	cursor       — posição confirmada na sequência global do sync (JSON cru)
//	lock         — flock que serializa escritores (ex.: captura one-shot
//	               disparada de outro terminal durante uma sessão de foco)
//
// O relógio de Lamport não é persistido à parte: todo lc já visto pelo
// dispositivo está no log (via Append ou Merge), então o contador corrente é
// max(lc do log) — equivalente ao contador separado do cliente JS.
package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/onofre-matheus/tcc/cli/core"
)

const tsLayout = "2006-01-02T15:04:05.000Z"

type Store struct {
	dir   string
	clock func() time.Time // injetável para testes determinísticos
	genID func() string
}

type Option func(*Store)

func WithClock(clock func() time.Time) Option {
	return func(s *Store) { s.clock = clock }
}

func WithGenID(genID func() string) Option {
	return func(s *Store) { s.genID = genID }
}

// Open prepara a réplica no diretório dado, criando-o se preciso.
func Open(dir string, opts ...Option) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("criar diretório da réplica: %w", err)
	}
	s := &Store{dir: dir, clock: time.Now, genID: UUIDv7}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

func (s *Store) path(name string) string { return filepath.Join(s.dir, name) }

// lock serializa escritores entre processos via flock; devolve o unlock.
func (s *Store) lock() (func(), error) {
	f, err := os.OpenFile(s.path("lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("obter lock da réplica: %w", err)
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// Events lê o log completo (já em ordem de gravação; a ordem total é
// responsabilidade de core.Normalize dentro das projeções).
func (s *Store) Events() ([]core.Event, error) {
	f, err := os.Open(s.path("events.jsonl"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var events []core.Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var event core.Event
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, fmt.Errorf("linha corrompida em events.jsonl: %w", err)
		}
		events = append(events, event)
	}
	return events, scanner.Err()
}

// DeviceID devolve o id estável da instalação, criando-o na primeira chamada.
func (s *Store) DeviceID() (string, error) {
	unlock, err := s.lock()
	if err != nil {
		return "", err
	}
	defer unlock()
	return s.deviceIDLocked()
}

func (s *Store) deviceIDLocked() (string, error) {
	raw, err := os.ReadFile(s.path("device"))
	if err == nil && len(raw) > 0 {
		return string(raw), nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	device := "dev-" + s.genID()
	if err := os.WriteFile(s.path("device"), []byte(device), 0o644); err != nil {
		return "", err
	}
	return device, nil
}

// Append emite um evento local (payload v1): lc = max(lc do log) + 1,
// id/ts do dispositivo.
func (s *Store) Append(eventType string, payload any) (core.Event, error) {
	return s.AppendV(eventType, 1, payload)
}

// AppendV emite um evento com versão de payload explícita (ex.:
// session.ended v2, que carrega reason?).
func (s *Store) AppendV(eventType string, v int, payload any) (core.Event, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return core.Event{}, fmt.Errorf("serializar payload: %w", err)
	}

	unlock, err := s.lock()
	if err != nil {
		return core.Event{}, err
	}
	defer unlock()

	events, err := s.Events()
	if err != nil {
		return core.Event{}, err
	}
	device, err := s.deviceIDLocked()
	if err != nil {
		return core.Event{}, err
	}

	event := core.Event{
		ID:      s.genID(),
		Type:    eventType,
		V:       v,
		LC:      maxLC(events) + 1,
		TS:      s.clock().UTC().Format(tsLayout),
		Device:  device,
		Payload: raw,
	}

	line, err := json.Marshal(event)
	if err != nil {
		return core.Event{}, err
	}
	f, err := os.OpenFile(s.path("events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return core.Event{}, err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return core.Event{}, err
	}
	return event, nil
}

// Merge incorpora eventos recebidos do sync: deduplica por id e regrava o log
// normalizado (o Lamport avança por consequência, já que max(lc) cresce).
func (s *Store) Merge(incoming []core.Event) (int, error) {
	unlock, err := s.lock()
	if err != nil {
		return 0, err
	}
	defer unlock()

	events, err := s.Events()
	if err != nil {
		return 0, err
	}
	seen := make(map[string]bool, len(events))
	for _, e := range events {
		seen[e.ID] = true
	}

	added := 0
	for _, e := range incoming {
		if seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		events = append(events, e)
		added++
	}

	if err := s.rewrite(core.Normalize(events)); err != nil {
		return 0, err
	}
	return added, nil
}

// Outbox lista os envelopes locais candidatos a upload. Por ora, todos: o
// servidor deduplica por id e não lê payload, então reenviar é idempotente.
func (s *Store) Outbox() ([]core.Event, error) {
	return s.Events()
}

// Cursor devolve a posição confirmada do sync (JSON cru; nil = nunca sincronizou).
func (s *Store) Cursor() (json.RawMessage, error) {
	raw, err := os.ReadFile(s.path("cursor"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return raw, err
}

func (s *Store) SetCursor(cursor json.RawMessage) error {
	return os.WriteFile(s.path("cursor"), cursor, 0o644)
}

// rewrite substitui o log inteiro atomicamente (arquivo temporário + rename).
func (s *Store) rewrite(events []core.Event) error {
	tmp, err := os.CreateTemp(s.dir, "events-*.jsonl")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	w := bufio.NewWriter(tmp)
	for _, event := range events {
		line, err := json.Marshal(event)
		if err != nil {
			tmp.Close()
			return err
		}
		if _, err := w.Write(append(line, '\n')); err != nil {
			tmp.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path("events.jsonl"))
}

func maxLC(events []core.Event) int64 {
	var max int64
	for _, e := range events {
		if e.LC > max {
			max = e.LC
		}
	}
	return max
}
