// @vitest-environment happy-dom
//
// Sessões salvas (catálogo 1.5) no side panel: escolher uma preenche o filtro
// da revisão; salvar grava study.saved (o mesmo nome reaproveita o id);
// apagar grava study.deleted. E o pedido "estudar ao redor" vindo da tela de
// grafo chega pelo storage e vira filtro.

import { describe, expect, it } from "vitest";
import { $, ago, ev, mountSidepanel, settle, storage } from "./sidepanel-env.js";

const { lastOf } = await mountSidepanel([
  ev("s1", 1, "deck.created", ago(3), { deck_id: "d1", name: "SO", tags: [] }),
  ev("s2", 2, "card.created", ago(2), { card_id: "c1", deck_id: "d1", front: "processo?", back: "b", tags: ["so"] }),
  ev("s3", 3, "card.created", ago(2), { card_id: "c2", front: "OSI?", back: "b", tags: ["redes"] }, 2),
  ev("s4", 4, "card.created", ago(2), { card_id: "c3", front: "thread?", back: "b", tags: ["so/threads"] }, 2),
  ev("s5", 5, "card.reviewed", ago(1), { card_id: "c3", result: "correct" }),
  ev("s6", 6, "study.saved", ago(1), { study_id: "st1", name: "Véspera", tags: ["so"], practice: true }),
]);

const options = (id) => [...$(id).options].map((o) => o.textContent);

describe("side panel: sessões salvas e pedido do grafo", () => {
  it("escolher a sessão salva preenche o filtro (com o treino)", async () => {
    expect(options("savedStudySel")).toEqual(["sessões salvas…", "Véspera"]);
    $("savedStudySel").value = "st1";
    $("savedStudySel").dispatchEvent(new Event("change"));
    await settle();
    expect($("reviewTags").value).toBe("#so");
    expect($("reviewPractice").checked).toBe(true);
    expect($("startReview").textContent).toBe("Revisar flashcards (1 + 1 treino) · filtrado");
    expect($("deleteStudy").hidden).toBe(false);
  });

  it("salvar grava o filtro atual; o mesmo nome atualiza a mesma sessão", async () => {
    $("reviewTags").value = "#Redes";
    $("reviewPractice").checked = false;
    window.prompt = () => "Redes";
    $("saveStudy").click();
    await settle();
    const saved = (await lastOf("study.saved")).payload;
    expect(saved).toMatchObject({ name: "Redes", tags: ["redes"], decks: [], around: [], practice: false });
    expect($("savedStudySel").selectedOptions[0].textContent).toBe("Redes");

    $("reviewTags").value = "#redes #so";
    $("saveStudy").click();
    await settle();
    const again = (await lastOf("study.saved")).payload;
    expect(again.study_id).toBe(saved.study_id);
    expect(again.tags).toEqual(["redes", "so"]);
  });

  it("apagar grava study.deleted", async () => {
    window.confirm = () => true;
    $("deleteStudy").click();
    await settle();
    expect((await lastOf("study.deleted")).payload.study_id).toBeTruthy();
    expect(options("savedStudySel")).toEqual(["sessões salvas…", "Véspera"]);
  });

  it("o pedido do grafo vira filtro 'ao redor de' e é consumido", async () => {
    await chrome.storage.local.set({ review_request: { around: ["tag:redes"], at: Date.now() } });
    await settle();
    expect($("reviewAround").value).toBe("tag:redes");
    expect($("reviewTags").value).toBe("");
    expect($("reviewFilter").open).toBe(true);
    expect($("startReview").textContent).toBe("Revisar flashcards (1) · filtrado");
    expect(storage.has("review_request")).toBe(false);
  });
});
