// Calendário único (spec/SPEC.md §3, Organização) — RF01.
// Estratégia do livro Safren: um só calendário para compromissos com hora.
// `upcoming` = compromissos ainda não encerrados, do mais próximo ao mais
// distante — é o que a extensão mostra como "próximos agendamentos".

import { normalize } from "./envelope.js";

export function project(events, { now }) {
  const appointments = {};

  for (const event of normalize(events)) {
    if (event.type === "appointment.created") {
      const { appointment_id, title, starts_at, ends_at } = event.payload;
      appointments[appointment_id] = { title, starts_at, ends_at };
    } else if (event.type === "appointment.cancelled") {
      delete appointments[event.payload.appointment_id]; // tombstone
    }
  }

  const nowMs = Date.parse(now);
  const upcoming = Object.entries(appointments)
    .filter(([, a]) => Date.parse(a.ends_at) >= nowMs)
    .sort(([idA, a], [idB, b]) => {
      if (a.starts_at !== b.starts_at) return a.starts_at < b.starts_at ? -1 : 1;
      return idA < idB ? -1 : 1;
    })
    .map(([id]) => id);

  return { appointments, upcoming };
}
