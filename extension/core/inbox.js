// Caixa de entrada de triagem (spec/SPEC.md §3, Captura e triagem) — RF04.
// Notas e distrações capturadas e ainda não triadas, em ordem de captura.

import { normalize } from "./envelope.js";

export function project(events) {
  const notes = {};
  const distractions = {};
  const noteOrder = [];
  const distractionOrder = [];
  const triagedNotes = new Set();
  const triagedDistractions = new Set();

  for (const event of normalize(events)) {
    const { payload } = event;
    switch (event.type) {
      case "note.captured":
        notes[payload.note_id] = {
          text: payload.text,
          url: payload.url ?? null,
          page_title: payload.page_title ?? null,
        };
        noteOrder.push(payload.note_id);
        break;
      case "note.triaged":
        triagedNotes.add(payload.note_id);
        break;
      case "distraction.captured":
        distractions[payload.distraction_id] = {
          text: payload.text,
          session_id: payload.session_id,
        };
        distractionOrder.push(payload.distraction_id);
        break;
      case "distraction.triaged":
        triagedDistractions.add(payload.distraction_id);
        break;
    }
  }

  return {
    notes,
    distractions,
    pending_notes: noteOrder.filter((id) => !triagedNotes.has(id)),
    pending_distractions: distractionOrder.filter(
      (id) => !triagedDistractions.has(id)
    ),
  };
}
