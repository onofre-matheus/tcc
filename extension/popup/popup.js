import * as store from "../ui/store.js";
import { MASCOT_SVG } from "../ui/mascot.js";
import { mountDebugBar } from "../ui/debug.js";

const $ = (id) => document.getElementById(id);
document.querySelector("#brand").insertAdjacentHTML("afterbegin", MASCOT_SVG);

// --- Datas locais ---
const dayFmt = new Intl.DateTimeFormat("en-CA", {
  year: "numeric", month: "2-digit", day: "2-digit",
});
const localKey = (d) => dayFmt.format(d);
const timeFmt = new Intl.DateTimeFormat("pt-BR", { hour: "2-digit", minute: "2-digit" });
const monthFmt = new Intl.DateTimeFormat("pt-BR", { month: "long", year: "numeric" });
const dayLabelFmt = new Intl.DateTimeFormat("pt-BR", { weekday: "long", day: "numeric", month: "long" });
const DOW = ["dom", "seg", "ter", "qua", "qui", "sex", "sáb"];

const simNow = () => new Date(store.nowIso()); // relógio (com offset de debug)
let view = { y: simNow().getFullYear(), m: simNow().getMonth() };
let selectedKey = localKey(simNow());

// --- Render ---
async function render() {
  const [cal, tasksState, statsState] = await Promise.all([
    store.state.calendar(), store.state.tasks(), store.state.stats(),
  ]);
  renderStreak(statsState);
  renderCalendar(cal);
  renderDay(cal);
  renderTasks(tasksState);
}

function plural(n, one, many) {
  return `${n} ${n === 1 ? one : many}`;
}

function renderStreak(s) {
  const flame = s.streak > 0 ? "🔥" : "•";
  $("streak").textContent =
    `${flame} ${plural(s.streak, "dia", "dias")} de sequência · ${plural(s.reviews_today, "revisão", "revisões")} hoje`;
}

function byDay(appointments) {
  const map = new Map();
  for (const [id, a] of Object.entries(appointments)) {
    const key = localKey(new Date(a.starts_at));
    if (!map.has(key)) map.set(key, []);
    map.get(key).push(id);
  }
  return map;
}

let appointmentsByDay = new Map();

function renderCalendar(cal) {
  appointmentsByDay = byDay(cal.appointments);
  $("calTitle").textContent = monthFmt.format(new Date(view.y, view.m, 1));
  $("calDow").innerHTML = DOW.map((d) => `<div>${d}</div>`).join("");

  const firstWeekday = new Date(view.y, view.m, 1).getDay();
  const daysInMonth = new Date(view.y, view.m + 1, 0).getDate();
  const todayKey = localKey(simNow());
  const cells = [];
  for (let i = 0; i < firstWeekday; i++) cells.push('<div class="cal-cell blank"></div>');
  for (let day = 1; day <= daysInMonth; day++) {
    const key = localKey(new Date(view.y, view.m, day));
    const cls = ["cal-cell", "selectable"];
    if (key === todayKey) cls.push("today");
    if (key === selectedKey) cls.push("selected");
    const dot = appointmentsByDay.has(key) ? '<span class="dot"></span>' : "";
    cells.push(`<div class="${cls.join(" ")}" data-key="${key}">${day}${dot}</div>`);
  }
  $("calGrid").innerHTML = cells.join("");
  for (const cell of $("calGrid").querySelectorAll(".selectable")) {
    cell.onclick = () => { selectedKey = cell.dataset.key; render(); };
  }
}

function renderDay(cal) {
  const [y, m, d] = selectedKey.split("-").map(Number);
  $("dayLabel").textContent = dayLabelFmt.format(new Date(y, m - 1, d));
  const ids = (appointmentsByDay.get(selectedKey) ?? [])
    .sort((a, b) => (cal.appointments[a].starts_at < cal.appointments[b].starts_at ? -1 : 1));
  const box = $("dayAppointments");
  box.innerHTML = ids.length
    ? ids.map((id) => {
        const a = cal.appointments[id];
        const t = `${timeFmt.format(new Date(a.starts_at))}–${timeFmt.format(new Date(a.ends_at))}`;
        return `<div class="appt"><span class="time">${t}</span><span class="grow title">${esc(a.title)}</span>
          <button class="btn ghost sm" data-cancel="${id}" title="Cancelar compromisso">✕</button></div>`;
      }).join("")
    : '<div class="empty">Sem compromissos.</div>';
  for (const el of box.querySelectorAll("[data-cancel]")) {
    el.onclick = () => store.cancelAppointment(el.dataset.cancel);
  }
}

