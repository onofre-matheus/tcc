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
   │ Enter → novo bloco na mesma tarefa (VERMELHO)
   │ fim da pausa → [Enter] voltar à tarefa · [q] sair
   ▼
AZUL …
```

- **AZUL**: bordas/acentos azuis. Estado de planejar, capturar, triar.
- **VERMELHO**: acento quente, timer gigante, UI mínima — sinal de "executando,
  não me interrompa". Vale para `foco` e `revisar` (ambos são `session.started`).
- **VERDE**: tons calmos. Mostra o que a sessão rendeu (minutos, revisões,
  distrações anotadas) e os próximos passos: `[Enter]` voltar à tarefa (novo
  bloco, mesmo `target_id`), `[t]` triar a caixa, `[q]` sair.
- **Fim da pausa**: quando o timer verde toca, `pause.ended` é gravado e a tela
  **não fecha** — espera, com o relógio parado, `[Enter]` voltar à tarefa ·
  `[t]` triagem · `[q]` sair. Cumprir a pausa não pode custar mais do que
  pulá-la: se o fim da pausa fechasse o programa, voltar exigiria redigitar
  `pnn foco N`, enquanto pular com Enter mantém a tarefa — o incentivo seria
  pular a pausa.
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
| fim do bloco com a página aberta no editor (Ctrl+E) | urgente: "N min de foco concluídos 🐘 · salve e volte ao pnn para a pausa de M min" — sai pelo relógio de parede, porque a tela está parada |
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

| Comando | task (dia) | deck (decks) | card (cartas) | appointment (agenda) | tag (tags) | study (sessoes) | page (paginas) |
|---|---|---|---|---|---|---|---|
| `pnn editar N ["texto"] [--data D] [--frente F] [--verso V] [--tag T] [--untag T] [--tags "…"]` | `task.edited` | `deck.renamed` | `card.edited` | — | `tag.renamed` | `study.saved` (renomeia) | `page.edited` (sem texto: abre o editor) |
| `pnn apagar N` | `task.deleted` | ✗ (arquive) | ✗ (arquive) | `appointment.cancelled` | ✗ (renomeie/untag) | `study.deleted` | ✗ (arquive) |
| `pnn arquivar N` | ✗ (feito/apagar) | `deck.archived` | `card.archived` | ✗ (apagar) | ✗ | ✗ | `page.archived` |

A distinção segue a spec: **apagar é tombstone** (tarefa descartada e
compromisso cancelado não têm valor histórico), **arquivar preserva** (o
histórico de estudo de cartões e decks fica; eles apenas saem da fila).
`pnn decks --arquivados` e `pnn cartas --arquivados` mostram o acervo
arquivado.

Tags de cartão (catálogo 1.3): `--tag` acrescenta, `--untag` remove e `--tags`
substitui a lista (`--tags ""` limpa). O `card.edited` leva a **lista final
inteira** — `tags` é um campo só no LWW por campo. Nada mudou, nada é gravado.

Renomear tag (catálogo 1.4): `pnn tags` numera a árvore e `pnn editar N
"novo-nome"` emite `tag.renamed` — a tag e as descendentes (`so/x` → `novo/x`)
mudam em cartões, páginas, decks e sinapses já gravados; renomear para uma tag que já
existe funde as duas. Vale para o que foi gravado até ali: uma tag criada
depois com o nome antigo é uma tag nova.

### Captura e triagem (RF02, RF06, RF09)

| Comando | Evento(s) |
|---|---|
| `pnn n "texto" [--link URL]` | `note.captured` (com `url` se `--link`) |
| `pnn caixa` | — (inbox; nota com link vira hyperlink OSC 8 clicável) |
| `pnn triagem` | `note.triaged` / `distraction.triaged` (+ `card.created` / `task.created`) |

`pnn triagem` é interativa, **um item por vez**: `[c]artão` (escolhe deck na
árvore ou "(sem deck)"; tags opcionais), `[t]arefa`, `[d]escartar`, `[s]eguir`. Esvazia a lista temporária ao
final (RF06).

### Páginas (catálogo 1.6)

Anotação longa e de uso constante — o que não é passagem (nota) nem estudo
(cartão). Escreve-se no `$EDITOR` do usuário, não numa TUI.

| Comando | Evento(s) |
|---|---|
| `pnn p "título" [--tag T]…` | `page.created` (abre o editor no corpo) |
| `pnn paginas [--tag T]… [--todas] [--por-tag] [--arquivadas] [--json]` | — (as fixadas e as 15 editadas por último, com rodapé do que ficou de fora; numerada, com `⚡N`) |
| `pnn pagina N\|título [-e] [--cru]` | — (lê formatada e paginada, com as referências numeradas; `-e` abre no editor → `page.edited`) |
| `pnn buscar "texto"` | — (páginas e cartões; numerada, com o trecho que casa) |
| `pnn fixar N` | `page.edited` com `pinned` (alterna) |
| `pnn escrever N\|título` | `page.edited` (o editor do pnn, com os comandos `/`, fora do foco) |

- **O texto é só texto; o resto é metadado.** O editor abre um `.md`
  temporário (0600) com **só o corpo**, em markdown puro. Título, tags e
  sinapses não se escrevem no texto — como nos cartões, mudam por comando:
  `pnn editar N "novo título"`, `pnn editar N --tag/--untag/--tags` (ou
  `pnn ligar N '#tag'`, que classifica), `pnn ligar`/`pnn desligar`.
  Escrever e organizar ficam separados (como
  captura e triagem), o texto nunca é varrido (SPEC §3, Conexões) e um corpo
  que começa com `---` é só texto.
- **A página mostra o que está ligado a ela**, como referência:

  ```
  📌 Documentação do portal
  #trabalho/portal #kubernetes · editada hoje · criada ontem
  ⚡ 2 → "O que é um Pod?"  3 ← página Diário
  ```

  As tags ficam na linha da meta (classificação); as sinapses, numeradas: a
  página é o 1 e o que está ligado a ela, 2 em diante (`→` sai dela, `←`
  chega nela): `pnn pagina 3` abre a página ligada, `pnn editar 2 --frente …`
  edita o cartão, `pnn desligar 1 2` desfaz, `pnn revisar --ao-redor 1`
  estuda os cartões ligados à página **e os das tags dela** (SPEC §4.11).
- **Nada do que foi escrito se perde**: editor que sai com erro ou falha ao
  gravar não perdem o texto — o temporário fica e o caminho vai na mensagem.
  Esvaziar a página é uma edição válida (a versão anterior fica no log).
- **Só o que mudou é gravado**: o corpo leva `base` = a `version` lida ao
  abrir o editor (SPEC §3, Páginas). Se outra edição chegou enquanto o editor
  estava aberto (sync, outro terminal), a CLI avisa — a detecção de conflito
  propriamente dita lê `base` e é uma etapa futura.
- **Editor**: `$PNN_EDITOR`, `$VISUAL`, `$EDITOR` (nessa ordem; senão
  `editor`, `nano`, `vi`), executado pelo shell como no git (`code --wait`
  funciona).
- **Leitura**: o corpo é formatado com o glamour, no tema escuro por padrão
  (`PNN_TEMA=claro`, ou `COLORFGBG` de fundo claro, troca o tema — perguntar
  a cor de fundo ao terminal custa 5 s nos que não respondem), e passa pelo
  paginador (`$PNN_PAGER`, `$PAGER` ou `less` com `LESS=FRX`: texto curto
  sai direto). Fora do terminal, sem cor e sem paginador; `--cru` imprime o
  markdown como está (`pnn pagina 1 --cru > nota.md`).
- **Achar pelo título**: sem caixa nem acento, todas as palavras
  (`pnn pagina manutencao cluster`). Título idêntico vence; várias
  candidatas viram uma lista numerada. A busca (`pnn buscar`) olha título,
  tags e texto; páginas que casam no título vêm primeiro — e, como mistura
  páginas e cartões numa tela só, serve para ligar um ao outro
  (`pnn buscar pod` → `pnn ligar 1 3`).
- Arquivar e apagar seguem a tabela acima: página não se apaga — arquivar
  preserva o texto e as sinapses.
- `pnn tags` conta as páginas de cada tag (SPEC §4.10), junto dos cartões.
- **A lista não vira paredão.** O acervo de páginas só cresce, então
  `pnn paginas` mostra **as fixadas e as 15 editadas por último**, com um
  rodapé "… e mais N — `--todas`, `--por-tag`, `--tag T` ou `pnn buscar`".
  `--por-tag` agrupa as páginas na árvore de tags, como pastas: cada tag com
  quantas páginas tem na subárvore e as 5 mais recentes dela (o resto, pelo
  `--tag` indicado); a página com duas tags aparece nas duas com o mesmo
  número, e as sem tag vão no fim. Com `--tag`, a árvore se recorta na
  subárvore. `--todas` tira os limites; `--arquivadas` lista só as
  arquivadas. (300 páginas: 16 linhas na lista simples, 45 na agrupada.)
- **Ainda não**: páginas no `pnn grafo` (a sinapse com página fica fora do
  desenho, SPEC §4.14).

### Estudo (RF08, RF09, RF11)

| Comando | Evento(s) |
|---|---|
| `pnn decks` | — (decks + leitner; árvore `::` com vencidos por deck, numerada) |
| `pnn deck "Pai::Filho"` | `deck.created` |
| `pnn carta [deck] [--tag T]… [--sem-deck]` | `card.created` (interativo: frente, verso, fonte e tags opcionais; v2 sem deck) |
| `pnn cartas [deck] [--tag T]…` | — (manutenção do acervo: numera os cartões para editar/arquivar) |
| `pnn tags` | — (tags em árvore `/`, numerada, com cartões, páginas, vencidos e sinapses de cada uma) |
| `pnn ligar A B [C…] [--relacao R]` | `link.created` (sinapse A → B, A → C…); com `'#tag'` de um lado: `card.edited`/`page.edited` (classifica) |
| `pnn desligar A B [C…]` | `link.removed` (todas as sinapses do par, em qualquer direção); com `'#tag'`: tira a tag |
| `pnn sinapses [nó]` (alias `conexoes`) | — (sem nó: o que tem sinapses, mais ligados primeiro; com nó: a vizinhança, numerada) |
| `pnn grafo [nó] [--raio R] [--sem-cartoes] [--json]` | — (o acervo como grafo em DOT; com nó, o grafo local) |
| `pnn sessao "Nome" [deck] [--tag T]… [--ao-redor N]… [--treino]` | `study.saved` (o mesmo nome atualiza o filtro) |
| `pnn sessoes` | — (sessões salvas, numeradas, com vencidos e treino de cada uma hoje) |
| `pnn revisar [deck] [--tag T]… [--ao-redor N]… [--treino] [--sessao N\|"Nome"]` | `session.started/ended`, `card.explained`, `card.reviewed` (+ `card.practiced` no treino) |

**Tags livres e cartão solto (catálogo 1.3)**: o deck é opcional — no seletor
de `pnn carta`/triagem, "(sem deck)" (último item) cria o cartão organizado só
por tags (`card.created` v2 sem `deck_id`); sem nenhum deck, o formulário abre
direto assim. O campo de tags aceita espaço ou vírgula, com ou sem `#`, e `/`
para hierarquia (`so/escalonamento`); fica preenchido para o próximo cartão da
série. Tags efetivas = as do cartão ∪ as do deck.

