// Service worker: menus de contexto (botão direito), abertura do side panel e
// check-ins de atenção.
// - "Anotar seleção": captura rápida (distractibility delay do Safren) — grava
//   uma nota com o texto selecionado e a origem, para triar depois.
// - "Procrastina Não": abre o side panel de estudo na aba atual.
// - Check-in: o alarme periódico (armado pelo side panel ao iniciar a sessão
//   com checkin_every) pergunta "você está na tarefa?" via notificação do
//   navegador — funciona com o painel fechado; a resposta vira checkin.logged.
// - Fim do foco: o alarme pnn-focus-end (armado ao iniciar a sessão) avisa
//   quando o tempo planejado acaba — também pelo service worker, então a
//   notificação chega com o painel fechado ou minimizado. É ele quem grava o
//   session.ended nesse caso; o painel, aberto, apenas espelha o fim.

import { captureNote, logCheckin, endSession, log, ready, state, syncNow, syncQuietly } from "./ui/store.js";
import { dueReminders, reminderOffsets } from "./core/calendar.js";
import { loadSyncConfig, CONFIG_KEY } from "./storage/sync_config.js";

const MENU_CAPTURE = "capture-selection";
const MENU_PALACE = "open-palace";

// Sync periódico: é o que faz "o que eu fizer num lado aparecer no outro" sem
// ninguém apertar nada. Oportunista — offline ou sem configuração, falha em
// silêncio e tenta de novo no próximo tique (ver syncQuietly).
const SYNC_ALARM = "pnn-sync";
const SYNC_PERIOD_MINUTES = 15;

// Atalhos para o console do service worker (chrome://extensions → "service
// worker"): configurar a chave e disparar um ciclo à mão sem esperar o alarme.
//   await pnn.configurar({ bucket: "…", region: "…", accessKeyId: "…", secretAccessKey: "…" })
//   await pnn.sync()
//   await pnn.config()
globalThis.pnn = {
  sync: () => syncNow(),
  config: () => loadSyncConfig(),
  configurar: (config) => chrome.storage.local.set({ [CONFIG_KEY]: config }),
};

chrome.runtime.onInstalled.addListener(() => {
  chrome.contextMenus.removeAll(() => {
    chrome.contextMenus.create({
      id: MENU_CAPTURE,
      title: 'Anotar seleção: "%s"',
      contexts: ["selection"],
    });
    chrome.contextMenus.create({
      id: MENU_PALACE,
      title: "Abrir Procrastina Não",
      contexts: ["all"],
    });
  });
  syncAppointmentReminders();
  chrome.alarms.create(SYNC_ALARM, { periodInMinutes: SYNC_PERIOD_MINUTES });
});

// Rearma o alarme da agenda ao abrir o navegador (o service worker pode ter
// sido descarregado) e sempre que o log de eventos muda — assim um compromisso
// recém-criado no popup já entra na fila de lembretes sem precisar reabri-lo.
chrome.runtime.onStartup.addListener(() => {
  syncAppointmentReminders();
  // O alarme periódico não sobrevive a todo reinício; rearmar é barato.
  chrome.alarms.create(SYNC_ALARM, { periodInMinutes: SYNC_PERIOD_MINUTES });
  syncQuietly(); // pega no ar o que os outros dispositivos fizeram enquanto isso
});
chrome.storage.onChanged.addListener((changes, area) => {
  if (area === "local" && changes.events) syncAppointmentReminders();
});

// Abre o side panel também ao clicar no ícone com o botão... não: o ícone abre
// o popup. O side panel abre pelo menu de contexto.
chrome.contextMenus.onClicked.addListener(async (info, tab) => {
  if (info.menuItemId === MENU_CAPTURE && info.selectionText) {
    await captureNote(info.selectionText, {
      url: info.pageUrl,
      pageTitle: tab?.title,
    });
  } else if (info.menuItemId === MENU_PALACE && tab?.id != null) {
    chrome.sidePanel.open({ tabId: tab.id });
  }
});

// ---------- Check-in de atenção (self-monitoring, Safren) ----------
// O side panel arma o alarme e grava em active_checkin qual sessão perguntar e
// até quando (end_ms). Se o painel fechou e a sessão já venceu, o alarme se
// desarma sozinho — nenhuma notificação órfã.
const CHECKIN_ALARM = "pnn-checkin";
const CHECKIN_KEY = "active_checkin";

// Fim do foco (RF do lembrete de encerramento): o side panel arma este alarme
// para o instante final e grava em active_focus qual sessão encerrar. Como
// dispara no service worker, a notificação chega mesmo com o painel fechado.
const FOCUS_END_ALARM = "pnn-focus-end";
const FOCUS_KEY = "active_focus";

// Fim da pausa (RF do lembrete de pausa): o side panel arma este alarme para o
// instante planejado da pausa. Ao disparar, apenas avisa "hora de voltar" — não
// grava pause.ended, pois a duração real vem de quando o usuário de fato volta.
const PAUSE_END_ALARM = "pnn-pause-end";
const PAUSE_KEY = "active_pause";

// ---------- Lembretes de compromissos (RF: avisar antes) ----------
// Mesmo mecanismo do "fim do foco", aplicado à agenda: um alarme acorda o
// service worker no próximo lembrete e dispara a notificação mesmo com o painel
// fechado. A regra de *quando* lembrar é pura (core/calendar.js); aqui ficam só
// o disparo, o dedupe (fired_appt_reminders) e o reagendamento do alarme.
const APPT_ALARM = "pnn-appt-reminders";
const APPT_FIRED_KEY = "fired_appt_reminders";

