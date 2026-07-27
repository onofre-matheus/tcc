// Barra de debug: "viaja no tempo" para testar vencimentos do Leitner e
// streaks, e semeia o cenário de demonstração dos prints. Monta num container
// e reflete o relógio simulado (store.nowIso).
import * as store from "./store.js";
import { buildDemoEvents } from "./demo-events.js";

const dateFmt = new Intl.DateTimeFormat("pt-BR", {
  weekday: "short", day: "numeric", month: "short",
});

export async function mountDebugBar(container) {
  function render() {
    const days = store.debug.offsetDays();
    const label = dateFmt.format(new Date(store.nowIso()));
    const badge = days > 0 ? ` +${days}d` : days < 0 ? ` ${days}d` : "";
    container.innerHTML = `
      <span class="dbg-label" title="Relógio simulado">🐛 ${label}${badge}</span>
      <button class="btn ghost sm" data-step="1">+1d</button>
      <button class="btn ghost sm" data-step="7">+7d</button>
      <button class="btn ghost sm" data-step="-1">−1d</button>
      <button class="btn ghost sm" data-step="reset">hoje</button>
      <button class="btn ghost sm" data-step="demo"
        title="Semeia o cenário de demonstração (idempotente)">🌱 demo</button>
      <button class="btn ghost sm" data-step="export"
        title="Baixa o log de eventos (para o painel em site/)">⬇ eventos</button>`;
    for (const btn of container.querySelectorAll("[data-step]")) {
      btn.onclick = () => {
        if (btn.dataset.step === "demo") return store.log.merge(buildDemoEvents(store.nowIso()));
        if (btn.dataset.step === "reset") return store.debug.reset();
        if (btn.dataset.step === "export") return exportEvents();
        return store.debug.advanceDays(Number(btn.dataset.step));
      };
    }
  }
  store.onChange(render); // re-renderiza quando o offset muda (outro contexto)
  await store.ready;
  render();
}

// Exporta o log inteiro como JSON — é o arquivo que o painel em `site/` carrega
// (o site é um terceiro cliente só-leitura sobre o mesmo log de eventos).
async function exportEvents() {
  const events = await store.log.events();
  const blob = new Blob([JSON.stringify(events, null, 2)], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `procrastina-nao-eventos-${new Date().toISOString().slice(0, 10)}.json`;
  a.click();
  URL.revokeObjectURL(url);
}
