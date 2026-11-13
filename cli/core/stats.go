// Consistência de estudo (spec/SPEC.md §4.4) — RF06.
// Dia estudado = dia local com ≥1 card.reviewed, session.ended ou, desde o
// 1.7, leitura (book.progressed, annotation.created).
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
		switch event.Type {
		case "card.reviewed", "session.ended", "book.progressed", "annotation.created":
		default:
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
