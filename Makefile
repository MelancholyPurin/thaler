# Shortcuts for working on Thaler. Run `make` to list them.

GO        ?= go
# Pinned, and run through `go run` so nothing has to be installed (the first run builds it, about a minute).
GOLANGCI_LINT ?= $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
# Also pinned and run through `go run`; needs the network, since it reads the vulnerability database.
GOVULNCHECK ?= $(GO) run golang.org/x/vuln/cmd/govulncheck@v1.9.0
# Pinned like the others; it builds and publishes the release (see .goreleaser.yaml). The first run builds it.
GORELEASER ?= $(GO) run github.com/goreleaser/goreleaser/v2@v2.19.0
BIN       ?= thaler
# What `thaler --version` prints: the nearest tag, else the commit, and -dirty when files changed.
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null)
COVERFILE ?= coverage.out

.DEFAULT_GOAL := help
VERSION_DATE ?= $(shell date +%F)
.PHONY: help build smoke release-check release i18n-todo test test-race test-scripts lint tidy-check vuln fmt check check-coverage clean

help: ## List the targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{ printf "  make %-15s %s\n", $$1, $$2 }'

build: ## Build ./thaler, with its version (see VERSION)
	$(GO) build -ldflags "-X main.version=$(VERSION)" -o $(BIN) ./cmd/thaler

release-notes: ## Print the release notes of the commits in RANGE (RANGE=v0.2.0..HEAD VERSION=v0.3.0; ROW=1 prints the roadmap line)
	@$(GO) run ./cmd/releasenotes $(if $(ROW),-row) -version $(VERSION) -date $(VERSION_DATE) $(RANGE)

smoke: ## Run a built binary through the paths that must not break; FROM=<tag or commit> also checks that a database of that release opens in this build
	@dir="$$(mktemp -d)" && trap 'rm -rf "$$dir"' EXIT && \
	$(GO) build -o "$$dir/thaler" ./cmd/thaler && \
	if [ -n "$(FROM)" ]; then \
		mkdir "$$dir/src" && git archive "$(FROM)" | tar -x -C "$$dir/src" && \
		(cd "$$dir/src" && $(GO) build -o "$$dir/previous" ./cmd/thaler) && \
		.github/scripts/smoke.sh "$$dir/thaler" "$$dir/previous"; \
	else \
		.github/scripts/smoke.sh "$$dir/thaler"; \
	fi

release-check: ## Check .goreleaser.yaml and build the release archives into dist/ without publishing anything
	$(GORELEASER) check
	$(GORELEASER) release --snapshot --clean

release: ## Publish the release of the tag at HEAD with the notes in NOTES (the Release workflow does this; needs GITHUB_TOKEN)
	@test -n "$(NOTES)" || { echo "usage: make release NOTES=notes.md"; exit 2; }
	$(GORELEASER) release --clean --release-notes $(NOTES)

i18n-todo: ## List the messages a language still has to translate, as JSON to fill in, with the English on standard error (make i18n-todo LOCALE=es)
	@$(GO) run ./cmd/i18ntodo $(LOCALE)

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

check: lint tidy-check test-scripts test smoke ## Most of what CI runs: lint, go.mod check, tests, smoke test (also try test-race and vuln)

check-coverage: ## Run the tests and print coverage per package and in total
	$(GO) test -coverprofile=$(COVERFILE) ./...
	@echo
	@$(GO) tool cover -func=$(COVERFILE) | tail -n 1

clean: ## Remove build output, the release archives and the coverage profile
	rm -rf $(BIN) $(COVERFILE) dist
