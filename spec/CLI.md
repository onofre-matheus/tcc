# Design da CLI — `pnn`

Cliente de terminal do **Procrastina Não**. Mesmo contrato da extensão
(`SPEC.md` + `spec/vectors/` integralmente verdes), com **réplica própria** em
`~/.pnn/` sincronizada por `pnn sync` — nunca arquivo compartilhado com a
extensão (decisão de jul/2026).

## 1. Princípios de experiência

1. **`pnn` sem argumentos responde "e agora?"** — agenda do dia, lista A/B/C e
   cartões vencidos em uma única tela (calendário único + lista-mestra, RF01).
2. **Captura é uma linha, nunca uma tela** — `pnn t|n|c "..."` grava o evento e
   devolve o prompt imediatamente (RF02). Funciona inclusive durante uma sessão
   de foco, a partir de outro terminal (mesmo log local; `flock` no arquivo).
3. **Sessões são imersivas** — `pnn foco` e `pnn revisar` tomam o terminal
   (alternate screen) e **mudam a cor de todo o aplicativo**: o modo é
   informação ambiente, percebida sem leitura.
4. **Cor nunca é o único sinal** — cada modo tem símbolo, título e layout
   próprios (daltonismo vermelho×verde; respeita `NO_COLOR`; degrada para
   terminais de 16/256 cores).
5. **Números efêmeros, não UUIDs** — a tela do dia numera itens 1..n e o
   mapeamento fica em `~/.pnn/last-view.json`; daí `pnn feito 2`,
   `pnn foco 1` (estilo Taskwarrior).
6. **Toda leitura aceita `--json`** — scriptável, testável, e demonstra no TCC
   que as duas superfícies consomem a mesma projeção.

## 2. Máquina de modos e cores

```
AZUL — organizar/planejar (padrão)
  dia · captura · triagem · decks · sync
   │ pnn foco N | pnn revisar
   ▼
VERMELHO — sessão em andamento (foco em tarefa OU revisão)
  timer grande + linha de distração; nada mais na tela
   │ timer soa → session.ended (completed)     Esc → interrupted,
   ▼                                            volta direto ao AZUL
VERDE — pausa (timebreak)                       (pausa é recompensa
  descanso cronometrado + resumo da sessão      de sessão concluída)
   │ fim da pausa (ou Enter)
   ▼
AZUL …
```

- **AZUL**: bordas/acentos azuis. Estado de planejar, capturar, triar.
- **VERMELHO**: acento quente, timer gigante, UI mínima — sinal de "executando,
  não me interrompa". Vale para `foco` e `revisar` (ambos são `session.started`).
- **VERDE**: tons calmos. Mostra o que a sessão rendeu (minutos, revisões,
  distrações anotadas) e os próximos passos: `[Enter]` novo foco, `[t]` triar a
  caixa, `[q]` sair.
- Pausa padrão de 5 min (`--pausa N` para ajustar). Desde o catálogo 1.2 a
  pausa **gera evento** (`pause.started/ended`; retroativa via `pause.logged`)
  — Safren exige pausa com duração definida, e registrá-la alimenta a
  retrospectiva (`pnn semana`).
- Sessão interrompida (Esc) **pula a pausa verde** e volta ao azul: o descanso
  cronometrado é consequência de completar o bloco (reforço comportamental).
  Ao interromper, uma linha opcional pergunta o motivo (Enter pula) —
  `session.ended` v2 com `reason?`; os motivos agregados aparecem na
  retrospectiva para o próprio aluno validar seus padrões.

### Notificações de desktop

Três momentos precisam alcançar o usuário mesmo com o terminal atrás de outra
janela — é o mesmo papel que a extensão cumpre no navegador (paridade):

| momento | aviso |
|---|---|
| timer do foco/revisão soa | urgente: "N min de foco concluídos 🐘 · pausa de M min começou" |
| fim da pausa | urgente: "Fim da pausa · de volta ao foco" |
| check-in de atenção | comum: repete a pergunta que está na tela |

Quem interrompe (Esc, Ctrl+C) está no teclado: não há o que avisar.

