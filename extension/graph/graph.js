// Tela de grafo (spec §4.14), estilo Obsidian: desenha a projeção `graph`
// com o layout de forças de ui/force.js. Daqui também se cria sinapse (⚡ ligar
// a… e clique no destino; numa tag, classifica o cartão), se desfaz sinapse e
// se pede ao side panel uma revisão
// "ao redor" do nó — pelo storage, porque é outra página.

import * as store from "../ui/store.js";
import { createLayout, bounds } from "../ui/force.js";

const $ = (id) => document.getElementById(id);
const SVG_NS = "http://www.w3.org/2000/svg";
const KINDS = ["card", "tag", "deck"];
const LARGE = 400; // acima disso, abre sem os cartões (a repulsão é O(n²))

let full = { nodes: [], edges: [] }; // projeção graph
let links = { links: {}, neighbors: {}, nodes: [] }; // projeção links (para desligar)
let shown = { nodes: [], edges: [] }; // o que está desenhado
let selected = null; // id do nó em foco
let linkFrom = null; // modo "ligar a…": a origem
let view = { x: 0, y: 0, scale: 1 }; // pan e zoom
let windowId = null; // para abrir o side panel no clique (gesto do usuário)
const positions = new Map(); // id → {x, y, fixed}, preservado entre redesenhos
const radii = new Map(); // id → raio desenhado (a seta para na borda)
const byLabel = new Map(); // rótulo da busca → id

chrome.windows?.getCurrent?.().then((w) => (windowId = w?.id ?? null)).catch(() => {});

const nodeById = (id) => full.nodes.find((n) => n.id === id);
const truncate = (s, max) => ([...String(s)].length > max ? `${[...String(s)].slice(0, max - 1).join("")}…` : String(s));
const labelOf = (node) => (!node ? "" : node.kind === "tag" ? `#${node.label}` : node.kind === "deck" ? `deck ${node.label}` : `“${truncate(node.label, 60)}”`);
const esc = (s) => String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));

function el(tag, attrs = {}, parent) {
  const node = document.createElementNS(SVG_NS, tag);
  for (const [k, v] of Object.entries(attrs)) node.setAttribute(k, v);
  parent?.appendChild(node);
  return node;
}

function canvasSize() {
  const r = $("canvas").getBoundingClientRect();
  return { width: r.width || 800, height: r.height || 600 };
}

// Filtro de tipos e, com nó em foco, o grafo local (busca em largura sem
// direção sobre o que está visível, até a profundidade escolhida).
function visibleGraph() {
  const kinds = new Set(KINDS.filter((k) => $(`show-${k}`).checked));
  let ids = new Set(full.nodes.filter((n) => kinds.has(n.kind) || n.id === selected).map((n) => n.id));
  let edges = full.edges.filter((e) => ids.has(e.from) && ids.has(e.to));
  const depth = Number($("depth").value);
  if (depth > 0 && selected && ids.has(selected)) {
    const reached = new Set([selected]);
    let frontier = [selected];
    for (let step = 0; step < depth; step++) {
      const next = [];
      for (const e of edges) {
        for (const [a, b] of [[e.from, e.to], [e.to, e.from]]) {
          if (frontier.includes(a) && !reached.has(b)) { reached.add(b); next.push(b); }
        }
      }
      frontier = next;
    }
    ids = reached;
    edges = edges.filter((e) => ids.has(e.from) && ids.has(e.to));
  }
  return { nodes: full.nodes.filter((n) => ids.has(n.id)), edges };
}

const radiusOf = (node, degree) => (node.kind === "card" ? 5 : 6) + Math.min(Math.sqrt(degree) * 1.6, 10);

function build() {
  shown = visibleGraph();
  const { width, height } = canvasSize();
  const fresh = shown.nodes.some((n) => !positions.has(n.id));
  const layout = createLayout(shown.nodes, shown.edges, { width, height, previous: positions });
  if (fresh) layout.run(shown.nodes.length <= 150 ? 300 : shown.nodes.length <= LARGE ? 150 : 60);
  for (const [id, p] of layout.positions) positions.set(id, p);
  draw();
  if (fresh) fit();
  $("stats").textContent = `${shown.nodes.length} de ${full.nodes.length} nós · ${shown.edges.length} arestas`;
}

