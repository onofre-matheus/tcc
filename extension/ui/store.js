// Ponte entre a UI e o log de eventos (core + storage).
// Instância única do EventLog sobre chrome.storage.local, compartilhada por
// popup, side panel e service worker (todos leem/escrevem o mesmo log local).
// Comandos = emissão de eventos; leituras = projeções puras do core.

import { EventLog } from "../storage/log.js";
import { chromeArea } from "../storage/kv.js";
import { uuidv7 } from "../storage/id.js";
import { sync, verifyOwnObject, remoteOf } from "../storage/sync.js";
import { s3Bucket } from "../storage/s3.js";
import { loadSyncConfig } from "../storage/sync_config.js";

import { project as leitner } from "../core/leitner.js";
import { project as tasks } from "../core/tasks.js";
import { project as decks } from "../core/decks.js";
import { project as stats } from "../core/stats.js";
import { project as inbox } from "../core/inbox.js";
import { project as calendar } from "../core/calendar.js";
import { project as review } from "../core/review.js";
import { project as tags, normalizeTags, normalizeTag } from "../core/tags.js";
import { project as study } from "../core/study.js";
import { project as links, cleanNode } from "../core/links.js";
import { project as studies } from "../core/studies.js";
import { project as graph } from "../core/graph.js";
import { project as pages } from "../core/pages.js";
import { project as books } from "../core/books.js";
import { sealBook, bookKey } from "../storage/livro_cifra.js";
import { readEpubInfo } from "./epub.js";
import { attentionWindowSeconds } from "../core/attention.js";

// Entrada livre de tags e tags efetivas (cartão ∪ deck), para a UI.
export { parseTags, effectiveTags, normalizeTag } from "../core/tags.js";
// Nós do grafo de conexões ("kind:id"), para a UI.
export { parseNode, nodeKey } from "../core/links.js";

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
  // Como os lembretes disparam por chrome.alarms (relógio real), ignoram o
  // offset de DEBUG e projetam o calendário no agora de parede.
  calendarNow: () => log.project(calendar, { now: new Date().toISOString(), tz: tz() }),
  tasks: () => log.project(tasks),
  leitner: () => log.project(leitner, dayParams()),
  decks: () => log.project(decks),
  tags: () => log.project(tags, dayParams()),
  // Sessão personalizada (spec §4.11): { decks: [deck_id], tags: [tag],
  // around: ["kind:id"] } → { queue: vencidos que casam, practice: não
  // vencidos que casam }.
  study: (filter = {}) => log.project(study, { ...dayParams(), ...filter }),
  // Conexões (spec §4.12): { links, neighbors, nodes }.
  links: () => log.project(links),
  // Páginas (spec §4.15): { pages, order } — por ora só para rotular o nó
  // `page:` nas conexões; a escrita das páginas é da CLI.
  pages: () => log.project(pages),
  // Livros (spec §4.16): { books, annotations, order } — a lista do painel e
  // o rótulo do nó `book:` nas sinapses; ler e anotar é da CLI.
  books: () => log.project(books),
  // Sessões salvas (spec §4.13): { studies, order }.
  studies: () => log.project(studies),
  // Grafo do acervo (spec §4.14): { nodes, edges }, para a tela de grafo.
  graph: () => log.project(graph, dayParams()),
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

// importance? (v2, opcional): "important" pede a cascata de lembretes com
// antecedência (ver core/calendar.js). Sem ele, mantém o evento v1 intacto.
export const createAppointment = (title, starts_at, ends_at, { importance } = {}) => {
  const payload = { appointment_id: id("a"), title, starts_at, ends_at };
  return importance
    ? log.append("appointment.created", { ...payload, importance }, { v: 2 })
    : log.append("appointment.created", payload);
};

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

// deck_id null/"" = cartão solto, organizado só pelas tags: card.created v2
// sem o campo deck_id (catálogo 1.3). Com deck, o evento v1 fica intacto.
export async function createCard(deck_id, front, back, { tags = [], ...extra } = {}) {
  const card_id = id("c");
  const fields = { front, back, tags: normalizeTags(tags), ...extra };
  if (deck_id) await log.append("card.created", { card_id, deck_id, ...fields });
  else await log.append("card.created", { card_id, ...fields }, { v: 2 });
  return card_id;
}

export const reviewCard = (card_id, result, session_id) =>
  log.append("card.reviewed", { card_id, result, ...(session_id ? { session_id } : {}) });

// Treino fora do vencimento (modo livre da sessão personalizada): grava a
// resposta sem mexer na caixa — nenhuma projeção de agendamento lê o evento.
export const practiceCard = (card_id, result, session_id) =>
  log.append("card.practiced", { card_id, result, ...(session_id ? { session_id } : {}) });

