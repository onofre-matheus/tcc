// Layout de forças (Fruchterman–Reingold) em JS puro — a extensão não tem
// bundler, e um d3 inteiro para isto custaria mais que o algoritmo. Nós se
// repelem, arestas puxam como molas, a gravidade segura o desenho no centro,
// e a "temperatura" limita o passo e esfria a cada iteração.
//
// Determinístico: a posição inicial vem de um hash do id, então a mesma
// entrada dá o mesmo desenho (testável, e o grafo não "pula" ao reabrir).
// Repulsão O(n²): folgado até algumas centenas de nós; acima disso a tela
// sugere esconder os cartões ou usar o grafo local.

// FNV-1a de 32 bits → [0, 1).
export function hash01(s) {
  let h = 0x811c9dc5;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return (h >>> 0) / 4294967296;
}

// nodes: [{ id }]; edges: [{ from, to }]; previous: Map id → {x, y} de um
// desenho anterior (mantém o lugar de quem já estava na tela).
//
// gravity: a soma das repulsões num nó da borda cresce com n·k²/R e a
// gravidade com g·R; como k² ∝ área/n, o raio de equilíbrio (R² ≈ 0,36·área/g)
// não depende de n. Com g = 1 o desenho cabe no quadro — medido: 32 nós num
// quadro de 1100×600 ocupam ~620×750 sem sobreposição; com 0,05, componentes
// desconexos se espalhavam por ~2500 px e o enquadramento encolhia tudo.
export function createLayout(nodes, edges, { width = 800, height = 600, previous, gravity = 1 } = {}) {
  const cx = width / 2, cy = height / 2;
  const k = Math.sqrt((width * height) / Math.max(nodes.length, 1)) * 0.6;
  const spread = Math.min(width, height) / 2.2;
  const positions = new Map();
  for (const node of nodes) {
    const old = previous?.get(node.id);
    const angle = hash01(node.id) * 2 * Math.PI;
    const radius = Math.sqrt(hash01(`${node.id}#r`)) * spread;
    positions.set(node.id, {
      x: old?.x ?? cx + radius * Math.cos(angle),
      y: old?.y ?? cy + radius * Math.sin(angle),
      fixed: old?.fixed ?? false,
    });
  }
  const springs = edges
    .filter((e) => positions.has(e.from) && positions.has(e.to) && e.from !== e.to)
    .map((e) => [positions.get(e.from), positions.get(e.to)]);
  const points = [...positions.values()];
  let temperature = Math.min(width, height) / 8;

  function step() {
    const dx = new Float64Array(points.length), dy = new Float64Array(points.length);
    for (let i = 0; i < points.length; i++) {
      for (let j = i + 1; j < points.length; j++) {
        let ox = points[i].x - points[j].x, oy = points[i].y - points[j].y;
        let d2 = ox * ox + oy * oy;
        if (d2 < 0.01) { // sobrepostos: afasta numa direção determinística
          ox = 0.1 * (i - j);
          oy = 0.1;
          d2 = ox * ox + oy * oy;
        }
        const f = (k * k) / d2; // repulsão k²/d, já dividida por d
        dx[i] += ox * f; dy[i] += oy * f;
        dx[j] -= ox * f; dy[j] -= oy * f;
      }
    }
    const index = new Map(points.map((p, i) => [p, i]));
    for (const [a, b] of springs) {
      const ox = a.x - b.x, oy = a.y - b.y;
      const d = Math.sqrt(ox * ox + oy * oy) || 0.01;
      const f = d / k; // atração d²/k, já dividida por d
      const ia = index.get(a), ib = index.get(b);
      dx[ia] -= ox * f; dy[ia] -= oy * f;
      dx[ib] += ox * f; dy[ib] += oy * f;
    }
    for (let i = 0; i < points.length; i++) {
      const p = points[i];
      if (p.fixed) continue;
      dx[i] += (cx - p.x) * gravity; // gravidade
      dy[i] += (cy - p.y) * gravity;
      const len = Math.sqrt(dx[i] * dx[i] + dy[i] * dy[i]);
      if (len > 0) {
        const move = Math.min(len, temperature);
        p.x += (dx[i] / len) * move;
        p.y += (dy[i] / len) * move;
      }
    }
    temperature = Math.max(temperature * 0.97, 0.5);
  }

  return {
    positions,
    step,
    run(iterations) {
      for (let i = 0; i < iterations; i++) step();
    },
    reheat(t) {
      temperature = Math.max(temperature, t);
    },
  };
}

// Caixa que contém todos os pontos, para enquadrar o desenho na tela.
export function bounds(positions) {
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  for (const { x, y } of positions.values()) {
    minX = Math.min(minX, x); minY = Math.min(minY, y);
    maxX = Math.max(maxX, x); maxY = Math.max(maxY, y);
  }
  return Number.isFinite(minX) ? { minX, minY, maxX, maxY } : { minX: 0, minY: 0, maxX: 0, maxY: 0 };
}
