import * as store from "../ui/store.js";
import { MASCOT_SVG } from "../ui/mascot.js";
import { mountDebugBar } from "../ui/debug.js";
import { mountSyncPanel } from "../ui/sync-panel.js";

const $ = (id) => document.getElementById(id);
const esc = (s) => String(s).replace(/[&<>]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" }[c]));
document.querySelector("#brand").insertAdjacentHTML("afterbegin", MASCOT_SVG);

// Estado local (não é projeção): sessão de foco e a revisão em andamento.
let session = null; // { id, kind, endMs, interval }
let review = null; // { queue, practiceFrom, index, cards, decks, deckId, revealed }
let selectedDeck = null; // deck do formulário de cartão: null = ainda não escolhido; "" = (sem deck)
let dayTasks = { tasks: {}, day_list: [] }; // para o seletor de tarefa do foco
// Último retrato do acervo (refresh): nomeia os nós do grafo de conexões.
let graph = { cards: {}, decks: {}, pages: {}, tasks: {}, links: { links: {}, neighbors: {}, nodes: [] } };
const nodeByLabel = new Map(); // rótulo da sugestão → "kind:id"

// Check-in de atenção (self-monitoring, Safren): o alarme dispara no service
// worker (funciona com o painel fechado); esta chave diz a ele qual sessão
// perguntar e até quando. Fora da sessão, a chave não existe.
const CHECKIN_ALARM = "pnn-checkin";
const CHECKIN_KEY = "active_checkin";

// Fim do foco: o mesmo padrão do check-in. O alarme dispara no service worker
// (funciona com o painel fechado); active_focus diz qual sessão encerrar e
// quando. É o service worker quem grava o session.ended e notifica no fim —
// o painel, aberto, só espelha o encerramento.
const FOCUS_END_ALARM = "pnn-focus-end";
const FOCUS_KEY = "active_focus";

async function armCheckin(sessionId, everyMinutes, taskTitle, endMs) {
  if (!everyMinutes || !chrome.alarms) return;
  await chrome.storage.local.set({
    [CHECKIN_KEY]: { session_id: sessionId, title: taskTitle ?? "", end_ms: endMs },
  });
  chrome.alarms.create(CHECKIN_ALARM, {
    delayInMinutes: everyMinutes,
    periodInMinutes: everyMinutes,
  });
}
async function disarmCheckin() {
  if (!chrome.alarms) return;
  chrome.alarms.clear(CHECKIN_ALARM);
  await chrome.storage.local.remove(CHECKIN_KEY);
}

async function armFocusEnd(sessionId, kind, taskTitle, endMs) {
  if (!chrome.alarms) return;
  await chrome.storage.local.set({
    [FOCUS_KEY]: { session_id: sessionId, kind, title: taskTitle ?? "", end_ms: endMs },
  });
  chrome.alarms.create(FOCUS_END_ALARM, { when: endMs });
}
async function disarmFocusEnd() {
  if (!chrome.alarms) return;
  chrome.alarms.clear(FOCUS_END_ALARM);
  await chrome.storage.local.remove(FOCUS_KEY);
}

// Fim da pausa: mesmo padrão do fim do foco. O alarme dispara no service worker
// (funciona com o painel fechado) e só avisa "hora de voltar" — não encerra a
// pausa, pois a duração real vem de quando o usuário volta.
const PAUSE_END_ALARM = "pnn-pause-end";
const PAUSE_KEY = "active_pause";

async function armPauseEnd(pauseId, endMs) {
  if (!chrome.alarms) return;
  await chrome.storage.local.set({ [PAUSE_KEY]: { pause_id: pauseId, end_ms: endMs } });
  chrome.alarms.create(PAUSE_END_ALARM, { when: endMs });
}
async function disarmPauseEnd() {
  if (!chrome.alarms) return;
  chrome.alarms.clear(PAUSE_END_ALARM);
  await chrome.storage.local.remove(PAUSE_KEY);
}

// ---------- Sessão de foco / pomodoro ----------
function plural(n, one, many) {
  return `${n} ${n === 1 ? one : many}`;
}

function showActive(kindLabel) {
  $("focusIdle").hidden = true;
  $("focusActive").hidden = false;
  $("sessionKind").textContent = kindLabel;
}
function showIdle() {
  $("focusActive").hidden = true;
  $("focusIdle").hidden = false;
  $("reviewPanel").hidden = true;
}
function paintTimer(ms) {
  const total = Math.max(0, Math.round(ms / 1000));
  const mm = String(Math.floor(total / 60)).padStart(2, "0");
  const ss = String(total % 60).padStart(2, "0");
  $("timer").textContent = `${mm}:${ss}`;
}
function tick() {
  const left = session.endMs - Date.now();
  paintTimer(left);
  if (left <= 0) completeLocally();
}
async function beginSession(kind, label) {
  // Começar um foco encerra uma pausa em curso (estados mutuamente exclusivos):
  // fecha o registro e desarma o lembrete de fim de pausa para não notificar à toa.
  if (pause) {
    clearInterval(pause.interval);
    const pid = pause.id;
    pause = null;
    $("pauseBtn").textContent = "☕ Pausa";
    await disarmPauseEnd();
    await store.endPause(pid);
  }
  const minutes = Math.max(1, Number($("minutes").value) || 25);
  // Alvo da sessão: a tarefa escolhida (foco) ou o deck (revisão) — target_id.
  const taskId = kind === "task" ? $("focusTask").value || undefined : undefined;
  const taskTitle = taskId ? dayTasks.tasks[taskId]?.title : undefined;
  const checkinEvery = Math.max(0, Number($("checkinEvery").value) || 0) || undefined;
  const id = await store.startSession(kind, minutes, taskId ?? review?.deckId, checkinEvery);
  const endMs = Date.now() + minutes * 60000;
  session = { id, kind, endMs, interval: null };
  paintTimer(minutes * 60000);
  showActive(taskTitle ? `${label} · ${taskTitle}` : label);
  session.interval = setInterval(tick, 1000);
  await armCheckin(id, checkinEvery, taskTitle, endMs);
  await armFocusEnd(id, kind, taskTitle, endMs);
}

// Fim natural (tempo esgotado): quem grava o session.ended e notifica é o
// service worker (alarme pnn-focus-end) — assim funciona com o painel fechado.
// O painel, se aberto, apenas zera a UI e deixa o registro para o alarme, sem
// gravar em duplicidade. Não desarma o alarme de fim: é ele que vai notificar.
function completeLocally() {
  if (!session) return;
  clearInterval(session.interval);
  session = null;
  review = null;
  showIdle();
  disarmCheckin();
}

// Encerramento antecipado (botão "Encerrar sessão" ou pausa): aqui é o painel
// quem grava, e desarma o alarme de fim para não disparar um "concluído" falso.
async function finish(outcome, reason) {
  if (!session) return;
  clearInterval(session.interval);
  const id = session.id;
  session = null;
  review = null;
  showIdle();
  await disarmCheckin();
  await disarmFocusEnd();
  await store.endSession(id, outcome, reason);
}

// Painel reaberto no meio de uma sessão de foco: o alarme segue armado no
// service worker, então retomamos a contagem visual para não parecer parado.
// A revisão não é restaurada (a fila de cartões vive só em memória); mesmo
// assim o alarme registra e notifica o fim normalmente.
async function restoreActiveSession() {
  if (session || !chrome.storage?.local) return;
  const { [FOCUS_KEY]: focus } = await chrome.storage.local.get(FOCUS_KEY);
  if (!focus || focus.kind === "review" || Date.now() >= focus.end_ms) return;
  session = { id: focus.session_id, kind: focus.kind, endMs: focus.end_ms, interval: null };
  paintTimer(focus.end_ms - Date.now());
  showActive(focus.title ? `Foco · ${focus.title}` : "Foco");
  session.interval = setInterval(tick, 1000);
}

// Interromper pergunta o motivo (opcional): a interrupção vira dado revisável
// na retrospectiva, não fracasso — automonitoramento à la Safren.
function interruptWithReason() {
  const reason = window.prompt("O que tirou você da sessão? (opcional — cancele ou deixe vazio para pular)");
  finish("interrupted", (reason ?? "").trim() || undefined);
}

// ---------- Pausa cronometrada (catálogo v1.2; durações 5–25) ----------
let pause = null; // { id, endMs, interval }

async function togglePause() {
  const btn = $("pauseBtn");
  if (pause) {
    clearInterval(pause.interval);
    const pid = pause.id;
    pause = null;
    btn.textContent = "☕ Pausa";
    await disarmPauseEnd();
    await store.endPause(pid);
    return;
  }
  // Break durante o foco encerra a sessão (a tarefa "desmarca"): pausa é um
  // estado próprio, não um parêntese dentro da sessão (SPEC §3).
  if (session) {
    await finish("interrupted", "pausa");
    $("focusTask").value = "";
  }
  const minutes = Number($("pauseMin").value) || 5;
  const pid = await store.startPause(minutes);
  const endMs = Date.now() + minutes * 60000;
  pause = { id: pid, endMs, interval: null };
  await armPauseEnd(pid, endMs);
  const paint = () => {
    const left = Math.max(0, pause.endMs - Date.now());
    const mm = String(Math.floor(left / 60000)).padStart(2, "0");
    const ss = String(Math.floor(left / 1000) % 60).padStart(2, "0");
    btn.textContent = left > 0 ? `☕ ${mm}:${ss} — clique para voltar` : "☕ acabou — clique para voltar";
  };
  paint();
  pause.interval = setInterval(paint, 1000);
}
$("pauseBtn").onclick = togglePause;

$("startFocus").onclick = () => beginSession("task", "Foco");
// ---------- Sessão personalizada (spec §4.11) ----------
// Filtro: deck (subárvore "::") E tags (cada uma inclui as descendentes "/").
// O treino livre acrescenta os não vencidos depois da fila; a resposta deles
// vira card.practiced, que não mexe na caixa.
function reviewFilter() {
  const deck = $("reviewDeckSel").value, around = $("reviewAround").value;
  return { decks: deck ? [deck] : [], tags: store.parseTags($("reviewTags").value), around: around ? [around] : [] };
}

async function updateReviewButton() {
  const filter = reviewFilter();
  const st = await store.state.study(filter);
  const practice = $("reviewPractice").checked ? st.practice.length : 0;
  const filtered = filter.decks.length > 0 || filter.tags.length > 0 || filter.around.length > 0;
  const total = st.queue.length + practice;
  // Nada a revisar: a sessão seria para nada — o botão desabilita e o
  // destaque primário passa para "Iniciar foco".
  const rev = $("startReview");
  rev.textContent = `Revisar flashcards (${st.queue.length}${practice ? ` + ${practice} treino` : ""})${filtered ? " · filtrado" : ""}`;
  rev.disabled = total === 0;
  rev.classList.toggle("primary", total > 0);
  rev.classList.toggle("ghost", total === 0);
  $("startFocus").classList.toggle("primary", total === 0);
}
$("reviewDeckSel").onchange = updateReviewButton;
$("reviewTags").oninput = updateReviewButton;
$("reviewPractice").onchange = updateReviewButton;
$("reviewAround").onchange = updateReviewButton;

$("startReview").onclick = async () => {
  const filter = reviewFilter();
  const [st, lt, dk] = await Promise.all([
    store.state.study(filter), store.state.leitner(), store.state.decks(),
  ]);
  const practice = $("reviewPractice").checked ? st.practice : [];
  review = {
    queue: [...st.queue, ...practice],
    practiceFrom: st.queue.length, // daqui em diante, treino (card.practiced)
    index: 0,
    cards: lt.cards,
    decks: dk.decks,
    deckId: filter.decks[0], // alvo da sessão (target_id), quando filtrada por deck
    revealed: false,
  };
  await beginSession("review", practice.length ? "Revisão + treino" : "Revisão");
  $("reviewPanel").hidden = false;
  renderCard();
};
$("endFocus").onclick = () => interruptWithReason();
$("captureDistraction").onclick = async () => {
  const text = $("distraction").value.trim();
  if (!text || !session) return;
  await store.captureDistraction(session.id, text);
  $("distraction").value = "";
};

// ---------- Revisão Leitner ----------
// Andaime de explicação escolhido para o cartão atual (spec §4.1). Feynman é o
// padrão; "4causas" troca o campo único pelos quatro sub-campos guiados.
let reviewFrame = "feynman";
function setFrame(frame) {
  reviewFrame = frame;
  $("feynmanBox").hidden = frame === "4causas";
  $("causesBox").hidden = frame !== "4causas";
  document.querySelectorAll("#frameSel button").forEach((b) => {
    b.classList.toggle("primary", b.dataset.frame === frame);
    b.classList.toggle("ghost", b.dataset.frame !== frame);
  });
}
document.querySelectorAll("#frameSel button").forEach((b) => {
  b.onclick = () => setFrame(b.dataset.frame);
});

// Concatena as 4 causas em "chave: valor" por linha, ignorando as vazias.
function collectCauses() {
  return [...document.querySelectorAll("#causesBox .cause")]
    .map((el) => [el.dataset.key, el.value.trim()])
    .filter(([, v]) => v)
    .map(([k, v]) => `${k}: ${v}`)
    .join("\n");
}

function renderCard() {
  const card = $("card"), empty = $("reviewEmpty");
  if (!review || review.index >= review.queue.length) {
    card.hidden = true;
    empty.hidden = false;
    $("reviewProgress").textContent = review ? `${review.queue.length} revisado(s)` : "";
    return;
  }
  empty.hidden = true;
  card.hidden = false;
  review.revealed = false;
  const c = review.cards[review.queue[review.index]];
  const practice = review.index >= review.practiceFrom;
  $("cardFace").innerHTML = esc(c.front ?? "");
  // Deck, caixa e o aviso de treino em texto (a cor nunca é o único sinal);
  // tags efetivas (cartão ∪ deck) logo abaixo.
  const deckName = c.deck_id ? review.decks[c.deck_id]?.name ?? "" : "(sem deck)";
  const tags = store.effectiveTags(c, review.decks);
  $("cardDeck").innerHTML =
    `${esc(deckName)} · caixa ${c.box}${practice ? " · <strong>treino</strong> (não altera a caixa)" : ""}` +
    (tags.length ? `<div class="card-tags">${tags.map((t) => `<span class="tag">#${esc(t)}</span>`).join(" ")}</div>` : "");
  $("cardSrc").innerHTML = "";
  // Explicação em branco + a anterior (se houver) para comparar. O método volta
  // ao último usado no cartão (last_frame), default Feynman.
  $("feynman").value = "";
  document.querySelectorAll("#causesBox .cause").forEach((el) => (el.value = ""));
  setFrame(c.last_frame ?? "feynman");
  const prev = $("prevExplanation");
  if (c.last_explanation) {
    $("prevExplanationText").textContent = c.last_explanation;
    prev.hidden = false;
    prev.open = false;
  } else {
    prev.hidden = true;
  }
  $("reveal").hidden = false;
  $("grade").hidden = true;
  $("cardLinks").hidden = true;
  $("reviewProgress").textContent = `${review.index + 1} / ${review.queue.length}`;
}
$("reveal").onclick = async () => {
  const c = review.cards[review.queue[review.index]];
  // Salva a explicação antes de mostrar a resposta, no andaime escolhido.
  const explanation =
    reviewFrame === "4causas" ? collectCauses() : $("feynman").value.trim();
  if (explanation)
    await store.explainCard(review.queue[review.index], explanation, session?.id, reviewFrame);
  $("cardFace").innerHTML = `${esc(c.front ?? "")}<hr style="border:none;border-top:1px solid var(--border);margin:12px 0"/>${esc(c.back ?? "")}`;
  if (c.source_url) {
    $("cardSrc").innerHTML = `fonte: <a href="${esc(c.source_url)}" target="_blank" rel="noopener noreferrer">${esc(c.source_title || c.source_url)}</a>`;
  }
  $("reveal").hidden = true;
  $("grade").hidden = false;
  $("cardLinks").hidden = false;
  $("linkMsg").textContent = "";
  await renderCardLinks();
};

// ---------- Conexões (catálogo 1.4, estilo Obsidian) ----------
const truncate = (s, max) => ([...String(s)].length > max ? `${[...String(s)].slice(0, max - 1).join("")}…` : String(s));

// Rótulo humano do nó: frente do cartão, "deck Nome", "#tag", "página
// Título" ou "tarefa Título" (páginas e tarefas como ponta de sinapse,
// catálogo 1.6; as ligações com elas são feitas na CLI).
function nodeLabel(key) {
  const n = store.parseNode(key);
  if (!n) return key;
  if (n.kind === "card") return graph.cards[n.id] ? `“${truncate(graph.cards[n.id].front ?? "", 50)}”` : key;
  if (n.kind === "deck") return `deck ${graph.decks[n.id]?.name ?? n.id}`;
  if (n.kind === "page") return graph.pages[n.id] ? `página ${truncate(graph.pages[n.id].title, 50)}` : key;
  if (n.kind === "task") {
    const task = graph.tasks[n.id];
    return task ? `tarefa ${truncate(task.title, 50)}${task.done ? " ✔" : ""}` : key;
  }
  return `#${n.id}`;
}

// Grafo local de x: cada vizinho com a direção (→ x aponta, ← aponta para x,
// ↔ as duas), as relações e as conexões do par (para desligar todas).
function neighborhood(links, x) {
  return (links.neighbors[x] ?? []).map((y) => {
    const pair = Object.entries(links.links).filter(
      ([, l]) => (l.from === x && l.to === y) || (l.from === y && l.to === x)
    );
    const out = pair.some(([, l]) => l.from === x), inc = pair.some(([, l]) => l.from === y);
    const relations = [...new Set(pair.map(([, l]) => l.relation).filter(Boolean))].sort();
    return { key: y, arrow: out && inc ? "↔" : out ? "→" : "←", relations, ids: pair.map(([id]) => id) };
  });
}

const currentCardKey = () =>
  review && review.index < review.queue.length ? `card:${review.queue[review.index]}` : null;

async function renderCardLinks() {
  const x = currentCardKey();
  if (!x) return;
  graph.links = await store.state.links();
  const pairs = neighborhood(graph.links, x);
  const items = pairs.map((nb, i) => {
    const rel = nb.relations.length ? ` <span class="meta">(${esc(nb.relations.join(", "))})</span>` : "";
    return `<div class="link-item"><span class="grow">${nb.arrow} ${esc(nodeLabel(nb.key))}${rel}</span>
      <button type="button" class="btn ghost sm" data-unlink="${i}" title="Desfazer a sinapse">✕</button></div>`;
  });
  const list = $("cardLinkList");
  list.innerHTML = items.join("") || '<div class="meta">nenhuma — conecte a um cartão, deck ou #tag abaixo</div>';
  list.querySelectorAll("[data-unlink]").forEach((el) => {
    el.onclick = async () => {
      await store.unlinkAll(pairs[Number(el.dataset.unlink)].ids);
      await renderCardLinks();
    };
  });
}

// O alvo vem da lista de sugestões (cartão, deck, #tag existente) ou é uma
// "#tag" nova digitada à mão.
function resolveNodeInput(text) {
  const t = text.trim();
  if (nodeByLabel.has(t)) return nodeByLabel.get(t);
  const tag = t.startsWith("#") ? store.normalizeTag(t) : "";
  return tag ? `tag:${tag}` : null;
}

$("linkBtn").onclick = async () => {
  const x = currentCardKey();
  if (!x) return;
  const y = resolveNodeInput($("linkTarget").value);
  const msg = $("linkMsg");
  if (!y) {
    msg.textContent = "Escolha um cartão, deck ou #tag da lista (tag nova: comece com #).";
    return;
  }
  const relation = $("linkRelation").value.trim();
  const target = store.parseNode(y);
  // Tag classifica, não é ponta de sinapse (1.6): ligar a uma #tag acrescenta
  // a tag ao cartão.
  if (target.kind === "tag") {
    if (relation) {
      msg.textContent = "Relação é de sinapse; tag classifica — tire a relação para dar a tag ao cartão.";
      return;
    }
    const added = await store.tagCard(store.parseNode(x).id, target.id);
    $("linkTarget").value = "";
    msg.textContent = added ? `🏷 tag adicionada: #${target.id}` : `O cartão já tem #${target.id}.`;
    return;
  }
  const already = neighborhood(graph.links, x).find((nb) => nb.key === y);
  if (y === x) {
    msg.textContent = "Um cartão não se liga a si mesmo.";
    return;
  }
  // Redundante: sem relação, o par já ligado; com relação, a mesma relação.
  if (already && (!relation || already.relations.includes(relation))) {
    msg.textContent = `Já há sinapse com ${nodeLabel(y)}.`;
    return;
  }
  await store.linkNodes(store.parseNode(x), target, relation);
  $("linkTarget").value = "";
  $("linkRelation").value = "";
  msg.textContent = `⚡ sinapse com ${nodeLabel(y)}`;
  await renderCardLinks();
};
async function grade(result) {
  const cardId = review.queue[review.index];
  const practice = review.index >= review.practiceFrom;
  await (practice ? store.practiceCard : store.reviewCard)(cardId, result, session?.id);
  review.index++;
  renderCard();
}
$("correct").onclick = () => grade("correct");
$("wrong").onclick = () => grade("wrong");

// Manutenção durante a revisão (catálogo v1.1): o typo aparece na hora de
// revisar — corrige-se ali mesmo (card.edited, LWW por campo) ou arquiva-se
// (card.archived: sai da fila, o histórico fica). Nenhum dos dois é revisão:
// caixa e vencimento não mudam.
$("editCard").onclick = async () => {
  if (!review || review.index >= review.queue.length) return;
  const cardId = review.queue[review.index];
  const c = review.cards[cardId];
  const front = window.prompt("Frente:", c.front ?? "");
  if (front === null) return;
  const back = window.prompt("Verso:", c.back ?? "");
  if (back === null) return;
  const tagsIn = window.prompt("Tags (espaço ou vírgula; vazio limpa):", (c.tags ?? []).map((t) => `#${t}`).join(" "));
  if (tagsIn === null) return;
  const fields = {};
  if (front.trim() && front.trim() !== c.front) fields.front = front.trim();
  if (back.trim() && back.trim() !== c.back) fields.back = back.trim();
  // tags é um campo só no LWW do card.edited: vai a lista final inteira.
  const tags = store.parseTags(tagsIn);
  if (tags.join(" ") !== (c.tags ?? []).join(" ")) fields.tags = tags;
  if (!Object.keys(fields).length) return;
  await store.editCard(cardId, fields);
  Object.assign(c, fields); // reflete na revisão em andamento
  renderCard();
};
$("archiveCardBtn").onclick = async () => {
  if (!review || review.index >= review.queue.length) return;
  const cardId = review.queue[review.index];
  if (!window.confirm("Arquivar este cartão? Ele sai da fila; o histórico fica.")) return;
  await store.archiveCard(cardId);
  review.index++;
  renderCard();
};

// ---------- Caixa de entrada ----------
function renderInbox(inb) {
  const box = $("inbox");
  const notes = inb.pending_notes.map((id) => {
    const n = inb.notes[id];
    const hasLink = isHttpUrl(n.url);
    // Link acionável: abre em nova aba (o que você pediu). Só http(s), com o
    // href escapado, para o campo livre não virar vetor de href malicioso.
    const open = hasLink
      ? `<a class="btn ghost sm" href="${esc(n.url).replace(/"/g, "%22")}" target="_blank" rel="noopener noreferrer" title="Abrir em nova aba">🔗 abrir</a>`
      : "";
    return `<div class="inbox-item">
      <div class="kind">nota${hasLink ? " · com link" : ""}</div>
      <div class="text">${esc(n.text)}</div>
      <div class="actions">
        ${open}
        <button class="btn sm" data-note-card="${id}">→ cartão</button>
        <button class="btn sm" data-note-task="${id}">→ tarefa</button>
        <button class="btn ghost sm" data-note-drop="${id}">descartar</button>
      </div></div>`;
  });
  const dis = inb.pending_distractions.map((id) => {
    const d = inb.distractions[id];
    return `<div class="inbox-item">
      <div class="kind">distração</div>
      <div class="text">${esc(d.text)}</div>
      <div class="actions">
        <button class="btn sm" data-dis-done="${id}">feito</button>
        <button class="btn sm" data-dis-task="${id}">→ tarefa</button>
        <button class="btn ghost sm" data-dis-drop="${id}">descartar</button>
      </div></div>`;
  });
  box.innerHTML = notes.concat(dis).join("") || '<div class="empty">Caixa de entrada vazia.</div>';

  const on = (sel, fn) => box.querySelectorAll(sel).forEach((el) => (el.onclick = () => fn(el)));
  on("[data-note-task]", async (el) => {
    const id = el.dataset.noteTask;
    const task = await store.createTask(currentNote(id));
    await store.triageNote(id, "to_task", { task_id: task });
  });
  on("[data-note-drop]", (el) => store.triageNote(el.dataset.noteDrop, "discarded"));
  on("[data-note-card]", async (el) => {
    const id = el.dataset.noteCard;
    const n = lastInbox.notes[id];
    // Deck e tags vêm do formulário de cartão (seção Decks); "(sem deck)"
    // cria o cartão solto, organizado só pelas tags.
    const extra = { tags: store.parseTags($("cardTags").value) };
    if (n?.url) extra.source_url = n.url; // preserva a referência de origem
    if (n?.page_title) extra.source_title = n.page_title;
    const card = await store.createCard(selectedDeck || null, n?.text ?? "", "(preencher o verso)", extra);
    await store.triageNote(id, "to_card", { card_id: card });
  });
  on("[data-dis-done]", (el) => store.triageDistraction(el.dataset.disDone, "done"));
  on("[data-dis-drop]", (el) => store.triageDistraction(el.dataset.disDrop, "discarded"));
  on("[data-dis-task]", async (el) => {
    const id = el.dataset.disTask;
    const task = await store.createTask(currentDistraction(id));
    await store.triageDistraction(id, "to_task", { task_id: task });
  });
}
let lastInbox = { notes: {}, distractions: {} };
const currentNote = (id) => lastInbox.notes[id]?.text ?? "";
const currentDistraction = (id) => lastInbox.distractions[id]?.text ?? "";

// Normaliza o link digitado à mão: sem esquema, assume https:// (assim
// "github.com/x/pull/1" já vira clicável). Vazio continua vazio.
function normalizeUrl(raw) {
  if (!raw) return "";
  return /^[a-z]+:\/\//i.test(raw) ? raw : `https://${raw}`;
}
// Só linka http(s): evita href com javascript: e afins vindo do campo livre.
const isHttpUrl = (u) => /^https?:\/\//i.test(u ?? "");

$("noteForm").onsubmit = async (e) => {
  e.preventDefault();
  const text = $("noteText").value.trim();
  if (!text) return;
  const url = normalizeUrl($("noteUrl").value.trim());
  await store.captureNote(text, url ? { url } : {});
  e.target.reset();
};

// ---------- Decks ----------
// Hierarquia estilo Anki: o nome do deck codifica o caminho com "::"
// (deckPai::deckFilho::deckNeto). A árvore é derivada dos nomes; níveis
// intermediários sem deck.created próprio viram nós implícitos (sem id, não
// recebem cartões diretamente). A contagem de um pai soma a de seus descendentes.
export const DECK_SEP = "::";

function buildDeckTree(decks, counts) {
  const root = { children: new Map() };
  const nodeAt = (segments) => {
    let node = root, path = "";
    for (const seg of segments) {
      path = path ? `${path}${DECK_SEP}${seg}` : seg;
      if (!node.children.has(seg)) {
        node.children.set(seg, { seg, path, id: null, tags: [], own: 0, total: 0, children: new Map() });
      }
      node = node.children.get(seg);
    }
    return node;
  };
  for (const [id, d] of Object.entries(decks)) {
    const segments = d.name.split(DECK_SEP).map((s) => s.trim()).filter(Boolean);
    if (!segments.length) continue;
    const node = nodeAt(segments);
    node.id = id;
    node.tags = d.tags || [];
    node.own = counts[id] ?? 0;
  }
  const rollup = (node) => {
    node.total = node.own;
    for (const child of node.children.values()) node.total += rollup(child);
    return node.total;
  };
  for (const child of root.children.values()) rollup(child);
  return root;
}

// Nós reais (com id), em profundidade, para o seletor de deck do cartão.
function flattenReal(node, depth, out) {
  for (const child of [...node.children.values()].sort((a, b) => a.seg.localeCompare(b.seg, "pt"))) {
    if (child.id) out.push({ id: child.id, path: child.path, depth });
    flattenReal(child, depth + 1, out);
  }
  return out;
}

function renderDecks(dk, lt) {
  // Arquivados ficam fora da árvore, do seletor e da contagem (catálogo v1.1).
  const activeDecks = Object.fromEntries(
    Object.entries(dk.decks).filter(([, d]) => !d.archived)
  );
  const counts = {};
  for (const c of Object.values(lt.cards)) {
    if (!c.archived) counts[c.deck_id] = (counts[c.deck_id] ?? 0) + 1;
  }
  const tree = buildDeckTree(activeDecks, counts);

  const rows = [];
  const walk = (node, depth) => {
    for (const child of [...node.children.values()].sort((a, b) => a.seg.localeCompare(b.seg, "pt"))) {
      const implicit = child.id ? "" : " implicit";
      const tags = child.tags.map((t) => `<span class="tag">#${esc(t)}</span>`).join(" ");
      const actions = child.id
        ? `<button class="btn ghost sm" data-deck-rename="${child.id}" title="Renomear deck">✎</button>
           <button class="btn ghost sm" data-deck-archive="${child.id}" title="Arquivar deck">🗄</button>`
        : "";
      rows.push(`<div class="deck${implicit}" style="--depth:${depth}">
        <span class="grow">${esc(child.seg)} ${tags}</span>${actions}
        <span class="count">${plural(child.total, "cartão", "cartões")}</span></div>`);
      walk(child, depth + 1);
    }
  };
  walk(tree, 0);
  $("decks").innerHTML = rows.length ? rows.join("") : '<div class="empty">Nenhum deck ainda.</div>';

  $("decks").querySelectorAll("[data-deck-rename]").forEach((el) => {
    el.onclick = async () => {
      const id = el.dataset.deckRename;
      const name = window.prompt("Novo nome do deck (use :: para hierarquia):", dk.decks[id].name);
      const clean = (name ?? "").split(DECK_SEP).map((s) => s.trim()).filter(Boolean).join(DECK_SEP);
      if (clean && clean !== dk.decks[id].name) await store.renameDeck(id, clean);
    };
  });
  $("decks").querySelectorAll("[data-deck-archive]").forEach((el) => {
    el.onclick = async () => {
      const id = el.dataset.deckArchive;
      if (window.confirm(`Arquivar o deck "${dk.decks[id].name}"? Os cartões saem da fila; o histórico fica.`)) {
        await store.archiveDeck(id);
      }
    };
  });

  const sel = $("cardDeckSel");
  const real = flattenReal(tree, 0, []);
  const deckOptions = real
    .map((d) => `<option value="${d.id}">${"  ".repeat(d.depth)}${esc(d.path)}</option>`)
    .join("");
  // "(sem deck)" fica no fim: o cartão solto vive só das tags (catálogo 1.3).
  // Sem nenhum deck, é a única opção — o formulário aparece mesmo assim.
  sel.innerHTML = `${deckOptions}<option value="">(sem deck)</option>`;
  if (selectedDeck === null || (selectedDeck !== "" && !real.some((d) => d.id === selectedDeck))) {
    selectedDeck = real[0]?.id ?? "";
  }
  sel.value = selectedDeck;

  // Seletor de deck do filtro da revisão: mesma árvore, "todos" por padrão.
  const rsel = $("reviewDeckSel");
  const prev = rsel.value;
  rsel.innerHTML = `<option value="">todos os decks</option>${deckOptions}`;
  rsel.value = real.some((d) => d.id === prev) ? prev : "";
}

// Tags livres (spec §4.10): chips com cartões e vencidos de cada tag; clicar
// num chip filtra a revisão por ela. As mesmas tags alimentam as sugestões
// dos campos de tag (datalist).
function renderTags(tg, lk) {
  $("tagSuggestions").innerHTML = tg.names.map((t) => `<option value="#${esc(t)}"></option>`).join("");
  const chips = tg.names.map((t) => {
    const { cards, due } = tg.tags[t];
    const dueLabel = due ? ` · <span class="due">${plural(due, "vencido", "vencidos")}</span>` : "";
    const degree = lk.neighbors[`tag:${t}`]?.length;
    const linkLabel = degree ? ` · ⚡${degree}` : "";
    return `<button type="button" class="btn ghost sm chip" data-tag="${esc(t)}" title="Clique: filtra a revisão por #${esc(t)} · botão direito: renomeia">#${esc(t)} · ${cards}${dueLabel}${linkLabel}</button>`;
  });
  $("tagChips").innerHTML = chips.length ? `<span class="meta">Tags:</span> ${chips.join("")}` : "";
  $("tagChips").querySelectorAll("[data-tag]").forEach((el) => {
    el.onclick = () => {
      $("reviewTags").value = `#${el.dataset.tag}`;
      $("reviewFilter").open = true;
      updateReviewButton();
      $("focusPanel").scrollIntoView({ behavior: "smooth" });
    };
    // Renomear (tag.renamed): vale para cartões, decks e conexões já
    // gravados, inclusive as descendentes (so/x → novo/x).
    el.oncontextmenu = async (e) => {
      e.preventDefault();
      const tag = el.dataset.tag;
      const to = window.prompt(`Renomear #${tag} (e as descendentes ${tag}/…) para:`, `#${tag}`);
      if (to !== null) await store.renameTag(tag, to);
    };
  });
}

// Sugestões de nó (cartões ativos, decks ativos, tags) para "conectar a…" e
// o seletor "ao redor de" da revisão, com os nós que têm conexão.
function renderGraph(lt, dk, tg, lk, pg, tk) {
  graph = { cards: lt.cards, decks: dk.decks, pages: pg.pages, tasks: tk.tasks, links: lk };
  nodeByLabel.clear();
  for (const [id, c] of Object.entries(lt.cards)) {
    if (!c.archived && !(c.deck_id && dk.decks[c.deck_id]?.archived)) nodeByLabel.set(`cartão: ${c.front}`, `card:${id}`);
  }
  for (const [id, d] of Object.entries(dk.decks)) if (!d.archived) nodeByLabel.set(`deck: ${d.name}`, `deck:${id}`);
  for (const t of tg.names) nodeByLabel.set(`#${t}`, `tag:${t}`);
  $("nodeSuggestions").innerHTML = [...nodeByLabel.keys()].map((l) => `<option value="${esc(l).replace(/"/g, "&quot;")}"></option>`).join("");

  const sel = $("reviewAround");
  const prev = sel.value;
  sel.innerHTML = '<option value="">ao redor de: — (qualquer)</option>' +
    lk.nodes.map((k) => `<option value="${esc(k).replace(/"/g, "&quot;")}">ao redor de: ${esc(nodeLabel(k))} · ⚡${lk.neighbors[k].length}</option>`).join("");
  setAround(prev);
}

// Seleciona o nó do "ao redor de"; um nó sem conexões (vindo do grafo ou de
// uma sessão salva) ganha a própria opção — a vizinhança dele é ele mesmo.
function setAround(key) {
  const sel = $("reviewAround");
  if (key && ![...sel.options].some((o) => o.value === key)) {
    const option = document.createElement("option");
    option.value = key;
    option.textContent = `ao redor de: ${nodeLabel(key)}`;
    sel.append(option);
  }
  sel.value = key || "";
}

// Preenche o filtro da revisão (sessão salva ou pedido do grafo) e abre o
// painel do filtro, para o usuário ver o que vai estudar antes de começar.
async function applyFilter({ decks = [], tags = [], around = [], practice = false } = {}) {
  const deck = decks.find((id) => [...$("reviewDeckSel").options].some((o) => o.value === id)) ?? "";
  $("reviewDeckSel").value = deck;
  $("reviewTags").value = tags.map((t) => `#${t}`).join(" ");
  setAround(around[0] ?? "");
  $("reviewPractice").checked = !!practice;
  $("reviewFilter").open = true;
  await updateReviewButton();
}

// ---------- Sessões salvas (catálogo 1.5) ----------
let savedStudies = { studies: {}, order: [] };
let pendingStudyName = null; // recém-salva: selecionada quando chegar na projeção

function renderStudies(st) {
  savedStudies = st;
  const sel = $("savedStudySel");
  const pending = st.order.find((id) => st.studies[id].name === pendingStudyName);
  if (pending) pendingStudyName = null;
  const want = pending ?? sel.value;
  sel.innerHTML = '<option value="">sessões salvas…</option>' +
    st.order.map((id) => `<option value="${esc(id)}">${esc(st.studies[id].name)}</option>`).join("");
  sel.value = st.studies[want] ? want : "";
  $("deleteStudy").hidden = !sel.value;
}
$("savedStudySel").onchange = async () => {
  const study = savedStudies.studies[$("savedStudySel").value];
  $("deleteStudy").hidden = !study;
  if (study) await applyFilter(study);
};
$("saveStudy").onclick = async () => {
  const current = savedStudies.studies[$("savedStudySel").value];
  const name = window.prompt("Nome da sessão (o mesmo nome atualiza o filtro):", current?.name ?? "");
  if (!name?.trim()) return;
  const filter = reviewFilter();
  // Mesmo nome = a mesma sessão: study.saved com o mesmo id substitui o filtro.
  const study_id = savedStudies.order.find((id) => savedStudies.studies[id].name === name.trim());
  pendingStudyName = name.trim();
  await store.saveStudy({ study_id, name, ...filter, practice: $("reviewPractice").checked });
};
$("deleteStudy").onclick = async () => {
  const id_ = $("savedStudySel").value;
  const study = savedStudies.studies[id_];
  if (!study || !window.confirm(`Apagar a sessão "${study.name}"? Só o filtro some — os cartões ficam.`)) return;
  await store.deleteStudy(id_);
  $("savedStudySel").value = "";
};

// ---------- Grafo (outra aba) e o "estudar ao redor" que volta dele ----------
$("openGraph").onclick = () => {
  const url = chrome.runtime?.getURL?.("graph/graph.html") ?? "../graph/graph.html";
  if (chrome.tabs?.create) chrome.tabs.create({ url });
  else window.open(url, "_blank");
};
async function takeReviewRequest() {
  const request = await store.takeReviewRequest();
  if (!request) return;
  $("savedStudySel").value = "";
  $("deleteStudy").hidden = true;
  await applyFilter(request);
  $("focusPanel").scrollIntoView?.({ behavior: "smooth" });
}
store.onReviewRequest(takeReviewRequest);
$("cardDeckSel").onchange = (e) => { selectedDeck = e.target.value; };
$("deckForm").onsubmit = async (e) => {
  e.preventDefault();
  // Normaliza o caminho: apara cada segmento e descarta vazios ("A :: B" → "A::B").
  const name = $("deckName").value.split(DECK_SEP).map((s) => s.trim()).filter(Boolean).join(DECK_SEP);
  if (!name) return;
  await store.createDeck(name);
  e.target.reset();
};
$("cardForm").onsubmit = async (e) => {
  e.preventDefault();
  const front = $("cardFront").value.trim(), back = $("cardBack").value.trim();
  if (!front || !back) return;
  const url = $("cardSource").value.trim();
  const extra = { tags: store.parseTags($("cardTags").value), ...(url ? { source_url: url } : {}) };
  await store.createCard(selectedDeck || null, front, back, extra);
  // As tags ficam para o próximo cartão da série (como na CLI).
  $("cardFront").value = ""; $("cardBack").value = ""; $("cardSource").value = "";
};

// ---------- Retrospectiva semanal ----------
// weekOffset navega no histórico: 0 = semana atual, 1 = passada, e assim por
// diante — a projeção é pura em (eventos, now, tz), então basta deslocar o
// instante de referência 7 dias por semana.
let weekOffset = 0;
const DOW_PT = ["dom", "seg", "ter", "qua", "qui", "sex", "sáb"];
const brDate = (iso) => `${iso.slice(8, 10)}/${iso.slice(5, 7)}`;

async function renderWeek() {
  const refMs = Date.parse(store.nowIso()) - weekOffset * 7 * 86400000;
  const w = await store.state.review(new Date(refMs).toISOString());

  $("weekLabel").textContent =
    `${brDate(w.week_start)} – ${brDate(w.week_end)}` +
    (weekOffset ? ` · ${weekOffset} sem. atrás` : "");
  $("weekNext").disabled = weekOffset === 0;

  const rows = Object.keys(w.days).sort().map((day) => {
    const d = w.days[day];
    const wd = DOW_PT[new Date(`${day}T12:00:00Z`).getUTCDay()];
    const parts = [];
    if (d.focus_minutes) parts.push(`${d.focus_minutes} min foco`);
    if (d.reviews) parts.push(`${d.reviews} rev.`);
    if (d.pause_minutes) parts.push(`${d.pause_minutes} min pausa`);
    if (d.completed || d.interrupted) parts.push(`${d.completed}✔ ${d.interrupted}✗`);
    if (d.checkins) parts.push(`na tarefa ${d.checkins_on_task}/${d.checkins}`);
    return `<div class="deck"><span class="grow">${wd} ${brDate(day)}</span>
      <span class="count">${parts.join(" · ") || "—"}</span></div>`;
  });

  const t = w.totals;
  const totCheckins = t.checkins ? ` · na tarefa ${t.checkins_on_task}/${t.checkins}` : "";
  rows.push(`<div class="deck"><span class="grow"><b>total</b></span>
    <span class="count">${t.focus_minutes} min foco · ${t.reviews} rev. · ${t.pause_minutes} min pausa · ${t.completed}✔ ${t.interrupted}✗${totCheckins}</span></div>`);

  const reasons = Object.entries(w.reasons)
    .map(([r, n]) => `<span class="tag">${n}× ${esc(r)}</span>`)
    .join(" ");
  $("weekBody").innerHTML =
    rows.join("") +
    (reasons ? `<div class="meta" style="margin-top:6px">interrupções: ${reasons}</div>` : "") +
    (w.appointment_minutes ? `<div class="meta" style="margin-top:4px">agendado: ${w.appointment_minutes} min · executado: ${t.focus_minutes} min</div>` : "");
}
$("weekPrev").onclick = () => { weekOffset++; renderWeek(); };
$("weekNext").onclick = () => { if (weekOffset > 0) { weekOffset--; renderWeek(); } };

// Seletor de tarefa do foco: a lista do dia (A→B→C), preservando a seleção.
function renderFocusTasks(t) {
  dayTasks = t;
  const sel = $("focusTask");
  const prev = sel.value;
  sel.innerHTML =
    '<option value="">(sem tarefa específica)</option>' +
    t.day_list
      .map((id) => {
        const task = t.tasks[id];
        const pri = task.priority ? `[${task.priority}] ` : "";
        return `<option value="${id}">${pri}${esc(task.title)}</option>`;
      })
      .join("");
  if (t.day_list.includes(prev)) sel.value = prev;
}

// ---------- Refresh geral ----------
async function refresh() {
  const [s, inb, dk, lt, tk, mins, tg, lk, st, pg] = await Promise.all([
    store.state.stats(), store.state.inbox(), store.state.decks(),
    store.state.leitner(), store.state.tasks(), store.state.attentionMinutes(),
    store.state.tags(), store.state.links(), store.state.studies(), store.state.pages(),
  ]);
  lastInbox = inb;
  const flame = s.streak > 0 ? "🔥" : "•";
  $("streak").textContent = `${flame} ${plural(s.streak, "dia", "dias")} · ${lt.queue.length} para revisar`;
  $("suggestion").textContent = `sugestão: ${mins} min`;
  if ($("focusActive").hidden && !$("minutes").value) $("minutes").value = mins;
  renderFocusTasks(tk);
  renderInbox(inb);
  renderDecks(dk, lt);
  renderTags(tg, lk);
  renderGraph(lt, dk, tg, lk, pg, tk);
  renderStudies(st);
  await renderWeek();
  await updateReviewButton();
}

store.onChange(refresh);
mountDebugBar(document.getElementById("debugBar"));
mountSyncPanel(document.getElementById("syncPanel"));
store.ready.then(refresh).then(restoreActiveSession).then(takeReviewRequest);