// Sinapse — a conexão explícita entre dois nós { kind, id } (catálogo 1.4;
// "sinapse" nas telas desde o 1.6); relation é livre ("prerequisito"…). As
// telas não criam mais sinapse com ponta tag: ligar a uma tag classifica
// (tagCard).
export async function linkNodes(from, to, relation) {
  const a = cleanNode(from), b = cleanNode(to);
  if (!a || !b) throw new Error("nó inválido");
  const rel = String(relation ?? "").trim();
  const link_id = id("l");
  await log.append("link.created", { link_id, from: a, to: b, ...(rel ? { relation: rel } : {}) });
  return link_id;
}

// Classificar (1.6): a tag é campo do cartão, não ponta de sinapse — acrescenta
// a tag às próprias do cartão (o card.edited leva a lista final, LWW por
// campo). false = o cartão não existe ou já tinha a tag (própria ou do deck).
export async function tagCard(card_id, tag) {
  const t = normalizeTag(tag);
  const [{ cards }, { decks }] = await Promise.all([state.leitner(), state.decks()]);
  const card = cards[card_id];
  if (!card || !t) return false;
  const own = card.tags ?? [];
  const deckTags = card.deck_id ? decks[card.deck_id]?.tags ?? [] : [];
  if (own.includes(t) || deckTags.includes(t)) return false;
  await editCard(card_id, { tags: normalizeTags([...own, t]) });
  return true;
}

// Desligar remove todas as conexões do par: duas criadas em dispositivos
// diferentes são arestas distintas (spec §3).
export async function unlinkAll(link_ids) {
  for (const link_id of link_ids) await log.append("link.removed", { link_id });
}

// tag.renamed (catálogo 1.4): renomeia a tag e as descendentes nas
// ocorrências já gravadas (cartões, decks, conexões). Vazio ou igual: nada.
export async function renameTag(from, to) {
  const a = normalizeTag(from), b = normalizeTag(to);
  if (!a || !b || a === b) return false;
  await log.append("tag.renamed", { from: a, to: b });
  return true;
}

// Sessão salva (catálogo 1.5): o filtro com um nome. Salvar de novo com o
// mesmo study_id substitui a sessão inteira (é assim que se edita).
export async function saveStudy({ study_id, name, decks = [], tags = [], around = [], practice = false }) {
  const id_ = study_id || id("st");
  await log.append("study.saved", {
    study_id: id_, name: String(name).trim(), decks, tags: normalizeTags(tags), around, practice: !!practice,
  });
  return id_;
}

export const deleteStudy = (study_id) => log.append("study.deleted", { study_id });

// Pedido de revisão vindo de outra tela (o grafo → "estudar ao redor"): não é
// evento — é estado de UI, como a sessão ativa. O side panel o aplica no
// filtro e o apaga.
export const REVIEW_REQUEST_KEY = "review_request";
export const requestReview = (filter) =>
  chrome.storage.local.set({ [REVIEW_REQUEST_KEY]: { ...filter, at: Date.now() } });
export async function takeReviewRequest() {
  const { [REVIEW_REQUEST_KEY]: request } = await chrome.storage.local.get(REVIEW_REQUEST_KEY);
  if (request) await chrome.storage.local.remove(REVIEW_REQUEST_KEY);
  return request ?? null;
}
export function onReviewRequest(callback) {
  chrome.storage.onChanged.addListener((changes, area) => {
    if (area === "local" && changes[REVIEW_REQUEST_KEY]?.newValue) callback();
  });
}

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

// --- Sincronização (SPEC §5) ---
// Extensão e CLI são, para o bucket, dois dispositivos quaisquer: cada uma
// escreve só o próprio objeto e lê os dos outros. O estado do último ciclo
// fica guardado para a UI mostrar sem precisar sincronizar de novo.
export const SYNC_STATUS_KEY = "sync_status";

// A trava réplica ↔ bucket (SPEC §5, Login e logout): o log que já
// sincronizou com um bucket não fala com outro — o sync sobe tudo o que este
// navegador emitiu, e trocar o bucket por baixo levaria os eventos de um para
// o outro. "Sair deste dispositivo" é o caminho para trocar.
export const SYNC_REMOTE_KEY = "sync_remote";

async function checkRemote(config) {
  const bound = (await chrome.storage.local.get(SYNC_REMOTE_KEY))[SYNC_REMOTE_KEY];
  if (bound && bound !== remoteOf(config)) {
    throw new Error(`este navegador pertence a ${bound}, e a configuração aponta para ${remoteOf(config)} — o sync levaria os eventos de um bucket para o outro; para trocar de bucket, "Sair deste dispositivo" antes`);
  }
}

