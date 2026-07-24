import { describe, it, expect } from "vitest";
import { reminderOffsets, reminderLabel, dueReminders } from "../core/calendar.js";

const MIN = 60000;
const iso = (ms) => new Date(ms).toISOString();

describe("agenda de lembretes (reminderOffsets)", () => {
  it("compromisso comum lembra só na reta final", () => {
    expect(reminderOffsets()).toEqual([1440, 60, 10, 0]);
  });

  it("importante ganha a cascata semanal antes da reta final", () => {
    const off = reminderOffsets("important");
    expect(off.slice(0, 8)).toEqual([80640, 70560, 60480, 50400, 40320, 30240, 20160, 10080]);
    expect(off.slice(-4)).toEqual([1440, 60, 10, 0]);
  });
});

describe("rótulos (reminderLabel)", () => {
  it("concorda em número e gênero", () => {
    expect(reminderLabel(10080)).toBe("Falta 1 semana");
    expect(reminderLabel(20160)).toBe("Faltam 2 semanas");
    expect(reminderLabel(1440)).toBe("Falta 1 dia");
    expect(reminderLabel(60)).toBe("Falta 1 hora");
    expect(reminderLabel(10)).toBe("Faltam 10 minutos");
    expect(reminderLabel(0)).toBe("Começa agora");
  });
});

describe("lembretes vencidos e próximo despertar (dueReminders)", () => {
  const now = Date.parse("2026-07-24T12:00:00Z");
  const nowIso = iso(now);
  const appt = (id, startMs, importance) => ({
    [id]: { title: id, starts_at: iso(startMs), ends_at: iso(startMs + 60 * MIN), ...(importance ? { importance } : {}) },
  });

  it("dispara o lembrete de 1 hora quando o instante chega", () => {
    // Começa em 60 min: o lembrete "1 hora antes" cai exatamente agora.
    const cal = { appointments: appt("a", now + 60 * MIN) };
    const { due } = dueReminders(cal, { now: nowIso });
    expect(due.map((d) => d.offset)).toContain(60);
    expect(due.find((d) => d.offset === 60).label).toBe("Falta 1 hora");
  });

  it("não repete um lembrete já disparado (fired)", () => {
    const cal = { appointments: appt("a", now + 60 * MIN) };
    const { due } = dueReminders(cal, { now: nowIso, fired: { "a@60": true } });
    expect(due.some((d) => d.offset === 60)).toBe(false);
  });

  it("ignora lembretes muito atrasados ao criar um compromisso próximo", () => {
    // Compromisso daqui a 2h criado agora: o "1 dia antes" venceu há ~22h e não
    // deve disparar (fora da janela de tolerância); só a reta final futura conta.
    const cal = { appointments: appt("a", now + 2 * 60 * MIN) };
    const { due, next } = dueReminders(cal, { now: nowIso });
    expect(due.some((d) => d.offset === 1440)).toBe(false);
    expect(next).toBe(now + 60 * MIN); // próximo: "1 hora antes"
  });

  it("aponta o próximo despertar para o lembrete futuro mais próximo", () => {
    const cal = { appointments: appt("a", now + 3 * 24 * 60 * MIN, "important") };
    const { next } = dueReminders(cal, { now: nowIso });
    expect(next).toBe(now + (3 * 24 * 60 - 1440) * MIN); // 1 dia antes do início
  });

  it("compromissos passados não geram lembrete nem despertar", () => {
    const cal = { appointments: appt("a", now - 60 * MIN) };
    expect(dueReminders(cal, { now: nowIso })).toEqual({ due: [], next: null });
  });
});
