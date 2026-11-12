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

## 3. Catálogo de eventos (v1.6)

O catálogo 1.1 acrescentou ao 1.0 os eventos de **edição e arquivamento**; o
1.2, **pausas e o motivo de interrupção** (marcados ▸ nas tabelas;
`session.ended` ganhou v2 de payload); o 1.3 acrescenta **tags livres, cartão
sem deck e treino fora do vencimento** (marcados "(1.3)"; `card.created`
ganhou v2 de payload); o 1.4, **conexões explícitas e renomeação de tag**
(marcados "(1.4)"); o 1.5, **sessões de estudo salvas** (marcados "(1.5)");
o 1.6, **páginas** — anotações longas de consulta (marcados "(1.6)").
As adições são compatíveis —
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
| `card.created` (v2, 1.3) | `card_id, deck_id?, front, back, source_url?, source_title?, tags[]` |
| `card.edited` (1.1) | `card_id, front?, back?, tags[]?` |
| `card.archived` (1.1) | `card_id` |
| `card.reviewed` | `card_id, result: "correct"\|"wrong", session_id?` |
| `card.practiced` (1.3) | `card_id, result: "correct"\|"wrong", session_id?` |
| `card.explained` (v2) | `card_id, text, frame?: "feynman"\|"4causas", session_id?` |

`card.edited` não altera caixa nem vencimento — corrigir o texto não é rever o
cartão. `card.archived` tira o cartão da fila preservando caixa e histórico;
`deck.archived` esvazia a fila do deck inteiro sem marcar os cartões
individualmente (desarquivar o deck, evento futuro, os devolveria intactos).

**Cartão sem deck (v2 de `card.created`)**: `deck_id` passa a ser opcional —
o cartão solto é organizado só pelas tags. O cliente emite v2 **apenas quando
não há deck**; com deck, o evento v1 segue intacto. Projeções leem v1 e v2
(`deck_id` ausente ou `""` ≡ sem deck).

**Treino (`card.practiced`)**: a resposta a um cartão **não vencido**,
estudado no modo livre da sessão personalizada (§4.11). É registro, não
agendamento: **nenhuma projeção de agendamento o lê** — estudar antes da hora
não promove nem regride o cartão. Clientes antigos o ignoram por ser tipo
desconhecido, então o estado converge entre versões. O dia de estudo já é
contado pelo `session.ended` da sessão (§4.4 inalterado).

`card.explained` registra a explicação do usuário antes de ver o verso, e a
anterior é reapresentada para que a lacuna feche iterativamente. O `frame`
(v2, ausente ≡ `feynman`) nomeia o **andaime de elaboração** usado: `feynman`
(explicar com as próprias palavras, como se ensinasse alguém) ou `4causas`
(enquadrar o conceito nas quatro causas — material, formal, eficiente, final).
É um campo **aberto**: um método de estudo novo entra como um valor de `frame`
novo, sem alterar o evento nem as projeções (OCP). Os andaimes operacionalizam
*elaborative interrogation* / *self-explanation*; o `text` guarda a explicação
já concatenada (uma linha `causa: resposta` por causa, no caso de `4causas`).

### Tags livres (1.3)

A **identidade da tag é o próprio nome normalizado**: não existe
`tag.created` nem `tag_id`, então dois dispositivos que escrevem `#SO` e `so`
convergem para a mesma tag sem regra de merge. Tags aparecem em
`card.created`/`card.edited` (`tags[]`) e em `deck.created` (`tags[]`, as
"transversais").

**Normalização** (aplicada pelas projeções a todo `tags[]` lido, e pelos
clientes na entrada):

1. apara as pontas e remove os `#` iniciais;
2. passa a minúsculas;
3. divide por `/` (hierarquia, estilo Obsidian: `so/escalonamento`); em cada
   segmento, sequências de espaço viram `-` e as pontas são aparadas;
   segmentos vazios são descartados;
4. tag que resulta vazia é descartada; a lista é deduplicada mantendo a
   primeira ocorrência.

Ex.: `["#SO ", "Prova P2", "/so// Threads /", "so"]` → `["so", "prova-p2",
"so/threads"]`. O `::` continua sendo exclusivo dos decks.

