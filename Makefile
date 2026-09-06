BIN     := hunk
PKG     := ./...
BIN_DIR := bin

# Where `go install` puts the binary. GOBIN wins when it is set; otherwise Go
# uses GOPATH/bin. Asking the toolchain keeps this right on every OS.
GO_BIN := $(shell go env GOBIN)
ifeq ($(GO_BIN),)
GO_BIN := $(shell go env GOPATH)/bin
endif

# Args passed to `make run`, e.g. `make run ARGS="old.txt new.txt"`.
ARGS ?=

.DEFAULT_GOAL := help

## build: compile the binary into ./bin
.PHONY: build
build:
	go build -o $(BIN_DIR)/$(BIN) .

## run: build and run against the current working tree (or ARGS)
.PHONY: run
run:
	go run . $(ARGS)

## install: install hunk onto your PATH — works on any OS
.PHONY: install
install:
	go install .
	@echo "installed: $(GO_BIN)/$(BIN)"
	@echo "add $(GO_BIN) to your PATH to run $(BIN) by name"

## test: run the test suite
.PHONY: test
test:
	go test $(PKG)

## test-race: run the tests with the race detector
.PHONY: test-race
test-race:
	go test -race $(PKG)

## cover: run the tests and open a coverage report
.PHONY: cover
cover:
	go test -coverprofile=coverage.out $(PKG)
	go tool cover -html=coverage.out

## fmt: format the code
.PHONY: fmt
fmt:
	gofmt -w .

## vet: run go vet
.PHONY: vet
vet:
	go vet $(PKG)

## lint: run golangci-lint — see golangci-lint.run to install
.PHONY: lint
lint:
	golangci-lint run

## tidy: sync go.mod / go.sum
.PHONY: tidy
tidy:
	go mod tidy

## hooks: install the pre-commit hook (gofmt + lint before every commit)
.PHONY: hooks
hooks:
	git config core.hooksPath githooks

## check: fmt, vet, and test — run before pushing
.PHONY: check
check: fmt vet test

## clean: remove build output
.PHONY: clean
clean:
	rm -rf $(BIN_DIR) coverage.out

## help: list the available targets
.PHONY: help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | awk -F ': ' '{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
