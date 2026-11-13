// @vitest-environment happy-dom
//
// O painel é o caminho por onde a credencial entra, então o que precisa ser
// provado é o comportamento do segredo: nunca voltar para a tela, e campo em
// branco preservar o que já está guardado (senão salvar a região apagaria a
// chave sem avisar).

import { beforeEach, describe, expect, it, vi } from "vitest";

// O painel importa ui/store.js, que fala com chrome no topo do módulo — o
// stub precisa existir antes do import.
const storage = new Map();
globalThis.chrome = {
  storage: {
    local: {
      async get(keys) {
        const wanted = typeof keys === "string" ? [keys] : Array.isArray(keys) ? keys : [];
        const out = {};
        for (const k of wanted) if (storage.has(k)) out[k] = storage.get(k);
        return out;
      },
      async set(items) {
        for (const [k, v] of Object.entries(items)) storage.set(k, v);
      },
      async remove(key) {
        storage.delete(key);
      },
    },
    managed: undefined,
    onChanged: { addListener() {} },
  },
};

const { mountSyncPanel } = await import("../ui/sync-panel.js");
const { CONFIG_KEY } = await import("../storage/sync_config.js");
const store = await import("../ui/store.js");

const CREDENCIAL = {
  bucket: "meu-bucket",
  region: "sa-east-1",
  accessKeyId: "AKIAEXEMPLO",
  secretAccessKey: "segredo-original",
};

async function montar() {
  const container = document.createElement("section");
  document.body.replaceChildren(container);
  await mountSyncPanel(container);
  return {
    container,
    $: (id) => container.querySelector("#" + id),
    async salvar() {
      container.querySelector("#syncForm").dispatchEvent(new Event("submit"));
      await vi.waitFor(() => expect(container.querySelector("#syncFormMsg").textContent).not.toBe(""));
    },
  };
}

beforeEach(() => {
  storage.clear();
  vi.spyOn(store, "syncQuietly").mockResolvedValue({ sent: 0, received: 0 });
});

describe("painel de sincronização", () => {
  it("diz que não está configurado quando não há credencial", async () => {
    const { $ } = await montar();
    expect($("syncState").textContent).toBe("não configurado");
  });

  it("salva a credencial digitada em storage.local", async () => {
    const panel = await montar();
    panel.$("syncBucket").value = "meu-bucket";
    panel.$("syncRegion").value = "sa-east-1";
    panel.$("syncKey").value = "AKIAEXEMPLO";
    panel.$("syncSecret").value = "segredo-original";
    await panel.salvar();

    expect(storage.get(CONFIG_KEY)).toMatchObject(CREDENCIAL);
  });

  it("nunca escreve o segredo de volta na tela", async () => {
    storage.set(CONFIG_KEY, { ...CREDENCIAL });
    const { container, $ } = await montar();

    expect($("syncSecret").value).toBe("");
    expect($("syncSecret").placeholder).toContain("guardado");
    expect(container.innerHTML).not.toContain("segredo-original");
    // ...mas o que não é segredo volta, para poder editar.
    expect($("syncBucket").value).toBe("meu-bucket");
    expect($("syncKey").value).toBe("AKIAEXEMPLO");
  });

  it("preserva o segredo guardado quando o campo fica em branco", async () => {
    storage.set(CONFIG_KEY, { ...CREDENCIAL });
    const panel = await montar();

    panel.$("syncRegion").value = "us-east-1"; // muda só a região
    await panel.salvar();

    expect(storage.get(CONFIG_KEY).secretAccessKey).toBe("segredo-original");
    expect(storage.get(CONFIG_KEY).region).toBe("us-east-1");
  });

  it("a senha dos livros: guardada, nunca de volta na tela, preservada em branco", async () => {
    storage.set(CONFIG_KEY, { ...CREDENCIAL });
    let panel = await montar();
    panel.$("syncBookPass").value = "  senha dos livros  ";
    await panel.salvar();
    expect(storage.get(CONFIG_KEY).bookPassphrase).toBe("senha dos livros"); // aparada, como na CLI

    panel = await montar();
    expect(panel.$("syncBookPass").value).toBe("");
    expect(panel.$("syncBookPass").placeholder).toContain("guardado");
    expect(panel.container.innerHTML).not.toContain("senha dos livros\"");
    panel.$("syncRegion").value = "us-east-1";
    await panel.salvar();
    expect(storage.get(CONFIG_KEY).bookPassphrase).toBe("senha dos livros");
  });

  it("Sair deste dispositivo: só aparece configurado, pergunta antes e diz o que conferiu", async () => {
    let panel = await montar();
    expect(panel.$("syncLogout").hidden).toBe(true);

    storage.set(CONFIG_KEY, { ...CREDENCIAL });
    panel = await montar();
    expect(panel.$("syncLogout").hidden).toBe(false);
    const logout = vi.spyOn(store, "logout").mockResolvedValue({ verified: 7 });

    vi.spyOn(window, "confirm").mockReturnValueOnce(false);
    panel.$("syncLogout").click();
    await vi.waitFor(() => expect(window.confirm).toHaveBeenCalled());
    expect(logout).not.toHaveBeenCalled();

    window.confirm.mockReturnValueOnce(true);
    panel.$("syncLogout").click();
    await vi.waitFor(() => expect(panel.$("syncState").textContent).toContain("navegador limpo — 7 evento(s)"));

    logout.mockRejectedValueOnce(new Error("S3 PUT 503"));
    window.confirm.mockReturnValueOnce(true);
    panel.$("syncLogout").click();
    await vi.waitFor(() => expect(panel.$("syncState").textContent).toContain("nada foi apagado: S3 PUT 503"));
  });

  it("troca o segredo quando um novo é digitado", async () => {
    storage.set(CONFIG_KEY, { ...CREDENCIAL });
    const panel = await montar();

    panel.$("syncSecret").value = "segredo-novo";
    await panel.salvar();

    expect(storage.get(CONFIG_KEY).secretAccessKey).toBe("segredo-novo");
  });

  it("esquece a credencial deste navegador", async () => {
    storage.set(CONFIG_KEY, { ...CREDENCIAL });
    const { $ } = await montar();

    $("syncForget").click();
    await vi.waitFor(() => expect(storage.has(CONFIG_KEY)).toBe(false));
    await vi.waitFor(() => expect($("syncState").textContent).toBe("não configurado"));
  });

  it("mostra o resultado do último ciclo", async () => {
    storage.set(CONFIG_KEY, { ...CREDENCIAL });
    storage.set("sync_status", {
      at: new Date().toISOString(),
      ok: true,
      sent: 3,
      received: 2,
    });

    const { $ } = await montar();
    expect($("syncState").textContent).toContain("▲ 3");
    expect($("syncState").textContent).toContain("▼ 2");
  });

  it("mostra a falha do último ciclo em vez de engolir", async () => {
    storage.set(CONFIG_KEY, { ...CREDENCIAL });
    storage.set("sync_status", {
      at: new Date().toISOString(),
      ok: false,
      error: "S3 GET 403: SignatureDoesNotMatch",
    });

    const { $ } = await montar();
    expect($("syncState").textContent).toContain("SignatureDoesNotMatch");
  });
});
