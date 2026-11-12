// Conexões explícitas (spec/SPEC.md §3 "Conexões e renomeação de tag" e
// §4.12): o [[link]] do Obsidian como evento. Uma conexão liga dois nós —
// cartão, deck, tag, página ou tarefa (1.6) — e a vizinhança é não
// direcionada. O id da tag é o próprio nome normalizado, então ligar a uma
// tag não exige que ela "exista".

import { normalize } from "./envelope.js";
import { normalizeTag, renameTag, decodeRename } from "./tags.js";

const KINDS = new Set(["card", "deck", "tag", "page", "task"]);

// Normaliza o id de tag e valida o nó (kind conhecido, id não vazio); null =
// inválido. Existência de cartão/deck/página é checada por quem tem o estado.
export function cleanNode(node) {
  const kind = node?.kind;
  if (!KINDS.has(kind)) return null;
  const id = kind === "tag" ? normalizeTag(node.id) : String(node.id ?? "").trim();
  return id ? { kind, id } : null;
}

// Forma do nó na saída das projeções: "kind:id".
export const nodeKey = ({ kind, id }) => `${kind}:${id}`;

// Lê a chave "kind:id" (o id pode conter ":"; corta no primeiro).
export function parseNode(key) {
  const s = String(key ?? "");
  const at = s.indexOf(":");
  return at < 0 ? null : cleanNode({ kind: s.slice(0, at), id: s.slice(at + 1) });
}

const cmp = (a, b) => (a < b ? -1 : a > b ? 1 : 0);

export function project(events) {
  const cards = new Set(), decks = new Set(), pages = new Set(), tasks = new Set();
  const edges = new Map(); // link_id → { from, to, relation }
  const removed = new Set();
  const exists = (n) =>
    n.kind === "card" ? cards.has(n.id)
      : n.kind === "deck" ? decks.has(n.id)
      : n.kind === "page" ? pages.has(n.id)
      : n.kind === "task" ? tasks.has(n.id)
      : true;

  for (const event of normalize(events)) {
    const p = event.payload ?? {};
    if (event.type === "card.created") cards.add(p.card_id);
    else if (event.type === "deck.created") decks.add(p.deck_id);
    // a mesma validação da projeção de páginas (§3, Páginas): sem título, a
    // página não existe e não se conecta
    else if (event.type === "page.created") { if (p.page_id && String(p.title ?? "").trim()) pages.add(p.page_id); }
    else if (event.type === "task.created") { if (p.task_id) tasks.add(p.task_id); }
    // apagar é tombstone: a tarefa sai do estado, e as sinapses dela junto
    else if (event.type === "task.deleted") tasks.delete(p.task_id);
    else if (event.type === "link.created") {
      // o primeiro link_id vence; o removido não volta
      if (!p.link_id || edges.has(p.link_id) || removed.has(p.link_id)) continue;
      const from = cleanNode(p.from), to = cleanNode(p.to);
      if (!from || !to || !exists(from) || !exists(to) || nodeKey(from) === nodeKey(to)) continue;
      const relation = String(p.relation ?? "").trim() || null;
      edges.set(p.link_id, { from, to, relation });
    } else if (event.type === "link.removed") {
      if (edges.delete(p.link_id)) removed.add(p.link_id);
    } else if (event.type === "tag.renamed") {
      const rename = decodeRename(p);
      if (!rename) continue;
      for (const e of edges.values()) {
        for (const n of [e.from, e.to]) if (n.kind === "tag") n.id = renameTag(n.id, rename.from, rename.to);
      }
    }
  }

  const links = {}, neighbors = {};
  for (const [id, e] of edges) {
    const from = nodeKey(e.from), to = nodeKey(e.to);
    if (from === to) continue; // virou laço por renomeação: deixa de valer
    if ([e.from, e.to].some((n) => n.kind === "task" && !tasks.has(n.id))) continue; // tarefa apagada depois
    links[id] = { from, to, relation: e.relation };
    for (const [a, b] of [[from, to], [to, from]]) {
      const list = (neighbors[a] ??= []);
      if (!list.includes(b)) list.push(b);
    }
  }
  for (const list of Object.values(neighbors)) list.sort(cmp);
  // Ordem canônica por unidade de código, nunca localeCompare (igual ao Go).
  return { links, neighbors, nodes: Object.keys(neighbors).sort(cmp) };
}