const whenFmt = new Intl.DateTimeFormat("pt-BR", {
  weekday: "short", day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit",
});

async function syncAppointmentReminders() {
  if (!chrome.alarms) return;
  await ready;
  const cal = await state.calendarNow();
  const stored = await chrome.storage.local.get(APPT_FIRED_KEY);
  const fired = stored[APPT_FIRED_KEY] ?? {};
  const now = new Date().toISOString();
  const nowMs = Date.parse(now);

  const { due, next } = dueReminders(cal, { now, fired });

  for (const r of due) {
    const near = r.offset <= 60; // reta final (1 hora, 10 min, na hora)
    chrome.notifications.create(`appt:${r.key}:${Date.now()}`, {
      type: "basic",
      iconUrl: "icons/icon128.png",
      title: `${r.importance === "important" ? "⭐" : "📅"} ${r.label}: ${r.title}`,
      message: whenFmt.format(new Date(r.starts_at)),
      priority: near ? 2 : 1,
      requireInteraction: r.offset <= 10, // reta final fica na tela até ver
    });
    fired[r.key] = true;
  }

  // Poda o dedupe: mantém só chaves de compromissos ainda futuros (com folga),
  // para não crescer indefinidamente conforme a agenda avança.
  const live = new Set();
  for (const [id, a] of Object.entries(cal.appointments)) {
    if (Date.parse(a.starts_at) >= nowMs - 6 * 60 * 60 * 1000) {
      for (const off of reminderOffsets(a.importance)) live.add(`${id}@${off}`);
    }
  }
  for (const k of Object.keys(fired)) if (!live.has(k)) delete fired[k];
  await chrome.storage.local.set({ [APPT_FIRED_KEY]: fired });

  // Reagenda para o próximo lembrete. Alarmes MV3 persistem entre reinícios do
  // navegador, então um instante distante (semanas à frente) é confiável.
  chrome.alarms.clear(APPT_ALARM);
  if (next != null) chrome.alarms.create(APPT_ALARM, { when: next });
}

chrome.alarms.onAlarm.addListener(async (alarm) => {
  if (alarm.name === APPT_ALARM) {
    await syncAppointmentReminders();
    return;
  }

  if (alarm.name === SYNC_ALARM) {
    await syncQuietly();
    return;
  }

  if (alarm.name === PAUSE_END_ALARM) {
    const { [PAUSE_KEY]: pause } = await chrome.storage.local.get(PAUSE_KEY);
    chrome.alarms.clear(PAUSE_END_ALARM);
    await chrome.storage.local.remove(PAUSE_KEY);
    if (!pause) return;
    chrome.notifications.create(`pause-done:${pause.pause_id}`, {
      type: "basic",
      iconUrl: "icons/icon128.png",
      title: "Pausa concluída ☕",
      message: "Hora de voltar aos estudos — clique em “voltar” quando estiver pronto.",
      priority: 2,
    });
    return;
  }

  if (alarm.name === CHECKIN_ALARM) {
    const { [CHECKIN_KEY]: active } = await chrome.storage.local.get(CHECKIN_KEY);
    if (!active || Date.now() > active.end_ms) {
      chrome.alarms.clear(CHECKIN_ALARM);
      chrome.storage.local.remove(CHECKIN_KEY);
      return;
    }
    chrome.notifications.create(`checkin:${active.session_id}:${Date.now()}`, {
      type: "basic",
      iconUrl: "icons/icon128.png",
      title: "Você está na tarefa?",
      message: active.title || "Sessão de foco em andamento",
      buttons: [{ title: "Sim, na tarefa" }, { title: "Não, distraí" }],
      requireInteraction: true, // fica na tela até o usuário responder
    });
    return;
  }

  if (alarm.name === FOCUS_END_ALARM) {
    const { [FOCUS_KEY]: focus } = await chrome.storage.local.get(FOCUS_KEY);
    chrome.alarms.clear(FOCUS_END_ALARM);
    await chrome.storage.local.remove(FOCUS_KEY);
    if (!focus) return;
    // O foco acabou: encerra também o check-in de atenção da mesma sessão.
    chrome.alarms.clear(CHECKIN_ALARM);
    await chrome.storage.local.remove(CHECKIN_KEY);
    // Grava o encerramento só se o painel ainda não o fez (caso painel fechado).
    // Se o painel estava aberto e já registrou, aqui só notifica — sem duplicar.
    await ready;
    const events = await log.events();
    const ended = events.some(
      (e) => e.type === "session.ended" && e.payload.session_id === focus.session_id
    );
    if (!ended) await endSession(focus.session_id, "completed");
    const isReview = focus.kind === "review";
    chrome.notifications.create(`focus-done:${focus.session_id}`, {
      type: "basic",
      iconUrl: "icons/icon128.png",
      title: isReview ? "Revisão concluída! 🎉" : "Foco concluído! 🎉",
      message: focus.title
        ? `Você concluiu: ${focus.title}`
        : "Bom trabalho — que tal uma pausa?",
    });
    return;
  }
});

chrome.notifications.onButtonClicked.addListener(async (notifId, buttonIndex) => {
  if (!notifId.startsWith("checkin:")) return;
  const sessionId = notifId.split(":")[1];
  await logCheckin(sessionId, buttonIndex === 0);
  chrome.notifications.clear(notifId);
});

// Clique no corpo da notificação (sem responder) apenas a dispensa.
chrome.notifications.onClicked.addListener((notifId) => {
  if (
    notifId.startsWith("checkin:") ||
    notifId.startsWith("focus-done:") ||
    notifId.startsWith("appt:") ||
    notifId.startsWith("pause-done:")
  ) {
    chrome.notifications.clear(notifId);
  }
});
