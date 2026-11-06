// Listagem de decks (spec/SPEC.md §3, Estudo) — RF07.
// Deck = unidade de estudo com nome e tags transversais (reescritas por
// tag.renamed).

import { normalize } from "./envelope.js";
import { normalizeTags, renameTags, decodeRename } from "./tags.js";

export function project(events) {
  const decks = {};

  for (const event of normalize(events)) {
    if (event.type === "deck.created") {
      const { deck_id, name, tags } = event.payload;
      decks[deck_id] = { name, tags: normalizeTags(tags), archived: false };
    } else if (event.type === "deck.renamed") {
      const deck = decks[event.payload.deck_id];
      if (deck) deck.name = event.payload.name; // último nome vence
    } else if (event.type === "tag.renamed") {
      const rename = decodeRename(event.payload);
      if (rename) for (const deck of Object.values(decks)) deck.tags = renameTags(deck.tags, rename.from, rename.to);
    } else if (event.type === "deck.archived") {
      const deck = decks[event.payload.deck_id];
      if (deck) deck.archived = true;
    }
  }

  return { decks };
}
