# `pnn` — a CLI do Procrastina Não, de ponta a ponta

Documento de consulta. Explica **como a CLI funciona por dentro**: da linha que
você digita ao arquivo que ela grava, e de volta à tela. É o mesmo padrão para
todo comando, então serve de mapa geral.

Contratos de referência: [`spec/SPEC.md`](../spec/SPEC.md) (modelo de eventos) e
[`spec/CLI.md`](../spec/CLI.md) (design da experiência). Este README descreve a
**implementação**; quando os dois divergirem, a spec é a fonte de verdade.

---

## 1. A ideia central: nada é "salvo", tudo é "registrado"

A CLI **não guarda tarefas, cartões ou compromissos**. Ela guarda um **log de
eventos** append-only — uma lista de fatos que aconteceram, em ordem, que nunca
se altera. O que você vê na tela (`pnn dia`, `pnn decks`) é **calculado na hora**
a partir desse log por funções puras chamadas *projeções*.

```
        você digita            grava um fato              recalcula do zero
   pnn t "Ler cap. 3"  ──►  task.created no log  ──►  projeção tasks  ──►  tela
```

Consequências práticas dessa escolha (event sourcing):

- **Editar não apaga.** Corrigir o título de uma tarefa grava um `task.edited`;
  o `task.created` original continua lá. Como no livro-razão de um contador: a
  correção é um lançamento novo, nunca uma rasura.
- **A história fica.** O log conta a estória: `criada → priorizada A → editada →
  concluída`. Nada se perde.
- **Duas telas, um cálculo.** A CLI e a extensão leem o mesmo tipo de log com as
  mesmas projeções — por isso convergem.

---

## 2. Anatomia de um evento (o "envelope")

Cada linha do log é um JSON com esta forma fixa (`spec/SPEC.md` §1,
implementada em [`core/event.go`](core/event.go)):

```json
{
  "id":     "0197c9a4-…",               // UUIDv7, único global
  "type":   "task.created",             // namespace.ação
  "v":      1,                          // versão do formato deste tipo
  "lc":     42,                         // relógio de Lamport do dispositivo
  "ts":     "2026-07-08T12:00:00.000Z", // relógio de parede, sempre UTC
  "device": "dev-3f2a…",                // id estável da instalação
  "payload": { "task_id": "t1", "title": "Ler cap. 3" }  // campos do tipo
}
```

- **`id`** — UUIDv7 ([`store/id.go`](store/id.go)): 48 bits de tempo + aleatório.
  Cresce no tempo, o que ajuda ordenação e depuração.
- **`lc`** (Lamport) — contador lógico do dispositivo; ordena eventos entre
  máquinas mesmo sem relógios sincronizados. Ao emitir, `lc = max(lc do log) + 1`.
- **`payload`** fica **opaco** para o log (`json.RawMessage`): cada projeção
  decodifica só os campos dos tipos que lhe interessam. É o que permite
  adicionar tipos novos sem quebrar quem já existe.

---

## 3. Camadas do código

```
cmd/pnn/main.go        entry point; decide pnn vs. procrastina por argv[0]
   │
internal/cli/*.go      um arquivo por comando (Cobra): valida args, monta payload
   │
internal/app/          contexto de execução: réplica, fuso, relógio, cor, saída
   │
store/                 o log local em ~/.pnn: Append, Events, Merge, lock
   │
core/                  projeções puras (tasks, leitner, decks…) + envelope
```

Uma **regra de ouro** atravessa tudo: `core/` é o mesmo contrato do cliente JS
e passa o **corpus de vetores** em `spec/vectors/` (`go test ./core/`). Toda
mudança de comportamento nasce como vetor antes de virar código.

### `store/` — a réplica local (`~/.pnn/`)

[`store/store.go`](store/store.go) é a fonte de verdade da CLI. O diretório
(configurável por `PNN_HOME`) contém:

| Arquivo | Papel |
|---|---|
| `events.jsonl` | o log: um envelope JSON por linha, append-only |
| `device` | id estável da instalação (`dev-…`), criado na 1ª escrita |
| `cursor` | posição confirmada do sync (ainda não usada — ver §7) |
| `lock` | `flock` que serializa escritores |

