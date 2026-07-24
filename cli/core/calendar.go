// Calendário único (spec/SPEC.md §3, Organização) — RF01.
// Estratégia do livro Safren: um só calendário para compromissos com hora.
// `upcoming` = compromissos ainda não encerrados, do mais próximo ao mais
// distante.
package core

import (
	"cmp"
	"slices"
	"time"
)

type Appointment struct {
	Title    string `json:"title"`
	StartsAt string `json:"starts_at"`
	EndsAt   string `json:"ends_at"`
	// importance? (v2, opcional): "important" muda a agenda de lembretes —
	// cascata semanal com antecedência, além da reta final. Ausente = comum.
	Importance string `json:"importance,omitempty"`
}

type CalendarState struct {
	Appointments map[string]Appointment `json:"appointments"`
	Upcoming     []string               `json:"upcoming"`
}

func init() {
	Register("calendar", func(events []Event, p Params) (any, error) {
		return Calendar(events, p)
	})
}

func Calendar(events []Event, p Params) (CalendarState, error) {
	state := CalendarState{Appointments: map[string]Appointment{}, Upcoming: []string{}}

	for _, event := range Normalize(events) {
		switch event.Type {
		case "appointment.created":
			pl, err := decodePayload[struct {
				AppointmentID string `json:"appointment_id"`
				Title         string `json:"title"`
				StartsAt      string `json:"starts_at"`
				EndsAt        string `json:"ends_at"`
				Importance    string `json:"importance"`
			}](event)
			if err != nil {
				return CalendarState{}, err
			}
			state.Appointments[pl.AppointmentID] = Appointment{
				Title: pl.Title, StartsAt: pl.StartsAt, EndsAt: pl.EndsAt, Importance: pl.Importance,
			}

		case "appointment.cancelled":
			pl, err := decodePayload[struct {
				AppointmentID string `json:"appointment_id"`
			}](event)
			if err != nil {
				return CalendarState{}, err
			}
			delete(state.Appointments, pl.AppointmentID) // tombstone
		}
	}

	now, err := time.Parse(time.RFC3339, p.Now)
	if err != nil {
		return CalendarState{}, err
	}

	for id, a := range state.Appointments {
		endsAt, err := time.Parse(time.RFC3339, a.EndsAt)
		if err != nil {
			return CalendarState{}, err
		}
		if !endsAt.Before(now) {
			state.Upcoming = append(state.Upcoming, id)
		}
	}
	slices.SortFunc(state.Upcoming, func(idA, idB string) int {
		a, b := state.Appointments[idA], state.Appointments[idB]
		if a.StartsAt != b.StartsAt {
			return cmp.Compare(a.StartsAt, b.StartsAt)
		}
		return cmp.Compare(idA, idB)
	})
	return state, nil
}
