// Adaptadores de armazenamento chave-valor assíncrono.
// A camada de log (log.js) só conhece esta interface — { get, set } — o que
// mantém o núcleo testável no Node sem um navegador e desacopla de
// chrome.storage (native messaging / File System Access ficam de trabalho
// futuro sob a mesma interface).

// chrome.storage.local expõe Promises no Manifest V3 (Chrome 88+).
export function chromeArea(area) {
  return {
    async get(key) {
      const result = await area.get(key);
      return result[key];
    },
    async set(key, value) {
      await area.set({ [key]: value });
    },
  };
}

// Implementação em memória para testes; clona na fronteira para imitar a
// serialização (structured clone) que o chrome.storage faz de fato.
export function memoryKV(initial = {}) {
  const store = new Map(Object.entries(structuredClone(initial)));
  return {
    async get(key) {
      return store.has(key) ? structuredClone(store.get(key)) : undefined;
    },
    async set(key, value) {
      store.set(key, structuredClone(value));
    },
  };
}
