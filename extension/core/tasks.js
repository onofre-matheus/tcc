// Lista do dia (spec/SPEC.md §4.3) — RF02/RF03.
// Tarefas ativas ordenadas A→B→C (sem prioridade = C); empate por criação.

import { normalize } from "./envelope.js";

const PRIORITY_RANK = { A: 0, B: 1, C: 2 };
const MAX_ACTIVE_A = 4;

function rankOf(priority) {
  return PRIORITY_RANK[priority ?? "C"];
}

export function project(events) {
  const tasks = new Map();
  let seq = 0;

  for (const event of normalize(events)) {
    const { payload } = event;
    if (event.type === "task.created") {
      tasks.set(payload.task_id, {
        title: payload.title,
        parent_id: payload.parent_id ?? null,
        due_date: payload.due_date ?? null,
        priority: null,
        done: false,
        seq: seq++,
      });
    } else if (event.type === "task.prioritized") {
      const task = tasks.get(payload.task_id);
      if (task) task.priority = payload.priority; // última repriorização vence
    } else if (event.type === "task.edited") {
      const task = tasks.get(payload.task_id);
      if (task) {
        // last-writer-wins por campo: só os campos presentes mudam
        if (payload.title !== undefined) task.title = payload.title;
        if (payload.due_date !== undefined) task.due_date = payload.due_date;
      }
    } else if (event.type === "task.deleted") {
      tasks.delete(payload.task_id); // tombstone: eventos posteriores são ignorados
    } else if (event.type === "task.completed") {
      const task = tasks.get(payload.task_id);
      if (task) task.done = true;
    }
  }

  const active = [...tasks.entries()].filter(([, t]) => !t.done);

  const day_list = active
    .slice()
    .sort(([, a], [, b]) => {
      const dr = rankOf(a.priority) - rankOf(b.priority);
      return dr !== 0 ? dr : a.seq - b.seq;
    })
    .map(([id]) => id);

  const activeA = active.filter(([, t]) => t.priority === "A").length;

  const tasksOut = {};
  for (const [id, { title, parent_id, due_date, priority, done }] of tasks) {
    tasksOut[id] = { title, parent_id, due_date, priority, done };
  }

  return {
    tasks: tasksOut,
    day_list,
    alerts: { too_many_a: activeA > MAX_ACTIVE_A },
  };
}