O `lock` existe por um motivo concreto: você pode capturar de outro terminal
(`pnn n "ideia"`) **durante uma sessão de foco** rodando no primeiro. Os dois
processos escrevem o mesmo arquivo; o `flock` serializa.

`Append` (o coração da escrita) faz, sob lock: lê o log → calcula
`lc = max(lc) + 1` → carimba `id`, `ts` (UTC), `device` → acrescenta a linha.
Nunca reescreve o passado. (A única reescrita é `Merge`, do sync, que
deduplica por `id` e regrava o log normalizado — atômico via arquivo temporário
+ `rename`.)

### `core/` — as projeções

Uma projeção é uma função pura `f(eventos, parâmetros) → estado`. Antes de
projetar, todo evento passa por `core.Normalize`
([`core/envelope.go`](core/envelope.go)):

1. **Ordena** por `(lc, ts, device, id)` — a *ordem total* determinística.
2. **Deduplica** por `id` (mantém a primeira ocorrência).

Por isso qualquer permutação do mesmo conjunto de eventos dá o mesmo estado
(**convergência**) e reprocessar não muda nada (**idempotência**) — as duas
propriedades que os vetores com `"permutations": true` verificam.

Projeções e o que entregam:

| Projeção | Lê os eventos… | Entrega |
|---|---|---|
| `tasks` | task.* | lista do dia A→B→C, com árvore de subtarefas |
| `leitner` | card.* (exceto card.practiced), deck.archived, tag.renamed | cartões, caixa 1–5 e fila de vencidos |
| `decks` | deck.*, tag.renamed | decks com nome, tags e flag de arquivado |
| `tags` | card.*, deck.*, tag.renamed | tags (normalizadas, hierarquia `/`) com cartões e vencidos |
| `study` | card.*, deck.*, link.*, page.* + filtro `{decks, tags, around}` | sessão personalizada: fila filtrada (inclusive pela vizinhança de sinapses; ao redor de página, também as tags dela) + não vencidos para treino |
| `links` | link.*, tag.renamed, card.created, deck.created, page.created | sinapses (conexões explícitas), vizinhança não direcionada e nós ligados |
| `studies` | study.*, tag.renamed | sessões salvas (o filtro com um nome) |
| `graph` | card.*, deck.*, link.*, tag.renamed | o acervo como grafo: nós (cartões ativos, decks, tags) e arestas (link, tag, deck, parent) |
| `calendar` | appointment.* | compromissos e próximos (`upcoming`) |
| `inbox` | note.*, distraction.* | pendências de triagem |
| `stats` | card.reviewed, session.ended | streak, dias estudados, revisões de hoje |
| `attention` | session.* | janela de atenção (mediana das sessões) |
| `review` | session.*, pause.*, card.reviewed, appointment.created | retrospectiva da semana que contém `now` |

Como projeções não leem o relógio do sistema, quem depende de "hoje" recebe
`now` (instante UTC) e `tz` (fuso IANA) por parâmetro — em produção montados por
`app.Params()`; nos testes, fixos.

---

## 4. O ciclo de um comando, passo a passo

Exemplo real: `pnn t "Revisar TCC" -p A`.

1. **`main.go`** monta o `App` (abre `~/.pnn`, detecta fuso, decide cor) e
   entrega os args ao Cobra.
2. **`internal/cli/captura.go`** (`newTCmd`) valida: título obrigatório,
   prioridade em {A,B,C}, data em AAAA-MM-DD. Monta o payload com um `task_id`
   novo (`store.UUIDv7()`).
3. **`Store.Append("task.created", payload)`** grava o envelope no log.
4. **Prioridade é um segundo evento**: `pnn t … -p A` emite **dois** eventos —
   `task.created` e depois `task.prioritized`. Prioridade não é campo de
   criação; é uma transição de estado, igual a repriorizar depois.
5. O comando imprime a confirmação e devolve o prompt (captura é uma linha,
   nunca uma tela).

Quando você roda `pnn dia`, a projeção `tasks` relê o log inteiro, aplica os
dois eventos e mostra a tarefa como `[A] Revisar TCC`. O estado exibido nunca
foi "salvo" — foi recomputado.

### Números efêmeros (por que `pnn feito 2` funciona)

