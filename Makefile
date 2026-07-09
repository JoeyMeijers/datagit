# datagit — build, test, and development tasks.
#
# Common targets:
#   make build      compile ./bin/dgit
#   make test       run all tests
#   make check      fmt + vet + test (what CI should run)
#   make install    install into $(go env GOPATH)/bin
#   make cover      test with coverage report
#   make clean      remove build artifacts

BINARY  := dgit
BIN_DIR := bin
PKG     := ./...
MAIN    := ./cmd/dgit

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

# mattn/go-sqlite3 requires cgo (see ADR-0004).
export CGO_ENABLED := 1

.DEFAULT_GOAL := build

.PHONY: build
build: ## Compile the binary into ./bin
	go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY) $(MAIN)

.PHONY: install
install: ## Install into GOPATH/bin
	go install $(LDFLAGS) $(MAIN)

.PHONY: test
test: ## Run all tests
	go test $(PKG)

.PHONY: cover
cover: ## Run tests with coverage summary and HTML report
	go test -coverprofile=coverage.out $(PKG)
	go tool cover -func=coverage.out | tail -1
	go tool cover -html=coverage.out -o coverage.html
	@echo "open coverage.html for details"

.PHONY: race
race: ## Run tests with the race detector
	go test -race $(PKG)

.PHONY: fmt
fmt: ## Format all Go files
	gofmt -w .

.PHONY: fmt-check
fmt-check: ## Fail if any file is unformatted
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "unformatted:"; echo "$$out"; exit 1; fi

.PHONY: vet
vet: ## Run static analysis
	go vet $(PKG)

.PHONY: check
check: fmt-check vet test ## fmt-check + vet + test (CI entry point)

.PHONY: tidy
tidy: ## Sync go.mod / go.sum
	go mod tidy

.PHONY: run
run: build ## Build then run (pass args via ARGS="...")
	$(BIN_DIR)/$(BINARY) $(ARGS)

.PHONY: fixtures
fixtures: ## Regenerate Parquet test fixtures (requires python3 + pyarrow)
	cd internal/format/testdata && python3 gen_fixtures.py

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) coverage.out coverage.html

.PHONY: help
help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'
