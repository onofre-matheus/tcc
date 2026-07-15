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
| `leitner` | card.*, deck.archived | cartões, caixa 1–5 e fila de vencidos |
| `decks` | deck.* | decks com nome, tags e flag de arquivado |
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
| `pnn` ou `pnn dia` | — | a tela "e agora?": agenda + lista A/B/C + vencidos |
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

### Estudo

| Comando | Evento(s) | Notas |
|---|---|---|
| `pnn decks [--arquivados]` | — | árvore `::` com vencidos por deck, numerada |
| `pnn deck "Pai::Filho" [--tag t]` | `deck.created` | hierarquia por convenção de nome |
| `pnn carta [deck]` | `card.created` | TUI em série: frente → verso → fonte |
| `pnn cartas [deck] [--arquivados]` | — | manutenção do acervo, numerada |
| `pnn revisar [deck] [-m MIN]` | `session.started/ended`, `card.explained`, `card.reviewed` | TUI vermelha, fila frágil→consolidada |

### Sessão

| Comando | Evento(s) | Notas |
|---|---|---|
| `pnn foco [N] [-m MIN] [--pausa MIN] [--checkin MIN]` | `session.started/ended` (+`distraction.captured`, `checkin.logged`, `pause.*`) | `-m` padrão = janela de atenção; Esc pergunta o motivo (opcional); `--checkin N` pergunta "na tarefa? [s/n]" a cada N min (sino); `Ctrl+P` = break de 5/10/15/20/25 min que encerra a sessão (`reason: "pausa"`) |
| `pnn pausa [MIN]` | `pause.started` | pausa avulsa com duração definida (Safren) |
| `pnn volta` | `pause.ended` | encerra a pausa aberta |
| `pnn pausa --das HH:MM --ate HH:MM` | `pause.logged` | retroativa: tempo do domínio no payload |
| `pnn semana [N] [--de D]` | — | retrospectiva de qualquer semana (projeção `review`) |
| `pnn sync` | — | **não implementado** (ver §7) |

### Edição e arquivamento (catálogo v1.1)

Três verbos genéricos operam sobre o item **N da última tela**; o `kind` salvo
decide o evento ([`internal/cli/editar.go`](internal/cli/editar.go)):

| Comando | task | deck | card | appointment |
|---|---|---|---|---|
| `pnn editar N ["texto"] [--data D] [--frente F] [--verso V]` | `task.edited` | `deck.renamed` | `card.edited` | — |
| `pnn apagar N` | `task.deleted` | ✗ arquive | ✗ arquive | `appointment.cancelled` |
| `pnn arquivar N` | ✗ feito/apagar | `deck.archived` | `card.archived` | ✗ apagar |

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

## 7. Sincronização — o que existe e o que falta

O lado cliente está pronto e testado: `Store.Merge` (fusão idempotente por
`id`), `Outbox` e `Cursor` já existem em [`store/store.go`](store/store.go). O
que **não** existe ainda é o transporte: `pnn sync` não está registrado como
comando. No estado atual, a réplica da CLI (`~/.pnn`) e a da extensão evoluem
de forma independente. Isso é intencional e documentado como limitação/trabalho
futuro na monografia — nenhuma outra propriedade *local-first* é afetada: cada
cliente é plenamente funcional offline.

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