const PRI_CYCLE = { A: "B", B: "C", C: "A" };
function renderTasks(t) {
  $("taskAlert").innerHTML = t.alerts.too_many_a
    ? '<div class="alert">⚠️ Mais de 4 tarefas A ativas. Reavalie o que é mesmo urgente.</div>'
    : "";
  const list = $("taskList");
  if (!t.day_list.length) { list.innerHTML = '<div class="empty">Nada pendente. 🎉</div>'; return; }

  // Quebra de tarefas (Safren, §2.2): monta a árvore pai→subtarefas a partir da
  // lista plana. day_list já vem ordenado A→B→C, então iterá-la preserva essa
  // ordem entre irmãos. Subtarefa cujo pai não está mais ativo vira raiz para
  // não sumir da lista do dia.
  const active = new Set(t.day_list);
  const childrenOf = new Map();
  const roots = [];
  for (const id of t.day_list) {
    const parent = t.tasks[id].parent_id;
    if (parent && active.has(parent)) {
      (childrenOf.get(parent) ?? childrenOf.set(parent, []).get(parent)).push(id);
    } else {
      roots.push(id);
    }
  }

  const rows = [];
  const walk = (id, depth) => {
    const task = t.tasks[id];
    const pri = task.priority ?? "C";
    rows.push(`<div class="item${depth ? " sub" : ""}" style="--depth:${depth}">
      <button class="pri ${pri}" data-pri="${id}" title="Trocar prioridade" aria-label="Trocar prioridade">${task.priority ?? "–"}</button>
      <span class="grow title" title="${esc(task.title)}">${esc(task.title)}</span>
      <span class="item-actions">
        <button class="btn ghost sm" data-add="${id}" title="Quebrar em subtarefa" aria-label="Quebrar em subtarefa">＋</button>
        <button class="btn ghost sm" data-edit="${id}" title="Editar título" aria-label="Editar título">✎</button>
        <button class="btn ghost sm" data-del="${id}" title="Apagar" aria-label="Apagar">✕</button>
      </span>
      <button class="btn ghost sm" data-done="${id}" title="Concluir" aria-label="Concluir">✓</button>
    </div>`);
    for (const kid of childrenOf.get(id) ?? []) walk(kid, depth + 1);
  };
  for (const id of roots) walk(id, 0);
  list.innerHTML = rows.join("");

  for (const el of list.querySelectorAll("[data-pri]")) {
    el.onclick = async () => {
      const cur = t.tasks[el.dataset.pri].priority;
      await store.prioritizeTask(el.dataset.pri, cur ? PRI_CYCLE[cur] : "A");
    };
  }
  for (const el of list.querySelectorAll("[data-done]")) {
    el.onclick = () => store.completeTask(el.dataset.done);
  }
  for (const el of list.querySelectorAll("[data-add]")) {
    el.onclick = () => openSubtaskForm(el.closest(".item"), el.dataset.add);
  }
  for (const el of list.querySelectorAll("[data-edit]")) {
    el.onclick = () => openEditForm(el.closest(".item"), el.dataset.edit, t.tasks[el.dataset.edit].title);
  }
  for (const el of list.querySelectorAll("[data-del]")) {
    el.onclick = () => store.deleteTask(el.dataset.del); // tombstone (task.deleted)
  }
}

// Formulário inline para quebrar uma tarefa em uma subtarefa (passo menor).
// É transitório: o re-render após createTask o descarta.
function openSubtaskForm(itemEl, parentId) {
  const existing = itemEl.nextElementSibling;
  if (existing?.classList.contains("subtask-form")) {
    existing.querySelector("input").focus();
    return;
  }
  const depth = Number(itemEl.style.getPropertyValue("--depth") || 0) + 1;
  const form = document.createElement("form");
  form.className = "subtask-form";
  form.style.setProperty("--depth", depth);
  form.innerHTML = '<input placeholder="Subtarefa (passo menor)" aria-label="Subtarefa" required />';
  itemEl.after(form);
  const input = form.querySelector("input");
  input.focus();
  form.onsubmit = async (e) => {
    e.preventDefault();
    const title = input.value.trim();
    if (title) await store.createTask(title, { parentId });
  };
  input.onkeydown = (e) => { if (e.key === "Escape") form.remove(); };
}

// Formulário inline para corrigir o título (task.edited, LWW por campo).
// Mesmo padrão transitório do subtask-form: o re-render após o evento o descarta.
function openEditForm(itemEl, taskId, currentTitle) {
  const existing = itemEl.nextElementSibling;
  if (existing?.classList.contains("subtask-form")) existing.remove();
  const form = document.createElement("form");
  form.className = "subtask-form";
  form.style.setProperty("--depth", Number(itemEl.style.getPropertyValue("--depth") || 0));
  form.innerHTML = '<input aria-label="Novo título" required />';
  itemEl.after(form);
  const input = form.querySelector("input");
  input.value = currentTitle;
  input.focus();
  input.select();
  form.onsubmit = async (e) => {
    e.preventDefault();
    const title = input.value.trim();
    if (title && title !== currentTitle) await store.editTask(taskId, { title });
    else form.remove();
  };
  input.onkeydown = (e) => { if (e.key === "Escape") form.remove(); };
}

const esc = (s) => String(s).replace(/[&<>]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" }[c]));

// --- Ações ---
$("prevMonth").onclick = () => { view.m--; if (view.m < 0) { view.m = 11; view.y--; } render(); };
$("nextMonth").onclick = () => { view.m++; if (view.m > 11) { view.m = 0; view.y++; } render(); };

$("taskForm").onsubmit = async (e) => {
  e.preventDefault();
  const title = $("taskTitle").value.trim();
  if (!title) return;
  await store.createTask(title, { priority: $("taskPri").value || undefined });
  e.target.reset();
};

$("apptForm").onsubmit = async (e) => {
  e.preventDefault();
  const title = $("apptTitle").value.trim();
  const start = $("apptStart").value, end = $("apptEnd").value;
  if (!title || !start || !end) return;
  // Interpreta o horário no fuso local do dia selecionado e grava em UTC.
  const startsAt = new Date(`${selectedKey}T${start}`).toISOString();
  const endsAt = new Date(`${selectedKey}T${end}`).toISOString();
  await store.createAppointment(title, startsAt, endsAt);
  e.target.reset();
};

store.onChange(render);
mountDebugBar(document.getElementById("debugBar"));
store.ready.then(() => {
  // Ao carregar o offset persistido, reposiciona o calendário no dia simulado.
  const n = simNow();
  view = { y: n.getFullYear(), m: n.getMonth() };
  selectedKey = localKey(n);
  render();
});