Você nunca digita UUID. Toda tela que lista itens grava um mapa
número→id em `~/.pnn/last-view.json` ([`internal/app/view.go`](internal/app/view.go)).
Assim `pnn feito 2`, `pnn foco 1`, `pnn editar 3` resolvem o número **da última
tela** para o id real. Estilo Taskwarrior. Se o número não existir mais, o
comando manda você rodar `pnn dia` de novo.

Cada tela salva o `kind` do item (`task`, `deck`, `card`, `appointment`) — é o
que permite os verbos genéricos `editar`/`apagar`/`arquivar` saberem qual
evento emitir (§6).

---

## 5. Referência de comandos

Todo comando de leitura aceita `--json` (scriptável e testável).

### Organização

| Comando | Evento(s) | Notas |
|---|---|---|
| `pnn` ou `pnn dia` | — | a tela "e agora?": agenda + lista A/B/C (com `⚡N` nas tarefas ligadas a páginas) + vencidos |
| `pnn t "título" [-p A\|B\|C] [--data AAAA-MM-DD] [--sub N]` | `task.created` (+`task.prioritized`) | `--sub N` = subtarefa do item N |
| `pnn feito N` | `task.completed` | |
| `pnn pri N A\|B\|C` | `task.prioritized` | última prioridade vence |
| `pnn c "título" HH:MM-HH:MM [--dia AAAA-MM-DD]` | `appointment.created` | hora local → UTC no envelope |
| `pnn agenda` | — | próximos compromissos, numerados |

### Captura e triagem

| Comando | Evento(s) | Notas |
|---|---|---|
| `pnn n "texto"` | `note.captured` | vai para a caixa de entrada |
| `pnn caixa` | — | lista pendências de triagem |
| `pnn triagem` | `note.triaged`/`distraction.triaged` (+`card.created`/`task.created`) | TUI, um item por vez |

### Páginas (catálogo 1.6)

Anotações longas e de uso constante. O `$EDITOR` abre **só o corpo**; título,
tags e sinapses são metadado e mudam por comando (`pnn editar N "título"`,
`--tag`, `pnn ligar`), como nos cartões — a página os mostra como referência
numerada. Detalhes em [`spec/CLI.md`](../spec/CLI.md) §3, "Páginas".

| Comando | Evento(s) | Notas |
|---|---|---|
| `pnn p "título" [--tag T]…` | `page.created` | abre o editor no corpo (markdown puro) |
| `pnn paginas [--tag T]… [--todas] [--por-tag] [--arquivadas] [--json]` | — | as fixadas e as 15 editadas por último (rodapé com o resto); `--por-tag` agrupa na árvore de tags (5 por tag); `--arquivadas` só as arquivadas; numerada, com `⚡N` |
| `pnn pagina N\|título [-e] [--cru]` | — (`-e`: `page.edited`) | lê formatada (glamour) e paginada, com as sinapses numeradas (a página é o 1); acha pelo título sem caixa nem acento |
| `pnn buscar "texto"` | — | páginas (título, tags, texto) e cartões, com o trecho que casa |
| `pnn fixar N` | `page.edited` (`pinned`) | alterna fixar/desafixar |
| `pnn escrever N\|título` | `page.edited` (+ os eventos dos comandos `/`) | o editor do pnn fora do foco: `/tarefa`, `/tarefas`, `/feito`, `/cartao`, `/tag`, `/ligar`; `Ctrl+Z`/`Ctrl+Y` desfaz/refaz; `Esc` grava e sai |

### Estudo