**Sessão personalizada (SPEC §4.11)**: `pnn revisar` filtra a fila por deck
(subárvore `::`) e/ou `--tag` (inclui as descendentes `/`; várias = qualquer
uma); deck e tag juntos combinam com E. `--treino` acrescenta, depois dos
vencidos, os **não vencidos** que casam: a TUI marca `TREINO · não altera a
caixa`, e a resposta vira `card.practiced` — estudar antes da hora não promove
o cartão. `pnn cartas --tag` usa a mesma regra para listar o acervo.

**Sinapses (catálogo 1.4, SPEC §4.12; o nome vem do 1.6)**, estilo
Obsidian: cartões e páginas são os neurônios, a **sinapse** (⚡) é a ligação
entre eles. `pnn ligar A B` cria uma sinapse entre duas **coisas** — cartão,
página, tarefa (do `pnn dia`) ou deck. **A tag não é ponta de sinapse: ela
classifica.** Com uma
`'#tag'` de um lado e um cartão ou página do outro, `pnn ligar` dá a tag ao
item (`card.edited`/`page.edited`, como `pnn editar N --tag`) e `pnn desligar`
a tira — "ter a tag" e "estar ligado à tag" eram quase o mesmo e só
confundiam. Deck ↔ tag, tarefa ↔ tag e tag ↔ tag são recusados (as tags do
deck são as da criação; tarefa não tem tags nem descrição — o contexto dela é
uma página ligada; assuntos se relacionam por páginas), assim como `--relacao` com tag
(para "pré-requisito de um assunto", ligue à página do assunto). Recusa não
grava nada, nem a parte válida do comando. Sinapses com tag gravadas antes
do 1.6 continuam valendo e se desfazem pelo `desligar`.

