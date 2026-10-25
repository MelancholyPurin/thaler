# Shortcuts for working on Thaler. Run `make` to list them.

GO        ?= go
BIN       ?= thaler
COVERFILE ?= coverage.out

.DEFAULT_GOAL := help
.PHONY: help build test test-race lint fmt check check-coverage clean

help: ## List the targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{ printf "  make %-15s %s\n", $$1, $$2 }'

build: ## Build ./thaler
	$(GO) build -o $(BIN) ./cmd/thaler

test: ## Run the tests
	$(GO) test ./...

test-race: ## Run the tests with the race detector (needs a C compiler)
	$(GO) test -race ./...

lint: ## Check formatting and run go vet, the same checks as CI
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "These files are not gofmt-formatted (run: make fmt):"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	$(GO) vet ./...

fmt: ## Format all Go files in place
	gofmt -w .

check: lint test ## Everything CI runs: lint, then test

check-coverage: ## Run the tests and print coverage per package and in total
	$(GO) test -coverprofile=$(COVERFILE) ./...
	@echo
	@$(GO) tool cover -func=$(COVERFILE) | tail -n 1

clean: ## Remove build output and the coverage profile
	rm -f $(BIN) $(COVERFILE)