**Entrada livre nas superfícies**: o texto é separado por espaço ou vírgula e
cada pedaço é normalizado (`#SO #Prova-P2, redes` → `so`, `prova-p2`,
`redes`).

**Tags efetivas** de um cartão = as dele ∪ as do próprio deck (nessa ordem,
deduplicadas). **Hierarquia**: a tag `t` **inclui** `u` se `u == t` ou `u`
começa com `t + "/"` (`so` inclui `so/concorrencia`, não `sobre`).

### Conexões e renomeação de tag (1.4)

| Tipo | Payload |
|---|---|
| `link.created` (1.4) | `link_id, from: {kind, id}, to: {kind, id}, relation?` |
| `link.removed` (1.4) | `link_id` |
| `tag.renamed` (1.4) | `from, to` |

**Conexão** — nas interfaces, **sinapse** (⚡), desde o 1.6: cartões e páginas
são os neurônios, a sinapse é a ligação entre eles — é uma aresta explícita
entre dois **nós**, criada pelo usuário — o
`[[link]]` do Obsidian como evento: o texto do cartão não é varrido em busca
de links. Nó = `{kind, id}` com `kind ∈ card | deck | tag | page | task`
(`page` e `task` desde o 1.6); o `id` da tag é o nome normalizado (é por isso
que a tag não tem `tag_id`). Na saída das projeções o nó vira a chave
`"kind:id"` (`card:c1`, `deck:d1`, `tag:so/threads`, `page:p1`, `task:t1`).

- A aresta guarda a direção (`from → to`) porque `relation` pode ser
  direcional (`prerequisito`: *from* é pré-requisito de *to*); a vizinhança é
  não direcionada. `relation` é **aberto**, como o `frame` do
  `card.explained`; ausente = conexão simples.
- `link.created` é **ignorado** se algum nó for inválido: `kind` desconhecido
  (um tipo de nó novo entra como `kind` novo, e clientes antigos o ignoram),
  `id` vazio (inclusive tag que normaliza para vazia), cartão, deck, página
  ou tarefa inexistente naquela posição da ordem total (§3: referência a id
  inexistente); tag sempre existe. A página existe a partir de um
  `page.created` válido (§3, Páginas: com título; o primeiro vence); a
  tarefa, do `task.created` até o `task.deleted`. Laço (`from` = `to`)
  também é ignorado.
- `link.removed` é **tombstone** da conexão. Duas conexões do mesmo par
  (criadas em dispositivos diferentes) são arestas distintas — remover uma não
  remove a outra, então a superfície, ao "desligar" dois nós, remove todas as
  do par.
- Arquivar cartão, deck ou página não remove conexões (a história fica);
  quem exibe decide se mostra. Concluir tarefa também não. **Apagar tarefa**
  é tombstone (§3, Organização): a tarefa sai do estado e as conexões dela
  saem junto.
- **Tarefa (1.6)**: tarefa não tem descrição (§3, Organização) — o contexto
  dela é uma página ligada por sinapse, que a sessão de foco mostra e deixa
  editar.
- **Página (1.6)**: tags e conexões de página são metadado, criados por
  evento como os dos cartões — o corpo nunca é varrido. A página é o lugar
  onde o usuário **vê** o que está ligado a ela, não onde escreve as
  ligações.
- **Tag classifica; sinapse liga coisas (1.6).** "Ter a tag" (o campo
  `tags`) e "estar ligado à tag" (conexão com ponta `tag`) significavam quase
  o mesmo para quem usa. A partir do 1.6, as **superfícies não criam conexão
  com ponta `tag`**: ligar um cartão ou uma página a uma tag **classifica** o
  item (`card.edited`/`page.edited` com a tag acrescentada às próprias), e
  desligar tira a tag. Deck ↔ tag e tag ↔ tag são recusados — as tags do
  deck são as da criação, e assuntos se relacionam por páginas: o assunto que
  reúne coisas (ou que é "pré-requisito" de algo) é uma página, ligada por
  sinapses. As conexões com ponta `tag` já gravadas **continuam válidas** e
  as projeções as leem como sempre (o log é imutável; os vetores do 1.4
  seguem iguais).