Notificação é enfeite — nenhuma falha de desktop derruba ou trava a sessão. O
disparo sai em segundo plano (a TUI nunca gagueja) e o programa espera no
máximo 3 s por ele ao sair. `PNN_SILENCIO=1` desliga tudo.

No **WSL** o lado Linux normalmente não tem daemon de notificação: quando
D-Bus, `notify-send` e `kdialog` falham, o aviso vai para a área de trabalho
que existe ali, a do Windows, via toast nativo (`powershell.exe`).

Fundamentação (para o texto): o modo por cor é processamento pré-atentivo — o
estudante sabe em que estado está sem ler nada (menos carga de memória; afeto
de Norman); vermelho = alerta/execução, verde = restauração. A redundância com
símbolos e layout cobre daltonismo (acessibilidade, cap. 3).

## 3. Superfície de comandos

### Organização (RF01, RF03, RF04)

| Comando | Evento(s) emitido(s) |
|---|---|
| `pnn` ou `pnn dia` | — (projeções calendar + tasks + leitner + stats) |
| `pnn t "título" [-p A\|B\|C] [--sub N] [--data AAAA-MM-DD]` | `task.created` (+ `task.prioritized`) |
| `pnn feito N` | `task.completed` |
| `pnn pri N A\|B\|C` | `task.prioritized` |
| `pnn c "título" 16:00-17:00 [--dia AAAA-MM-DD] [--importante]` | `appointment.created` (v2 com `importance` se `--importante`) |
| `pnn agenda` | — (calendar; numera os próximos compromissos) |

`--sub N` cria subtarefa do item N da tela do dia (quebra de tarefas, RF04).
A tela do dia sinaliza `⚠ >4 A ativas` (RF03) e tarefas B tocadas antes das A.
`--importante` marca o compromisso como `importance: "important"`: a tela do dia
o destaca com ⭐ como marca d'água ("⭐ … · em N dias") — o equivalente da CLI
aos lembretes com antecedência que a extensão dispara por notificação. A regra
de *quando* lembrar é a projeção `reminders` compartilhada (SPEC §4.9).

### Edição e arquivamento (catálogo v1.1)

Três verbos genéricos operam sobre o item N **da última tela** — o kind salvo
em `last-view.json` decide o evento:

| Comando | task (dia) | deck (decks) | card (cartas) | appointment (agenda) |
|---|---|---|---|---|
| `pnn editar N ["texto"] [--data D] [--frente F] [--verso V]` | `task.edited` | `deck.renamed` | `card.edited` | — |
| `pnn apagar N` | `task.deleted` | ✗ (arquive) | ✗ (arquive) | `appointment.cancelled` |
| `pnn arquivar N` | ✗ (feito/apagar) | `deck.archived` | `card.archived` | ✗ (apagar) |

A distinção segue a spec: **apagar é tombstone** (tarefa descartada e
compromisso cancelado não têm valor histórico), **arquivar preserva** (o
histórico de estudo de cartões e decks fica; eles apenas saem da fila).
`pnn decks --arquivados` e `pnn cartas --arquivados` mostram o acervo
arquivado.

### Captura e triagem (RF02, RF06, RF09)

| Comando | Evento(s) |
|---|---|
| `pnn n "texto" [--link URL]` | `note.captured` (com `url` se `--link`) |
| `pnn caixa` | — (inbox; nota com link vira hyperlink OSC 8 clicável) |
| `pnn triagem` | `note.triaged` / `distraction.triaged` (+ `card.created` / `task.created`) |

`pnn triagem` é interativa, **um item por vez**: `[c]artão` (escolhe deck na
árvore), `[t]arefa`, `[d]escartar`, `[s]eguir`. Esvazia a lista temporária ao
final (RF06).

### Estudo (RF08, RF09, RF11)

| Comando | Evento(s) |
|---|---|
| `pnn decks` | — (decks + leitner; árvore `::` com vencidos por deck, numerada) |
| `pnn deck "Pai::Filho"` | `deck.created` |
| `pnn carta [deck]` | `card.created` (interativo: frente, verso, fonte opcional) |
| `pnn cartas [deck]` | — (manutenção do acervo: numera os cartões para editar/arquivar) |
| `pnn revisar [deck]` | `session.started/ended`, `card.explained`, `card.reviewed` |

