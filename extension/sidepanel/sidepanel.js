import * as store from "../ui/store.js";
import { MASCOT_SVG } from "../ui/mascot.js";
import { mountDebugBar } from "../ui/debug.js";

const $ = (id) => document.getElementById(id);
const esc = (s) => String(s).replace(/[&<>]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" }[c]));
document.querySelector("#brand").insertAdjacentHTML("afterbegin", MASCOT_SVG);

// Estado local (não é projeção): sessão de foco e a revisão em andamento.
let session = null; // { id, kind, endMs, interval }
let review = null; // { queue, index, cards, revealed }
let selectedDeck = "";
let dayTasks = { tasks: {}, day_list: [] }; // para o seletor de tarefa do foco

// Check-in de atenção (self-monitoring, Safren): o alarme dispara no service
// worker (funciona com o painel fechado); esta chave diz a ele qual sessão
// perguntar e até quando. Fora da sessão, a chave não existe.
const CHECKIN_ALARM = "pnn-checkin";
const CHECKIN_KEY = "active_checkin";

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
  if (left <= 0) finish("completed");
}
async function beginSession(kind, label) {
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
}
async function finish(outcome, reason) {
  if (!session) return;
  clearInterval(session.interval);
  const id = session.id;
  session = null;
  review = null;
  showIdle();
  await disarmCheckin();
  await store.endSession(id, outcome, reason);
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
  pause = { id: pid, endMs: Date.now() + minutes * 60000, interval: null };
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
$("startReview").onclick = async () => {
  const lt = await store.state.leitner();
  review = { queue: [...lt.queue], index: 0, cards: lt.cards, revealed: false };
  await beginSession("review", "Revisão");
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
  $("cardFace").innerHTML = esc(c.front ?? "");
  $("cardDeck").textContent = `caixa ${c.box}`;
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
};
async function grade(result) {
  const cardId = review.queue[review.index];
  await store.reviewCard(cardId, result, session?.id);
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
  const fields = {};
  if (front.trim() && front.trim() !== c.front) fields.front = front.trim();
  if (back.trim() && back.trim() !== c.back) fields.back = back.trim();
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
    const src = n.url ? ` <span class="tag">🔗</span>` : "";
    return `<div class="inbox-item">
      <div class="kind">nota${src ? " · com fonte" : ""}</div>
      <div class="text">${esc(n.text)}${src}</div>
      <div class="actions">
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
    if (!selectedDeck) { alert("Crie/escolha um deck primeiro (seção Decks)."); return; }
    const n = lastInbox.notes[id];
    const source = {};
    if (n?.url) source.source_url = n.url; // preserva a referência de origem
    if (n?.page_title) source.source_title = n.page_title;
    const card = await store.createCard(selectedDeck, n?.text ?? "", "(preencher o verso)", source);
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

$("noteForm").onsubmit = async (e) => {
  e.preventDefault();
  const text = $("noteText").value.trim();
  if (!text) return;
  await store.captureNote(text);
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
  sel.innerHTML = real
    .map((d) => `<option value="${d.id}">${"  ".repeat(d.depth)}${esc(d.path)}</option>`)
    .join("");
  if (real.length) {
    if (!real.some((d) => d.id === selectedDeck)) selectedDeck = real[0].id;
    sel.value = selectedDeck;
  } else selectedDeck = "";
  // Sem deck não há onde criar cartão: um formulário com seletor vazio parece
  // quebrado — só aparece quando existe ao menos um deck real.
  $("cardForm").style.display = real.length ? "flex" : "none";
}
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
  if (!front || !back || !selectedDeck) return;
  const url = $("cardSource").value.trim();
  await store.createCard(selectedDeck, front, back, url ? { source_url: url } : {});
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
  const [s, inb, dk, lt, tk, mins] = await Promise.all([
    store.state.stats(), store.state.inbox(), store.state.decks(),
    store.state.leitner(), store.state.tasks(), store.state.attentionMinutes(),
  ]);
  lastInbox = inb;
  const flame = s.streak > 0 ? "🔥" : "•";
  $("streak").textContent = `${flame} ${plural(s.streak, "dia", "dias")} · ${lt.queue.length} para revisar`;
  $("suggestion").textContent = `sugestão: ${mins} min`;
  if ($("focusActive").hidden && !$("minutes").value) $("minutes").value = mins;
  renderFocusTasks(tk);
  renderInbox(inb);
  renderDecks(dk, lt);
  await renderWeek();
  // Fila vazia: revisar não leva a lugar nenhum (sessão para nada) — o botão
  // desabilita e o destaque primário passa para "Iniciar foco".
  const hasDue = lt.queue.length > 0;
  const rev = $("startReview");
  rev.textContent = `Revisar flashcards (${lt.queue.length})`;
  rev.disabled = !hasDue;
  rev.classList.toggle("primary", hasDue);
  rev.classList.toggle("ghost", !hasDue);
  $("startFocus").classList.toggle("primary", !hasDue);
}

store.onChange(refresh);
mountDebugBar(document.getElementById("debugBar"));
store.ready.then(refresh);