| Comando | Evento(s) | Notas |
|---|---|---|
| `pnn decks [--arquivados]` | — | árvore `::` com vencidos por deck, numerada |
| `pnn deck "Pai::Filho" [--tag t]` | `deck.created` | hierarquia por convenção de nome |
| `pnn carta [deck] [--tag T]… [--sem-deck]` | `card.created` (v2 sem deck) | TUI em série: frente → verso → fonte → tags; "(sem deck)" cria cartão solto |
| `pnn cartas [deck] [--tag T]… [--arquivados]` | — | manutenção do acervo, numerada, com as tags |
| `pnn tags [--json]` | — | tags em árvore `/`, numerada, com cartões, páginas, vencidos e sinapses |
| `pnn ligar A B [C…] [--relacao R]` | `link.created` (sinapse) ou `card.edited`/`page.edited` (com `'#tag'`: classifica) | sinapse entre coisas (cartão, página, tarefa, deck); nó = N da última tela, `'#tag'`, nome de deck ou título de página. Tag não é ponta de sinapse; tarefa não tem tags |
| `pnn desligar A B [C…]` | `link.removed` (ou tira a tag) | desfaz todas as sinapses do par; com `'#tag'`, tira a tag do item |
| `pnn sinapses [nó] [--json]` | — | alias `conexoes`; sem nó, o que tem sinapses; com nó, a vizinhança numerada (o nó é o 1) |
| `pnn grafo [nó] [--raio R] [--sem-cartoes] [--json]` | — | o acervo em DOT: `pnn grafo \| dot -Tsvg > grafo.svg`; com nó, o grafo local |
| `pnn sessao "Nome" [deck] [--tag T]… [--ao-redor N]… [--treino]` | `study.saved` | salva o filtro de revisão com um nome (o mesmo nome atualiza) |
| `pnn sessoes [--json]` | — | sessões salvas, numeradas, com vencidos e treino de hoje |
| `pnn revisar [deck] [--tag T]… [--ao-redor N]… [--treino] [--sessao N] [-m MIN]` | `session.started/ended`, `card.explained`, `card.reviewed`, `card.practiced` | TUI vermelha, fila frágil→consolidada; `--treino` inclui os não vencidos sem mexer nas caixas; `--ao-redor` estuda a vizinhança de um nó; conexões aparecem depois do verso |

### Sessão

| Comando | Evento(s) | Notas |
|---|---|---|
| `pnn foco [N] [-m MIN] [--pausa MIN] [--checkin MIN] [--relogio]` | `session.started/ended` (+`distraction.captured`, `checkin.logged`, `pause.*`) | `-m` padrão = janela de atenção; Esc pergunta o motivo (opcional); `--checkin N` pergunta "na tarefa? [s/n]" a cada N min (sino); `Ctrl+P` = break de 5/10/15/20/25 min que encerra a sessão (`reason: "pausa"`); com a tarefa ligada a página (⚡), abre no **modo de escrita**: a página num editor na própria tela, relógio na barra, `/` abre comandos (`/pagina`, `/tarefa`, `/tarefas`, `/feito`, `/cartao`, `/tag`, `/ligar`, `/distracao`), `Ctrl+S` grava, `Ctrl+Z` desfaz; sem página, o `/` da linha de distração abre os mesmos comandos para a tarefa (`/pagina` cria e começa a escrever; `/tarefa` cria subtarefa) e `Ctrl+O` já sugere `/pagina`; `--relogio` abre no relógio grande (`Ctrl+O` escreve); `Ctrl+E` leva a página ao `$EDITOR` |
| `pnn pausa [MIN]` | `pause.started` | pausa avulsa com duração definida (Safren) |
| `pnn volta` | `pause.ended` | encerra a pausa aberta |
| `pnn pausa --das HH:MM --ate HH:MM` | `pause.logged` | retroativa: tempo do domínio no payload |
| `pnn semana [N] [--de D]` | — | retrospectiva de qualquer semana (projeção `review`) |
| `pnn sync` | — | troca eventos com o bucket S3 (sobe o seu objeto em `.jsonl.zst`, baixa os dos outros); ver §7 |

### Edição e arquivamento (catálogo v1.1)

Três verbos genéricos operam sobre o item **N da última tela**; o `kind` salvo
decide o evento ([`internal/cli/editar.go`](internal/cli/editar.go)):

| Comando | task | deck | card | appointment | tag | study | page |
|---|---|---|---|---|---|---|---|
| `pnn editar N ["texto"] [--data D] [--frente F] [--verso V] [--tag T] [--untag T] [--tags "…"]` | `task.edited` | `deck.renamed` | `card.edited` | — | `tag.renamed` | `study.saved` | `page.edited` |
| `pnn apagar N` | `task.deleted` | ✗ arquive | ✗ arquive | `appointment.cancelled` | ✗ | `study.deleted` | ✗ arquive |
| `pnn arquivar N` | ✗ feito/apagar | `deck.archived` | `card.archived` | ✗ apagar | ✗ | ✗ | `page.archived` |