Fluxo de revisão (TUI vermelha): frente → campo "explique com suas palavras"
(Feynman; vazio = pular) → revela verso + explicação anterior para comparar →
`a` acertei / `e` errei → próximo da fila (frágil→consolidada) até o timer
soar (RF11). `source_url` vira hyperlink OSC 8 clicável.

**Andaime da explicação**: no campo de explicação, `Tab` alterna entre Feynman
(campo único) e **4 causas** (wizard de 4 passos — material, formal, eficiente,
final; `Enter` avança, vazio pula a causa). As 4 causas gravam um único
`card.explained` v2 com `frame: "4causas"` e o texto concatenado `causa:
resposta` por linha; o `frame` é um campo aberto para métodos de estudo futuros.

### Sessão e sincronização (RF05, RF06, RNF02)

| Comando | Evento(s) |
|---|---|
| `pnn foco [N] [-m MIN] [--pausa MIN] [--checkin MIN]` | `session.started/ended` (v2, `reason?` no Esc) + `checkin.logged` + `pause.started/ended` (pausa verde) |
| `pnn pausa [MIN]` | `pause.started` (avulsa, cronometrada; padrão 5 min) |
| `pnn volta` | `pause.ended` |
| `pnn pausa --das HH:MM --ate HH:MM [--dia D]` | `pause.logged` (retroativa — "esqueci de apertar o botão") |
| `pnn semana [N] [--de AAAA-MM-DD]` | — (review; retrospectiva de qualquer semana) |
| `pnn sync` | — (bucket S3, SPEC §5; imprime `▲ enviados · ▼ recebidos`) |

`pnn sync` é uma operação só e idempotente: baixa o que os outros dispositivos
subiram, funde no log local (dedup por `id` + ordem total) e sobe os eventos
que este dispositivo autorou. Configure com `PNN_S3_BUCKET` (e `PNN_S3_PREFIX`,
`PNN_S3_ENDPOINT`, `PNN_S3_REGION`); as credenciais vêm da cadeia padrão da
AWS, nunca de `~/.pnn`. Como cada dispositivo escreve só a própria chave no
bucket, não há conflito a resolver nem escrita perdida — ver SPEC §5.

`pnn semana` é a revisão à la Safren: foco, pausas e revisões por dia, motivos
de interrupção agregados e o contraste agendado × executado. `N` volta N
semanas; `--de` navega para a semana de qualquer data — a projeção é pura em
`(eventos, now, tz)`, então o histórico inteiro é acessível só variando o
instante de referência.

`-m` padrão = janela de atenção (mediana das últimas sessões, RF05). Durante o
foco, a linha de input fica **sempre focada**: qualquer texto + Enter grava
`distraction.captured` e limpa (adiamento de distrações, RF06).

**Check-in de atenção** (`--checkin N`): a cada N minutos a tela notifica e
pergunta "Você está na tarefa *X*?" — `[s]` sim / `[n]` distraí grava um
`checkin.logged` binário; `Esc` pula sem gravar. É o *self-monitoring of
attention* de Safren: os check-ins agregam em `pnn semana` como "na tarefa em
X de Y checagens". **Break** (`Ctrl+P`): escolhe a duração da pausa (5/10/15/
20/25 min), **encerra a sessão** (`session.ended` interrupted, `reason:
"pausa"`) e entra na tela verde — a pausa é um estado próprio, não um
parêntese dentro do foco; a tarefa "desmarca" e o novo foco é uma escolha
nova.

## 4. Telas

### `pnn` — AZUL

```
🐘 Procrastina Não                   🔥 3 dias · 2 revisões hoje
────────────────────────────────────────────────────────────
 quarta, 8 de julho

 16:00–17:00  Reunião com orientador

 1 [A] Escrever seção 4.6
 2 [A]  ↳ Rascunhar requisitos
 3 [B] Ler capítulo 3

 7 cartões vencidos → pnn revisar
────────────────────────────────────────────────────────────
 caixa de entrada: 2 pendentes → pnn triagem
```

### `pnn foco 1` — VERMELHO

