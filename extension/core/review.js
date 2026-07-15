// Retrospectiva semanal (spec/SPEC.md §4.7) — a revisão à la Safren: o que
// foi feito, quando, e o que interrompeu. A semana projetada (seg–dom) é a
// que contém a data local de `now`; navegar no histórico é variar o `now`.

import { normalize } from "./envelope.js";
import { localDate, addDays } from "./time.js";

// mondayOf: segunda-feira da semana que contém a data civil dada.
function mondayOf(isoDate) {
  const [y, m, d] = isoDate.split("-").map(Number);
  const weekday = new Date(Date.UTC(y, m - 1, d)).getUTCDay(); // 0 = domingo
  return addDays(isoDate, -((weekday + 6) % 7));
}

// Duração em minutos (arredondada); null para intervalos vazios/invertidos.
function minutesBetween(fromTs, toTs) {
  const ms = Date.parse(toTs) - Date.parse(fromTs);
  if (!Number.isFinite(ms) || ms <= 0) return null;
  return Math.round(ms / 60000);
}

export function project(events, { now, tz }) {
  const weekStart = mondayOf(localDate(now, tz));
  const weekEnd = addDays(weekStart, 6);

  const days = {};
  for (let i = 0; i < 7; i++) {
    days[addDays(weekStart, i)] = {
      reviews: 0, focus_minutes: 0, pause_minutes: 0, completed: 0, interrupted: 0,
      checkins: 0, checkins_on_task: 0,
    };
  }
  const inWeek = (day) => day >= weekStart && day <= weekEnd;

  const sessions = new Map();
  const pauses = new Map();
  const ensure = (map, id) => {
    if (!map.has(id)) map.set(id, {});
    return map.get(id);
  };

  const reasons = {};
  let appointment_minutes = 0;

  for (const event of normalize(events)) {
    const { payload } = event;
    if (event.type === "session.started") {
      ensure(sessions, payload.session_id).startTs = event.ts;
    } else if (event.type === "session.ended") {
      const s = ensure(sessions, payload.session_id);
      s.endTs = event.ts;
      s.outcome = payload.outcome;
      if (payload.reason !== undefined) s.reason = payload.reason; // v2; v1 ≡ sem motivo
    } else if (event.type === "pause.started") {
      ensure(pauses, payload.pause_id).startTs = event.ts;
    } else if (event.type === "pause.ended") {
      ensure(pauses, payload.pause_id).endTs = event.ts;
    } else if (event.type === "pause.logged") {
      // Retroativa: o tempo do domínio vem no payload, não no ts.
      const p = ensure(pauses, payload.pause_id);
      p.startTs = payload.starts_at;
      p.endTs = payload.ends_at;
    } else if (event.type === "card.reviewed") {
      const day = localDate(event.ts, tz);
      if (inWeek(day)) days[day].reviews += 1;
    } else if (event.type === "checkin.logged") {
      // Automonitoramento de atenção: "na tarefa em X de Y checagens".
      const day = localDate(event.ts, tz);
      if (inWeek(day)) {
        days[day].checkins += 1;
        if (payload.on_task) days[day].checkins_on_task += 1;
      }
    } else if (event.type === "appointment.created") {
      const minutes = minutesBetween(payload.starts_at, payload.ends_at);
      const day = localDate(payload.starts_at, tz);
      if (minutes !== null && inWeek(day)) appointment_minutes += minutes;
    }
  }

  for (const s of sessions.values()) {
    if (!s.startTs || !s.endTs) continue; // par não fechado é ignorado
    const minutes = minutesBetween(s.startTs, s.endTs);
    if (minutes === null) continue;
    const day = localDate(s.startTs, tz);
    if (!inWeek(day)) continue;
    days[day].focus_minutes += minutes;
    if (s.outcome === "interrupted") {
      days[day].interrupted += 1;
      if (s.reason) reasons[s.reason] = (reasons[s.reason] ?? 0) + 1;
    } else {
      days[day].completed += 1;
    }
  }

  for (const p of pauses.values()) {
    if (!p.startTs || !p.endTs) continue;
    const minutes = minutesBetween(p.startTs, p.endTs);
    if (minutes === null) continue;
    const day = localDate(p.startTs, tz);
    if (inWeek(day)) days[day].pause_minutes += minutes;
  }

  const totals = {
    reviews: 0, focus_minutes: 0, pause_minutes: 0, completed: 0, interrupted: 0,
    checkins: 0, checkins_on_task: 0,
  };
  for (const d of Object.values(days)) {
    totals.reviews += d.reviews;
    totals.focus_minutes += d.focus_minutes;
    totals.pause_minutes += d.pause_minutes;
    totals.completed += d.completed;
    totals.interrupted += d.interrupted;
    totals.checkins += d.checkins;
    totals.checkins_on_task += d.checkins_on_task;
  }

  return { week_start: weekStart, week_end: weekEnd, days, totals, reasons, appointment_minutes };
}
