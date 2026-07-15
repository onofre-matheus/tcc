// Retrospectiva semanal (spec/SPEC.md §4.7) — a revisão à la Safren: o que
// foi feito, quando, e o que interrompeu. A semana projetada (seg–dom) é a
// que contém a data local de `now`; navegar no histórico é variar o `now`.
package core

import (
	"math"
	"time"
)

type ReviewDay struct {
	Reviews        int `json:"reviews"`
	FocusMinutes   int `json:"focus_minutes"`
	PauseMinutes   int `json:"pause_minutes"`
	Completed      int `json:"completed"`
	Interrupted    int `json:"interrupted"`
	Checkins       int `json:"checkins"`
	CheckinsOnTask int `json:"checkins_on_task"`
}

type ReviewState struct {
	WeekStart          string               `json:"week_start"`
	WeekEnd            string               `json:"week_end"`
	Days               map[string]ReviewDay `json:"days"`
	Totals             ReviewDay            `json:"totals"`
	Reasons            map[string]int       `json:"reasons"`
	AppointmentMinutes int                  `json:"appointment_minutes"`
}

func init() {
	Register("review", func(events []Event, p Params) (any, error) {
		return Review(events, p)
	})
}

func Review(events []Event, p Params) (ReviewState, error) {
	today, err := LocalDate(p.Now, p.TZ)
	if err != nil {
		return ReviewState{}, err
	}
	weekStart, err := mondayOf(today)
	if err != nil {
		return ReviewState{}, err
	}
	weekEnd, err := AddDays(weekStart, 6)
	if err != nil {
		return ReviewState{}, err
	}

	state := ReviewState{
		WeekStart: weekStart,
		WeekEnd:   weekEnd,
		Days:      make(map[string]ReviewDay, 7),
		Reasons:   map[string]int{},
	}
	for i := 0; i < 7; i++ {
		day, err := AddDays(weekStart, i)
		if err != nil {
			return ReviewState{}, err
		}
		state.Days[day] = ReviewDay{}
	}
	inWeek := func(day string) bool { return day >= weekStart && day <= weekEnd }

	type interval struct {
		startTS, endTS  string
		outcome, reason string
	}
	sessions := map[string]*interval{}
	pauses := map[string]*interval{}
	ensure := func(m map[string]*interval, id string) *interval {
		if m[id] == nil {
			m[id] = &interval{}
		}
		return m[id]
	}

	for _, event := range Normalize(events) {
		switch event.Type {
		case "session.started":
			pl, err := decodePayload[struct {
				SessionID string `json:"session_id"`
			}](event)
			if err != nil {
				return ReviewState{}, err
			}
			ensure(sessions, pl.SessionID).startTS = event.TS

		case "session.ended":
			pl, err := decodePayload[struct {
				SessionID string  `json:"session_id"`
				Outcome   string  `json:"outcome"`
				Reason    *string `json:"reason"` // v2; v1 ≡ sem motivo
			}](event)
			if err != nil {
				return ReviewState{}, err
			}
			s := ensure(sessions, pl.SessionID)
			s.endTS = event.TS
			s.outcome = pl.Outcome
			if pl.Reason != nil {
				s.reason = *pl.Reason
			}

		case "pause.started":
			pl, err := decodePayload[struct {
				PauseID string `json:"pause_id"`
			}](event)
			if err != nil {
				return ReviewState{}, err
			}
			ensure(pauses, pl.PauseID).startTS = event.TS

		case "pause.ended":
			pl, err := decodePayload[struct {
				PauseID string `json:"pause_id"`
			}](event)
			if err != nil {
				return ReviewState{}, err
			}
			ensure(pauses, pl.PauseID).endTS = event.TS

		case "pause.logged":
			// Retroativa: o tempo do domínio vem no payload, não no ts.
			pl, err := decodePayload[struct {
				PauseID  string `json:"pause_id"`
				StartsAt string `json:"starts_at"`
				EndsAt   string `json:"ends_at"`
			}](event)
			if err != nil {
				return ReviewState{}, err
			}
			p := ensure(pauses, pl.PauseID)
			p.startTS, p.endTS = pl.StartsAt, pl.EndsAt

		case "card.reviewed":
			day, err := LocalDate(event.TS, p.TZ)
			if err != nil {
				return ReviewState{}, err
			}
			if inWeek(day) {
				d := state.Days[day]
				d.Reviews++
				state.Days[day] = d
			}

		case "checkin.logged":
			// Automonitoramento de atenção: "na tarefa em X de Y checagens".
			pl, err := decodePayload[struct {
				OnTask bool `json:"on_task"`
			}](event)
			if err != nil {
				return ReviewState{}, err
			}
			day, err := LocalDate(event.TS, p.TZ)
			if err != nil {
				return ReviewState{}, err
			}
			if inWeek(day) {
				d := state.Days[day]
				d.Checkins++
				if pl.OnTask {
					d.CheckinsOnTask++
				}
				state.Days[day] = d
			}

		case "appointment.created":
			pl, err := decodePayload[struct {
				StartsAt string `json:"starts_at"`
				EndsAt   string `json:"ends_at"`
			}](event)
			if err != nil {
				return ReviewState{}, err
			}
			day, err := LocalDate(pl.StartsAt, p.TZ)
			if err != nil {
				return ReviewState{}, err
			}
			if minutes, ok := minutesBetween(pl.StartsAt, pl.EndsAt); ok && inWeek(day) {
				state.AppointmentMinutes += minutes
			}
		}
	}

	for _, s := range sessions {
		if s.startTS == "" || s.endTS == "" {
			continue // par não fechado é ignorado
		}
		minutes, ok := minutesBetween(s.startTS, s.endTS)
		if !ok {
			continue
		}
		day, err := LocalDate(s.startTS, p.TZ)
		if err != nil {
			return ReviewState{}, err
		}
		if !inWeek(day) {
			continue
		}
		d := state.Days[day]
		d.FocusMinutes += minutes
		if s.outcome == "interrupted" {
			d.Interrupted++
			if s.reason != "" {
				state.Reasons[s.reason]++
			}
		} else {
			d.Completed++
		}
		state.Days[day] = d
	}

	for _, pause := range pauses {
		if pause.startTS == "" || pause.endTS == "" {
			continue
		}
		minutes, ok := minutesBetween(pause.startTS, pause.endTS)
		if !ok {
			continue
		}
		day, err := LocalDate(pause.startTS, p.TZ)
		if err != nil {
			return ReviewState{}, err
		}
		if !inWeek(day) {
			continue
		}
		d := state.Days[day]
		d.PauseMinutes += minutes
		state.Days[day] = d
	}

	for _, d := range state.Days {
		state.Totals.Reviews += d.Reviews
		state.Totals.FocusMinutes += d.FocusMinutes
		state.Totals.PauseMinutes += d.PauseMinutes
		state.Totals.Completed += d.Completed
		state.Totals.Interrupted += d.Interrupted
		state.Totals.Checkins += d.Checkins
		state.Totals.CheckinsOnTask += d.CheckinsOnTask
	}
	return state, nil
}

// mondayOf devolve a segunda-feira da semana que contém a data civil dada.
func mondayOf(isoDate string) (string, error) {
	t, err := time.Parse(civilDate, isoDate)
	if err != nil {
		return "", err
	}
	return AddDays(isoDate, -((int(t.Weekday()) + 6) % 7))
}

// minutesBetween devolve a duração em minutos (arredondada) entre dois
// instantes; ok=false para intervalos vazios ou invertidos, que são ignorados.
func minutesBetween(fromTS, toTS string) (int, bool) {
	from, err := time.Parse(time.RFC3339, fromTS)
	if err != nil {
		return 0, false
	}
	to, err := time.Parse(time.RFC3339, toTS)
	if err != nil {
		return 0, false
	}
	d := to.Sub(from)
	if d <= 0 {
		return 0, false
	}
	return int(math.Round(d.Minutes())), true
}
