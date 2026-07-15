// Datas civis no fuso do usuário (spec/SPEC.md §4).
// Projeções recebem `now` e `tz`; nada aqui lê o relógio do sistema.

export function localDate(tsUtc, tz) {
  return new Intl.DateTimeFormat("en-CA", {
    timeZone: tz,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(new Date(tsUtc));
}

export function addDays(isoDate, days) {
  const [y, m, d] = isoDate.split("-").map(Number);
  return new Date(Date.UTC(y, m - 1, d + days)).toISOString().slice(0, 10);
}
