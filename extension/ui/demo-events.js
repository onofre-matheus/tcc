// Cenário de demonstração para os prints da monografia — espelho fiel de
// scripts/seed-demo.sh. Puro (sem chrome), recebe o "agora" e devolve
// envelopes prontos para EventLog.merge(): ids fixos + dedup por id tornam a
// injeção idempotente, como um download de sync de outro dispositivo.
//
// Cenário (datas relativas a `nowIso`, vale em qualquer dia):
//   - streak de 4 dias terminando ontem
//   - 3 cartões vencidos hoje; o 1º da fila (Round-Robin) tem fonte e
//     explicação de Feynman anterior
//   - janela de atenção com mediana de 27 min (26/24/31/28; a sessão
//     interrompida de 9 min fica fora)
//   - a sessão de ontem tem check-in de atenção (na tarefa em 1 de 2)
//   - caixa de entrada com 1 nota (com fonte) e 1 distração
//   - agenda de hoje (+1h) e de amanhã (14h), lista A/B/C com 2 subtarefas

const DEVICE = "seed-ext";

export function buildDemoEvents(nowIso) {
  const now = new Date(nowIso);
  const dayISO = (daysAgo) =>
    new Date(now.getTime() - daysAgo * 86400000).toISOString().slice(0, 10);
  const at = (daysAgo, hhmm) => `${dayISO(daysAgo)}T${hhmm}:00.000Z`;

  let lc = 0;
  const ev = (id, type, ts, payload, v = 1) => ({
    id, type, v, lc: ++lc, ts, device: DEVICE, payload,
  });

  // Compromissos de hoje (+1h, em UTC) e de amanhã (14h no fuso local).
  const inHours = (h) => new Date(now.getTime() + h * 3600000).toISOString();
  const tomorrowAt = (hourLocal) => {
    const d = new Date(now);
    d.setDate(d.getDate() + 1);
    d.setHours(hourLocal, 0, 0, 0);
    return d.toISOString();
  };

  return [
    // Decks: hierarquia por "::" e tags transversais
    ev("seed-01", "deck.created", at(6, "12:00"), { deck_id: "d-so", name: "Sistemas Operacionais", tags: ["so"] }),
    ev("seed-02", "deck.created", at(6, "12:00"), { deck_id: "d-esc", name: "Sistemas Operacionais::Escalonamento", tags: ["so"] }),
    ev("seed-03", "deck.created", at(6, "12:01"), { deck_id: "d-en", name: "Inglês::Vocabulário", tags: ["idiomas"] }),

    // Cartões: c-rr com fonte, para o link no print da revisão
    ev("seed-04", "card.created", at(6, "12:02"), {
      card_id: "c-rr", deck_id: "d-esc",
      front: "O que é o escalonamento Round-Robin?",
      back: "Preempção por fatias de tempo (quantum): cada processo executa um quantum e volta ao fim da fila circular.",
      source_url: "https://pages.cs.wisc.edu/~remzi/OSTEP/cpu-sched.pdf",
      source_title: "OSTEP — Scheduling: Introduction",
      tags: ["so", "escalonamento"],
    }),
    ev("seed-05", "card.created", at(6, "12:03"), {
      card_id: "c-pt", deck_id: "d-so",
      front: "Qual a diferença entre processo e thread?",
      back: "Processo tem espaço de endereçamento próprio; threads do mesmo processo compartilham esse espaço.",
      tags: ["so"],
    }),
    ev("seed-06", "card.created", at(5, "12:00"), {
      card_id: "c-en", deck_id: "d-en",
      front: "procrastinate",
      back: "procrastinar; adiar deliberadamente uma tarefa",
      tags: ["idiomas"],
    }),

    // D-4: sessão de revisão completa (26 min), Feynman + 3 revisões.
    // c-rr e c-pt acertam → caixa 2, vencem em D-1 (já vencidos hoje).
    ev("seed-07", "session.started", at(4, "12:00"), { session_id: "s1", kind: "review", planned_minutes: 25 }),
    ev("seed-08", "card.explained", at(4, "12:03"), { card_id: "c-rr", text: "É o escalonador que dá uma fatia de tempo para cada processo e vai revezando em círculo.", session_id: "s1" }),
    ev("seed-09", "card.reviewed", at(4, "12:04"), { card_id: "c-rr", result: "correct", session_id: "s1" }),
    ev("seed-10", "card.reviewed", at(4, "12:08"), { card_id: "c-pt", result: "correct", session_id: "s1" }),
    ev("seed-11", "card.reviewed", at(4, "12:12"), { card_id: "c-en", result: "wrong", session_id: "s1" }),
    ev("seed-12", "session.ended", at(4, "12:26"), { session_id: "s1", outcome: "completed" }),

    // D-3: sessão de 24 min; c-en acerta → caixa 2, vence hoje (D-3 + 3 dias)
    ev("seed-13", "session.started", at(3, "12:00"), { session_id: "s2", kind: "review", planned_minutes: 25 }),
    ev("seed-14", "card.reviewed", at(3, "12:05"), { card_id: "c-en", result: "correct", session_id: "s2" }),
    ev("seed-15", "session.ended", at(3, "12:24"), { session_id: "s2", outcome: "completed" }),

    // D-2: uma interrompida (fora da mediana) e uma completa de 31 min
    ev("seed-16", "session.started", at(2, "12:00"), { session_id: "s3", kind: "task", planned_minutes: 25 }),
    ev("seed-17", "session.ended", at(2, "12:09"), { session_id: "s3", outcome: "interrupted" }),
    ev("seed-18", "session.started", at(2, "14:00"), { session_id: "s4", kind: "task", planned_minutes: 30 }),
    ev("seed-19", "session.ended", at(2, "14:31"), { session_id: "s4", outcome: "completed" }),

    // D-1: sessão de 28 min com check-in de atenção (RF07: a cada 10 min,
    // "na tarefa?" — sim às 12:10, não às 12:20) + pendências de triagem
    ev("seed-20", "session.started", at(1, "12:00"), { session_id: "s5", kind: "task", planned_minutes: 27, checkin_every: 10 }, 2),
    ev("seed-21", "distraction.captured", at(1, "12:10"), { distraction_id: "x-mail", session_id: "s5", text: "responder o e-mail do orientador" }),
    ev("seed-36", "checkin.logged", at(1, "12:10"), { checkin_id: "k-s5-1", session_id: "s5", on_task: true }),
    ev("seed-37", "checkin.logged", at(1, "12:20"), { checkin_id: "k-s5-2", session_id: "s5", on_task: false }),
    ev("seed-22", "session.ended", at(1, "12:28"), { session_id: "s5", outcome: "completed" }),
    ev("seed-23", "note.captured", at(1, "15:00"), { note_id: "n-ostep", text: "Ler o cap. 10 do OSTEP (escalonamento multiprocessador)", url: "https://pages.cs.wisc.edu/~remzi/OSTEP/", page_title: "Operating Systems: Three Easy Pieces" }),

    // Hoje: agenda e lista A/B/C com quebra de tarefas
    ev("seed-24", "appointment.created", nowIso, { appointment_id: "a-aula", title: "Aula de Redes de Computadores", starts_at: inHours(1), ends_at: inHours(2) }),
    ev("seed-25", "appointment.created", nowIso, { appointment_id: "a-orient", title: "Orientação com o Renan", starts_at: tomorrowAt(14), ends_at: tomorrowAt(15) }),
    ev("seed-26", "task.created", nowIso, { task_id: "t-imp", title: "Escrever a seção Implementação do capítulo 4" }),
    ev("seed-27", "task.prioritized", nowIso, { task_id: "t-imp", priority: "A" }),
    ev("seed-28", "task.created", nowIso, { task_id: "t-ost", title: "Revisar o capítulo de escalonamento do OSTEP" }),
    ev("seed-29", "task.prioritized", nowIso, { task_id: "t-ost", priority: "B" }),
    ev("seed-30", "task.created", nowIso, { task_id: "t-bib", title: "Organizar as referências no BibTeX" }),
    ev("seed-31", "task.prioritized", nowIso, { task_id: "t-bib", priority: "C" }),
    ev("seed-32", "task.created", nowIso, { task_id: "t-tab", title: "Descrever a tabela comando-requisito", parent_id: "t-imp" }),
    ev("seed-33", "task.prioritized", nowIso, { task_id: "t-tab", priority: "A" }),
    ev("seed-34", "task.created", nowIso, { task_id: "t-pri", title: "Tirar os prints da extensão", parent_id: "t-imp" }),
    ev("seed-35", "task.prioritized", nowIso, { task_id: "t-pri", priority: "A" }),
  ];
}
