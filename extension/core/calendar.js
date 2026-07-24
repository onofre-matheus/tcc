// Calendário único (spec/SPEC.md §3, Organização) — RF01.
// Estratégia do livro Safren: um só calendário para compromissos com hora.
// `upcoming` = compromissos ainda não encerrados, do mais próximo ao mais
// distante — é o que a extensão mostra como "próximos agendamentos".

import { normalize } from "./envelope.js";

export function project(events, { now }) {
  const appointments = {};

  for (const event of normalize(events)) {
    if (event.type === "appointment.created") {
      const { appointment_id, title, starts_at, ends_at, importance } = event.payload;
      appointments[appointment_id] = { title, starts_at, ends_at };
      // importance? (v2, opcional): "important" muda a agenda de lembretes —
      // cascata semanal com antecedência, além da reta final. Ausente = comum.
      if (importance) appointments[appointment_id].importance = importance;
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

// ---------- Lembretes de compromissos (RF: avisar antes) ----------
// A extensão notifica com o painel fechado (service worker + chrome.alarms).
// Aqui fica só a regra pura de *quando* lembrar — testável e sem I/O; o
// disparo e o agendamento do alarme vivem em background.js.

const WEEK = 7 * 24 * 60; // minutos
const DAY = 24 * 60;
const HOUR = 60;

// Quantos minutos antes do início lembramos. Compromisso do dia a dia recebe só
// a reta final (1 dia, 1 hora, 10 min). Marcado como "important", ganha antes
// disso uma cascata semanal (até 8 semanas): "marquei com 1 mês, me lembre 1×
// por semana". O 0 avisa na hora exata de começar.
const NEAR = [DAY, HOUR, 10, 0];
const WEEKLY = [8, 7, 6, 5, 4, 3, 2, 1].map((w) => w * WEEK);

export function reminderOffsets(importance) {
  return importance === "important" ? [...WEEKLY, ...NEAR] : NEAR;
}

// Rótulo humano da antecedência ("Falta 1 semana", "Faltam 10 minutos").
export function reminderLabel(offsetMin) {
  if (offsetMin === 0) return "Começa agora";
  const verb = (n) => (n === 1 ? "Falta" : "Faltam");
  if (offsetMin % WEEK === 0) {
    const w = offsetMin / WEEK;
    return `${verb(w)} ${w} ${w === 1 ? "semana" : "semanas"}`;
  }
  if (offsetMin % DAY === 0) {
    const d = offsetMin / DAY;
    return `${verb(d)} ${d} ${d === 1 ? "dia" : "dias"}`;
  }
  if (offsetMin % HOUR === 0) {
    const h = offsetMin / HOUR;
    return `${verb(h)} ${h} ${h === 1 ? "hora" : "horas"}`;
  }
  return `Faltam ${offsetMin} minutos`;
}

// Lembretes vencidos agora e o próximo instante a agendar, a partir da projeção
// do calendário. `fired` é o conjunto (chave→true) de lembretes já notificados,
// para não repetir. `graceMs` descarta lembretes muito atrasados: se eu criar
// hoje um compromisso para daqui a 2h, não faz sentido disparar o "falta 1 dia"
// que "venceu" ontem. Retorna { due: [...], next: msEpoch | null }.
export function dueReminders({ appointments }, { now, fired = {}, graceMs = 6 * 60 * 60 * 1000 }) {
  const nowMs = Date.parse(now);
  const due = [];
  let next = null;

  for (const [id, a] of Object.entries(appointments)) {
    const startMs = Date.parse(a.starts_at);
    if (startMs < nowMs) continue; // já começou/passou: sem lembrete
    for (const offset of reminderOffsets(a.importance)) {
      const at = startMs - offset * 60000;
      if (at <= nowMs) {
        const key = `${id}@${offset}`;
        if (at > nowMs - graceMs && !fired[key]) {
          due.push({ key, id, offset, title: a.title, starts_at: a.starts_at, importance: a.importance, label: reminderLabel(offset) });
        }
      } else if (next === null || at < next) {
        next = at; // instante futuro mais próximo a acordar o service worker
      }
    }
  }

  return { due, next };
}