function draw() {
  const svg = $("canvas");
  svg.replaceChildren();
  const marker = el("marker", { id: "arrow", viewBox: "0 0 10 10", refX: "10", refY: "5", markerWidth: "7", markerHeight: "7", orient: "auto" }, el("defs", {}, svg));
  el("path", { d: "M0,0 L10,5 L0,10 z", class: "arrowhead" }, marker);
  const viewport = el("g", { id: "viewport" }, svg);
  const edgesG = el("g", {}, viewport), nodesG = el("g", {}, viewport);

  const degree = new Map();
  for (const e of shown.edges) for (const id of [e.from, e.to]) degree.set(id, (degree.get(id) ?? 0) + 1);
  for (const e of shown.edges) {
    const line = el("line", { class: `edge ${e.kind}`, "data-from": e.from, "data-to": e.to }, edgesG);
    if (e.kind === "link") line.setAttribute("marker-end", "url(#arrow)");
    if (e.relation) el("title", {}, line).textContent = e.relation;
  }
  for (const node of shown.nodes) {
    const g = el("g", { class: `node ${node.kind}${node.due ? " due" : ""}`, "data-id": node.id }, nodesG);
    const r = radiusOf(node, degree.get(node.id) ?? 0);
    radii.set(node.id, r);
    if (node.kind === "deck") el("rect", { x: -r, y: -r, width: 2 * r, height: 2 * r, rx: 3 }, g);
    else el("circle", { r }, g);
    const text = el("text", { x: r + 3, y: 3.5 }, g);
    text.textContent = node.kind === "tag" ? `#${node.label}` : truncate(node.label, node.kind === "card" ? 28 : 40);
    el("title", {}, g).textContent = labelOf(node);
  }
  applyPositions();
  applyView();
  applySelection();
}

function applyPositions() {
  for (const g of $("canvas").querySelectorAll(".node")) {
    const p = positions.get(g.dataset.id);
    g.setAttribute("transform", `translate(${p.x.toFixed(1)},${p.y.toFixed(1)})`);
  }
  for (const line of $("canvas").querySelectorAll(".edge")) {
    const a = positions.get(line.dataset.from), b = positions.get(line.dataset.to);
    let { x, y } = b;
    if (line.classList.contains("link")) { // a seta para na borda do alvo
      const r = (radii.get(line.dataset.to) ?? 6) + 2;
      const d = Math.hypot(b.x - a.x, b.y - a.y) || 1;
      x = b.x - ((b.x - a.x) / d) * r;
      y = b.y - ((b.y - a.y) / d) * r;
    }
    line.setAttribute("x1", a.x.toFixed(1)); line.setAttribute("y1", a.y.toFixed(1));
    line.setAttribute("x2", x.toFixed(1)); line.setAttribute("y2", y.toFixed(1));
  }
}

function applyView() {
  $("viewport")?.setAttribute("transform", `translate(${view.x.toFixed(1)},${view.y.toFixed(1)}) scale(${view.scale.toFixed(3)})`);
  $("canvas").classList.toggle("zoomed", view.scale > 1.6);
}

function fit() {
  const visible = new Map(shown.nodes.map((n) => [n.id, positions.get(n.id)]));
  const { minX, minY, maxX, maxY } = bounds(visible);
  const { width, height } = canvasSize();
  const scale = Math.min(width / (maxX - minX + 160), height / (maxY - minY + 80), 2);
  view = { scale, x: width / 2 - ((minX + maxX) / 2) * scale, y: height / 2 - ((minY + maxY) / 2) * scale };
  applyView();
}

function applySelection() {
  const near = new Set(selected ? [selected] : []);
  for (const e of shown.edges) {
    if (e.from === selected) near.add(e.to);
    if (e.to === selected) near.add(e.from);
  }
  const svg = $("canvas");
  svg.classList.toggle("focus", !!selected);
  svg.classList.toggle("linking", !!linkFrom);
  for (const g of svg.querySelectorAll(".node")) {
    g.classList.toggle("selected", g.dataset.id === selected);
    g.classList.toggle("near", near.has(g.dataset.id) && g.dataset.id !== selected);
    g.classList.toggle("faded", !!selected && !near.has(g.dataset.id));
  }
  for (const line of svg.querySelectorAll(".edge")) {
    line.classList.toggle("faded", !!selected && line.dataset.from !== selected && line.dataset.to !== selected);
  }
}

// ---------- Painel do nó ----------
const EDGE_WORDS = { tag: "tag", deck: "deck", parent: "hierarquia" };

