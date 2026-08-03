BINARY  := lealing
PKG     := ./cmd/lealing
BIN_DIR := bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

# Onde o wrapper é instalado e para onde ele aponta. O wrapper guarda estes
# caminhos absolutos, então mover o repositório exige rodar `make install`
# de novo.
INSTALL_DIR ?= $(HOME)/.local/bin
REPO_DIR    := $(abspath .)
GO_BIN      := $(shell command -v go)

# Tamanho e teclas usados por `make render`, sobrescrevíveis:
#   make render SIZE=120x34 KEYS='/git'
SIZE ?= 150x44
KEYS ?=

.PHONY: all build build-windows cross snapshot release run test bench cover lint fmt vet render tidy clean install last-version release-patch release-minor release-major

all: build

build: ## compila o binário em bin/
	@mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY) $(PKG)

build-windows: ## compila o .exe para Windows em bin/
	@mkdir -p $(BIN_DIR)
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' \
		-o $(BIN_DIR)/$(BINARY).exe $(PKG)

# O código específico de plataforma não usa build tag — o que muda é o
# processo que ele dispara —, então uma quebra só aparece no build cruzado
# quando alguém importa um pacote que não existe do outro lado.
cross: ## verifica se o código compila nas plataformas suportadas
	@for target in darwin/amd64 darwin/arm64 windows/amd64 windows/arm64 linux/amd64 linux/arm64; do \
		os=$${target%/*}; arch=$${target#*/}; \
		printf '%-16s ' $$target; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build ./... && echo ok || exit 1; \
	done

# Compila e empacota tudo o que a release publicaria, sem tocar no GitHub.
# É como conferir o .goreleaser.yaml antes de empurrar uma tag.
snapshot: ## monta os artefatos de release em dist/ sem publicar
	goreleaser release --snapshot --clean

release: ## solicita à pipeline a publicação de VERSION=vX.Y.Z
	@if [ "$(origin VERSION)" != "command line" ]; then \
		echo 'uso: make release VERSION=vX.Y.Z (ou make release-patch/release-minor/release-major)'; \
		exit 2; \
	fi
	@gh workflow run release.yml --field version='$(VERSION)'
	@echo 'pipeline de $(VERSION) acionada: gh run list --workflow release.yml'

last-version: ## mostra a última tag publicada no repositório remoto
	@git fetch --tags -q origin 2>/dev/null || true
	@git tag --sort=-v:refname | head -1

release-patch release-minor release-major: ## calcula a próxima versão a partir da última tag e publica
	@git fetch --tags -q origin 2>/dev/null || true
	@last=$$(git tag --sort=-v:refname | head -1); \
	last=$${last:-v0.0.0}; \
	ver=$${last#v}; \
	major=$$(echo $$ver | cut -d. -f1); \
	minor=$$(echo $$ver | cut -d. -f2); \
	patch=$$(echo $$ver | cut -d. -f3); \
	case $@ in \
		release-patch) patch=$$((patch+1));; \
		release-minor) minor=$$((minor+1)); patch=0;; \
		release-major) major=$$((major+1)); minor=0; patch=0;; \
	esac; \
	next="v$$major.$$minor.$$patch"; \
	echo "última versão: $$last  →  nova versão: $$next"; \
	$(MAKE) release VERSION=$$next

run: ## abre a TUI
	go run $(PKG)

install: build ## instala o wrapper em $(INSTALL_DIR) (recompila sozinho ao rodar)
	@mkdir -p $(INSTALL_DIR)
	@sed -e 's|@@REPO@@|$(REPO_DIR)|g' -e 's|@@GO@@|$(GO_BIN)|g' \
		scripts/lealing-wrapper.sh > $(INSTALL_DIR)/$(BINARY)
	@chmod +x $(INSTALL_DIR)/$(BINARY)
	@echo "instalado: $(INSTALL_DIR)/$(BINARY) → $(REPO_DIR)"
	@case ":$$PATH:" in \
		*":$(INSTALL_DIR):"*) echo "$(INSTALL_DIR) já está no PATH — rode: $(BINARY)" ;; \
		*) echo "AVISO: $(INSTALL_DIR) não está no PATH. Adicione ao ~/.zprofile:"; \
		   echo "  export PATH=\"$(INSTALL_DIR):\$$PATH\"" ;; \
	esac

uninstall: ## remove o wrapper do PATH
	@rm -f $(INSTALL_DIR)/$(BINARY)
	@echo "removido: $(INSTALL_DIR)/$(BINARY)"

test: ## roda a suíte
	go test ./...

cover: ## relatório de cobertura no navegador
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

bench: ## custo de um frame de render
	go test ./internal/adapter/inbound/tui/... -run XXX -bench . -benchmem

race: ## suíte com o detector de corrida
	go test -race ./...

vet: ## análise estática do toolchain
	go vet ./...

fmt: ## formata tudo
	gofmt -w -s .

# Renderiza um frame estático. Sem TTY o lipgloss desliga a cor, então
# forçamos o perfil para ver o resultado real em um pipe.
render: ## imprime um frame estático (SIZE=, KEYS=)
	@CLICOLOR_FORCE=1 go run $(PKG) -ephemeral -render $(SIZE) -keys '$(KEYS)'

tidy: ## sincroniza go.mod/go.sum
	go mod tidy

clean: ## remove artefatos
	rm -rf $(BIN_DIR) coverage.out

help: ## lista os alvos
	@grep -E '^[a-z-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'
