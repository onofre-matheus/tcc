# Especificação do modelo de eventos

Contrato compartilhado entre todos os clientes (extensão Chrome, CLI Go, futuros).
Os vetores de conformidade em `spec/vectors/` são a forma executável desta
especificação: **toda implementação de cliente deve passar o corpus integralmente**.

## 1. Envelope

Todo evento é um objeto JSON com exatamente estes campos:

```json
{
  "id":     "0197c9a4-…",               // UUIDv7; único global; nunca reutilizado
  "type":   "card.reviewed",            // namespace.ação, minúsculo
  "v":      1,                          // versão do esquema DESTE tipo
  "lc":     42,                         // relógio de Lamport do dispositivo emissor
  "ts":     "2026-07-07T23:31:00.000Z", // relógio de parede, sempre UTC ISO-8601
  "device": "d-3f2a…",                  // id estável da instalação
  "payload": { }                        // campos específicos do tipo
}
```

Regras:

- **Imutabilidade**: evento gravado nunca é alterado ou removido.
- **Lamport**: cada dispositivo mantém um contador; ao emitir, `lc = contador + 1`;
  ao receber eventos no sync, `contador = max(contador, max(lc recebidos))`.
- **Versionamento**: mudanças de formato de payload criam `v` novo; o código de
  projeção lê todas as versões anteriores para sempre.
- Nos vetores de conformidade, `id` e `device` usam formas legíveis
  (`evt-001`, `dev-a`); em produção, UUIDv7.

## 2. Ordem total e idempotência

Antes de qualquer projeção, o log é:

1. **Ordenado** por `(lc, ts, device, id)` — comparação numérica em `lc`,
   lexicográfica nos demais.
2. **Deduplicado** por `id` (mantém-se a primeira ocorrência).

Consequência: qualquer permutação do mesmo conjunto de eventos produz o mesmo
estado projetado (**convergência**), e eventos duplicados não alteram o
resultado (**idempotência**). Vetores com `"permutations": true` verificam isso
embaralhando a entrada.

## 3. Catálogo de eventos (v1.2)

O catálogo 1.1 acrescentou ao 1.0 os eventos de **edição e arquivamento**; o
1.2 acrescenta **pausas e o motivo de interrupção** (novidades marcadas ▸ nas
tabelas; `session.ended` ganhou v2 de payload). As adições são compatíveis —
clientes antigos continuam válidos: **tipos de evento desconhecidos são
ignorados pelas projeções**, sem interromper o processamento (vetor
`envelope_tipo_desconhecido_ignorado`), e campos novos opcionais são
ignorados por projeções antigas.

Semântica comum dos eventos novos:

- **Edição é last-writer-wins por campo**: só os campos presentes no payload
  são sobrescritos, na posição do evento na ordem total do §2. Duas edições
  concorrentes de campos distintos se combinam; do mesmo campo, vence a última.
- **Arquivar preserva a história** (o item sai das filas/listas mas permanece
  no estado, com `archived: true`); **apagar/cancelar é tombstone** (o item sai
  do estado projetado). Em ambos os casos o log segue imutável — como no
  livro-razão contábil, a correção é um lançamento novo, nunca uma rasura.
- Evento que referencia id inexistente (ou já apagado) é ignorado.

### Estudo (sistema Leitner)

| Tipo | Payload |
|---|---|
| `deck.created` | `deck_id, name, tags[]` |
| `deck.renamed` (1.1) | `deck_id, name` |
| `deck.archived` (1.1) | `deck_id` |
| `card.created` | `card_id, deck_id, front, back, source_url?, source_title?, tags[]` |
| `card.edited` (1.1) | `card_id, front?, back?, tags[]?` |
| `card.archived` (1.1) | `card_id` |
| `card.reviewed` | `card_id, result: "correct"\|"wrong", session_id?` |
| `card.explained` (v2) | `card_id, text, frame?: "feynman"\|"4causas", session_id?` |

`card.edited` não altera caixa nem vencimento — corrigir o texto não é rever o
cartão. `card.archived` tira o cartão da fila preservando caixa e histórico;
`deck.archived` esvazia a fila do deck inteiro sem marcar os cartões
individualmente (desarquivar o deck, evento futuro, os devolveria intactos).

