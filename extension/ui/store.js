// Ponte entre a UI e o log de eventos (core + storage).
// Instância única do EventLog sobre chrome.storage.local, compartilhada por
// popup, side panel e service worker (todos leem/escrevem o mesmo log local).
// Comandos = emissão de eventos; leituras = projeções puras do core.

import { EventLog } from "../storage/log.js";
import { chromeArea } from "../storage/kv.js";
import { uuidv7 } from "../storage/id.js";

import { project as leitner } from "../core/leitner.js";
import { project as tasks } from "../core/tasks.js";
import { project as decks } from "../core/decks.js";
import { project as stats } from "../core/stats.js";
import { project as inbox } from "../core/inbox.js";
import { project as calendar } from "../core/calendar.js";
import { project as review } from "../core/review.js";
import { attentionWindowSeconds } from "../core/attention.js";

// Deslocamento de tempo para DEBUG: viaja no tempo para testar vencimentos do
// Leitner e streaks sem esperar dias reais. Afeta o `now` das projeções e o
// `ts` dos eventos emitidos. Persistido e sincronizado entre popup/side panel.
const DEBUG_OFFSET_KEY = "debug_offset_ms";
let debugOffsetMs = 0;

const tz = () => Intl.DateTimeFormat().resolvedOptions().timeZone;
export const nowIso = () => new Date(Date.now() + debugOffsetMs).toISOString();
const dayParams = () => ({ now: nowIso(), tz: tz() });
const id = (prefix) => `${prefix}-${uuidv7()}`;

export const log = new EventLog(chromeArea(chrome.storage.local), { clock: nowIso });

// Carrega o offset persistido antes do primeiro render; mantém sincronizado.
export const ready = chrome.storage.local.get(DEBUG_OFFSET_KEY).then((r) => {
  debugOffsetMs = r[DEBUG_OFFSET_KEY] ?? 0;
});
chrome.storage.onChanged.addListener((changes, area) => {
  if (area === "local" && changes[DEBUG_OFFSET_KEY]) {
    debugOffsetMs = changes[DEBUG_OFFSET_KEY].newValue ?? 0;
  }
});

export const debug = {
  offsetDays: () => Math.round(debugOffsetMs / 86400000),
  async advanceDays(days) {
    debugOffsetMs += days * 86400000;
    await chrome.storage.local.set({ [DEBUG_OFFSET_KEY]: debugOffsetMs });
  },
  async reset() {
    debugOffsetMs = 0;
    await chrome.storage.local.set({ [DEBUG_OFFSET_KEY]: 0 });
  },
};

// --- Leituras (projeções) ---

export const state = {
  calendar: () => log.project(calendar, dayParams()),
  tasks: () => log.project(tasks),
  leitner: () => log.project(leitner, dayParams()),
  decks: () => log.project(decks),
  stats: () => log.project(stats, dayParams()),
  inbox: () => log.project(inbox),
  // A retrospectiva navega no histórico variando só o instante de referência:
  // `now` dentro de qualquer semana projeta aquela semana (spec §4.7).
  review: (nowOverride) =>
    log.project(review, { now: nowOverride ?? nowIso(), tz: tz() }),
  async attentionMinutes() {
    return Math.round((await log.project(attentionWindowSeconds)) / 60);
  },
};

// --- Comandos (emissão de eventos) ---

export async function createTask(title, { priority, parentId, dueDate } = {}) {
  const task_id = id("t");
  const payload = { task_id, title };
  if (parentId) payload.parent_id = parentId;
  if (dueDate) payload.due_date = dueDate;
  await log.append("task.created", payload);
  if (priority) await log.append("task.prioritized", { task_id, priority });
  return task_id;
}

export const prioritizeTask = (task_id, priority) =>
  log.append("task.prioritized", { task_id, priority });

// Edição LWW por campo (catálogo v1.1): só os campos presentes mudam.
export const editTask = (task_id, fields) =>
  log.append("task.edited", { task_id, ...fields });

export const completeTask = (task_id) =>
  log.append("task.completed", { task_id });

export const deleteTask = (task_id) =>
  log.append("task.deleted", { task_id });