O tempo restante é um despertador 8-bit: grande o bastante para ser lido de
longe, sem "consultar" a tela. O dois-pontos pisca a cada segundo — é o sinal
de que a sessão está correndo, e não congelada. O desenho não depende de cor
(NO_COLOR e saída redirecionada continuam legíveis) e o horário vai também em
texto na borda de baixo, para leitor de tela. Em terminal estreito demais para
o gabinete (< 38 colunas), degrada para uma linha: `▐█  23:41  █▌`.

```
● FOCO · Escrever seção 4.6

                        ▄▄▄                        ▄▄▄
                       ▐███▌                      ▐███▌
                     ┏━━ FOCO ━━━━━━━━━━━━━━━━━━━━━━━━━━┓
                     ┃                                  ┃
                     ┃  ██████ ██████    ██  ██     ██  ┃
                     ┃      ██     ██ ██ ██  ██     ██  ┃
                     ┃  ██████ ██████    ██████     ██  ┃
                     ┃  ██         ██ ██     ██     ██  ┃
                     ┃  ██████ ██████        ██     ██  ┃
                     ┃                                  ┃
                     ┗━━━━━━━━━━━━━━━━━━━━━━━━━ 23:41 ━━┛
                       ▀▀                            ▀▀

 Distração? anote e volte ao trabalho
 Enter grava e limpa · Esc encerra · Ctrl+P pausa
```

### Pausa — VERDE

```
✔ 25 min de foco · 🐘 mandou bem!

                        ▄▄▄                        ▄▄▄
                       ▐███▌                      ▐███▌
                     ┏━━ PAUSA ━━━━━━━━━━━━━━━━━━━━━━━━━┓
                     ┃                                  ┃
                     ┃  ██████ ██  ██    ██████ ██████  ┃
                     ┃  ██  ██ ██  ██ ██     ██     ██  ┃
                     ┃  ██  ██ ██████    ██████ ██████  ┃
                     ┃  ██  ██     ██ ██     ██ ██      ┃
                     ┃  ██████     ██    ██████ ██████  ┃
                     ┃                                  ┃
                     ┗━━━━━━━━━━━━━━━━━━━━━━━━━ 04:32 ━━┛
                       ▀▀                            ▀▀

 a pausa é parte do bloco — descanse até o despertador

 Nesta sessão: 2 distrações anotadas → triar depois
 [Enter] novo foco · [t] triagem · [q] sair
```

## 5. Easter egg: `procrastina`

Junto com `pnn`, instala-se um alias `procrastina`. Executá-lo no momento de
fraqueza é a intervenção que dá nome ao sistema. A intervenção não pode
falhar por erro de digitação nesse momento: o typo comum `procastina` (sem o
1º "r") e os infinitivos `procrastinar`/`procastinar` são aceitos, tanto como
subcomando quanto por `argv[0]`:

```
$ procrastina

        Não. 🐘

 3 dias de sequência — não quebra hoje.
 Um passo pequeno: [A] Rascunhar requisitos
 → pnn foco 2
```

Comportamento: mascote + "Não." + streak atual + **a menor subtarefa A ativa**
(quebra de tarefas: o próximo passo mais fácil de começar) com o comando de
foco pronto para copiar. Sem tarefa A ativa, sugere `pnn revisar` se houver
cartões vencidos; sem nada pendente, elogia e manda descansar. É a síntese do
sistema em um comando: no impulso de desistir, o custo de recomeçar cai para
uma linha.

## 6. Implementação (quando chegar a hora)

- **Go** com Cobra (comandos), Bubble Tea (TUI das sessões) e Lipgloss
  (temas adaptativos claro/escuro; perfis de cor com degradação automática).
- Pacotes: `core/` (projeções puras portadas do JS, passando
  `spec/vectors/` — mesma bateria, TDD), `store/` (`events.jsonl` append-only
  + Lamport + device id + cursor), `ui/` (temas azul/vermelho/verde).
- `procrastina` = segundo entry point/symlink do mesmo binário (decide pelo
  `argv[0]`).
- Ordem sugerida: core + vetores → comandos one-shot (captura/dia) →
  TUI foco/revisão → sync.

## 7. Decisões fechadas (2026-07-08)

- Nome do app: **Procrastina Não** (antes "Palácio da Memória").
- Binário: **`pnn`**; alias-intervenção **`procrastina`**.
- Comandos em **português**.
