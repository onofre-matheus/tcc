// Sessão personalizada (spec/SPEC.md §4.11): a fila Leitner filtrada por
// decks, tags e/ou vizinhança de conexões, mais a lista de treino (não
// vencidos) para o modo livre.
// O treino grava card.practiced, que nenhuma projeção de agendamento lê —
// estudar antes da hora não promove o cartão.

import { normalize } from "./envelope.js";
import { project as leitner } from "./leitner.js";
import { project as decksProjection } from "./decks.js";
import { project as linksProjection, parseNode } from "./links.js";
import { normalizeTags, effectiveTags, tagUnder, activeCard } from "./tags.js";
import { project as pagesProjection } from "./pages.js";

// O deck id está na subárvore "::" do deck root; deck inexistente, em nada.
function deckUnder(decks, id, root) {
  const r = decks[root], d = decks[id];
  return !!(r && d && (d.name === r.name || d.name.startsWith(`${r.name}::`)));
}

// x inclui y: o mesmo cartão, a tag e as descendentes "/", o deck e a
// subárvore "::" (a mesma regra de inclusão dos filtros).
function nodeIncludes(decks, x, y) {
  if (x.kind !== y.kind) return false;
  if (x.kind === "tag") return tagUnder(y.id, x.id);
  if (x.kind === "deck") return deckUnder(decks, y.id, x.id);
  return x.id === y.id;
}

// Expande o around (spec §4.11): cada nó X mais os vizinhos de todo nó que X
// inclui — um salto só. null = sem restrição; [] (só inválidos) = nada casa.
function aroundNodes(decks, links, around) {
  if (!around?.length) return null;
  const out = new Map();
  const add = (n) => n && out.set(`${n.kind}:${n.id}`, n);
  for (const raw of around) {
    const x = parseNode(raw);
    if (!x) continue;
    add(x);
    for (const key of links.nodes) {
      if (nodeIncludes(decks, x, parseNode(key))) for (const nb of links.neighbors[key]) add(parseNode(nb));
    }
  }
  return [...out.values()];
}

// OU dentro de cada lista, E entre as listas; lista vazia não restringe.
// Decks por id (expandidos para a subárvore "::"); tags pelo nome normalizado;
// around por nó "kind:id" (só ele lê as conexões).
export function matcher(decks, { decks: deckFilter = [], tags: tagFilter = [], around = [] } = {}, links = { nodes: [], neighbors: {} }) {
  let deckIds = null;
  if (deckFilter.length) {
    deckIds = new Set();
    for (const id of deckFilter) {
      const root = decks[id];
      if (!root) continue; // deck inexistente no filtro não casa nada
      for (const [other, deck] of Object.entries(decks)) {
        if (deck.name === root.name || deck.name.startsWith(`${root.name}::`)) deckIds.add(other);
      }
    }
  }
  const wanted = normalizeTags(tagFilter);
  const near = aroundNodes(decks, links, around);
  const hasTagUnder = (card, prefixes) =>
    effectiveTags(card, decks).some((have) => prefixes.some((want) => tagUnder(have, want)));
  return (id, card) => {
    if (deckIds && !(card.deck_id && deckIds.has(card.deck_id))) return false;
    if (wanted.length && !hasTagUnder(card, wanted)) return false;
    if (!near) return true;
    return near.some((n) =>
      n.kind === "card" ? n.id === id
        : n.kind === "tag" ? hasTagUnder(card, [n.id])
          : !!card.deck_id && deckUnder(decks, card.deck_id, n.id));
  };
}

const cmp = (a, b) => (a < b ? -1 : a > b ? 1 : 0);

export function project(events, { now, tz, decks: deckFilter, tags: tagFilter, around } = {}) {
  const lt = leitner(events, { now, tz });
  const { decks } = decksProjection(events);
  const filter = { decks: deckFilter ?? [], tags: tagFilter ?? [], around: expandPageAround(events, around ?? []) };
  const match = matcher(decks, filter, linksProjection(events));

  const due = new Set(lt.queue);
  const queue = lt.queue.filter((id) => match(id, lt.cards[id]));

  // Ordem do treino = critério da fila (§4.1): caixa, vencimento, ts de
  // criação; o id desempata para a saída não depender da ordem de inserção.
  const created = {};
  for (const event of normalize(events)) {
    if (event.type === "card.created") created[event.payload.card_id] ??= event.ts;
  }
  const practice = Object.entries(lt.cards)
    .filter(([id, card]) => !due.has(id) && activeCard(card, decks) && match(id, card))
    .map(([id]) => id)
    .sort((a, b) => {
      const ca = lt.cards[a], cb = lt.cards[b];
      return ca.box - cb.box || cmp(ca.due, cb.due) || cmp(created[a], created[b]) || cmp(a, b);
    });

  return { queue, practice };
}

// Ao redor de uma página (1.6) é a vizinhança dela mais a vizinhança de cada
// tag dela (§4.11) — a tag é como a página diz sobre o que é.
function expandPageAround(events, around) {
  if (!around.some((key) => parseNode(key)?.kind === "page")) return around;
  const { pages } = pagesProjection(events);
  const out = [...around];
  for (const key of around) {
    const node = parseNode(key);
    if (node?.kind === "page") for (const tag of pages[node.id]?.tags ?? []) out.push(`tag:${tag}`);
  }
  return out;
}