/** Um ciclo de sync. `deps` troca config e bucket nos testes. */
export async function syncNow(deps = {}) {
  const config = deps.config ?? (await loadSyncConfig());
  await checkRemote(config);
  const result = await sync(log, deps.bucket ?? s3Bucket(config), config.prefix);
  await chrome.storage.local.set({
    [SYNC_REMOTE_KEY]: remoteOf(config),
    [SYNC_STATUS_KEY]: { at: new Date().toISOString(), ok: true, ...result },
  });
  return result;
}

/**
 * "Sair deste dispositivo" (SPEC §5, Login e logout): sincroniza, CONFERE
 * que o bucket tem tudo o que este navegador emitiu e só então apaga o log,
 * a credencial, a configuração e os alarmes. Qualquer falha antes da
 * conferência lança — e nada é apagado. Entrar de novo é preencher o
 * formulário do sync, que restaura o log.
 */
export async function logout(deps = {}) {
  const config = deps.config ?? (await loadSyncConfig());
  const bucket = deps.bucket ?? s3Bucket(config);
  await syncNow({ config, bucket });
  const verified = await verifyOwnObject(log, bucket, config.prefix);
  await chrome.storage.local.clear();
  await globalThis.chrome?.alarms?.clearAll?.();
  return { verified };
}

// Variante para o service worker: nunca lança. Sync é oportunista — o
// navegador pode estar offline, a política pode não ter chegado ainda — e uma
// exceção não tratada no alarme só polui o console.
export async function syncQuietly() {
  try {
    return await syncNow();
  } catch (error) {
    await chrome.storage.local.set({
      [SYNC_STATUS_KEY]: { at: new Date().toISOString(), ok: false, error: String(error.message ?? error) },
    });
    return null;
  }
}

// --- Livros (catálogo 1.7; SPEC §5, "Livros no bucket") ---
// O cadastro pela extensão: confere o EPUB, cifra com a senha dos livros e
// envia ao bucket — só então grava o book.registered (envio que falha não
// deixa cadastro sem arquivo). A CLI baixa no próximo `pnn sync`.

const hex = (buffer) => [...new Uint8Array(buffer)].map((b) => b.toString(16).padStart(2, "0")).join("");

export class BookPassphraseMissing extends Error {
  constructor() {
    super("defina a senha dos livros em Sincronização → Configurar bucket e chave — é ela que cifra o EPUB no bucket (a mesma PNN_LIVROS_SENHA da CLI)");
    this.name = "BookPassphraseMissing";
  }
}

/**
 * Cadastra o EPUB. `deps` troca config e bucket nos testes.
 * @returns {{ bookId, title, created: boolean, uploaded: boolean }}
 */
export async function registerBook(bytes, fileName, deps = {}) {
  const info = await readEpubInfo(bytes, fileName);
  const sha256 = hex(await crypto.subtle.digest("SHA-256", bytes));
  const config = deps.config ?? (await loadSyncConfig());
  if (!config.bookPassphrase) throw new BookPassphraseMissing();
  const bucket = deps.bucket ?? s3Bucket(config);

  const state = await log.project(books);
  const existing = Object.entries(state.books).find(([, b]) => b.sha256 === sha256);
  const bookId = existing?.[0] ?? id("book");
  const key = bookKey(config.prefix, bookId);
  if (existing) {
    // Já cadastrado (aqui ou na CLI): só reenvia se o bucket não tem.
    const there = (await bucket.list(key)).some((o) => o.key === key);
    if (!there) await bucket.put(key, await sealBook(config.bookPassphrase, bookId, bytes));
    return { bookId, title: existing[1].title, created: false, uploaded: !there };
  }
  await bucket.put(key, await sealBook(config.bookPassphrase, bookId, bytes));
  await log.append("book.registered", {
    book_id: bookId,
    title: info.title,
    ...(info.author ? { author: info.author } : {}),
    format: "epub",
    sha256,
  });
  if (!deps.bucket) await syncQuietly(); // o evento sobe já: a CLI vê no próximo sync
  return { bookId, title: info.title, created: true, uploaded: true };
}

export const syncStatus = async () =>
  (await chrome.storage.local.get(SYNC_STATUS_KEY))[SYNC_STATUS_KEY] ?? null;

// --- Reatividade entre contextos ---
// Qualquer append altera a chave "events"; assim popup e side panel se
// atualizam quando o outro (ou o service worker) grava.
export function onChange(callback) {
  chrome.storage.onChanged.addListener((changes, area) => {
    if (area === "local" && (changes.events || changes[DEBUG_OFFSET_KEY])) callback();
  });
}