function renderInfo() {
  const info = $("info");
  const node = nodeById(selected);
  if (!node) {
    info.hidden = true;
    return;
  }
  info.hidden = false;
  $("infoTitle").textContent = node.kind === "card" ? node.label : labelOf(node);
  $("infoKind").textContent =
    node.kind === "card" ? `cartão${node.due ? " · vencido" : ""}` : node.kind === "tag" ? "tag" : "deck";

  // Conexões explícitas (com ✕ para desfazer todas as do par) …
  const pairs = new Map();
  for (const [id, l] of Object.entries(links.links)) {
    if (l.from !== selected && l.to !== selected) continue;
    const other = l.from === selected ? l.to : l.from;
    const pair = pairs.get(other) ?? { ids: [], out: false, in: false, relations: new Set() };
    pair.ids.push(id);
    if (l.from === selected) pair.out = true; else pair.in = true;
    if (l.relation) pair.relations.add(l.relation);
    pairs.set(other, pair);
  }
  const items = [...pairs.entries()].sort(([a], [b]) => (a < b ? -1 : 1)).map(([other, p], i) => {
    const arrow = p.out && p.in ? "↔" : p.out ? "→" : "←";
    const rel = p.relations.size ? ` <span class="meta">(${esc([...p.relations].sort().join(", "))})</span>` : "";
    return `<div class="link-item"><span class="grow"><span class="jump" data-jump="${esc(other)}">${arrow} ${esc(labelOf(nodeById(other)) || other)}</span>${rel}</span>
      <button type="button" class="btn ghost sm" data-unlink="${i}" title="Desfazer a sinapse">✕</button></div>`;
  });
  $("infoLinks").innerHTML = items.length ? `<div class="meta">⚡ sinapses</div>${items.join("")}` : '<div class="meta">sem sinapses</div>';
  const pairList = [...pairs.entries()].sort(([a], [b]) => (a < b ? -1 : 1));
  $("infoLinks").querySelectorAll("[data-unlink]").forEach((b) => {
    b.onclick = async () => {
      await store.unlinkAll(pairList[Number(b.dataset.unlink)][1].ids);
      $("infoMsg").textContent = "sinapse desfeita";
    };
  });

  // … e as demais arestas (tags, deck, hierarquia), clicáveis para navegar.
  const other = full.edges
    .filter((e) => e.kind !== "link" && (e.from === selected || e.to === selected))
    .map((e) => {
      const id = e.from === selected ? e.to : e.from;
      return `<span class="jump" data-jump="${esc(id)}">${esc(labelOf(nodeById(id)))}</span> <span>(${EDGE_WORDS[e.kind]})</span>`;
    });
  $("infoOther").innerHTML = other.length ? `também: ${other.join(" · ")}` : "";
  info.querySelectorAll("[data-jump]").forEach((j) => (j.onclick = () => select(j.dataset.jump, { center: true })));
  $("linkMode").textContent = linkFrom ? "cancelar ligação" : "⚡ ligar a…";
}

function select(id, { center = false } = {}) {
  selected = id && nodeById(id) ? id : null;
  linkFrom = null;
  $("infoMsg").textContent = "";
  if (Number($("depth").value) > 0) build();
  else applySelection();
  if (center && selected && positions.has(selected)) {
    const p = positions.get(selected), { width, height } = canvasSize();
    view.x = width / 2 - p.x * view.scale;
    view.y = height / 2 - p.y * view.scale;
    applyView();
  }
  renderInfo();
}

// Tag classifica, não é ponta de sinapse (1.6): cartão ↔ tag dá a tag ao
// cartão; deck ↔ tag e tag ↔ tag são recusados (as tags do deck são as da
// criação; assuntos se relacionam por páginas, na CLI). Devolve a mensagem,
// ou null quando o par é de sinapse.
async function classify(from, target, relation) {
  const a = store.parseNode(from), b = store.parseNode(target);
  if (a.kind !== "tag" && b.kind !== "tag") return null;
  if (a.kind === "tag" && b.kind === "tag") return "tags não se ligam — para relacionar assuntos, crie páginas para eles (pnn p) e ligue as páginas";
  const [item, tag] = a.kind === "tag" ? [b, a] : [a, b];
  if (item.kind === "deck") return "deck não ganha tag pelo ligar — as tags do deck são as da criação";
  if (relation) return "relação é de sinapse; tag classifica — tire a relação para dar a tag ao cartão";
  const added = await store.tagCard(item.id, tag.id);
  return added ? `🏷 tag adicionada: ${labelOf(nodeById(`${item.kind}:${item.id}`))}  #${tag.id}` : `já tem #${tag.id}`;
}

