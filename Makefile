.PHONY: all build install test test-v test-race test-cover cover vet fmt clean help release-check release-snapshot release integration-test

BINARY := crypt
MODULE := github.com/veertuinc/crypt
GORELEASER_CONFIG := packaging/goreleaser.yaml

TAG     := $(shell git describe --exact-match --tags HEAD 2>/dev/null)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
VERSION ?= $(if $(TAG),$(TAG),dev)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X $(MODULE)/internal/meta.version=$(VERSION) \
           -X $(MODULE)/internal/meta.commit=$(COMMIT) \
           -X $(MODULE)/internal/meta.date=$(DATE)

all: build

build: ## Build ./crypt with version metadata
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

install: ## Install crypt onto $$GOPATH/bin or $$GOBIN
	go install -ldflags "$(LDFLAGS)" .

test: ## Run unit tests
	go test ./...

integration-test: build ## Run live Anka integration tests (requires base VM)
	./scripts/integration-test.sh

test-v: ## Run unit tests (verbose)
	go test -v ./...

test-race: ## Run unit tests with the race detector
	go test -race -count=1 ./...

test-cover: ## Run unit tests with coverage summary
	go test -cover ./...

cover: ## Write cover.out and open an HTML coverage report
	go test -coverprofile=cover.out ./...
	go tool cover -html=cover.out -o cover.html

vet: ## Run go vet
	go vet ./...

fmt: ## Format Go source with gofmt
	gofmt -w .

clean: ## Remove build artifacts
	rm -f $(BINARY) cover.out cover.html
	rm -rf dist/

release-check: ## Validate GoReleaser config
	goreleaser check --config $(GORELEASER_CONFIG)

release-snapshot: ## Dry-run a snapshot release (no publish)
	goreleaser release --snapshot --clean --config $(GORELEASER_CONFIG)

release: ## Cut a tagged release (requires git tag on HEAD)
	goreleaser release --clean --config $(GORELEASER_CONFIG)

help: ## Show this help
	@grep -E '^[a-zA-Z0-9_-]+:.*##' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