Um nó é o **número da última tela** (`pnn cartas`, `pnn decks`, `pnn tags`,
`pnn paginas`, `pnn pagina`, `pnn buscar`, `pnn sinapses`), uma **`'#tag'`**
(entre aspas: no shell, `#` começa comentário), o **nome exato de um deck** ou
o **título de uma página** (sem caixa nem acento: `pnn ligar "documentacao
do portal" 3`); sem `#`, um nome que não é deck nem página vale como tag se
ela já existir (tag nova pede o `#`). Com mais de dois nós, o primeiro é
ligado a cada um dos demais. `--relacao` dá nome à sinapse (`prerequisito`,
`exemplo`, livre); ligar de novo um par já ligado não grava nada (com
`--relacao`, só se a relação for a mesma). `pnn sinapses N` (o antigo
`pnn conexoes` continua valendo) numera o próprio nó como 1 e os vizinhos a
partir de 2 (`→` sai dele, `←` chega nele, `↔` as duas), para `pnn desligar 1
3` sem redigitar. `pnn revisar --ao-redor N` estuda a vizinhança: o nó, os
ligados a ele (um salto) e, para tag e deck, também os ligados às
descendentes; para página, também os cartões das tags dela. Na revisão, as
sinapses do cartão aparecem **depois do verso** (`⚡ sinapses: …`) —
material para elaborar, não pista para a resposta. `pnn cartas` mostra `⚡N`
nos cartões com sinapses.