async function linkTo(target) {
  const from = linkFrom;
  linkFrom = null;
  const relation = $("linkRelation").value.trim();
  const classified = await classify(from, target, relation);
  if (classified !== null) {
    $("infoMsg").textContent = classified;
    applySelection();
    renderInfo();
    return;
  }
  const already = Object.values(links.links).filter(
    (l) => (l.from === from && l.to === target) || (l.from === target && l.to === from)
  );
  // Redundante: sem relação, o par já ligado; com relação, a mesma relação.
  if (already.length && (!relation || already.some((l) => l.relation === relation))) {
    $("infoMsg").textContent = `já há sinapse com ${labelOf(nodeById(target))}`;
  } else {
    await store.linkNodes(store.parseNode(from), store.parseNode(target), relation);
    $("linkRelation").value = "";
    $("infoMsg").textContent = `⚡ sinapse: ${labelOf(nodeById(from))} → ${labelOf(nodeById(target))}`;
  }
  applySelection();
  renderInfo();
}

// ---------- Interação: clique, arrasto, pan e zoom ----------
let drag = null; // { id, x, y, moved }
let suppressClick = false;

$("canvas").addEventListener("pointerdown", (e) => {
  const g = e.target.closest?.(".node");
  drag = { id: g?.dataset.id ?? null, x: e.clientX, y: e.clientY, moved: false, start: { ...view } };
  $("canvas").setPointerCapture?.(e.pointerId);
});
$("canvas").addEventListener("pointermove", (e) => {
  if (!drag) return;
  const dx = e.clientX - drag.x, dy = e.clientY - drag.y;
  if (!drag.moved && Math.hypot(dx, dy) < 3) return;
  drag.moved = true;
  if (drag.id) { // arrastar um nó o fixa onde for solto
    const p = positions.get(drag.id);
    p.x += dx / view.scale; p.y += dy / view.scale; p.fixed = true;
    drag.x = e.clientX; drag.y = e.clientY;
    applyPositions();
  } else {
    view.x = drag.start.x + dx; view.y = drag.start.y + dy;
    applyView();
  }
});
$("canvas").addEventListener("pointerup", () => {
  suppressClick = !!drag?.moved;
  drag = null;
});
$("canvas").addEventListener("click", (e) => {
  if (suppressClick) { suppressClick = false; return; }
  const id = e.target.closest?.(".node")?.dataset.id ?? null;
  if (linkFrom && id && id !== linkFrom) linkTo(id);
  else select(id);
});
$("canvas").addEventListener("wheel", (e) => {
  e.preventDefault();
  const r = $("canvas").getBoundingClientRect();
  const mx = e.clientX - r.left, my = e.clientY - r.top;
  const scale = Math.min(Math.max(view.scale * Math.exp(-e.deltaY * 0.0015), 0.1), 6);
  view.x = mx - ((mx - view.x) / view.scale) * scale;
  view.y = my - ((my - view.y) / view.scale) * scale;
  view.scale = scale;
  applyView();
}, { passive: false });
document.addEventListener("keydown", (e) => {
  if (e.key !== "Escape") return;
  if (linkFrom) { linkFrom = null; applySelection(); renderInfo(); } else select(null);
});

for (const k of KINDS) $(`show-${k}`).onchange = build;
$("depth").onchange = () => {
  if (Number($("depth").value) > 0 && !selected) $("stats").textContent = "selecione um nó para o grafo local";
  build();
};
$("search").onchange = () => {
  const id = byLabel.get($("search").value.trim());
  if (id) {
    for (const k of KINDS) if (id.startsWith(`${k}:`)) $(`show-${k}`).checked = true;
    build();
    select(id, { center: true });
  }
};
$("linkMode").onclick = () => {
  linkFrom = linkFrom ? null : selected;
  $("infoMsg").textContent = linkFrom ? "clique no nó de destino (Esc cancela)" : "";
  applySelection();
  renderInfo();
};
// "Estudar ao redor": o side panel aplica o filtro (spec §4.11, around). O
// sidePanel.open vem primeiro, ainda dentro do gesto do clique.
$("studyAround").onclick = () => {
  if (!selected) return;
  if (windowId !== null) chrome.sidePanel?.open?.({ windowId })?.catch?.(() => {});
  store.requestReview({ around: [selected] });
  $("infoMsg").textContent = `filtro "ao redor de ${labelOf(nodeById(selected))}" enviado ao painel lateral`;
};

// ---------- Dados ----------
async function refresh() {
  [full, links] = await Promise.all([store.state.graph(), store.state.links()]);
  if (!positions.size && full.nodes.length > LARGE) {
    $("show-card").checked = false; // acervo grande: abre pela visão geral
  }
  byLabel.clear();
  for (const node of full.nodes) byLabel.set(labelOf(node), node.id);
  $("graphNodes").innerHTML = [...byLabel.keys()].map((l) => `<option value="${esc(l)}"></option>`).join("");
  if (selected && !nodeById(selected)) selected = null;
  build();
  renderInfo();
}

store.onChange(refresh);
export const ready = store.ready.then(refresh);
