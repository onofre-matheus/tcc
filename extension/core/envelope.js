// Ordem total e idempotência do log (spec/SPEC.md §2).
// Toda projeção recebe eventos já normalizados por esta função.

function compareEvents(a, b) {
  if (a.lc !== b.lc) return a.lc - b.lc;
  if (a.ts !== b.ts) return a.ts < b.ts ? -1 : 1;
  if (a.device !== b.device) return a.device < b.device ? -1 : 1;
  if (a.id !== b.id) return a.id < b.id ? -1 : 1;
  return 0;
}

export function normalize(events) {
  const sorted = [...events].sort(compareEvents);
  const seen = new Set();
  const result = [];
  for (const event of sorted) {
    if (seen.has(event.id)) continue;
    seen.add(event.id);
    result.push(event);
  }
  return result;
}
