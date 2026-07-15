#!/usr/bin/env bash
# Semeia uma réplica de demonstração do Procrastina Não para os prints da
# monografia (cap. 4, §Implementação). Re-rodável: recria o diretório do zero.
#
# Uso: scripts/seed-demo.sh [DIR]        (padrão: /tmp/pnn-demo)
#
# Cenário resultante (datas relativas a hoje, vale em qualquer dia):
#   - streak de 4 dias terminando ontem
#   - 3 cartões vencidos hoje; o 1º da fila tem fonte (OSC 8) e explicação
#     anterior (Feynman) — um único print do `pnn revisar` mostra tudo
#   - janela de atenção com mediana de 27 min (26/24/31/28; a sessão
#     interrompida de 9 min fica fora da mediana) → `pnn foco` sugere 27
#   - a sessão de ontem tem check-in de atenção (na tarefa em 1 de 2 — RF07;
#     aparece em `pnn semana` — no dia seguinte, `pnn semana 1`)
#   - caixa de entrada com 1 nota e 1 distração pendentes de triagem
#   - agenda de hoje e de amanhã, lista A/B/C com subtarefas
set -euo pipefail

DEMO="${1:-/tmp/pnn-demo}"
REPO="$(cd "$(dirname "$0")/.." && pwd)"

# Nunca apaga um diretório que não foi criado por este script.
if [ -e "$DEMO" ] && [ ! -f "$DEMO/.pnn-demo-seed" ]; then
  echo "erro: $DEMO existe e não é uma réplica de demonstração deste script" >&2
  exit 1
fi
rm -rf "$DEMO"
mkdir -p "$DEMO/bin"
touch "$DEMO/.pnn-demo-seed"

command -v go >/dev/null 2>&1 || PATH="$HOME/.local/go/bin:$PATH"
(cd "$REPO/cli" && go build -o "$DEMO/bin/pnn" ./cmd/pnn)
ln -sf pnn "$DEMO/bin/procrastina"

export PNN_HOME="$DEMO"
DN="$DEMO/bin/pnn"

# ---------------------------------------------------------------------------
# Fase 1 — histórico injetado direto no log, como se tivesse chegado pelo
# sync de outro dispositivo (device "seed-ext"). Comandos one-shot gravam
# ts = agora; streak, vencimentos e amostras de atenção exigem datas passadas.
EVENTS="$DEMO/events.jsonl"
LC=0
emit() { # emit <id> <tipo> <ts> <payload-json> [v]
  LC=$((LC + 1))
  printf '{"id":"%s","type":"%s","v":%d,"lc":%d,"ts":"%s","device":"seed-ext","payload":%s}\n' \
    "$1" "$2" "${5:-1}" "$LC" "$3" "$4" >>"$EVENTS"
}
d() { date -u -d "$1 days ago" +%Y-%m-%d; }
D6=$(d 6); D5=$(d 5); D4=$(d 4); D3=$(d 3); D2=$(d 2); D1=$(d 1)

# Decks: hierarquia por "::" e tags transversais
emit seed-01 deck.created "${D6}T12:00:00.000Z" '{"deck_id":"d-so","name":"Sistemas Operacionais","tags":["so"]}'
emit seed-02 deck.created "${D6}T12:00:05.000Z" '{"deck_id":"d-esc","name":"Sistemas Operacionais::Escalonamento","tags":["so"]}'
emit seed-03 deck.created "${D6}T12:00:10.000Z" '{"deck_id":"d-en","name":"Inglês::Vocabulário","tags":["idiomas"]}'

# Cartões: c-rr com fonte, para o hyperlink no print do revisar
emit seed-04 card.created "${D6}T12:01:00.000Z" '{"card_id":"c-rr","deck_id":"d-esc","front":"O que é o escalonamento Round-Robin?","back":"Preempção por fatias de tempo (quantum): cada processo executa um quantum e volta ao fim da fila circular.","source_url":"https://pages.cs.wisc.edu/~remzi/OSTEP/cpu-sched.pdf","source_title":"OSTEP — Scheduling: Introduction","tags":["so","escalonamento"]}'
emit seed-05 card.created "${D6}T12:02:00.000Z" '{"card_id":"c-pt","deck_id":"d-so","front":"Qual a diferença entre processo e thread?","back":"Processo tem espaço de endereçamento próprio; threads do mesmo processo compartilham esse espaço.","tags":["so"]}'
emit seed-06 card.created "${D5}T12:00:00.000Z" '{"card_id":"c-en","deck_id":"d-en","front":"procrastinate","back":"procrastinar; adiar deliberadamente uma tarefa","tags":["idiomas"]}'

