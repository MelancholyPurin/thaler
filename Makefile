# Shortcuts for working on Thaler. Run `make` to list them.

GO        ?= go
# Pinned, and run through `go run` so nothing has to be installed (the first run builds it, about a minute).
GOLANGCI_LINT ?= $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
# Also pinned and run through `go run`; needs the network, since it reads the vulnerability database.
GOVULNCHECK ?= $(GO) run golang.org/x/vuln/cmd/govulncheck@v1.9.0
BIN       ?= thaler
# What `thaler --version` prints: the nearest tag, else the commit, and -dirty when files changed.
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null)
COVERFILE ?= coverage.out

.DEFAULT_GOAL := help
.PHONY: help build test test-race test-scripts lint tidy-check vuln fmt check check-coverage clean

help: ## List the targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{ printf "  make %-15s %s\n", $$1, $$2 }'

build: ## Build ./thaler, with its version (see VERSION)
	$(GO) build -ldflags "-X main.version=$(VERSION)" -o $(BIN) ./cmd/thaler

test: ## Run the tests, in a random order (a failure prints the seed to repeat it with -shuffle=SEED)
	$(GO) test -shuffle=on ./...

test-race: ## Run the tests with the race detector (needs a C compiler)
	$(GO) test -race -shuffle=on ./...

test-scripts: ## Test the scripts CI uses (.github/scripts)
	@for t in .github/scripts/*_test.sh; do $$t || exit 1; done

lint: ## Check formatting and run golangci-lint (see .golangci.yml), the same checks as CI
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "These files are not gofmt-formatted (run: make fmt):"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	$(GOLANGCI_LINT) run ./...

tidy-check: ## Fail if go.mod or go.sum differ from what `go mod tidy` would write
	$(GO) mod tidy -diff

vuln: ## Scan the dependencies and the standard library for known vulnerabilities
	$(GOVULNCHECK) ./...

fmt: ## Format all Go files in place
	gofmt -w .

check: lint tidy-check test-scripts test ## Most of what CI runs: lint, go.mod check, tests (also try test-race and vuln)

check-coverage: ## Run the tests and print coverage per package and in total
	$(GO) test -coverprofile=$(COVERFILE) ./...
	@echo
	@$(GO) tool cover -func=$(COVERFILE) | tail -n 1

clean: ## Remove build output and the coverage profile
	rm -f $(BIN) $(COVERFILE)
