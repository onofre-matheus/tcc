// Sessão personalizada (spec/SPEC.md §4.11): a fila Leitner filtrada por
// decks e/ou tags, mais a lista de treino (não vencidos) para o modo livre.
// O treino grava card.practiced, que nenhuma projeção de agendamento lê —
// estudar antes da hora não promove o cartão.

import { normalize } from "./envelope.js";
import { project as leitner } from "./leitner.js";
import { project as decksProjection } from "./decks.js";
import { normalizeTags, effectiveTags, tagUnder, activeCard } from "./tags.js";

// OU dentro de cada lista, E entre as listas; lista vazia não restringe.
// Decks por id (expandidos para a subárvore "::"); tags pelo nome normalizado.
export function matcher(decks, { decks: deckFilter = [], tags: tagFilter = [] } = {}) {
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
  return (card) => {
    if (deckIds && !(card.deck_id && deckIds.has(card.deck_id))) return false;
    if (!wanted.length) return true;
    return effectiveTags(card, decks).some((have) => wanted.some((want) => tagUnder(have, want)));
  };
}

const cmp = (a, b) => (a < b ? -1 : a > b ? 1 : 0);

export function project(events, { now, tz, decks: deckFilter, tags: tagFilter } = {}) {
  const lt = leitner(events, { now, tz });
  const { decks } = decksProjection(events);
  const match = matcher(decks, { decks: deckFilter ?? [], tags: tagFilter ?? [] });

  const due = new Set(lt.queue);
  const queue = lt.queue.filter((id) => match(lt.cards[id]));

  // Ordem do treino = critério da fila (§4.1): caixa, vencimento, ts de
  // criação; o id desempata para a saída não depender da ordem de inserção.
  const created = {};
  for (const event of normalize(events)) {
    if (event.type === "card.created") created[event.payload.card_id] ??= event.ts;
  }
  const practice = Object.entries(lt.cards)
    .filter(([id, card]) => !due.has(id) && activeCard(card, decks) && match(card))
    .map(([id]) => id)
    .sort((a, b) => {
      const ca = lt.cards[a], cb = lt.cards[b];
      return ca.box - cb.box || cmp(ca.due, cb.due) || cmp(created[a], created[b]) || cmp(a, b);
    });

  return { queue, practice };
}