**Renomear tag** (`tag.renamed`): sem `tag_id`, renomear é um evento próprio.
Vale **na posição do evento na ordem total**: toda ocorrência anterior de
`from` — e das descendentes, `from/x` → `to/x`, como no Obsidian — é reescrita
para `to` nas tags dos cartões, nas tags dos decks e nas pontas das conexões,
e as listas são deduplicadas (renomear para uma tag existente funde as duas).
Ocorrência **posterior** de `from` é uma tag nova com o nome antigo — o nome
fica livre para reúso. `from` e `to` são normalizados; vazios ou iguais, o
evento é ignorado. Conexão cujas duas pontas viram o mesmo nó deixa de valer.

Um dispositivo que ainda não viu a renomeação e grava `from` depois dela na
ordem total reintroduz o nome antigo: é a semântica honesta do livro-razão
(renomear de novo resolve), preferida a um apelido eterno que impediria
reusar o nome.

### Sessões salvas (1.5)

| Tipo | Payload |
|---|---|
| `study.saved` (1.5) | `study_id, name, decks?: [deck_id], tags?: [tag], around?: [nó], practice?: bool` |
| `study.deleted` (1.5) | `study_id` |

A sessão personalizada (§4.11) salva com um nome — como os *decks filtrados*
do Anki, mas guardando o **filtro**, não a lista de cartões: reaberta, a
sessão reúne o que casar naquele dia. `practice` pede o modo treino por
padrão.

- `study.saved` **cria ou substitui a sessão inteira**: salvar de novo com o
  mesmo `study_id` é a edição, e o último vence. Não há LWW por campo porque o
  filtro só faz sentido inteiro. `name` é aparado; vazio, o evento é ignorado.
- `study.deleted` é **tombstone**: a sessão sai do estado e eventos
  posteriores sobre o mesmo id são ignorados.
- As tags do filtro e os nós de tag do `around` **acompanham `tag.renamed`**,
  como as demais tags — senão a sessão "Véspera da P2" esvaziaria em silêncio
  ao renomear `#p2`.

### Páginas (1.6)

| Tipo | Payload |
|---|---|
| `page.created` (1.6) | `page_id, title, body, tags[]` |
| `page.edited` (1.6) | `page_id, title?, body?, tags[]?, pinned?, base?` |
| `page.archived` (1.6) | `page_id` |

**Página** é texto longo de consulta e de uso constante — a anotação que
fica, ao contrário da `note.captured`, que é passagem para a triagem, e do
cartão, que é estudo. O corpo é markdown e **nenhuma projeção o interpreta**
(não procura links, tarefas nem cartões no texto): ler o markdown é papel da
superfície.

- `title` é aparado. `page.created` com título vazio é ignorado; no
  `page.edited`, título vazio deixa o anterior. `body` é guardado como veio
  (ausente no `page.created` ≡ `""`).
- `page.created` para um `page_id` que já existe é ignorado — vale o
  primeiro na ordem total.
- **Edição é LWW por campo** (§3), como no `card.edited`. `tags[]` substitui
  a lista inteira, com a normalização das tags livres, e acompanha
  `tag.renamed` como as tags dos cartões.
- `pinned` fixa a página no topo da lista — o acesso de uso constante.
  Ausente = sem mudança. Fixar não conta como edição (§4.15, `updated_at`).
- **`base`**: a edição que traz `body` diz em qual versão do corpo se
  baseou — o `id` do evento (`page.created`, ou `page.edited` com `body`) que
  o cliente leu antes de editar. No 1.6 o corpo ainda é LWW (vence a última
  edição na ordem total) e nenhuma projeção lê `base`; o campo é gravado
  desde já para que a detecção de conflito — duas edições com a mesma
  `base` — valha também para o histórico. Ausente = base desconhecida.
- `page.archived` tira a página da lista preservando o texto
  (`archived: true`), como o cartão; edições posteriores continuam valendo.

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
  imediatamente**). Sem deck (v2), `deck_id` é `null` na saída.
- `tags` saem normalizadas (§3, Tags livres) — as do próprio cartão; as
  efetivas (com as do deck) são calculadas por quem precisa (§4.10, §4.11).
