import { describe, it, expect } from "vitest";
import { memoryKV } from "../storage/kv.js";
import { EventLog } from "../storage/log.js";
import { buildDemoEvents } from "../ui/demo-events.js";
import { project as stats } from "../core/stats.js";
import { project as leitner } from "../core/leitner.js";
import { project as inbox } from "../core/inbox.js";
import { project as tasks } from "../core/tasks.js";
import { project as calendar } from "../core/calendar.js";
import { project as review } from "../core/review.js";
import { attentionWindowSeconds } from "../core/attention.js";

// Espelho de scripts/seed-demo.sh: o mesmo cenário dos prints da CLI,
// injetado na extensão via merge() como se tivesse chegado pelo sync.
const NOW = "2026-07-08T15:00:00.000Z";
const TZ = "America/Sao_Paulo";
const day = { now: NOW, tz: TZ };

async function seededLog() {
  const log = new EventLog(memoryKV());
  await log.merge(buildDemoEvents(NOW));
  return log;
}

describe("cenário de demonstração da extensão", () => {
  it("streak de 4 dias terminando ontem, sem revisões hoje", async () => {
    const log = await seededLog();
    const s = await log.project(stats, day);
    expect(s.streak).toBe(4);
    expect(s.reviews_today).toBe(0);
  });

  it("3 cartões vencidos hoje; o 1º da fila tem fonte e explicação anterior", async () => {
    const log = await seededLog();
    const l = await log.project(leitner, day);
    expect(l.queue).toEqual(["c-rr", "c-pt", "c-en"]);
    expect(l.cards["c-rr"].source_url).toContain("OSTEP");
    expect(l.cards["c-rr"].last_explanation).toBeTruthy();
    expect(l.cards["c-rr"].box).toBe(2);
  });

  it("janela de atenção = mediana de 27 min (a interrompida fica fora)", async () => {
    const log = await seededLog();
    expect(await log.project(attentionWindowSeconds)).toBe(27 * 60);
  });

  it("a sessão de ontem tem check-in de atenção: na tarefa em 1 de 2 (RF07)", async () => {
    const log = await seededLog();
    const w = await log.project(review, day);
    expect(w.totals.checkins).toBe(2);
    expect(w.totals.checkins_on_task).toBe(1);
  });

  it("caixa de entrada com 1 nota (com fonte) e 1 distração pendentes", async () => {
    const log = await seededLog();
    const i = await log.project(inbox);
    expect(i.pending_notes).toHaveLength(1);
    expect(i.pending_distractions).toHaveLength(1);
    expect(i.notes[i.pending_notes[0]].url).toBeTruthy();
  });

  it("lista do dia: A com 2 subtarefas, B e C, sem alerta de excesso", async () => {
    const log = await seededLog();
    const t = await log.project(tasks);
    expect(t.day_list).toHaveLength(5);
    const children = t.day_list.filter((id) => t.tasks[id].parent_id === "t-imp");
    expect(children).toHaveLength(2);
    expect(t.tasks["t-imp"].priority).toBe("A");
    expect(t.alerts.too_many_a).toBe(false);
  });

  it("agenda com compromissos de hoje, de amanhã e a defesa importante", async () => {
    const log = await seededLog();
    const c = await log.project(calendar, day);
    expect(c.upcoming).toHaveLength(3);
    expect(c.appointments["a-defesa"].importance).toBe("important");
  });

  it("é idempotente: injetar duas vezes não adiciona nada", async () => {
    const log = await seededLog();
    expect(await log.merge(buildDemoEvents(NOW))).toBe(0);
  });
});
