// Páginas (catálogo 1.6, spec/SPEC.md §3 "Páginas" e §4.15): o texto longo
// de consulta e de uso constante — a anotação que fica, ao contrário da nota
// capturada (passagem para a triagem) e do cartão (estudo). O corpo é
// markdown opaco: a projeção não o interpreta. Edição é LWW por campo; as
// tags acompanham tag.renamed; `version` é o que o cliente envia como `base`.

import { normalize } from "./envelope.js";
import { normalizeTags, renameTags, decodeRename } from "./tags.js";

const cmp = (a, b) => (a < b ? -1 : a > b ? 1 : 0);

export function project(events) {
  const pages = {};

  for (const event of normalize(events)) {
    const p = event.payload ?? {};
    if (event.type === "page.created") {
      const title = String(p.title ?? "").trim();
      if (!p.page_id || !title || pages[p.page_id]) continue;
      pages[p.page_id] = {
        title,
        body: typeof p.body === "string" ? p.body : "",
        tags: normalizeTags(p.tags),
        pinned: false,
        archived: false,
        version: event.id,
        created_at: event.ts,
        updated_at: event.ts,
      };
    } else if (event.type === "page.edited") {
      const page = pages[p.page_id];
      if (!page) continue;
      // last-writer-wins por campo; `base` só será lido pela detecção de
      // conflito — no 1.6 o corpo é LWW como os demais campos.
      let edited = false;
      if (typeof p.title === "string" && p.title.trim()) {
        page.title = p.title.trim();
        edited = true;
      }
      if (typeof p.body === "string") {
        page.body = p.body;
        page.version = event.id;
        edited = true;
      }
      if (Array.isArray(p.tags)) {
        page.tags = normalizeTags(p.tags);
        edited = true;
      }
      if (typeof p.pinned === "boolean") page.pinned = p.pinned;
      if (edited) page.updated_at = event.ts;
    } else if (event.type === "page.archived") {
      if (pages[p.page_id]) pages[p.page_id].archived = true;
    } else if (event.type === "tag.renamed") {
      // vale na posição do evento: só as tags já gravadas mudam
      const rename = decodeRename(p);
      if (!rename) continue;
      for (const page of Object.values(pages)) page.tags = renameTags(page.tags, rename.from, rename.to);
    }
  }

  const order = Object.keys(pages)
    .filter((id) => !pages[id].archived)
    .sort((a, b) => {
      const pa = pages[a], pb = pages[b];
      return (Number(pb.pinned) - Number(pa.pinned)) // fixadas primeiro
        || cmp(pb.updated_at, pa.updated_at) // mais recente primeiro
        || cmp(a, b);
    });
  return { pages, order };
}