- `card.reviewed`:
  - `correct` → caixa `min(caixa + 1, 5)`
  - `wrong` → caixa `max(caixa − 1, 1)` (variante atenuada: regride UMA caixa)
  - Em ambos: `due` = data local da revisão + `intervalo[nova caixa]`.
- Revisão de cartão inexistente é ignorada.
- `card.practiced` é ignorado (treino não altera caixa nem `due`).
- `card.edited`: sobrescreve apenas os campos presentes (`front`, `back`,
  `tags`), last-writer-wins por campo; **não altera caixa nem `due`**.
- `tag.renamed` reescreve as tags dos cartões já criados (§3, Conexões e
  renomeação de tag).
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

- Cada `deck.created` projeta nome e tags (transversais, normalizadas);
  `tag.renamed` as reescreve como nos cartões.
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
- `importance` (quando presente) é repassado em cada compromisso.

Saída: `{ appointments: { <appointment_id>: { title, starts_at, ends_at,
importance? } }, upcoming: [appointment_id…] }`.

### 4.9 Lembretes (`projection: "reminders"`)

Projeção pura dos lembretes de compromisso **ainda por vir**, a partir da agenda
e de `now`. Para cada compromisso, os lembretes caem em minutos antes do início:
compromisso comum em `[1440, 60, 10, 0]` (1 dia, 1 hora, 10 min, na hora);
`importance: "important"` ganha antes disso a cascata semanal
`[80640, 70560, 60480, 50400, 40320, 30240, 20160, 10080]` (8→1 semanas). Entra em
`pending` o lembrete cujo instante (`starts_at − offset`) seja `>= now`; a lista
é ordenada por `(instante ↑, appointment_id ↑, offset ↑)`. O rótulo concorda em
número e gênero ("Falta 1 semana", "Faltam 10 minutos", "Começa agora").

Não modela o que já foi notificado: **dedupe e entrega são estado de runtime de
cada cliente** — a extensão dispara notificação do navegador em segundo plano
(`chrome.alarms`), a CLI, que não roda com o terminal fechado, exibe o próximo
importante como marca d'água ao rodar `pnn`. A regra de *quando* lembrar é a
mesma nos dois e travada por este vetor.

Saída: `{ pending: [ { appointment_id, offset_minutes, label } … ] }`.

### 4.10 Tags (`projection: "tags"`)

- Universo: cartões **ativos** — não arquivados e de deck não arquivado
  (cartão sem deck nunca está em deck arquivado) — e páginas não arquivadas
  (1.6).
- Uma tag aparece se estiver entre as tags efetivas de algum cartão ativo,
  nas tags de algum deck não arquivado (zerada, se nenhum cartão a tiver) ou
  nas tags de alguma página não arquivada.
- `cards` = cartões ativos com a tag **exata** entre as efetivas; `due` =
  quantos desses estão na fila Leitner (§4.1); `pages` (1.6) = páginas não
  arquivadas com a tag exata.
- `names` = as tags em ordem canônica (comparação por unidade de código, não
  dependente de locale). A soma pela hierarquia (`so` reunindo `so/…`) é feita
  só na exibição, como a dos decks — e sem contar duas vezes o cartão que tem
  as duas tags.

Saída: `{ tags: { <tag>: { cards, due, pages } }, names: [tag…] }`.

### 4.11 Sessão personalizada (`projection: "study"`)

Recebe, além de `now`/`tz`, o filtro `decks?: [deck_id]`, `tags?: [tag]` e
`around?: [nó]` (1.4, nós como `"kind:id"`).

- **Deck**: o cartão casa se o seu `deck_id` estiver na **subárvore `::`** de
  algum deck listado (o próprio ou nome que começa com `nome + "::"`). Deck
  inexistente não casa nada; cartão sem deck nunca casa um filtro de deck.
- **Tag**: o cartão casa se alguma tag efetiva for **incluída** por alguma
  tag do filtro (§3: própria ou descendente `/`). As tags do filtro são
  normalizadas (`#SO` ≡ `so`).
