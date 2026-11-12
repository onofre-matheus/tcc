// @vitest-environment happy-dom
//
// Sinapses e renomeação de tag no side panel (catálogo 1.4; "sinapse" desde o
// 1.6): a fiação da tela até o log — "ao redor de" filtra a revisão pela
// vizinhança, as sinapses do cartão aparecem depois do verso (ligar a outro
// cartão, desligar; ligar a uma #tag classifica o cartão) e o botão direito no
// chip renomeia a tag (tag.renamed). A sinapse l0 com #redes é de antes do
// 1.6: continua valendo e se desfaz pela tela.

import { describe, expect, it } from "vitest";
import { $, ago, ev, mountSidepanel, settle } from "./sidepanel-env.js";

const { store, lastOf } = await mountSidepanel([
  ev("s1", 1, "deck.created", ago(3), { deck_id: "d1", name: "SO", tags: [] }),
  ev("s2", 2, "card.created", ago(2.03), { card_id: "c1", deck_id: "d1", front: "processo?", back: "programa em execução", tags: ["so"] }),
  ev("s3", 3, "card.created", ago(2.02), { card_id: "c2", front: "thread?", back: "fluxo de execução", tags: ["so/threads"] }, 2),
  ev("s4", 4, "card.created", ago(2.01), { card_id: "c3", front: "OSI?", back: "7 camadas", tags: ["redes"] }, 2),
  ev("s5", 5, "link.created", ago(2), { link_id: "l0", from: { kind: "card", id: "c1" }, to: { kind: "tag", id: "redes" } }),
]);

const linkItems = () => [...$("cardLinkList").querySelectorAll(".link-item")];

describe("side panel: conexões e renomeação de tag", () => {
  it("chip mostra as sinapses da tag; o seletor 'ao redor de' lista os nós ligados", () => {
    expect($("tagChips").textContent).toContain("#redes · 1 · 1 vencido · ⚡1");
    const around = [...$("reviewAround").options].map((o) => o.value);
    expect(around).toEqual(["", "card:c1", "tag:redes"]);
  });

  it("'ao redor de #redes' estuda o cartão da tag e o ligado a ela", async () => {
    $("reviewAround").value = "tag:redes";
    $("reviewAround").dispatchEvent(new Event("change"));
    await settle();
    expect($("startReview").textContent).toBe("Revisar flashcards (2) · filtrado");
  });

  it("as sinapses aparecem depois do verso; #tag classifica, cartão vira sinapse; desligar", async () => {
    $("startReview").click();
    await settle();
    expect($("cardFace").textContent).toBe("processo?");
    expect($("cardLinks").hidden).toBe(true); // antes do verso, nada de pista

    $("reveal").click();
    await settle();
    expect($("cardLinks").hidden).toBe(false);
    expect(linkItems().map((el) => el.textContent.replace(/\s+/g, " ").trim())).toEqual(["→ #redes ✕"]);

    // relação é de sinapse: com uma #tag, recusa sem gravar
    $("linkTarget").value = "#SO/Threads";
    $("linkRelation").value = "prerequisito";
    $("linkBtn").click();
    await settle();
    expect($("linkMsg").textContent).toContain("tire a relação");
    expect((await lastOf("card.edited"))).toBeUndefined();

    // sem relação, a #tag classifica o cartão (as próprias + a nova), sem sinapse
    $("linkRelation").value = "";
    $("linkBtn").click();
    await settle();
    expect((await lastOf("card.edited")).payload).toEqual({ card_id: "c1", tags: ["so", "so/threads"] });
    expect($("linkMsg").textContent).toBe("🏷 tag adicionada: #so/threads");

    $("linkTarget").value = "cartão: OSI?";
    $("linkBtn").click();
    await settle();
    expect((await lastOf("link.created")).payload).toMatchObject({ to: { kind: "card", id: "c3" } });
    expect((await lastOf("link.created")).payload).not.toHaveProperty("relation");
    expect($("linkMsg").textContent).toBe("⚡ sinapse com “OSI?”");
    const texts = linkItems().map((el) => el.textContent.replace(/\s+/g, " ").trim());
    expect(texts).toEqual(["→ “OSI?” ✕", "→ #redes ✕"]);

    linkItems()[1].querySelector("[data-unlink]").click();
    await settle();
    expect((await lastOf("link.removed")).payload).toEqual({ link_id: "l0" });
    expect(linkItems()).toHaveLength(1);

    $("linkTarget").value = "sem cerquilha";
    $("linkBtn").click();
    await settle();
    expect($("linkMsg").textContent).toContain("comece com #");
  });

  it("botão direito no chip renomeia a tag e as descendentes", async () => {
    window.prompt = () => "#Sistemas";
    const chip = $("tagChips").querySelector('[data-tag="so"]');
    chip.dispatchEvent(new Event("contextmenu", { cancelable: true }));
    await settle();
    expect((await lastOf("tag.renamed")).payload).toEqual({ from: "so", to: "sistemas" });
    expect($("tagChips").textContent).toContain("#sistemas/threads");
    expect((await store.state.links()).neighbors["card:c1"]).toEqual(["card:c3"]);
    expect((await store.state.leitner()).cards.c1.tags).toEqual(["sistemas", "sistemas/threads"]);

    window.prompt = () => null; // encerra a sessão sem motivo
    $("endFocus").click();
    await settle();
    expect((await lastOf("session.ended")).payload.outcome).toBe("interrupted");
  });
});