**Grafo (SPEC §4.14)**: `pnn grafo` escreve o acervo em DOT — cartões ativos
(vencidos em vermelho), decks e tags como nós; sinapses (setas com a
relação), tags (tracejado), pertença a deck e hierarquia `/`/`::`
(pontilhado) como arestas. Renderize com o Graphviz: `pnn grafo | dot -Tsvg >
grafo.svg`. Com um nó (`pnn grafo '#so'`), sai o **grafo local** — o nó e o que
está a até `--raio` arestas dele; `--sem-cartoes` deixa só tags e decks, a
visão geral. É a mesma projeção que a extensão desenha na tela de grafo.

**Sessões salvas (catálogo 1.5, SPEC §4.13)**, como os *decks filtrados* do
Anki: `pnn sessao "Véspera da P2" --tag prova-p2 --treino` guarda o
**filtro** (deck, `--tag`, `--ao-redor`, `--treino`) — não a lista de cartões,
então reaberta a sessão reúne o que casar naquele dia. Salvar de novo com o
mesmo nome atualiza o filtro (`study.saved` substitui a sessão inteira). `pnn
sessoes` lista com quantos vencidos e quantos para treinar cada uma tem hoje;
`pnn revisar --sessao N` (ou o nome) estuda — o filtro salvo não se combina
com deck/`--tag`/`--ao-redor` avulsos. As tags da sessão acompanham as
renomeações de tag.

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
| `pnn foco [N] [-m MIN] [--pausa MIN] [--checkin MIN] [--relogio]` | `session.started/ended` (v2, `reason?` no Esc) + `checkin.logged` + `pause.started/ended` (pausa verde) |
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
bucket, não há conflito a resolver nem escrita perdida — ver SPEC §5. O
objeto da CLI sobe comprimido com zstd (`<device>.jsonl.zst`): o log guarda o
texto inteiro de cada versão de página, e a compressão acha as repetições.

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

**Página da tarefa** (catálogo 1.6): tarefa não tem descrição — o contexto
dela é uma **página ligada por sinapse** (`pnn dia` → `pnn ligar "docs cli"
3`; o `pnn dia` mostra `⚡N` nas tarefas ligadas). O `pnn foco N` mostra as
páginas ligadas sob o relógio (`⚡ página docs cli`), e **`Ctrl+E`** abre a
página no `$EDITOR` **sem encerrar o bloco** (com várias, `[1]`..`[9]`
escolhe; `Ctrl+E` e não `e` porque a linha de distração está sempre
focada). O editor toma o terminal: a tela e os tiques param, e na volta o
tempo de parede que passou é **descontado do relógio** (check-ins do meio
não são feitos). O aviso de fim de bloco não depende da tela — toca no
horário mesmo com o vim aberto; se o bloco acabou durante a edição, ele se
encerra como concluído na volta e a pausa verde começa ali. O resultado da
edição (`✔ página salva`, ou o erro com o caminho do texto preservado)
aparece na própria tela.

**Modo de escrita** (o editor do pnn): com página ligada, o `pnn foco N` abre
**direto na página**, num editor dentro da própria tela, com o relógio numa
barra no topo e o que está ligado à página (⚡) no rodapé — ao contrário do
`$EDITOR`, o relógio, os check-ins e os avisos seguem vivos enquanto se
escreve (com várias páginas, pergunta em qual; `--relogio` abre no relógio
grande, e `Ctrl+O` entra na escrita depois). `Ctrl+S` grava (`page.edited`
com `base`), e a página também se grava sozinha: a cada 2 min com mudança
pendente, no fim do bloco (antes da pausa), ao sair e antes de abrir o
`$EDITOR` — nunca por tecla, porque cada gravação é o texto inteiro. `Esc`
volta ao relógio grande sem encerrar o bloco; `Ctrl+E` leva a página ao
`$EDITOR` para edição pesada e a relê na volta; `Ctrl+P` é a pausa e
`Ctrl+C` grava e encerra. O check-in aparece no rodapé e o `s`/`n` responde
sem virar texto.

