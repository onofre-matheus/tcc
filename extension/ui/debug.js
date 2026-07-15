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
        title="Semeia o cenário de demonstração (idempotente)">🌱 demo</button>`;
    for (const btn of container.querySelectorAll("[data-step]")) {
      btn.onclick = () => {
        if (btn.dataset.step === "demo") return store.log.merge(buildDemoEvents(store.nowIso()));
        if (btn.dataset.step === "reset") return store.debug.reset();
        return store.debug.advanceDays(Number(btn.dataset.step));
      };
    }
  }
  store.onChange(render); // re-renderiza quando o offset muda (outro contexto)
  await store.ready;
  render();
}
