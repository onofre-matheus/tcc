// Painel de sincronização do side panel: estado do último ciclo, botão para
// sincronizar na hora e o formulário da credencial.
//
// O formulário é a forma segura de configurar: grava em chrome.storage.local,
// que não é versionado. O contrário — chave embutida no código — iria para o
// repositório, que é público.
//
// O segredo nunca é escrito de volta no DOM: uma vez salvo, o campo mostra
// apenas um marcador, e deixá-lo em branco preserva o que já está guardado.

import * as store from "./store.js";
import { loadSyncConfig, SyncNotConfigured, CONFIG_KEY } from "../storage/sync_config.js";

const SAVED_PLACEHOLDER = "••••••••  (guardado)";

const relativo = (iso) => {
  const seconds = Math.round((Date.now() - new Date(iso)) / 1000);
  if (seconds < 60) return "agora há pouco";
  if (seconds < 3600) return `há ${Math.floor(seconds / 60)} min`;
  if (seconds < 86400) return `há ${Math.floor(seconds / 3600)} h`;
  return `há ${Math.floor(seconds / 86400)} d`;
};

export async function mountSyncPanel(container) {
  container.innerHTML = `
    <h2>Sincronização</h2>
    <div class="row" style="align-items: center">
      <span class="meta grow" id="syncState">—</span>
      <button class="btn sm" id="syncNow">Sincronizar</button>
    </div>
    <details id="syncSetup" style="margin-top: 10px">
      <summary class="meta" style="cursor: pointer">Configurar bucket e chave</summary>
      <form id="syncForm" style="display: flex; flex-direction: column; gap: 6px; margin-top: 8px">
        <input id="syncBucket" placeholder="Nome do bucket" autocomplete="off" required />
        <input id="syncRegion" placeholder="Região (ex.: sa-east-1)" autocomplete="off" />
        <input id="syncKey" placeholder="Access key ID" autocomplete="off" spellcheck="false" />
        <input id="syncSecret" type="password" placeholder="Secret access key" autocomplete="new-password" />
        <input id="syncEndpoint" placeholder="Endpoint (só R2/MinIO/Backblaze)" autocomplete="off" />
        <div class="row">
          <button class="btn sm primary grow" type="submit">Salvar</button>
          <button class="btn ghost sm" type="button" id="syncForget"
            title="Apaga a credencial guardada neste navegador">Esquecer</button>
        </div>
        <span class="meta" id="syncFormMsg"></span>
      </form>
    </details>`;

  const $ = (id) => container.querySelector("#" + id);
  const state = $("syncState");
  const formMsg = $("syncFormMsg");

  async function renderState() {
    try {
      const config = await loadSyncConfig();
      const status = await store.syncStatus();
      if (!status) {
        state.textContent = `pronto — ${config.bucket}`;
      } else if (status.ok) {
        state.textContent =
          `${relativo(status.at)}: ▲ ${status.sent} · ▼ ${status.received}`;
      } else {
        state.textContent = `falhou ${relativo(status.at)}: ${status.error}`;
      }
      state.title = `${config.bucket}/${config.prefix} · ${config.region}`;
    } catch (error) {
      state.textContent =
        error instanceof SyncNotConfigured ? "não configurado" : String(error.message ?? error);
      state.title = "";
    }
  }

  // Preenche o que já está guardado, menos o segredo.
  async function fillForm() {
    const stored = (await chrome.storage.local.get(CONFIG_KEY))[CONFIG_KEY] ?? {};
    $("syncBucket").value = stored.bucket ?? "";
    $("syncRegion").value = stored.region ?? "";
    $("syncKey").value = stored.accessKeyId ?? "";
    $("syncEndpoint").value = stored.endpoint ?? "";
    $("syncSecret").value = "";
    $("syncSecret").placeholder = stored.secretAccessKey
      ? SAVED_PLACEHOLDER
      : "Secret access key";
  }

  $("syncNow").onclick = async () => {
    const button = $("syncNow");
    button.disabled = true;
    state.textContent = "sincronizando…";
    await store.syncQuietly(); // nunca lança; o estado conta o que houve
    button.disabled = false;
    await renderState();
  };

  $("syncForm").onsubmit = async (event) => {
    event.preventDefault();
    const stored = (await chrome.storage.local.get(CONFIG_KEY))[CONFIG_KEY] ?? {};
    const config = {
      bucket: $("syncBucket").value.trim(),
      region: $("syncRegion").value.trim(),
      accessKeyId: $("syncKey").value.trim(),
      endpoint: $("syncEndpoint").value.trim(),
      // Em branco preserva o segredo guardado — o campo nunca o exibe de volta.
      secretAccessKey: $("syncSecret").value.trim() || stored.secretAccessKey || "",
    };
    await chrome.storage.local.set({ [CONFIG_KEY]: config });
    await fillForm();

    formMsg.textContent = "salvo · sincronizando…";
    const result = await store.syncQuietly();
    formMsg.textContent = result
      ? `▲ ${result.sent} enviado(s) · ▼ ${result.received} recebido(s)`
      : "não deu — veja a mensagem acima";
    await renderState();
  };

  $("syncForget").onclick = async () => {
    await chrome.storage.local.remove(CONFIG_KEY);
    await fillForm();
    formMsg.textContent = "credencial apagada deste navegador";
    await renderState();
  };

  await fillForm();
  await renderState();
}
