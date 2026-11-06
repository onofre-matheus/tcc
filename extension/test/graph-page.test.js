// @vitest-environment happy-dom
//
// Tela de grafo (spec §4.14): desenha a projeção, filtra por tipo, mostra o
// grafo local, e a partir do nó em foco liga, desliga e pede ao side panel
// uma revisão "ao redor" (pelo storage).

import { describe, expect, it } from "vitest";
import { $, ago, ev, mountPage, settle, storage } from "./sidepanel-env.js";

const { lastOf } = await mountPage("graph/graph", [
  ev("s1", 1, "deck.created", ago(3), { deck_id: "d1", name: "SO", tags: ["sistemas"] }),
  ev("s2", 2, "card.created", ago(2), { card_id: "c1", deck_id: "d1", front: "processo?", back: "b", tags: ["so/threads"] }),
  ev("s3", 3, "card.created", ago(2), { card_id: "c2", front: "OSI?", back: "b", tags: ["redes"] }, 2),
  ev("s4", 4, "link.created", ago(1), { link_id: "l1", from: { kind: "card", id: "c1" }, to: { kind: "card", id: "c2" }, relation: "prerequisito" }),
]);

const nodeIds = () => [...document.querySelectorAll("#canvas .node")].map((g) => g.dataset.id).sort();
const click = (id) =>
  document.querySelector(`#canvas .node[data-id="${id}"] circle, #canvas .node[data-id="${id}"] rect`)
    .dispatchEvent(new MouseEvent("click", { bubbles: true }));
const change = (id) => $(id).dispatchEvent(new Event("change"));

describe("tela de grafo", () => {
  it("desenha cartões, decks e tags (com ancestral implícito), com a conexão em seta", () => {
    expect(nodeIds()).toEqual(["card:c1", "card:c2", "deck:d1", "tag:redes", "tag:sistemas", "tag:so", "tag:so/threads"]);
    const link = document.querySelector("#canvas .edge.link");
    expect(link.getAttribute("marker-end")).toBe("url(#arrow)");
    expect(document.querySelectorAll("#canvas .edge")).toHaveLength(6);
    for (const g of document.querySelectorAll("#canvas .node")) {
      expect(g.getAttribute("transform")).toMatch(/^translate\(-?\d+(\.\d)?,-?\d+(\.\d)?\)$/);
    }
    expect(document.querySelector('#canvas .node[data-id="card:c1"]').classList.contains("due")).toBe(true);
  });

  it("esconde um tipo de nó", () => {
    $("show-card").checked = false;
    change("show-card");
    expect(nodeIds()).not.toContain("card:c1");
    expect($("stats").textContent).toBe("5 de 7 nós · 2 arestas");
    $("show-card").checked = true;
    change("show-card");
    expect(nodeIds()).toHaveLength(7);
  });

  it("clicar num nó abre o painel com conexões e demais arestas; o grafo local recorta", () => {
    click("card:c1");
    expect($("info").hidden).toBe(false);
    expect($("infoTitle").textContent).toBe("processo?");
    expect($("infoLinks").textContent.replace(/\s+/g, " ")).toContain("→ “OSI?” (prerequisito)");
    expect($("infoOther").textContent).toContain("deck SO (deck)");
    expect($("infoOther").textContent).toContain("#so/threads (tag)");

    $("depth").value = "1";
    change("depth");
    expect(nodeIds()).toEqual(["card:c1", "card:c2", "deck:d1", "tag:so/threads"]);
    $("depth").value = "0";
    change("depth");
  });

  it("🔗 ligar a… e clique no destino grava link.created; ✕ desfaz", async () => {
    $("linkRelation").value = "exemplo";
    $("linkMode").click();
    expect($("infoMsg").textContent).toContain("clique no nó de destino");
    click("tag:sistemas");
    await settle();
    expect((await lastOf("link.created")).payload).toMatchObject({
      from: { kind: "card", id: "c1" }, to: { kind: "tag", id: "sistemas" }, relation: "exemplo",
    });
    expect(document.querySelectorAll("#canvas .edge.link")).toHaveLength(2);

    $("infoLinks").querySelector("[data-unlink]").click(); // o primeiro par: OSI
    await settle();
    expect((await lastOf("link.removed")).payload).toEqual({ link_id: "l1" });
  });

  it("estudar ao redor manda o filtro ao side panel pelo storage", () => {
    $("studyAround").click();
    expect(storage.get("review_request")).toMatchObject({ around: ["card:c1"] });
    expect($("infoMsg").textContent).toContain("ao redor de “processo?”");
  });

  it("a busca foca o nó pelo rótulo", () => {
    $("search").value = "#redes";
    change("search");
    expect($("infoTitle").textContent).toBe("#redes");
    expect(document.querySelector('#canvas .node[data-id="tag:redes"]').classList.contains("selected")).toBe(true);
  });
});
