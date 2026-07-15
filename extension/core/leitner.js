// Projeção Leitner (spec/SPEC.md §4.1) — RF08/RF11.
// 5 caixas; acerto promove, erro regride UMA caixa (variante atenuada).

import { normalize } from "./envelope.js";
import { localDate, addDays } from "./time.js";

const MAX_BOX = 5;
const INTERVAL_DAYS = { 1: 1, 2: 3, 3: 7, 4: 14, 5: 30 };

export function project(events, { now, tz }) {
  const cards = new Map();
  const archivedDecks = new Set();

  for (const event of normalize(events)) {
    if (event.type === "card.created") {
      const { card_id, deck_id, front, back, source_url, source_title, tags } =
        event.payload;
      cards.set(card_id, {
        deck_id,
        box: 1,
        due: localDate(event.ts, tz),
        createdTs: event.ts,
        front,
        back,
        source_url: source_url ?? null,
        source_title: source_title ?? null,
        tags: tags ?? [],
        last_explanation: null,
        explanation_count: 0,
        last_frame: "feynman", // último andaime; ausente ≡ "feynman"
        archived: false,
      });
    } else if (event.type === "card.edited") {
      const card = cards.get(event.payload.card_id);
      if (!card) continue;
      // last-writer-wins por campo; caixa e vencimento não mudam
      const { front, back, tags } = event.payload;
      if (front !== undefined) card.front = front;
      if (back !== undefined) card.back = back;
      if (tags !== undefined) card.tags = tags ?? [];
    } else if (event.type === "card.archived") {
      const card = cards.get(event.payload.card_id);
      if (card) card.archived = true;
    } else if (event.type === "deck.archived") {
      archivedDecks.add(event.payload.deck_id);
    } else if (event.type === "card.explained") {
      const card = cards.get(event.payload.card_id);
      if (!card) continue; // explicação de cartão inexistente é ignorada
      card.last_explanation = event.payload.text;
      card.explanation_count += 1;
      card.last_frame = event.payload.frame ?? "feynman";
    } else if (event.type === "card.reviewed") {
      const card = cards.get(event.payload.card_id);
      if (!card) continue;
      card.box =
        event.payload.result === "correct"
          ? Math.min(card.box + 1, MAX_BOX)
          : Math.max(card.box - 1, 1);
      card.due = addDays(localDate(event.ts, tz), INTERVAL_DAYS[card.box]);
    }
  }

  const today = localDate(now, tz);
  const queue = [...cards.entries()]
    .filter(
      ([, card]) =>
        card.due <= today && !card.archived && !archivedDecks.has(card.deck_id)
    )
    .sort(([, a], [, b]) => {
      if (a.box !== b.box) return a.box - b.box;
      if (a.due !== b.due) return a.due < b.due ? -1 : 1;
      return a.createdTs < b.createdTs ? -1 : 1;
    })
    .map(([cardId]) => cardId);

  const cardsOut = {};
  for (const [cardId, card] of cards) {
    const {
      deck_id, box, due, front, back, source_url, source_title, tags,
      last_explanation, explanation_count, last_frame, archived,
    } = card;
    cardsOut[cardId] = {
      deck_id, box, due, front, back, source_url, source_title, tags,
      last_explanation, explanation_count, last_frame, archived,
    };
  }
  return { cards: cardsOut, queue };
}