# D-4: sessão de revisão completa (26 min), explicação Feynman + 3 revisões.
# c-rr e c-pt acertam → caixa 2, vencem em D-1 (já vencidos hoje).
emit seed-07 session.started "${D4}T12:00:00.000Z" '{"session_id":"s1","kind":"review","planned_minutes":25}'
emit seed-08 card.explained "${D4}T12:03:00.000Z" '{"card_id":"c-rr","text":"É o escalonador que dá uma fatia de tempo para cada processo e vai revezando em círculo.","session_id":"s1"}'
emit seed-09 card.reviewed "${D4}T12:04:00.000Z" '{"card_id":"c-rr","result":"correct","session_id":"s1"}'
emit seed-10 card.reviewed "${D4}T12:08:00.000Z" '{"card_id":"c-pt","result":"correct","session_id":"s1"}'
emit seed-11 card.reviewed "${D4}T12:12:00.000Z" '{"card_id":"c-en","result":"wrong","session_id":"s1"}'
emit seed-12 session.ended "${D4}T12:26:00.000Z" '{"session_id":"s1","outcome":"completed"}'

# D-3: sessão de 24 min; c-en acerta → caixa 2, vence hoje (D-3 + 3 dias)
emit seed-13 session.started "${D3}T12:00:00.000Z" '{"session_id":"s2","kind":"review","planned_minutes":25}'
emit seed-14 card.reviewed "${D3}T12:05:00.000Z" '{"card_id":"c-en","result":"correct","session_id":"s2"}'
emit seed-15 session.ended "${D3}T12:24:00.000Z" '{"session_id":"s2","outcome":"completed"}'

# D-2: uma interrompida (fica fora da mediana) e uma completa de 31 min
emit seed-16 session.started "${D2}T12:00:00.000Z" '{"session_id":"s3","kind":"task","planned_minutes":25}'
emit seed-17 session.ended "${D2}T12:09:00.000Z" '{"session_id":"s3","outcome":"interrupted"}'
emit seed-18 session.started "${D2}T14:00:00.000Z" '{"session_id":"s4","kind":"task","planned_minutes":30}'
emit seed-19 session.ended "${D2}T14:31:00.000Z" '{"session_id":"s4","outcome":"completed"}'

# D-1: sessão de 28 min + pendências de triagem (distração e nota com fonte)
emit seed-20 session.started "${D1}T12:00:00.000Z" '{"session_id":"s5","kind":"task","planned_minutes":27,"checkin_every":10}' 2
emit seed-21 distraction.captured "${D1}T12:10:00.000Z" '{"distraction_id":"x-mail","session_id":"s5","text":"responder o e-mail do orientador"}'
emit seed-36 checkin.logged "${D1}T12:10:30.000Z" '{"checkin_id":"k-s5-1","session_id":"s5","on_task":true}'
emit seed-37 checkin.logged "${D1}T12:20:00.000Z" '{"checkin_id":"k-s5-2","session_id":"s5","on_task":false}'
emit seed-22 session.ended "${D1}T12:28:00.000Z" '{"session_id":"s5","outcome":"completed"}'
emit seed-23 note.captured "${D1}T15:00:00.000Z" '{"note_id":"n-ostep","text":"Ler o cap. 10 do OSTEP (escalonamento multiprocessador)","url":"https://pages.cs.wisc.edu/~remzi/OSTEP/","page_title":"Operating Systems: Three Easy Pieces"}'

# ---------------------------------------------------------------------------
# Fase 2 — os dados de hoje entram pela própria CLI (exercita o binário real;
# o Lamport continua de max(lc) do log injetado).
AMANHA=$(date -d tomorrow +%Y-%m-%d)
H1=$(date -d '+1 hour' +%H:%M)
H2=$(date -d '+2 hours' +%H:%M)
"$DN" c "Aula de Redes de Computadores" "$H1-$H2"
"$DN" c "Orientação com o Renan" 14:00-15:00 --dia "$AMANHA"
"$DN" t "Escrever a seção Implementação do capítulo 4" -p A
"$DN" t "Revisar o capítulo de escalonamento do OSTEP" -p B
"$DN" t "Organizar as referências no BibTeX" -p C
"$DN" dia >/dev/null # materializa os números efêmeros para o --sub
"$DN" t "Descrever a tabela comando-requisito" --sub 1 -p A
"$DN" t "Tirar os prints da CLI" --sub 1 -p A

cat <<EOF

Réplica de demonstração pronta em $DEMO

No terminal dos prints (rode pouco antes de capturar, para o compromisso
de hoje ainda estar à frente do relógio):

  export PNN_HOME=$DEMO
  export PATH=$DEMO/bin:\$PATH

  pnn dia         → figuras/cli_dia.png
  pnn foco        → figuras/cli_foco.png   (digite uma distração e tecle Enter)
  pnn revisar     → figuras/cli_revisar.png (1º cartão: fonte + explicação anterior)
  procrastina        → figuras/cli_procrastina.png
  pnn caixa, pnn decks, pnn triagem, pnn dia --json também rendem prints, se quiser.
EOF
