// Layout de forças da tela de grafo (ui/force.js): determinístico, finito, e
// com o efeito que se espera de molas e repulsão.
import { describe, it, expect } from "vitest";
import { createLayout, bounds, hash01 } from "../ui/force.js";

const nodes = ["a", "b", "c", "d", "e", "f"].map((id) => ({ id }));
const edges = [{ from: "a", to: "b" }, { from: "c", to: "d" }, { from: "d", to: "e" }];

describe("createLayout", () => {
  it("é determinístico: a mesma entrada dá o mesmo desenho", () => {
    const one = createLayout(nodes, edges), two = createLayout(nodes, edges);
    one.run(100);
    two.run(100);
    for (const { id } of nodes) expect(one.positions.get(id)).toEqual(two.positions.get(id));
  });

  it("posições finitas, e ligados ficam mais perto que desligados", () => {
    const layout = createLayout(nodes, edges);
    layout.run(300);
    const p = layout.positions;
    for (const { x, y } of p.values()) expect(Number.isFinite(x) && Number.isFinite(y)).toBe(true);
    const dist = (u, v) => Math.hypot(p.get(u).x - p.get(v).x, p.get(u).y - p.get(v).y);
    expect(dist("a", "b")).toBeLessThan(dist("a", "f"));
    expect(dist("c", "d")).toBeLessThan(dist("c", "a"));
  });

  it("reaproveita o desenho anterior e não move nó fixado", () => {
    const previous = new Map([["a", { x: 10, y: 20, fixed: true }]]);
    const layout = createLayout(nodes, edges, { previous });
    layout.run(50);
    expect(layout.positions.get("a")).toMatchObject({ x: 10, y: 20 });
  });

  it("aguenta nós sobrepostos, grafo vazio e aresta para fora", () => {
    const same = new Map([["a", { x: 5, y: 5 }], ["b", { x: 5, y: 5 }]]);
    const layout = createLayout([{ id: "a" }, { id: "b" }], [{ from: "a", to: "zzz" }], { previous: same });
    layout.run(20);
    expect(layout.positions.get("a")).not.toEqual(layout.positions.get("b"));
    expect(bounds(createLayout([], []).positions)).toEqual({ minX: 0, minY: 0, maxX: 0, maxY: 0 });
    expect(hash01("x")).toBeGreaterThanOrEqual(0);
    expect(hash01("x")).toBeLessThan(1);
  });
});
