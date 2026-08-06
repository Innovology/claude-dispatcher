BIN := $(HOME)/.local/bin/claude-dispatcher

.PHONY: build install vet run

build:
	go build ./...

install:
	mkdir -p $(dir $(BIN))
	go build -o $(BIN) .
	@echo "installed $(BIN) — ensure $(dir $(BIN)) is on your PATH"

vet:
	go vet ./...

run: install
	$(BIN)
