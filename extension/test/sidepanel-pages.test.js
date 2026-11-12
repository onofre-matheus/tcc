// @vitest-environment happy-dom
//
// Páginas e tarefas (catálogo 1.6) no side panel: as ligações com elas são
// feitas na CLI, mas os nós `page:` e `task:` aparecem rotulados pelo título
// (não como "#<id>") e "ao redor da página" filtra a revisão pelos cartões
// ligados.

import { describe, expect, it } from "vitest";
import { $, ago, ev, mountSidepanel, settle } from "./sidepanel-env.js";

await mountSidepanel([
  ev("s1", 1, "card.created", ago(2.02), { card_id: "c1", front: "O que é um Pod?", back: "a menor unidade", tags: [] }, 2),
  ev("s2", 2, "card.created", ago(2.01), { card_id: "c2", front: "OSI?", back: "7 camadas", tags: ["redes"] }, 2),
  ev("s3", 3, "page.created", ago(2), { page_id: "p1", title: "Documentação do portal", body: "texto", tags: [] }),
  ev("s4", 4, "link.created", ago(1.9), { link_id: "l1", from: { kind: "page", id: "p1" }, to: { kind: "card", id: "c1" } }),
  ev("s5", 5, "task.created", ago(1.8), { task_id: "t1", title: "Terminar a documentação" }),
  ev("s6", 6, "link.created", ago(1.7), { link_id: "l2", from: { kind: "task", id: "t1" }, to: { kind: "page", id: "p1" } }),
]);

describe("side panel: página como ponta de conexão", () => {
  it("o seletor 'ao redor de' rotula a página pelo título", () => {
    const options = [...$("reviewAround").options].map((o) => [o.value, o.textContent]);
    expect(options).toContainEqual(["page:p1", "ao redor de: página Documentação do portal · ⚡2"]);
    // a tarefa também é ponta de sinapse (o contexto dela é a página)
    expect(options).toContainEqual(["task:t1", "ao redor de: tarefa Terminar a documentação · ⚡1"]);
  });

  it("'ao redor da página' estuda só o cartão ligado a ela", async () => {
    $("reviewAround").value = "page:p1";
    $("reviewAround").dispatchEvent(new Event("change"));
    await settle();
    expect($("startReview").textContent).toBe("Revisar flashcards (1) · filtrado");
  });
});
