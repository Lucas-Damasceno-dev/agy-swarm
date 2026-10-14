.PHONY: all build install test clean

BINARY_NAME=bin/agy-swarm

all: build

build:
	@mkdir -p bin
	go build -ldflags="-s -w" -o $(BINARY_NAME) ./cmd/agy-swarm
	@echo "✓ Compilado em $(BINARY_NAME)"

install: build
	@mkdir -p $(HOME)/.local/bin
	cp $(BINARY_NAME) $(HOME)/.local/bin/agy-swarm
	@if [ -d "/home/lucas/.local/bin" ] && [ "$(HOME)" != "/home/lucas" ]; then \
		mkdir -p /home/lucas/.local/bin && cp $(BINARY_NAME) /home/lucas/.local/bin/agy-swarm; \
	fi
	@echo "✓ Instalado em $(HOME)/.local/bin/agy-swarm"

test:
	go test -v ./...

test-race:
	go test -v -race ./...

clean:
	rm -rf bin/ .worktrees/

archive: clean
	@git archive --format=zip --output=agy-swarm-src.zip HEAD
	@echo "✓ Código-fonte limpo exportado em agy-swarm-src.zip"

