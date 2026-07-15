import { describe, it, expect } from "vitest";
import { memoryKV } from "../storage/kv.js";
import { EventLog } from "../storage/log.js";
import { uuidv7 } from "../storage/id.js";
import { project as tasks } from "../core/tasks.js";
import { project as leitner } from "../core/leitner.js";

// Log determinístico: ids sequenciais e relógio de parede fixo por chamada.
function fixtureLog(initial = {}) {
  let n = 0;
  const kv = memoryKV(initial);
  const log = new EventLog(kv, {
    genId: () => `id-${String(++n).padStart(3, "0")}`,
    clock: () => `2026-07-01T10:0${Math.min(n, 9)}:00.000Z`,
  });
  return { kv, log };
}

describe("EventLog.append", () => {
  it("monta envelopes válidos com lc incremental e device estável", async () => {
    const { log } = fixtureLog();
    const a = await log.append("task.created", { task_id: "t1", title: "A" });
    const b = await log.append("task.created", { task_id: "t2", title: "B" });

    expect(a.lc).toBe(1);
    expect(b.lc).toBe(2);
    expect(a.v).toBe(1);
    expect(a.device).toBe(b.device);
    expect(a.type).toBe("task.created");
    expect(a.payload).toEqual({ task_id: "t1", title: "A" });
    expect((await log.events()).length).toBe(2);
  });

  it("persiste no KV para que uma nova instância recupere o log e o lamport", async () => {
    const { kv, log } = fixtureLog();
    await log.append("task.created", { task_id: "t1", title: "A" });

    const reopened = new EventLog(kv, { genId: () => "id-x" });
    const next = await reopened.append("task.created", { task_id: "t2", title: "B" });
    expect(next.lc).toBe(2); // contador sobreviveu ao "restart"
    expect((await reopened.events()).length).toBe(2);
  });
});

describe("EventLog.project", () => {
  it("alimenta uma projeção pura com o log acumulado", async () => {
    const { log } = fixtureLog();
    await log.append("task.created", { task_id: "t1", title: "A" });
    await log.append("task.prioritized", { task_id: "t1", priority: "A" });
    await log.append("task.created", { task_id: "t2", title: "B" });

    const state = await log.project(tasks);
    expect(state.day_list).toEqual(["t1", "t2"]);
    expect(state.tasks.t1.priority).toBe("A");
  });

  it("um card recém-criado vence imediatamente na fila do Leitner", async () => {
    const { log } = fixtureLog();
    await log.append("deck.created", { deck_id: "d1", name: "Deck", tags: [] });
    const card = await log.append("card.created", {
      card_id: "c1",
      deck_id: "d1",
      front: "f",
      back: "b",
      tags: [],
    });

    const state = await log.project(leitner, {
      now: "2026-07-01T23:00:00Z",
      tz: "America/Sao_Paulo",
    });
    expect(state.queue).toContain(card.payload.card_id);
  });
});

describe("EventLog.merge (download do sync)", () => {
  const remote = (id, lc) => ({
    id,
    type: "task.created",
    v: 1,
    lc,
    ts: "2026-06-01T00:00:00.000Z",
    device: "dev-remote",
    payload: { task_id: id, title: id },
  });

  it("deduplica por id e avança o lamport para o maior lc recebido", async () => {
    const { log } = fixtureLog();
    await log.append("task.created", { task_id: "t1", title: "A" }); // lc=1

    const added = await log.merge([remote("r1", 5), remote("r2", 9)]);
    expect(added).toBe(2);

    const next = await log.append("task.created", { task_id: "t3", title: "C" });
    expect(next.lc).toBe(10); // max(1, 9) + 1
  });

  it("é idempotente: reaplicar os mesmos eventos não muda o log", async () => {
    const { log } = fixtureLog();
    await log.merge([remote("r1", 5), remote("r2", 9)]);
    const before = await log.events();

    const added = await log.merge([remote("r1", 5), remote("r2", 9)]);
    expect(added).toBe(0);
    expect(await log.events()).toEqual(before);
  });
});

describe("uuidv7", () => {
  it("emite a versão/variante corretas e é único", () => {
    const a = uuidv7();
    const b = uuidv7();
    expect(a).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
    );
    expect(a).not.toBe(b);
  });
});
