// Log de eventos append-only persistido sobre um KV (spec/SPEC.md §1, §2, §5).
// Fonte de verdade do cliente: o estado (decks, tarefas, etc.) é sempre uma
// projeção deste log. Guarda também o relógio de Lamport, o id do dispositivo
// e o cursor de sincronização.

import { normalize } from "../core/envelope.js";
import { uuidv7 } from "./id.js";

const KEYS = {
  log: "events",
  lamport: "lamport",
  device: "device",
  cursor: "cursor",
};

export class EventLog {
  // Dependências temporais/aleatórias são injetáveis para deixar os testes
  // determinísticos; em produção usam relógio e crypto reais.
  constructor(kv, { clock = () => new Date().toISOString(), genId = uuidv7 } = {}) {
    this.kv = kv;
    this.clock = clock;
    this.genId = genId;
  }

  async deviceId() {
    let device = await this.kv.get(KEYS.device);
    if (!device) {
      device = "dev-" + this.genId();
      await this.kv.set(KEYS.device, device);
    }
    return device;
  }

  async events() {
    return (await this.kv.get(KEYS.log)) ?? [];
  }

  // Emite um novo evento local: lc = contador + 1, id/ts do dispositivo.
  async append(type, payload, { v = 1 } = {}) {
    const lc = ((await this.kv.get(KEYS.lamport)) ?? 0) + 1;
    const event = {
      id: this.genId(),
      type,
      v,
      lc,
      ts: this.clock(),
      device: await this.deviceId(),
      payload,
    };
    const log = await this.events();
    log.push(event);
    await this.kv.set(KEYS.log, log);
    await this.kv.set(KEYS.lamport, lc);
    return event;
  }

  // Roda uma projeção pura (core/*) sobre o log corrente.
  async project(projection, params) {
    return projection(await this.events(), params);
  }

  // --- Sincronização (SPEC §5) ---

  async getCursor() {
    return (await this.kv.get(KEYS.cursor)) ?? null;
  }

  async setCursor(cursor) {
    await this.kv.set(KEYS.cursor, cursor);
  }

  // Envelopes locais candidatos a upload. Por ora, todos: o servidor deduplica
  // por id e não lê payload, então reenviar é correto (idempotente). Envio
  // incremental por confirmação é otimização de trabalho futuro.
  async outbox() {
    return this.events();
  }

  // Incorpora eventos recebidos do servidor: deduplica por id e avança o
  // relógio de Lamport para max(local, max(lc recebidos)).
  async merge(incoming) {
    const log = await this.events();
    const seen = new Set(log.map((e) => e.id));
    let lamport = (await this.kv.get(KEYS.lamport)) ?? 0;
    let added = 0;

    for (const event of incoming) {
      if (event.lc > lamport) lamport = event.lc;
      if (seen.has(event.id)) continue;
      seen.add(event.id);
      log.push(event);
      added++;
    }

    await this.kv.set(KEYS.log, normalize(log));
    await this.kv.set(KEYS.lamport, lamport);
    return added;
  }
}
