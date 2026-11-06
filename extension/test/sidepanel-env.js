// Ambiente das páginas da extensão para os testes de DOM (happy-dom): stub de
// chrome com onChanged funcional (a página se redesenha após cada evento), o
// corpo do HTML real e o log semeado antes de a página montar — o módulo fala
// com chrome no topo, então a ordem dos imports importa.

import { readFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

export const ago = (days) => new Date(Date.now() - days * 86400000).toISOString();
export const ev = (id, lc, type, ts, payload, v = 1) => ({ id, type, v, lc, ts, device: "dev-seed", payload });
export const $ = (id) => document.getElementById(id);
export const settle = async () => {
  for (let i = 0; i < 10; i++) await new Promise((r) => setTimeout(r, 5));
};

export const storage = new Map(); // o chrome.storage.local do stub, para inspeção

// Import explícito por página: o Vite não resolve import com caminho montado.
const PAGES = {
  "sidepanel/sidepanel": () => import("../sidepanel/sidepanel.js"),
  "graph/graph": () => import("../graph/graph.js"),
};

export async function mountPage(page, seed) {
  const listeners = [];
  globalThis.chrome = {
    storage: {
      local: {
        async get(keys) {
          const wanted = typeof keys === "string" ? [keys] : Array.isArray(keys) ? keys : [];
          const out = {};
          for (const k of wanted) if (storage.has(k)) out[k] = structuredClone(storage.get(k));
          return out;
        },
        async set(items) {
          const changes = {};
          for (const [k, v] of Object.entries(items)) {
            changes[k] = { oldValue: storage.get(k), newValue: v };
            storage.set(k, structuredClone(v));
          }
          for (const fn of listeners) fn(changes, "local");
        },
        async remove(key) {
          storage.delete(key);
        },
      },
      managed: undefined,
      onChanged: { addListener: (fn) => listeners.push(fn) },
    },
  };

  const here = dirname(fileURLToPath(import.meta.url));
  const html = readFileSync(join(here, "..", `${page}.html`), "utf8");
  document.body.innerHTML = html
    .slice(html.indexOf("<body>") + "<body>".length, html.indexOf("</body>"))
    .replace(/<script[\s\S]*?<\/script>/g, "");

  const store = await import("../ui/store.js");
  await store.log.merge(seed);
  const mod = await PAGES[page]();
  await mod.ready;
  await settle();
  const lastOf = async (type) => (await store.log.events()).filter((e) => e.type === type).at(-1);
  return { store, lastOf };
}

export const mountSidepanel = (seed) => mountPage("sidepanel/sidepanel", seed);
