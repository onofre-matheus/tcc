// Sessões salvas (spec/SPEC.md §3 "Sessões salvas" e §4.13): o filtro da
// sessão personalizada com um nome, como os decks filtrados do Anki — guarda
// o filtro, não a lista de cartões. study.saved cria ou substitui a sessão
// inteira; study.deleted é tombstone; as tags acompanham tag.renamed.

import { normalize } from "./envelope.js";
import { normalizeTags, renameTags, renameTag, decodeRename } from "./tags.js";
import { parseNode, nodeKey } from "./links.js";

// Chaves válidas normalizadas; a inválida fica como veio (e não casa nada).
function normalizeAround(raw) {
  const out = [];
  for (const key of raw ?? []) {
    const node = parseNode(key);
    const k = node ? nodeKey(node) : String(key);
    if (!out.includes(k)) out.push(k);
  }
  return out;
}

function cleanIds(raw) {
  const out = [];
  for (const id of (raw ?? []).map((x) => String(x ?? "").trim())) if (id && !out.includes(id)) out.push(id);
  return out;
}

const cmp = (a, b) => (a < b ? -1 : a > b ? 1 : 0);

export function project(events) {
  const studies = {};
  const deleted = new Set();

  for (const event of normalize(events)) {
    const p = event.payload ?? {};
    if (event.type === "study.saved") {
      const name = String(p.name ?? "").trim();
      if (!p.study_id || !name || deleted.has(p.study_id)) continue;
      studies[p.study_id] = {
        name,
        decks: cleanIds(p.decks),
        tags: normalizeTags(p.tags),
        around: normalizeAround(p.around),
        practice: p.practice === true,
      };
    } else if (event.type === "study.deleted") {
      if (studies[p.study_id]) {
        delete studies[p.study_id];
        deleted.add(p.study_id);
      }
    } else if (event.type === "tag.renamed") {
      const rename = decodeRename(p);
      if (!rename) continue;
      for (const study of Object.values(studies)) {
        study.tags = renameTags(study.tags, rename.from, rename.to);
        study.around = normalizeAround(study.around.map((key) => {
          const node = parseNode(key);
          return node?.kind === "tag" ? nodeKey({ kind: "tag", id: renameTag(node.id, rename.from, rename.to) }) : key;
        }));
      }
    }
  }

  const order = Object.keys(studies).sort((a, b) => cmp(studies[a].name, studies[b].name) || cmp(a, b));
  return { studies, order };
}
