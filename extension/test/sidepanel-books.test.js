// @vitest-environment happy-dom
//
// Livros (catálogo 1.7) no side panel: ler e anotar é da CLI, mas a seção
// Livros lista os livros com progresso e anotações, o nó `book:` aparece
// rotulado pelo título nas sinapses (não como "#<id>"), "ao redor do livro"
// filtra a revisão pelos cartões criados na leitura, e a retrospectiva
// semanal mostra a leitura do dia.

import { describe, expect, it } from "vitest";
import { $, ago, ev, mountSidepanel, settle } from "./sidepanel-env.js";

await mountSidepanel([
  ev("b1", 1, "book.registered", ago(2), { book_id: "bk1", title: "Mastering Your Adult ADHD", format: "epub", sha256: "x" }),
  ev("b2", 2, "card.created", ago(1.9), { card_id: "c1", front: "Metáfora da escada?", back: "TCC é a escada", source_title: "Mastering Your Adult ADHD, cap. 1, p. 9", tags: [] }, 2),
  ev("b3", 3, "card.created", ago(1.8), { card_id: "c2", front: "fora do livro", back: "x", tags: [] }, 2),
  ev("b4", 4, "link.created", ago(1.7), { link_id: "l1", from: { kind: "book", id: "bk1" }, to: { kind: "card", id: "c1" } }),
  ev("b5", 5, "annotation.created", ago(0), { annotation_id: "a1", book_id: "bk1", loc: { chapter: 1, para: 2 }, quote: "CBT is like a ladder." }),
  ev("b6", 6, "book.progressed", ago(0), { book_id: "bk1", loc: { chapter: 2, para: 1 }, percent: 12 }),
]);

describe("side panel: livro como ponta de sinapse", () => {
  it("o seletor 'ao redor de' rotula o livro pelo título", () => {
    const options = [...$("reviewAround").options].map((o) => [o.value, o.textContent]);
    expect(options).toContainEqual(["book:bk1", "ao redor de: livro Mastering Your Adult ADHD · ⚡1"]);
  });

  it("'ao redor do livro' estuda só o cartão ligado a ele", async () => {
    $("reviewAround").value = "book:bk1";
    $("reviewAround").dispatchEvent(new Event("change"));
    await settle();
    expect($("startReview").textContent).toBe("Revisar flashcards (1) · filtrado");
  });

  it("a seção Livros lista o livro com progresso e as anotações", () => {
    const list = $("bookList");
    expect(list.textContent).toContain("Mastering Your Adult ADHD");
    expect(list.textContent).toContain("12% · 1 anotação");
    const details = list.querySelector("details");
    expect(details.textContent).toContain("▍ “CBT is like a ladder.”");
    expect(details.textContent).toContain("seção 1");
    expect($("bookFile").getAttribute("accept")).toContain(".epub");
  });

  it("a retrospectiva semanal mostra a leitura do dia", () => {
    expect($("weekBody").textContent).toContain("📖 1 · 1 anot.");
    expect($("weekBody").textContent).toContain("📖 1 livro, 1 anot.");
  });
});