Distinção deliberada: **apagar é tombstone** (tarefa e compromisso descartados
não têm valor histórico — somem do estado); **arquivar preserva** (o histórico
de estudo de cartões e decks fica; eles só saem da fila). Edição é
**last-writer-wins por campo**: só os campos informados mudam.

### O alias `procrastina`

O mesmo binário, invocado por `argv[0]` ([`cmd/pnn/main.go`](cmd/pnn/main.go)).
No impulso de desistir: mostra o mascote, o streak e **o menor próximo passo**
(a menor subtarefa A ativa) com o comando de foco pronto. Aceita os typos
`procastina`/`procrastinar` — a intervenção não pode falhar por erro de
digitação no pior momento.

---

## 6. Modos por cor (as sessões)

`pnn foco` e `pnn revisar` tomam o terminal inteiro (Bubble Tea, tela
alternativa) e mudam a cor de tudo — o modo é informação ambiente, percebida
sem leitura:

```
AZUL   organizar/planejar (padrão): dia, captura, triagem, decks
VERMELHO  sessão em andamento: timer grande + linha de distração, nada mais
VERDE   pausa após concluir o bloco: resumo da sessão (não gera evento)
```

Cor **nunca é o único sinal** (daltonismo): cada modo tem símbolo, título e
layout próprios, respeita `NO_COLOR` e desliga quando a saída não é um terminal
([`internal/app/app.go`](internal/app/app.go), `colorEnabled`).

Durante o foco, a linha de input fica sempre focada: qualquer texto + Enter
grava `distraction.captured` e limpa — o *adiamento de distrações* do Safren.

Na revisão, antes de revelar o verso vem o **andaime de explicação**: `Tab`
alterna entre **Feynman** (campo único, "explique com suas palavras") e as
**4 causas** (wizard de 4 passos: material, formal, eficiente, final). Ambos
gravam `card.explained` — Feynman em v1; as 4 causas em v2 com `frame:
"4causas"` e o texto concatenado. O `frame` é um campo aberto: um método de
estudo novo entra como um valor novo, sem tocar na projeção Leitner (OCP).

---

## 7. Sincronização

`pnn sync` troca eventos com um bucket S3 ([`internal/sync/`](internal/sync/),
SPEC §5): baixa os objetos dos outros dispositivos que mudaram, funde no log
local (dedup por `id` + ordem total) e sobe o próprio. Cada dispositivo grava
só a sua chave, `<prefixo>/<device>.jsonl.zst` — por isso não há escrita
perdida nem conflito a resolver no transporte.

O objeto sobe **comprimido com zstd**: o log guarda o texto inteiro de cada
versão de página, e a compressão acha as repetições (como os packfiles do
git) — num log simulado com 150 páginas editadas 20 vezes, 18 MB viram 0,77 MB.
Quem lê reconhece o formato pelo conteúdo, então os `.jsonl` sem compressão
(os da extensão e os de antes) seguem valendo; o `.jsonl` antigo de quem
passou a comprimir fica congelado no bucket e o dedup absorve a repetição. O
log local continua JSONL puro.

Configuração: `PNN_S3_BUCKET` (e `PNN_S3_PREFIX`, `PNN_S3_ENDPOINT`,
`PNN_S3_REGION`); as credenciais vêm da cadeia padrão da AWS, nunca de `~/.pnn`.

---

## 8. Build, instalação e testes

```bash
make            # compila e instala pnn + alias procrastina em ~/.local/bin
make test       # go test ./...  (inclui o corpus de vetores de conformidade)
make build      # só compila em ./pnn, sem instalar
make uninstall  # remove do PATH
```

Variáveis de ambiente úteis:

| Var | Efeito |
|---|---|
| `PNN_HOME` | diretório da réplica (padrão `~/.pnn`) |
| `PNN_TZ` / `TZ` | fuso IANA usado nas projeções de "dia" |
| `NO_COLOR` | desliga cores |

Dica de depuração: como o log é texto, você inspeciona o que a CLI gravou com

```bash
cat ~/.pnn/events.jsonl        # um evento por linha
pnn dia --json                 # o estado projetado, em JSON
```