- **Ao redor** (1.4): a vizinhança de um nó X é X mais os vizinhos (§4.12) de
  todo nó que X **inclui** — o próprio cartão; a própria tag e as descendentes
  `/`; o próprio deck e a subárvore `::`. O cartão casa se algum nó da
  vizinhança o incluir (cartão: ele mesmo; tag: alguma tag efetiva; deck: o
  deck do cartão na subárvore). Um salto só — vizinho de vizinho não entra.
  O `id` de tag é normalizado; nó inválido não casa nada. **Página (1.6)**: a
  vizinhança dela **mais a vizinhança de cada tag dela** (como se o filtro
  trouxesse também `tag:<t>` para cada tag da página) — a tag é como a página
  diz sobre o que é, então estudar ao redor da página reúne os cartões
  ligados a ela e os dos assuntos dela.
- **Combinação**: OU dentro de cada lista, **E entre** as listas; lista vazia
  (ou ausente) não restringe.
- `queue` = a fila Leitner (§4.1) filtrada, **na mesma ordem**.
- `practice` = cartões ativos que casam e **não** estão vencidos, ordenados por
  `(caixa ↑, due ↑, ts de criação ↑, card_id ↑)` — o modo **treino** os
  apresenta depois da fila, e a resposta vira `card.practiced`.

Saída: `{ queue: [card_id…], practice: [card_id…] }`.

### 4.12 Conexões (`projection: "links"`)

- `links` = as conexões válidas e não removidas (§3, Conexões), com as pontas
  já renomeadas, escritas como `"kind:id"`, e `relation` (`null` quando
  ausente). Conexão que virou laço por renomeação não aparece.
- `neighbors` = para cada nó com ao menos uma conexão, os nós do outro lado —
  **sem direção**, sem repetição, em ordem canônica (unidade de código).
- `nodes` = os nós com conexão, em ordem canônica.

Saída: `{ links: { <link_id>: { from, to, relation } }, neighbors: { <nó>:
[nó…] }, nodes: [nó…] }`.

### 4.13 Sessões salvas (`projection: "studies"`)

- `studies` = as sessões salvas e não apagadas, cada uma com o último
  `study.saved`: `decks` (aparados, sem vazios nem repetição), `tags`
  normalizadas, `around` com as chaves de nó normalizadas (§4.12; chave
  inválida fica como veio — e não casa nada, §4.11), ambos já renomeados, e
  `practice` (`false` quando ausente).
- `order` = os ids por `(name ↑, study_id ↑)`, em unidade de código.

Saída: `{ studies: { <study_id>: { name, decks, tags, around, practice } },
order: [study_id…] }`.

### 4.14 Grafo (`projection: "graph"`)

O acervo como grafo, estilo Obsidian, para as superfícies desenharem (a
extensão em SVG, a CLI em DOT). Recebe `now`/`tz` (marca os vencidos).

- **Nós** `{ id, kind, label, due }`, com `id` = chave `"kind:id"`:
  - cartões **ativos** (§4.10), `label` = frente, `due` = está na fila
    Leitner (§4.1);
  - decks não arquivados, `label` = nome;
  - tags: as próprias dos cartões ativos, as dos decks não arquivados e as
    pontas de conexão (§4.12), **mais os ancestrais implícitos** (`so` de
    `so/threads`); `label` = nome.
  - páginas e tarefas (1.6) **ainda não são nós**: conexão com elas fica
    fora do grafo, inclusive a tag que só aparece como a outra ponta dela
    (que ficaria solta no desenho).
  - `due` é `false` fora dos cartões.
- **Arestas** `{ from, to, kind, relation }`, só entre nós do grafo:
  - `link` — cada conexão (§4.12), na direção gravada, com a `relation`;
  - `tag` — cartão → cada tag **própria**; deck → cada tag transversal (a
    herança chega ao cartão pelo deck, sem aresta repetida);
  - `deck` — cartão → o seu deck;
  - `parent` — tag → a tag pai (`so/threads` → `so`); deck → o ancestral
    `::` mais próximo que exista e não esteja arquivado (entre homônimos, o de
    menor id; nomes comparados com os segmentos aparados, §4.5).
  - `relation` é `null` fora de `link`; aresta idêntica aparece uma vez.