`card.explained` registra a explicação do usuário antes de ver o verso, e a
anterior é reapresentada para que a lacuna feche iterativamente. O `frame`
(v2, ausente ≡ `feynman`) nomeia o **andaime de elaboração** usado: `feynman`
(explicar com as próprias palavras, como se ensinasse alguém) ou `4causas`
(enquadrar o conceito nas quatro causas — material, formal, eficiente, final).
É um campo **aberto**: um método de estudo novo entra como um valor de `frame`
novo, sem alterar o evento nem as projeções (OCP). Os andaimes operacionalizam
*elaborative interrogation* / *self-explanation*; o `text` guarda a explicação
já concatenada (uma linha `causa: resposta` por causa, no caso de `4causas`).

### Captura e triagem

| Tipo | Payload |
|---|---|
| `note.captured` | `note_id, text, url?, page_title?` |
| `note.triaged` | `note_id, action: "to_card"\|"to_task"\|"discarded", card_id?, task_id?` |
| `distraction.captured` | `distraction_id, session_id, text` |
| `distraction.triaged` | `distraction_id, action: "done"\|"to_task"\|"discarded", task_id?` |

Triagem de nota que vira cartão emite **dois eventos**: `card.created` e
`note.triaged` com o `card_id` resultante.

### Organização

| Tipo | Payload |
|---|---|
| `task.created` | `task_id, title, parent_id?, due_date?` |
| `task.edited` (1.1) | `task_id, title?, due_date?` |
| `task.prioritized` | `task_id, priority: "A"\|"B"\|"C"` |
| `task.completed` | `task_id` |
| `task.deleted` (1.1) | `task_id` |
| `appointment.created` (v2) | `appointment_id, title, starts_at, ends_at, importance?` |
| `appointment.cancelled` (1.1) | `appointment_id` |

Tarefa **não tem campo de descrição** — decisão deliberada: tarefa é ação
(título imperativo, prioridade, prazo); contexto em texto livre pertence à
nota (`note.captured`), e a resposta do sistema para "não sei por onde
começar" é a decomposição em subtarefas (`parent_id`), não a anotação.
`task.deleted` é tombstone (descarte não tem valor histórico, ao contrário
do arquivamento de cartões); `appointment.cancelled` idem.

`importance` (v2 de `appointment.created`, opcional) marca o compromisso como
`"important"`. É lido pela extensão para os **lembretes com antecedência**:
compromisso comum avisa na reta final (1 dia, 1 hora, 10 min e na hora);
`"important"` ganha, antes disso, uma cascata semanal (até 8 semanas —
"marquei com 1 mês, me lembre 1× por semana") e aparece como marca d'água fixa
no topo. Os lembretes são notificações do navegador (service worker +
`chrome.alarms`), portanto uma capacidade da extensão; a CLI ignora o campo.
Projeções leem v1 e v2 (v1 ≡ sem importância).

### Sessões de foco e pausas

| Tipo | Payload |
|---|---|
| `session.started` (v2) | `session_id, kind: "review"\|"task", target_id?, planned_minutes, checkin_every?` |
| `session.ended` (v2) | `session_id, outcome: "completed"\|"interrupted", reason?` |
| ▸ `checkin.logged` | `checkin_id, session_id, on_task: bool` |
| ▸ `pause.started` | `pause_id, planned_minutes?` |
| ▸ `pause.ended` | `pause_id` |
| ▸ `pause.logged` | `pause_id, starts_at, ends_at` |

A duração real da sessão **não é campo**: deriva de `ts(ended) − ts(started)`.
O mesmo vale para o par `pause.started`/`pause.ended`.

`reason` (v2 de `session.ended`, opcional) registra por que o usuário
interrompeu — automonitoramento no espírito de Safren: a interrupção vira dado
revisável, não fracasso. Projeções leem v1 e v2 (v1 ≡ sem motivo).

`checkin_every` (v2 de `session.started`, opcional, minutos) pede à superfície
que pergunte periodicamente "você está na tarefa?" durante a sessão — o
*self-monitoring of attention* da literatura comportamental. Cada resposta é um
`checkin.logged` binário (`on_task`); a superfície é livre no mecanismo
(notificação do navegador, sino do terminal). v1 ≡ sem check-ins. Fazer uma
pausa durante a sessão a **encerra** (`session.ended` com
`outcome: "interrupted"`, `reason: "pausa"`): a pausa cronometrada é um estado
próprio, não um parêntese dentro do foco.

