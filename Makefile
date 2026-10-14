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
	@echo "✓ Instalado em $(HOME)/.local/bin/agy-swarm"

test:
	go test -v ./...

test-race:
	go test -v -race ./...

clean:
	rm -rf bin/ .worktrees/
