# Build e instalação do pnn (spec/CLI.md).
# `make` compila e instala em ~/.local/bin (já no PATH), com o symlink
# procrastina apontando para o mesmo binário (easter egg por argv[0]).

PREFIX ?= $(HOME)/.local
BINDIR := $(PREFIX)/bin
BIN    := pnn
ALIAS  := procrastina
PKG    := ./cmd/pnn

.PHONY: install
install: ## compila e instala pnn + alias procrastina no PATH (padrão)
	@mkdir -p $(BINDIR)
	go build -o $(BINDIR)/$(BIN) $(PKG)
	@ln -sf $(BINDIR)/$(BIN) $(BINDIR)/$(ALIAS)
	@echo "✔ instalado: $(BINDIR)/$(BIN) (alias: $(ALIAS))"
	@command -v $(BIN) >/dev/null || echo "⚠ $(BINDIR) não está no PATH — adicione: export PATH=\"$(BINDIR):\$$PATH\""

.PHONY: build
build: ## só compila o binário em ./pnn (não instala)
	go build -o $(BIN) $(PKG)

.PHONY: test
test: ## roda os testes (inclui o corpus de vetores de conformidade)
	go test ./...

.PHONY: uninstall
uninstall: ## remove pnn e o alias do PATH
	@rm -f $(BINDIR)/$(BIN) $(BINDIR)/$(ALIAS)
	@echo "✔ removido de $(BINDIR)"

.PHONY: clean
clean: ## apaga artefatos de build locais
	@rm -f $(BIN)

.PHONY: help
help: ## lista os alvos
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := install
