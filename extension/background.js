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

import { captureNote, logCheckin, endSession, log, ready } from "./ui/store.js";

const MENU_CAPTURE = "capture-selection";
const MENU_PALACE = "open-palace";

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

chrome.alarms.onAlarm.addListener(async (alarm) => {
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
  if (notifId.startsWith("checkin:") || notifId.startsWith("focus-done:")) {
    chrome.notifications.clear(notifId);
  }
});
