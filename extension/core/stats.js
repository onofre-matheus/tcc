// Consistência de estudo (spec/SPEC.md §4.4) — RF06.
// Dia estudado = dia local com ≥1 card.reviewed ou session.ended.
// Streak = dias estudados consecutivos terminando em hoje ou ontem.

import { normalize } from "./envelope.js";
import { localDate, addDays } from "./time.js";

export function project(events, { now, tz }) {
  const studiedDays = new Set();
  const today = localDate(now, tz);
  let reviews_today = 0;

  for (const event of normalize(events)) {
    if (event.type === "card.reviewed") {
      const day = localDate(event.ts, tz);
      studiedDays.add(day);
      if (day === today) reviews_today++;
    } else if (event.type === "session.ended") {
      studiedDays.add(localDate(event.ts, tz));
    }
  }

  let streak = 0;
  let cursor = studiedDays.has(today)
    ? today
    : studiedDays.has(addDays(today, -1))
      ? addDays(today, -1)
      : null;
  while (cursor && studiedDays.has(cursor)) {
    streak++;
    cursor = addDays(cursor, -1);
  }

  return { days_studied: studiedDays.size, streak, reviews_today };
}
