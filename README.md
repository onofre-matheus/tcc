# Procrastina Não — TCC

Trabalho de Conclusão de Curso: **"Desenvolvimento de um aplicativo para
auxiliar atividades de estudo"** (Matheus Oliveira — FACOM/UFU).

O **Procrastina Não** é um assistente de consistência nos estudos —
calendário, tarefas A/B/C, flashcards (Leitner) e captura rápida —
com arquitetura *local-first* sobre um **log de eventos append-only**.
A fundamentação vem da terapia cognitivo-comportamental para TDAH adulto
(Safren et al.). São dois clientes que leem o mesmo modelo de eventos:
uma **extensão de navegador** e uma **CLI em Go** (`pnn`).

## Estrutura do repositório

| Pasta | Conteúdo |
|---|---|
| `latex/` | Monografia em LaTeX (`main.tex`, classe `facom-ufu-abntex2`, `figuras/`) e o PDF compilado (`main.pdf`) |
| `cli/` | CLI em Go `pnn` (alias `procrastina`) — ver [`cli/README.md`](cli/README.md) |
| `extension/` | Extensão de navegador (Manifest V3): popup, side panel e o mesmo core do log de eventos |
| `spec/` | Especificação compartilhada: [`SPEC.md`](spec/SPEC.md) (modelo de eventos), [`CLI.md`](spec/CLI.md) e os vetores de conformidade (`vectors/`) |
| `scripts/` | Utilitários de apoio (ex.: `seed-demo.sh`) |

O `core/` da CLI (Go) e o da extensão (JS) implementam **o mesmo contrato** e são
validados pelo mesmo corpus de vetores em `spec/vectors/`.

## Como rodar a CLI

```bash
cd cli
make          # compila e instala pnn + alias procrastina em ~/.local/bin
make test     # go test ./... (inclui o corpus de vetores de conformidade)
pnn           # a tela "e agora?": agenda + tarefas A/B/C + revisões vencidas
```

Requer Go 1.26+. Detalhes de comandos, eventos e projeções em
[`cli/README.md`](cli/README.md).

## Como carregar a extensão

Em `chrome://extensions`, ative o **Modo de desenvolvedor** e use
**Carregar sem compactação** apontando para a pasta `extension/`.