- Ordem canônica (unidade de código): nós por `id`; arestas por `(from, to,
  kind, relation)`, com `null` antes.

Saída: `{ nodes: [ { id, kind, label, due } … ], edges: [ { from, to, kind,
relation } … ] }`.

### 4.15 Páginas (`projection: "pages"`)

- `pages` = toda página criada (§3, Páginas), **inclusive arquivada**, com:
  - `title`, `body`, `tags` (normalizadas e renomeadas), `pinned` (`false`
    se nunca fixada) e `archived`;
  - `version` = o `id` do último evento que gravou o corpo (`page.created`
    ou `page.edited` com `body`) — é o que o cliente envia como `base`;
  - `created_at` = `ts` do `page.created`;
  - `updated_at` = `ts` do último evento, na ordem total, que trouxe título
    (não vazio), corpo ou tags. Fixar e arquivar não mudam `updated_at`.
- `order` = as páginas **não arquivadas**: fixadas primeiro; dentro de cada
  grupo, `updated_at` decrescente; empate por `page_id` crescente (unidade de
  código).

Saída: `{ pages: { <page_id>: { title, body, tags, pinned, archived,
version, created_at, updated_at } }, order: [page_id…] }`.

## 5. Sincronização

O desenho original previa um servidor com uma única operação `POST /sync`
(sequência global, dedup por `id`, sem ler `payload`). **Implementado como
bucket S3** (2026-07-27): mesma semântica, sem servidor para escrever e
manter — o bucket continua sem ler `payload` e sem regra de negócio nenhuma.

Layout:

```
<prefixo>/<device>.jsonl.zst   — um objeto por dispositivo, JSONL comprimido com zstd
<prefixo>/<device>.jsonl       — o mesmo, sem compressão (clientes que não comprimem; objetos de antes)
```

**Compressão (2026-11).** O log guarda o **texto inteiro** de cada versão de
página (§3, Páginas), e cada sync reenvia o objeto inteiro do dispositivo. Em
vez de gravar só a diferença entre versões — o que mudaria o modelo de
eventos, exigiria reaplicar cadeias de patches e tornaria cada versão
dependente das anteriores —, o objeto sobe comprimido com **zstd** (RFC 8878),
cuja janela de megabytes acha a versão anterior da mesma página: é o que o git
faz, gravando o arquivo inteiro e deixando a compressão (os *packfiles*)
achar as repetições. Medido num log simulado de 12.150 eventos (150 páginas ×
20 edições): texto inteiro, 18 MB brutos → 0,77 MB com zstd; só as diferenças,
4,2 MB → 0,72 MB. O gzip, com janela de 32 KB, fica em 6,1 MB.

- **Quem lê reconhece o formato pelo conteúdo** — o número mágico do zstd
  (`28 B5 2F FD`) —, não pelo nome; ambos os sufixos valem. Comprimir é
  opcional para quem escreve (a extensão sobe `.jsonl`; a CLI, `.jsonl.zst`);
  ler os dois é obrigatório.
- **Migração sem coordenação**: um dispositivo que passa a comprimir sobe o
  `.jsonl.zst` no primeiro sync, mesmo sem evento novo; o `.jsonl` antigo fica
  congelado (a política abaixo não permite apagar) e quem baixa os dois não
  duplica nada — o dedup por `id` absorve, como em qualquer reenvio.
- O objeto vai com `Content-Type: application/zstd` e **sem**
  `Content-Encoding`, para nenhum cliente HTTP descomprimir por conta própria.
- Frame zstd ilegível ou truncado não derruba o sync, como a linha JSONL
  corrompida: o que se consegue ler entra, o resto é pulado.
- A compressão é só do transporte: o log local continua JSONL puro (legível
  com `grep`/`jq`), e o modelo de eventos e os vetores não mudam.

**Cada dispositivo grava somente o próprio objeto.** É essa regra que dispensa
servidor: como dois clientes nunca escrevem a mesma chave, não existe escrita
perdida — o risco clássico de "subir o arquivo inteiro", em que o último a
gravar apaga em silêncio o que o outro emitiu.

Um ciclo de sync é:

