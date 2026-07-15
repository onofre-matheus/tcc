// Caixa de entrada de triagem (spec/SPEC.md §4.6) — RF04.
// Notas e distrações capturadas e ainda não triadas, em ordem de captura.
package core

type Note struct {
	Text      string  `json:"text"`
	URL       *string `json:"url"`
	PageTitle *string `json:"page_title"`
}

type Distraction struct {
	Text      string `json:"text"`
	SessionID string `json:"session_id"`
}

type InboxState struct {
	Notes               map[string]Note        `json:"notes"`
	Distractions        map[string]Distraction `json:"distractions"`
	PendingNotes        []string               `json:"pending_notes"`
	PendingDistractions []string               `json:"pending_distractions"`
}

func init() {
	Register("inbox", func(events []Event, _ Params) (any, error) {
		return Inbox(events)
	})
}

func Inbox(events []Event) (InboxState, error) {
	state := InboxState{
		Notes:               map[string]Note{},
		Distractions:        map[string]Distraction{},
		PendingNotes:        []string{},
		PendingDistractions: []string{},
	}
	var noteOrder, distractionOrder []string
	triagedNotes := map[string]bool{}
	triagedDistractions := map[string]bool{}

	for _, event := range Normalize(events) {
		switch event.Type {
		case "note.captured":
			pl, err := decodePayload[struct {
				NoteID    string  `json:"note_id"`
				Text      string  `json:"text"`
				URL       *string `json:"url"`
				PageTitle *string `json:"page_title"`
			}](event)
			if err != nil {
				return InboxState{}, err
			}
			state.Notes[pl.NoteID] = Note{Text: pl.Text, URL: pl.URL, PageTitle: pl.PageTitle}
			noteOrder = append(noteOrder, pl.NoteID)

		case "note.triaged":
			pl, err := decodePayload[struct {
				NoteID string `json:"note_id"`
			}](event)
			if err != nil {
				return InboxState{}, err
			}
			triagedNotes[pl.NoteID] = true

		case "distraction.captured":
			pl, err := decodePayload[struct {
				DistractionID string `json:"distraction_id"`
				SessionID     string `json:"session_id"`
				Text          string `json:"text"`
			}](event)
			if err != nil {
				return InboxState{}, err
			}
			state.Distractions[pl.DistractionID] = Distraction{Text: pl.Text, SessionID: pl.SessionID}
			distractionOrder = append(distractionOrder, pl.DistractionID)

		case "distraction.triaged":
			pl, err := decodePayload[struct {
				DistractionID string `json:"distraction_id"`
			}](event)
			if err != nil {
				return InboxState{}, err
			}
			triagedDistractions[pl.DistractionID] = true
		}
	}

	for _, id := range noteOrder {
		if !triagedNotes[id] {
			state.PendingNotes = append(state.PendingNotes, id)
		}
	}
	for _, id := range distractionOrder {
		if !triagedDistractions[id] {
			state.PendingDistractions = append(state.PendingDistractions, id)
		}
	}
	return state, nil
}
