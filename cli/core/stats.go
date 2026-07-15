// Consistência de estudo (spec/SPEC.md §4.4) — RF06.
// Dia estudado = dia local com ≥1 card.reviewed ou session.ended.
// Streak = dias estudados consecutivos terminando em hoje ou ontem.
package core

type StatsState struct {
	DaysStudied  int `json:"days_studied"`
	Streak       int `json:"streak"`
	ReviewsToday int `json:"reviews_today"`
}

func init() {
	Register("stats", func(events []Event, p Params) (any, error) {
		return Stats(events, p)
	})
}

func Stats(events []Event, p Params) (StatsState, error) {
	today, err := LocalDate(p.Now, p.TZ)
	if err != nil {
		return StatsState{}, err
	}

	studiedDays := map[string]bool{}
	reviewsToday := 0

	for _, event := range Normalize(events) {
		if event.Type != "card.reviewed" && event.Type != "session.ended" {
			continue
		}
		day, err := LocalDate(event.TS, p.TZ)
		if err != nil {
			return StatsState{}, err
		}
		studiedDays[day] = true
		if event.Type == "card.reviewed" && day == today {
			reviewsToday++
		}
	}

	cursor := today
	if !studiedDays[cursor] {
		yesterday, err := AddDays(today, -1)
		if err != nil {
			return StatsState{}, err
		}
		cursor = yesterday
	}
	streak := 0
	for studiedDays[cursor] {
		streak++
		var err error
		cursor, err = AddDays(cursor, -1)
		if err != nil {
			return StatsState{}, err
		}
	}

	return StatsState{DaysStudied: len(studiedDays), Streak: streak, ReviewsToday: reviewsToday}, nil
}
