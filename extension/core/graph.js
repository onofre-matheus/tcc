// Grafo do acervo (spec/SPEC.md §4.14), estilo Obsidian: cartões ativos,
// decks e tags como nós; conexões explícitas, tags, pertença a deck e
// hierarquia (/ e ::) como arestas. É o contrato que a extensão desenha em
// SVG e a CLI em DOT — as duas, a partir da mesma projeção.

import { project as leitner } from "./leitner.js";
import { project as decksProjection } from "./decks.js";
import { project as linksProjection, parseNode } from "./links.js";
import { activeCard } from "./tags.js";

const cmp = (a, b) => (a < b ? -1 : a > b ? 1 : 0);

// O nome do deck com os segmentos "::" aparados e sem vazios (§4.5).
const deckPath = (name) => String(name).split("::").map((s) => s.trim()).filter(Boolean);

export function project(events, { now, tz }) {
  const lt = leitner(events, { now, tz });
  const { decks } = decksProjection(events);
  const lk = linksProjection(events);

  const nodes = new Map();
  const addTag = (tag) => { // a tag e os ancestrais implícitos
    for (let t = tag; ; t = t.slice(0, t.lastIndexOf("/"))) {
      nodes.set(`tag:${t}`, { id: `tag:${t}`, kind: "tag", label: t, due: false });
      if (!t.includes("/")) return;
    }
  };
  const due = new Set(lt.queue);
  for (const [id, card] of Object.entries(lt.cards)) {
    if (!activeCard(card, decks)) continue;
    nodes.set(`card:${id}`, { id: `card:${id}`, kind: "card", label: card.front, due: due.has(id) });
    card.tags.forEach(addTag);
  }
  for (const [id, deck] of Object.entries(decks)) {
    if (deck.archived) continue;
    nodes.set(`deck:${id}`, { id: `deck:${id}`, kind: "deck", label: deck.name, due: false });
    deck.tags.forEach(addTag);
  }
  for (const link of Object.values(lk.links)) {
    if (!graphLink(link)) continue; // conexão com página ainda não entra no grafo (§4.14)
    for (const end of [link.from, link.to]) {
      const node = parseNode(end);
      if (node?.kind === "tag") addTag(node.id);
    }
  }

  const edges = [];
  const add = (from, to, kind, relation = null) => {
    if (nodes.has(from) && nodes.has(to)) edges.push({ from, to, kind, relation });
  };
  for (const link of Object.values(lk.links)) add(link.from, link.to, "link", link.relation);
  // Deck ativo por caminho normalizado, para achar o pai "::" (menor id
  // entre homônimos).
  const byPath = new Map();
  for (const [id, deck] of Object.entries(decks)) {
    const path = deckPath(deck.name).join("::");
    if (!deck.archived && (!byPath.has(path) || id < byPath.get(path))) byPath.set(path, id);
  }
  for (const [key, node] of nodes) {
    if (node.kind === "card") {
      const card = lt.cards[key.slice("card:".length)];
      for (const tag of card.tags) add(key, `tag:${tag}`, "tag");
      if (card.deck_id) add(key, `deck:${card.deck_id}`, "deck");
    } else if (node.kind === "deck") {
      const deck = decks[key.slice("deck:".length)];
      for (const tag of deck.tags) add(key, `tag:${tag}`, "tag");
      const segments = deckPath(deck.name);
      for (let i = segments.length - 1; i >= 1; i--) {
        const parent = byPath.get(segments.slice(0, i).join("::"));
        if (parent) {
          add(key, `deck:${parent}`, "parent");
          break;
        }
      }
    } else if (node.label.includes("/")) {
      add(key, `tag:${node.label.slice(0, node.label.lastIndexOf("/"))}`, "parent");
    }
  }

  const rel = (r) => (r === null ? "" : `\u0001${r}`); // null antes de qualquer relação
  edges.sort((a, b) => cmp(a.from, b.from) || cmp(a.to, b.to) || cmp(a.kind, b.kind) || cmp(rel(a.relation), rel(b.relation)));
  const unique = edges.filter((e, i) => {
    const prev = edges[i - 1];
    return !prev || prev.from !== e.from || prev.to !== e.to || prev.kind !== e.kind || rel(prev.relation) !== rel(e.relation);
  });
  return { nodes: [...nodes.values()].sort((a, b) => cmp(a.id, b.id)), edges: unique };
}

// A conexão entra no grafo se as duas pontas são cartão, deck ou tag. Conexão
// com página (1.6) ainda não entra — nem a tag que só aparece como a outra
// ponta dela, que ficaria solta no desenho.
function graphLink(link) {
  return [link.from, link.to].every((end) => ["card", "deck", "tag"].includes(parseNode(end)?.kind));
}