`pause.logged` é o **lançamento retroativo** ("esqueci de apertar o botão"):
o tempo do domínio vai em `starts_at`/`ends_at` no payload, enquanto o `ts` do
envelope marca quando o fato foi registrado — como no livro-razão, corrige-se
com um lançamento novo, nunca reescrevendo o passado. Intervalos com
`ends_at <= starts_at` são ignorados pelas projeções, assim como pares
`started`/`ended` não fechados.

## 4. Projeções

Projeções são funções puras `f(eventos, parâmetros) → estado`. Toda projeção
que envolve "dia" recebe `now` (instante UTC) e `tz` (timezone IANA) como
parâmetros — nunca lê o relógio do sistema. "Data local" de um instante é a
data civil desse instante no fuso `tz`.

### 4.1 Leitner (`projection: "leitner"`)

- Caixas 1–5; intervalos por caixa: `[1, 3, 7, 14, 30]` dias.
- `card.created` → caixa 1; `due` = data local da criação (**vencido
  imediatamente**).
- `card.reviewed`:
  - `correct` → caixa `min(caixa + 1, 5)`
  - `wrong` → caixa `max(caixa − 1, 1)` (variante atenuada: regride UMA caixa)
  - Em ambos: `due` = data local da revisão + `intervalo[nova caixa]`.
- Revisão de cartão inexistente é ignorada.
- `card.edited`: sobrescreve apenas os campos presentes (`front`, `back`,
  `tags`), last-writer-wins por campo; **não altera caixa nem `due`**.
- `card.archived` → `archived: true`; `deck.archived` exclui da fila os
  cartões do deck sem alterar o `archived` individual de cada um.
- **Vencido**: `due <= hoje`, onde `hoje` = data local de `now`.
- **Fila de revisão**: cartões vencidos **não arquivados e de decks não
  arquivados**, ordenados por `(caixa ↑, due ↑, ts de criação ↑)`. A sessão
  consome a fila até o temporizador soar; a fila não tem tamanho máximo.
- **Andaimes (Feynman/4 causas)**: cada `card.explained` guarda a explicação; a
  projeção expõe a última (`last_explanation`), a contagem (`explanation_count`)
  e o último andaime (`last_frame`, default `feynman`), para a UI reapresentar a
  explicação anterior e o método na revisão seguinte.

Saída: `{ cards: { <card_id>: { deck_id, box, due, front, back, source_url,
source_title, tags, last_explanation, explanation_count, last_frame, archived }
}, queue: [card_id…] }`.

### 4.2 Janela de atenção (`projection: "attention"`)

- Amostras: duração em segundos das sessões com `outcome: "completed"`,
  pareando `session.started`/`session.ended` por `session_id`.
- Consideram-se as **últimas 10** amostras (ordem do log).
- Menos de **3** amostras → default **1500 s** (25 min).
- Resultado: **mediana** (quantidade par → média dos dois centrais).

Saída: `{ attention_seconds: <número> }`.

### 4.3 Lista do dia (`projection: "tasks"`)

- Tarefas não concluídas, ordenadas A → B → C; sem prioridade = C; empate:
  criação mais antiga primeiro.
- `task.prioritized`: a última prioridade emitida vence.
- `task.edited`: sobrescreve apenas os campos presentes (`title`, `due_date`),
  last-writer-wins por campo.
- `task.completed` remove a tarefa da lista do dia.
- `task.deleted` é tombstone: a tarefa sai de `tasks` e da lista do dia, e
  eventos posteriores sobre ela são ignorados.
- Alerta `too_many_a` se tarefas A **ativas** (não concluídas) > 4 (RF03).

Saída: `{ tasks: { <task_id>: { title, parent_id, due_date, priority, done } },
day_list: [task_id…], alerts: { too_many_a } }`.

### 4.4 Consistência (`projection: "stats"`)

- Dia estudado = dia local com ≥ 1 `card.reviewed` ou `session.ended`.
- Streak = dias estudados consecutivos terminando em `hoje` ou ontem.
- `reviews_today` conta apenas `card.reviewed` na data local de `now`.

Saída: `{ days_studied, streak, reviews_today }`.

### 4.5 Decks (`projection: "decks"`)

- Cada `deck.created` projeta nome e tags (transversais).
- `deck.renamed`: o último nome emitido vence (ordem total).
- `deck.archived` → `archived: true` (nome e tags preservados).

