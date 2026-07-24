// Lembretes de compromissos (RF: avisar antes) — porta 1:1 da regra pura de
// extension/core/calendar.js. O *quando* lembrar é compartilhado e travado por
// vetor de conformidade; a ENTREGA difere por plataforma: a extensão dispara
// notificação do navegador em background, a CLI exibe os lembretes ao rodar
// `pnn` (não roda com o terminal fechado). Ver spec/SPEC.md.
package core

import (
	"slices"
	"strconv"
	"time"
)

const (
	minutesPerWeek = 7 * 24 * 60
	minutesPerDay  = 24 * 60
	minutesPerHour = 60
)

// near = a reta final (1 dia, 1 hora, 10 min, na hora). weekly = a cascata
// semanal (até 8 semanas) que os compromissos "important" ganham antes disso.
var reminderNear = []int{minutesPerDay, minutesPerHour, 10, 0}
var reminderWeekly = []int{8, 7, 6, 5, 4, 3, 2, 1}

// ReminderOffsets devolve, em minutos antes do início, quando lembrar.
func ReminderOffsets(importance string) []int {
	if importance != "important" {
		return slices.Clone(reminderNear)
	}
	offsets := make([]int, 0, len(reminderWeekly)+len(reminderNear))
	for _, w := range reminderWeekly {
		offsets = append(offsets, w*minutesPerWeek)
	}
	return append(offsets, reminderNear...)
}

// ReminderLabel dá o rótulo humano da antecedência ("Falta 1 semana",
// "Faltam 10 minutos"), concordando em número e gênero como no cliente JS.
func ReminderLabel(offsetMin int) string {
	if offsetMin == 0 {
		return "Começa agora"
	}
	verb := func(n int) string {
		if n == 1 {
			return "Falta"
		}
		return "Faltam"
	}
	switch {
	case offsetMin%minutesPerWeek == 0:
		w := offsetMin / minutesPerWeek
		return verb(w) + " " + strconv.Itoa(w) + plur(w, " semana", " semanas")
	case offsetMin%minutesPerDay == 0:
		d := offsetMin / minutesPerDay
		return verb(d) + " " + strconv.Itoa(d) + plur(d, " dia", " dias")
	case offsetMin%minutesPerHour == 0:
		h := offsetMin / minutesPerHour
		return verb(h) + " " + strconv.Itoa(h) + plur(h, " hora", " horas")
	default:
		return "Faltam " + strconv.Itoa(offsetMin) + " minutos"
	}
}

func plur(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

type Reminder struct {
	AppointmentID string `json:"appointment_id"`
	OffsetMinutes int    `json:"offset_minutes"`
	Label         string `json:"label"`
}

// RemindersState = os lembretes ainda por vir (instante >= now), ordenados do
// mais próximo ao mais distante. É projeção pura do log + `now`: não conhece o
// que já foi notificado (dedupe/entrega são estado de runtime de cada cliente).
type RemindersState struct {
	Pending []Reminder `json:"pending"`
}

func init() {
	Register("reminders", func(events []Event, p Params) (any, error) {
		return Reminders(events, p)
	})
}

func Reminders(events []Event, p Params) (RemindersState, error) {
	calendar, err := Calendar(events, p)
	if err != nil {
		return RemindersState{}, err
	}
	now, err := time.Parse(time.RFC3339, p.Now)
	if err != nil {
		return RemindersState{}, err
	}

	type entry struct {
		r  Reminder
		at time.Time
	}
	var entries []entry
	for id, a := range calendar.Appointments {
		start, err := time.Parse(time.RFC3339, a.StartsAt)
		if err != nil {
			return RemindersState{}, err
		}
		for _, offset := range ReminderOffsets(a.Importance) {
			at := start.Add(-time.Duration(offset) * time.Minute)
			if at.Before(now) {
				continue // lembrete já passou: não está mais por vir
			}
			entries = append(entries, entry{
				r:  Reminder{AppointmentID: id, OffsetMinutes: offset, Label: ReminderLabel(offset)},
				at: at,
			})
		}
	}
	slices.SortFunc(entries, func(x, y entry) int {
		if !x.at.Equal(y.at) {
			return x.at.Compare(y.at)
		}
		if x.r.AppointmentID != y.r.AppointmentID {
			if x.r.AppointmentID < y.r.AppointmentID {
				return -1
			}
			return 1
		}
		return x.r.OffsetMinutes - y.r.OffsetMinutes
	})

	pending := make([]Reminder, len(entries))
	for i, e := range entries {
		pending[i] = e.r
	}
	return RemindersState{Pending: pending}, nil
}
