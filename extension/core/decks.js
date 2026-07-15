// Listagem de decks (spec/SPEC.md §3, Estudo) — RF07.
// Deck = unidade de estudo com nome e tags transversais.

import { normalize } from "./envelope.js";

export function project(events) {
  const decks = {};

  for (const event of normalize(events)) {
    if (event.type === "deck.created") {
      const { deck_id, name, tags } = event.payload;
      decks[deck_id] = { name, tags: tags ?? [], archived: false };
    } else if (event.type === "deck.renamed") {
      const deck = decks[event.payload.deck_id];
      if (deck) deck.name = event.payload.name; // último nome vence
    } else if (event.type === "deck.archived") {
      const deck = decks[event.payload.deck_id];
      if (deck) deck.archived = true;
    }
  }

  return { decks };
}