Saída: `{ decks: { <deck_id>: { name, tags[], archived } } }`.

**Hierarquia (convenção de nome, estilo Anki)**: o `name` pode codificar um
caminho pai→filho com o separador `::` (ex.: `Cálculo::Limites::Definição`). A
hierarquia é derivada do nome — não há campo `parent_id` de deck. Todo cliente
que exibe a árvore usa exatamente este separador; segmentos são aparados e os
vazios descartados (`A :: B` ≡ `A::B`). Níveis intermediários sem
`deck.created` próprio são nós implícitos (não recebem cartões). A contagem de
cartões de um deck, quando exibida em árvore, inclui a dos descendentes.

### 4.6 Caixa de entrada (`projection: "inbox"`)

- Notas e distrações capturadas e ainda **não triadas**, em ordem de captura
  (ordem total do log). Triagem correspondente as remove das pendências.

Saída: `{ notes: { <note_id>: { text, url, page_title } },
distractions: { <distraction_id>: { text, session_id } },
pending_notes: [note_id…], pending_distractions: [distraction_id…] }`.

### 4.7 Retrospectiva semanal (`projection: "review"`)

A projeção da revisão à la Safren: o que foi feito, quando, e o que
interrompeu. A **semana projetada é a que contém a data local de `now`**
(segunda a domingo) — navegar para semanas passadas é só passar um `now`
dentro da semana desejada; nenhum parâmetro extra existe.

Regras de agregação (datas locais em `tz`; minutos = `round(segundos/60)`):

- **Sessões**: pareadas por `session_id`; a duração real vai para o dia local
  de `session.started`. `outcome` conta em `completed`/`interrupted`;
  `reason`, quando presente em interrupção, agrega em `reasons`.
- **Pausas**: pares `pause.started`/`pause.ended` (dia local do início) e
  `pause.logged` (dia local de `starts_at` — a retroativa conta no dia em que
  a pausa ocorreu, não no dia em que foi registrada).
- **Revisões**: `card.reviewed` por dia local.
- **Check-ins**: `checkin.logged` por dia local — `checkins` conta todos,
  `checkins_on_task` os com `on_task: true` ("na tarefa em X de Y checagens").
- **Agenda**: `appointment_minutes` soma a duração dos compromissos cujo
  início cai na semana (tempo planejado, para contraste com o executado).
- `days` traz **os sete dias da semana**, zerados quando sem atividade.

Saída: `{ week_start, week_end, days: { <AAAA-MM-DD>: { reviews,
focus_minutes, pause_minutes, completed, interrupted, checkins,
checkins_on_task } }, totals: { idem }, reasons: { <motivo>: n },
appointment_minutes }`.

### 4.8 Agenda (`projection: "calendar"`)

- Cada `appointment.created` projeta título e intervalo.
- `appointment.cancelled` é tombstone: o compromisso sai de `appointments`
  e de `upcoming`.
- `upcoming` = compromissos ainda não encerrados (`ends_at >= now`),
  ordenados por `(starts_at ↑, id ↑)`.

Saída: `{ appointments: { <appointment_id>: { title, starts_at, ends_at } },
upcoming: [appointment_id…] }`.

## 5. Sincronização

Uma única operação:

```
POST /sync
→ { device, cursor, events: [eventos locais ainda não confirmados] }
← { events: [eventos do servidor após cursor], cursor: novo }
```

O servidor mantém uma sequência global de envelopes, deduplica por `id`, **não
lê `payload`** e não contém regra de negócio. `cursor` é a posição na
sequência global confirmada pelo dispositivo.

## 6. Formato dos vetores de conformidade

```json
{
  "name": "leitner_erro_regride_uma_caixa",
  "projection": "leitner",
  "now": "2026-07-10T12:00:00Z",
  "tz": "America/Sao_Paulo",
  "permutations": false,
  "events": [ … envelopes completos … ],
  "expected": { … asserção parcial sobre a saída da projeção … }
}
```

- `expected` é comparado por **inclusão parcial**: todo campo presente em
  `expected` deve ter valor idêntico na saída; campos ausentes são livres.
  Arrays são comparados por igualdade exata.
- `"permutations": true` → a suíte reexecuta com a lista de eventos
  embaralhada (≥ 5 permutações determinísticas) e exige o mesmo resultado.