1. listar o prefixo e baixar os objetos cujo `ETag` mudou;
2. unir ao log local (dedup por `id` + ordem total do §2);
3. subir os eventos cujo `device` é o próprio.

Nada disso precisa de lock, escrita condicional ou retry: evento é fato
imutável com `id` único, então a união de dois logs é o log completo, e a
operação é idempotente — rodar de qualquer máquina, em qualquer ordem, quantas
vezes quiser, converge para o mesmo estado.

`cursor` deixa de ser posição numa sequência global e passa a guardar os
`ETag` já baixados (e quantos eventos próprios já subiram). É **só otimização**:
perdido ou errado, o pior que acontece é rebaixar o que já se tem, e o dedup
absorve. Com o log local vazio ele é descartado de propósito, senão uma réplica
recém-restaurada acharia que está em dia e ficaria vazia para sempre.

### Credenciais

Em ambos os clientes a chave é **provisionada de fora** e nunca versionada — o
repositório é público, e chave AWS em repositório público é encontrada por
varredura automática.

- **CLI**: cadeia padrão da AWS (`AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`,
  `~/.aws/credentials`, `AWS_PROFILE`), inclusive credencial temporária do STS
  com `AWS_SESSION_TOKEN`. Configuração por `PNN_S3_BUCKET`, `PNN_S3_PREFIX`,
  `PNN_S3_ENDPOINT` (opcional, para R2/MinIO/Backblaze) e `PNN_S3_REGION`.
  Sem `PNN_S3_REGION`, a **região é resolvida pela cadeia da AWS** (`AWS_REGION`,
  `AWS_DEFAULT_REGION`, região do perfil) — fixar um padrão atropelaria o perfil
  e assinaria na região errada, falha que só aparece como `SignatureDoesNotMatch`.
- **Extensão**: cadeia equivalente — `chrome.storage.managed` (política do
  navegador, o provisionamento automático) e `chrome.storage.local`, que
  sobrepõe, como o ambiente sobrepõe o perfil na CLI. Esquema em
  `extension/schema.json`. Nenhum segredo é gravado em `~/.pnn` nem no código.

A extensão assina as requisições com **SigV4 implementado sobre Web Crypto**
(`extension/storage/sigv4.js`): sem bundler, trazer o SDK custaria um passo de
build e centenas de kB para três operações. A conformidade é travada por
vetores gerados pelo signer oficial do SDK Go (`test/sigv4_vectors.json`) —
mesma assinatura, byte a byte. Só `host` e `x-amz-*` são assinados;
`content-length` fica de fora porque o `fetch` do navegador proíbe defini-lo,
que é o que o SDK oficial de JavaScript também faz no navegador.

O `ListObjectsV2` é lido sem `DOMParser`: o service worker do Manifest V3 não
tem DOM.

### Política de acesso recomendada

`s3:GetObject`, `s3:PutObject` e `s3:ListBucket` restritos ao prefixo, **sem
`s3:DeleteObject`**, e versionamento ligado no bucket. Como o `device` nasce em
tempo de execução, a política não tem como se restringir a um único objeto —
então o que limita o estrago de uma chave vazada é não poder apagar, e o
versionamento tornar recuperável até a sobrescrita.

## 6. Formato dos vetores de conformidade

```json
{
  "name": "leitner_erro_regride_uma_caixa",
  "projection": "leitner",
  "now": "2026-07-10T12:00:00Z",
  "tz": "America/Sao_Paulo",
  "permutations": false,
  "params": { "decks": [], "tags": [], "around": [] },
  "events": [ … envelopes completos … ],
  "expected": { … asserção parcial sobre a saída da projeção … }
}
```

- `expected` é comparado por **inclusão parcial**: todo campo presente em
  `expected` deve ter valor idêntico na saída; campos ausentes são livres.
  Arrays são comparados por igualdade exata.
- `"permutations": true` → a suíte reexecuta com a lista de eventos
  embaralhada (≥ 5 permutações determinísticas) e exige o mesmo resultado.
- `params` (opcional) carrega parâmetros específicos da projeção além de
  `now`/`tz` — hoje, o filtro da sessão personalizada (§4.11), inclusive o
  `around` das conexões.
