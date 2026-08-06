BIN := $(HOME)/.local/bin/claude-dispatcher

.PHONY: build install vet run test lint check

build:
	go build ./...

install:
	mkdir -p $(dir $(BIN))
	go build -o $(BIN) .
	@echo "installed $(BIN) — ensure $(dir $(BIN)) is on your PATH"

vet:
	go vet ./...

test:
	go test ./... -race -count=1

lint:
	golangci-lint run
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }

check: build vet lint test

run: install
	$(BIN)
