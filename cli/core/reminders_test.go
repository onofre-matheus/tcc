package core

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestReminderOffsets(t *testing.T) {
	if got := ReminderOffsets(""); !reflect.DeepEqual(got, []int{1440, 60, 10, 0}) {
		t.Fatalf("comum: %v", got)
	}
	imp := ReminderOffsets("important")
	if want := []int{80640, 70560, 60480, 50400, 40320, 30240, 20160, 10080}; !reflect.DeepEqual(imp[:8], want) {
		t.Fatalf("cascata semanal: %v", imp[:8])
	}
	if want := []int{1440, 60, 10, 0}; !reflect.DeepEqual(imp[len(imp)-4:], want) {
		t.Fatalf("reta final: %v", imp[len(imp)-4:])
	}
}

func TestReminderLabel(t *testing.T) {
	cases := map[int]string{
		10080: "Falta 1 semana",
		20160: "Faltam 2 semanas",
		1440:  "Falta 1 dia",
		60:    "Falta 1 hora",
		10:    "Faltam 10 minutos",
		0:     "Começa agora",
	}
	for offset, want := range cases {
		if got := ReminderLabel(offset); got != want {
			t.Errorf("ReminderLabel(%d) = %q, quer %q", offset, got, want)
		}
	}
}

func apptCreated(apptID, start, end, importance string) Event {
	payload := map[string]any{"appointment_id": apptID, "title": apptID, "starts_at": start, "ends_at": end}
	v := 1
	if importance != "" {
		payload["importance"] = importance
		v = 2
	}
	raw, _ := json.Marshal(payload)
	return Event{ID: "e-" + apptID, Type: "appointment.created", V: v, LC: 1, TS: start, Device: "dev", Payload: raw}
}

func offsetsOf(pending []Reminder) []int {
	got := make([]int, len(pending))
	for i, r := range pending {
		got[i] = r.OffsetMinutes
	}
	return got
}

func TestRemindersPending(t *testing.T) {
	now := "2026-07-24T12:00:00Z"

	t.Run("comum lembra na reta final quando o instante chega", func(t *testing.T) {
		// Começa em 1h: "1 dia antes" já passou; ficam 1 hora / 10 min / na hora.
		events := []Event{apptCreated("a", "2026-07-24T13:00:00Z", "2026-07-24T14:00:00Z", "")}
		state, err := Reminders(events, Params{Now: now})
		if err != nil {
			t.Fatal(err)
		}
		if got := offsetsOf(state.Pending); !reflect.DeepEqual(got, []int{60, 10, 0}) {
			t.Fatalf("offsets = %v", got)
		}
		if state.Pending[0].Label != "Falta 1 hora" {
			t.Fatalf("label = %q", state.Pending[0].Label)
		}
	})

	t.Run("importante inclui a semanal e ordena do mais próximo ao mais distante", func(t *testing.T) {
		events := []Event{apptCreated("a", "2026-07-31T15:00:00Z", "2026-07-31T17:00:00Z", "important")}
		state, err := Reminders(events, Params{Now: now})
		if err != nil {
			t.Fatal(err)
		}
		if got := offsetsOf(state.Pending); !reflect.DeepEqual(got, []int{10080, 1440, 60, 10, 0}) {
			t.Fatalf("offsets = %v", got)
		}
	})

	t.Run("compromisso passado não gera lembrete", func(t *testing.T) {
		events := []Event{apptCreated("a", "2026-07-20T09:00:00Z", "2026-07-20T10:00:00Z", "")}
		state, err := Reminders(events, Params{Now: now})
		if err != nil {
			t.Fatal(err)
		}
		if len(state.Pending) != 0 {
			t.Fatalf("esperava vazio, veio %v", state.Pending)
		}
	})
}
