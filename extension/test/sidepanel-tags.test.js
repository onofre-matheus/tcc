// @vitest-environment happy-dom
//
// Tags livres, cartão solto e sessão personalizada no side panel (catálogo
// 1.3): o que precisa ser provado é a fiação da tela até o log — chip de tag
// filtra a revisão, o treino grava card.practiced (não card.reviewed) e o
// cartão "(sem deck)" sai como card.created v2 sem deck_id.

import { describe, expect, it } from "vitest";
import { $, ago, ev, mountSidepanel, settle } from "./sidepanel-env.js";

// Acervo: deck SO (tag transversal "sistemas") com 1 cartão vencido, e 1
// cartão solto já revisado (não vencido) com tag hierárquica.
const { store, lastOf } = await mountSidepanel([
  ev("s1", 1, "deck.created", ago(3), { deck_id: "d1", name: "SO", tags: ["sistemas"] }),
  ev("s2", 2, "card.created", ago(2), { card_id: "c1", deck_id: "d1", front: "processo?", back: "programa em execução", tags: [] }),
  ev("s3", 3, "card.created", ago(2), { card_id: "c2", front: "deadlock?", back: "espera circular", tags: ["so/concorrencia"] }, 2),
  ev("s4", 4, "card.reviewed", ago(1), { card_id: "c2", result: "correct" }),
]);

describe("side panel: tags, cartão solto e sessão personalizada", () => {
  it("chips de tag com cartões e vencidos (herdando a tag do deck)", async () => {
    await settle();
    const chips = $("tagChips").textContent;
    expect(chips).toContain("#sistemas · 1 · 1 vencido");
    expect(chips).toContain("#so/concorrencia · 1");
    expect($("startReview").textContent).toBe("Revisar flashcards (1)");
    expect([...$("cardDeckSel").options].at(-1).textContent).toBe("(sem deck)");
  });

  it("clicar no chip filtra a revisão; treino inclui o não vencido", async () => {
    $("tagChips").querySelector('[data-tag="so/concorrencia"]').click();
    await settle();
    expect($("reviewTags").value).toBe("#so/concorrencia");
    expect($("startReview").textContent).toBe("Revisar flashcards (0) · filtrado");
    expect($("startReview").disabled).toBe(true);

    $("reviewPractice").checked = true;
    $("reviewPractice").dispatchEvent(new Event("change"));
    await settle();
    expect($("startReview").textContent).toBe("Revisar flashcards (0 + 1 treino) · filtrado");
    expect($("startReview").disabled).toBe(false);
  });

  it("a resposta de treino grava card.practiced e não mexe na caixa", async () => {
    $("startReview").click();
    await settle();
    expect($("reviewPanel").hidden).toBe(false);
    expect($("cardDeck").textContent).toContain("(sem deck) · caixa 2 · treino");
    expect($("cardDeck").textContent).toContain("#so/concorrencia");

    $("reveal").click();
    await settle();
    $("correct").click();
    await settle();

    const practiced = await lastOf("card.practiced");
    expect(practiced.payload).toMatchObject({ card_id: "c2", result: "correct" });
    expect(await lastOf("card.reviewed")).toMatchObject({ id: "s4" }); // só a do seed
    const lt = await store.state.leitner();
    expect(lt.cards.c2.box).toBe(2);
    window.prompt = () => null; // o happy-dom não tem prompt: pula o motivo
    $("endFocus").click(); // encerra a sessão
    await settle();
    expect((await lastOf("session.ended")).payload.outcome).toBe("interrupted");
  });

  it('"(sem deck)" cria cartão solto: card.created v2 sem deck_id, tags normalizadas', async () => {
    $("cardDeckSel").value = "";
    $("cardDeckSel").dispatchEvent(new Event("change"));
    $("cardFront").value = "Modelo OSI?";
    $("cardBack").value = "7 camadas";
    $("cardTags").value = "#Redes, prova-p2";
    $("cardForm").dispatchEvent(new Event("submit", { cancelable: true }));
    await settle();

    const created = await lastOf("card.created");
    expect(created.v).toBe(2);
    expect(created.payload).not.toHaveProperty("deck_id");
    expect(created.payload).toMatchObject({ front: "Modelo OSI?", tags: ["redes", "prova-p2"] });
    expect($("cardTags").value).toBe("#Redes, prova-p2"); // fica para o próximo da série
    expect($("tagChips").textContent).toContain("#redes · 1 · 1 vencido");
  });
});