export const createAppointment = (title, starts_at, ends_at) =>
  log.append("appointment.created", { appointment_id: id("a"), title, starts_at, ends_at });

export const cancelAppointment = (appointment_id) =>
  log.append("appointment.cancelled", { appointment_id });

export const createDeck = (name, tags = []) =>
  log.append("deck.created", { deck_id: id("d"), name, tags });

export const renameDeck = (deck_id, name) =>
  log.append("deck.renamed", { deck_id, name });

export const archiveDeck = (deck_id) =>
  log.append("deck.archived", { deck_id });

export const editCard = (card_id, fields) =>
  log.append("card.edited", { card_id, ...fields });

export const archiveCard = (card_id) =>
  log.append("card.archived", { card_id });

export async function createCard(deck_id, front, back, extra = {}) {
  const card_id = id("c");
  await log.append("card.created", { card_id, deck_id, front, back, tags: [], ...extra });
  return card_id;
}

export const reviewCard = (card_id, result, session_id) =>
  log.append("card.reviewed", { card_id, result, ...(session_id ? { session_id } : {}) });

// Andaime opcional (catálogo v2): frame != "feynman" grava o método usado e
// sobe a versão do payload. Feynman é o padrão e mantém o evento v1 inalterado.
export const explainCard = (card_id, text, session_id, frame) => {
  const base = { card_id, text, ...(session_id ? { session_id } : {}) };
  return frame && frame !== "feynman"
    ? log.append("card.explained", { ...base, frame }, { v: 2 })
    : log.append("card.explained", base);
};

export const captureNote = (text, { url, pageTitle } = {}) =>
  log.append("note.captured", {
    note_id: id("n"),
    text,
    ...(url ? { url } : {}),
    ...(pageTitle ? { page_title: pageTitle } : {}),
  });

export async function triageNote(note_id, action, refs = {}) {
  await log.append("note.triaged", { note_id, action, ...refs });
}

export const captureDistraction = (session_id, text) =>
  log.append("distraction.captured", { distraction_id: id("x"), session_id, text });

export const triageDistraction = (distraction_id, action, refs = {}) =>
  log.append("distraction.triaged", { distraction_id, action, ...refs });

// v2: checkin_every? (minutos) pede o check-in periódico "você está na
// tarefa?" — self-monitoring de atenção (Safren). Sem ele, emite v1 intacto.
export async function startSession(kind, plannedMinutes, targetId, checkinEvery) {
  const session_id = id("s");
  const base = {
    session_id,
    kind,
    planned_minutes: plannedMinutes,
    ...(targetId ? { target_id: targetId } : {}),
  };
  if (checkinEvery) {
    await log.append("session.started", { ...base, checkin_every: checkinEvery }, { v: 2 });
  } else {
    await log.append("session.started", base);
  }
  return session_id;
}

// Resposta binária de um check-in de atenção.
export const logCheckin = (session_id, on_task) =>
  log.append("checkin.logged", { checkin_id: id("k"), session_id, on_task });

// v2: reason? registra por que a sessão foi interrompida (automonitoramento).
export const endSession = (session_id, outcome, reason) =>
  log.append(
    "session.ended",
    { session_id, outcome, ...(reason ? { reason } : {}) },
    { v: 2 }
  );

// Pausas (catálogo v1.2): com duração definida, como Safren pede.
export async function startPause(plannedMinutes) {
  const pause_id = id("p");
  await log.append("pause.started", {
    pause_id,
    ...(plannedMinutes ? { planned_minutes: plannedMinutes } : {}),
  });
  return pause_id;
}

export const endPause = (pause_id) => log.append("pause.ended", { pause_id });

// Retroativa ("esqueci de apertar o botão"): tempo do domínio no payload.
export const logPause = (starts_at, ends_at) =>
  log.append("pause.logged", { pause_id: id("p"), starts_at, ends_at });

// --- Reatividade entre contextos ---
// Qualquer append altera a chave "events"; assim popup e side panel se
// atualizam quando o outro (ou o service worker) grava.
export function onChange(callback) {
  chrome.storage.onChanged.addListener((changes, area) => {
    if (area === "local" && (changes.events || changes[DEBUG_OFFSET_KEY])) callback();
  });
}
