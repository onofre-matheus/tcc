// Tags livres (spec/SPEC.md §3 "Normalização de tag" e §4.10). A identidade
// da tag é o próprio nome normalizado — não existe tag.created nem tag_id —,
// então dois dispositivos que escrevem "#SO" e "so" convergem para a mesma tag
// sem regra de merge. Hierarquia por "/" (so/escalonamento), estilo Obsidian;
// o "::" continua sendo dos decks.

import { project as leitner } from "./leitner.js";
import { project as decksProjection } from "./decks.js";
// pages.js também importa daqui; o ciclo é seguro porque nenhum dos dois usa
// o outro na avaliação do módulo, só dentro das funções.
import { project as pagesProjection } from "./pages.js";

// Apara, tira os "#" iniciais, minúsculas, espaço interno vira "-" e
// segmentos "/" vazios somem. Tag que sobra vazia devolve "".
export function normalizeTag(raw) {
  return String(raw ?? "")
    .trim()
    .replace(/^#+/, "")
    .toLowerCase()
    .split("/")
    .map((seg) => seg.split(/\s+/).filter(Boolean).join("-"))
    .filter(Boolean)
    .join("/");
}

// Normaliza a lista, descarta vazias e deduplica mantendo a primeira ocorrência.
export function normalizeTags(raw) {
  const out = [];
  for (const tag of (raw ?? []).map(normalizeTag)) {
    if (tag && !out.includes(tag)) out.push(tag);
  }
  return out;
}

// Entrada livre da UI: separadas por espaço ou vírgula, com ou sem "#"
// ("#SO #Prova-P2, redes" → so, prova-p2, redes).
export const parseTags = (input) => normalizeTags(String(input ?? "").split(/[\s,]+/));

// tag.renamed (spec §3, Conexões e renomeação de tag): from e as
// descendentes viram to ("so/x" → "sistemas/x"); "sobre" não é descendente.
export const renameTag = (tag, from, to) => (tagUnder(tag, from) ? to + tag.slice(from.length) : tag);

// Renomeia a lista e deduplica — renomear para uma tag existente funde as duas.
export const renameTags = (tags, from, to) => normalizeTags(tags.map((t) => renameTag(t, from, to)));

// Lê um tag.renamed com os dois nomes normalizados; null = evento ignorado
// (nome vazio ou de-para iguais).
export function decodeRename(payload) {
  const from = normalizeTag(payload?.from), to = normalizeTag(payload?.to);
  return from && to && from !== to ? { from, to } : null;
}

// Tags efetivas = as do cartão ∪ as do próprio deck (as "transversais").
export const effectiveTags = (card, decks) =>
  normalizeTags([...(card.tags ?? []), ...(card.deck_id ? decks[card.deck_id]?.tags ?? [] : [])]);

// A própria tag ou uma descendente ("so" ⊇ "so/concorrencia", não "sobre").
export const tagUnder = (tag, prefix) => tag === prefix || tag.startsWith(`${prefix}/`);

// Não arquivado e de deck não arquivado (cartão sem deck nunca está em deck
// arquivado). Universo das projeções de tags e de estudo.
export const activeCard = (card, decks) =>
  !card.archived && !(card.deck_id && decks[card.deck_id]?.archived);

// Índice de tags (spec §4.10). A soma pela hierarquia é só de exibição.
export function project(events, { now, tz }) {
  const lt = leitner(events, { now, tz });
  const { decks } = decksProjection(events);
  const tags = {};
  for (const deck of Object.values(decks)) {
    if (deck.archived) continue;
    for (const tag of deck.tags) tags[tag] ??= { cards: 0, due: 0, pages: 0 };
  }
  const due = new Set(lt.queue);
  for (const [id, card] of Object.entries(lt.cards)) {
    if (!activeCard(card, decks)) continue;
    for (const tag of effectiveTags(card, decks)) {
      const info = (tags[tag] ??= { cards: 0, due: 0, pages: 0 });
      info.cards += 1;
      if (due.has(id)) info.due += 1;
    }
  }
  // páginas não arquivadas com a tag exata (1.6)
  for (const page of Object.values(pagesProjection(events).pages)) {
    if (page.archived) continue;
    for (const tag of page.tags) (tags[tag] ??= { cards: 0, due: 0, pages: 0 }).pages += 1;
  }
  // Ordem canônica por unidade de código (igual à comparação de bytes do Go
  // para o plano básico), nunca localeCompare: a saída não depende do locale.
  const names = Object.keys(tags).sort((a, b) => (a < b ? -1 : a > b ? 1 : 0));
  return { tags, names };
}
