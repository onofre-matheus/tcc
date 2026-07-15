// Janela de atenção (spec/SPEC.md §4.2) — RF05.
// Mediana das últimas 10 sessões concluídas; menos de 3 amostras → 25 min.

import { normalize } from "./envelope.js";

const DEFAULT_SECONDS = 1500;
const MIN_SAMPLES = 3;
const WINDOW = 10;

export function attentionWindowSeconds(events) {
  const startedAt = new Map();
  const durations = [];

  for (const event of normalize(events)) {
    if (event.type === "session.started") {
      startedAt.set(event.payload.session_id, event.ts);
    } else if (event.type === "session.ended") {
      const start = startedAt.get(event.payload.session_id);
      if (start === undefined) continue;
      if (event.payload.outcome === "completed") {
        durations.push((Date.parse(event.ts) - Date.parse(start)) / 1000);
      }
    }
  }

  const samples = durations.slice(-WINDOW);
  if (samples.length < MIN_SAMPLES) return DEFAULT_SECONDS;

  const sorted = [...samples].sort((a, b) => a - b);
  const mid = Math.floor(sorted.length / 2);
  return sorted.length % 2 === 1
    ? sorted[mid]
    : (sorted[mid - 1] + sorted[mid]) / 2;
}