**Comandos `/`** — na escrita e também no **relógio grande**: na escrita, o
`/` abre os comandos no começo da linha ou depois de um espaço (colado a uma
palavra é só uma barra: "e/ou", "http://"); no relógio grande, como primeiro
caractere da linha de distração. `//` escreve a barra. Filtrados enquanto se
digita; `Enter` executa o primeiro que casa (marcado com `▸`: `/tar` →
`/tarefa`), `Tab` completa, sem caixa nem acento (`/distração`, `/cartão`),
`Esc` fecha. A linha do comando não fica no texto — registrar sem trocar de
ferramenta no meio do trabalho (o adiamento de distrações, RF06, estendido).

Os comandos têm um **ponto de referência**: na escrita, a **página** aberta;
no relógio grande, a **tarefa em foco**. Daí `/ligar` e `/cartao` ligarem à
página ou à tarefa, e `/tag` (classificar) só valer na escrita. Com tarefa em
foco, `/tarefa` cria uma **subtarefa** dela (`parent_id`) — a decomposição que
a spec recomenda para "não sei por onde começar" —, e na escrita também a liga
à página. **Sem página ligada, `Ctrl+O` abre os comandos já com
`/pagina <título da tarefa>`**: um Enter cria a página e começa a escrever.
Todos emitem eventos que já existem:

| Comando | Na escrita (página) | No relógio grande (tarefa) | Evento(s) |
|---|---|---|---|
| `/pagina [Título]` | sem título, troca entre as páginas ligadas; com título, cria outra ligada à tarefa e passa a ela | cria a página ligada à tarefa (sem título: o da tarefa) e começa a escrever | `page.created` + `link.created` |
| `/tarefa Título` | subtarefa da em foco, **ligada à página** | subtarefa da em foco | `task.created` (`parent_id`) + `link.created` |
| `/tarefas` | lista as abertas com prioridade — a em foco, as da página (`⚡`), as do dia; o número conclui | idem | `task.completed` |
| `/feito [texto]` | conclui (a em foco e as da página primeiro); com texto que casa uma só, direto | idem | `task.completed` |
| `/cartao frente \| verso` | cartão solto ligado à página | cartão solto ligado à tarefa | `card.created` v2 + `link.created` |
| `/ligar palavras` | procura páginas e cartões (como o `pnn buscar`) e liga à página | idem, liga à tarefa (página ligada entra no `Ctrl+O`) | `link.created` |
| `/tag a b` · `/untag a` | classifica a página | — | `page.edited` (`tags`) |
| `/distracao texto` | anota a distração sem sair do foco (no `pnn escrever`, sem sessão, vira nota da caixa de entrada) | idem (o mesmo que a linha de distração) | `distraction.captured` (ou `note.captured`) |
| `/salvar` · `/sair` | o mesmo que `Ctrl+S` · `Esc` | — | — |

O editor é o `textarea` do bubbles: atalhos no estilo emacs (`Ctrl+A`/`End`,
`Alt+setas`), colar funciona. **`Ctrl+Z` desfaz e `Ctrl+Y` refaz** — o
textarea não tem, então o modo de escrita guarda retratos do texto antes de
cada grupo de edição (uma palavra, uma sequência de apagar, um Enter, uma
colagem) e devolve também o cursor ao lugar. Ainda não há busca dentro do
texto — para isso, o `Ctrl+E` e o vim. A página abre com o cursor no fim
do texto, onde se continua escrevendo.

**`pnn escrever N|título`**: o mesmo editor e os mesmos comandos **fora do
foco** — sem sessão, sem relógio, sem pausa; `Esc` grava e sai, e a CLI
imprime o resultado da última gravação.

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

 ⚡ página docs cli
 Distração? anote e volte ao trabalho
 Enter grava e limpa · Esc encerra · Ctrl+P pausa · Ctrl+E edita a página
```

A linha `⚡` e a dica do `Ctrl+E` só aparecem quando a tarefa tem página
ligada.

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
 [Enter] voltar a "Escrever seção 4.6" (25 min) · [t] triagem · [q] sair
```

Quando a pausa toca:

```
⏰ fim da pausa · 🐘 bora de novo?

 Nesta sessão: 2 distrações anotadas → triar depois

 [Enter] voltar a "Escrever seção 4.6" (25 min) · [t] triagem · [q] sair
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
