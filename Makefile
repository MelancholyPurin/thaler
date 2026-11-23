# Shortcuts for working on Thaler. Run `make` to list them.

GO        ?= go
# Pinned, and run through `go run` so nothing has to be installed (the first run builds it, about a minute).
GOLANGCI_LINT ?= $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
# Also pinned and run through `go run`; needs the network, since it reads the vulnerability database.
GOVULNCHECK ?= $(GO) run golang.org/x/vuln/cmd/govulncheck@v1.9.0
# Pinned like the others; it builds and publishes the release (see .goreleaser.yaml). The first run builds it.
GORELEASER ?= $(GO) run github.com/goreleaser/goreleaser/v2@v2.19.0
# Pinned too; signs the release checksums (REL-11). GoReleaser runs it by name, so it is
# installed into bin/cosign-<version>/ and put first on the PATH (the first run builds it, about a minute).
COSIGN_VERSION ?= v3.1.4
COSIGN_DIR := $(abspath bin/cosign-$(COSIGN_VERSION))
BIN       ?= thaler
# What `thaler --version` prints: the nearest tag, else the commit, and -dirty when files changed.
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null)
COVERFILE ?= coverage.out
# The total test coverage `make check` and CI require, in percent. It was 90.9% when it
# was set (ENG-7); lower it only on purpose, and say why in the pull request.
COVERAGE_FLOOR ?= 89

.DEFAULT_GOAL := help
VERSION_DATE ?= $(shell date +%F)
.PHONY: help build smoke release-check release release-verify i18n-todo i18n-check test test-race fuzz test-scripts translations-check lint tidy-check vuln fmt check check-coverage hooks clean

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

release-check: $(if $(SIGN),$(COSIGN_DIR)/cosign) ## Check .goreleaser.yaml and build the release archives into dist/ without publishing anything; SIGN=1 also signs the checksums (needs GitHub's OIDC token, so only in the Release workflow)
	$(GORELEASER) check
	PATH="$(COSIGN_DIR):$$PATH" $(GORELEASER) release --snapshot --clean $(if $(SIGN),,--skip=sign)

release: $(COSIGN_DIR)/cosign ## Publish the release of the tag at HEAD with the notes in NOTES, its checksums signed (the Release workflow does this; needs GITHUB_TOKEN, HOMEBREW_TAP_TOKEN and GitHub's OIDC token)
	@test -n "$(NOTES)" || { echo "usage: make release NOTES=notes.md"; exit 2; }
	PATH="$(COSIGN_DIR):$$PATH" $(GORELEASER) release --clean --release-notes $(NOTES)

release-verify: $(COSIGN_DIR)/cosign ## Check that dist/ (or DIR) is signed by the Release workflow and its archives match the checksums, as a download would be checked
	@COSIGN="$(COSIGN_DIR)/cosign" .github/scripts/verify-release.sh $(or $(DIR),dist)

$(COSIGN_DIR)/cosign:
	GOBIN="$(COSIGN_DIR)" $(GO) install github.com/sigstore/cosign/v3/cmd/cosign@$(COSIGN_VERSION)

i18n-todo: ## List the messages a language still has to translate, as JSON to fill in, with the English on standard error (make i18n-todo LOCALE=es)
	@$(GO) run ./cmd/i18ntodo $(LOCALE)

i18n-check: ## Check the message catalogs: missing, stale and unused messages, arguments (also part of make test)
	$(GO) test -count=1 -run 'Test_Catalogs|Test_catalogProblems|Test_usageProblems' ./internal/i18n

test: ## Run the tests, in a random order (a failure prints the seed to repeat it with -shuffle=SEED)
	$(GO) test -shuffle=on ./...

test-race: ## Run the tests with the race detector (needs a C compiler)
	$(GO) test -race -shuffle=on -timeout 30m ./...

# Each fuzz test, as package:name. `go test` runs their seeds, and the inputs that
# once failed (testdata/fuzz), with the other tests; `make fuzz` searches for more.
FUZZ_TARGETS = ./internal/ledger:Fuzz_ParseAmount ./internal/importer:Fuzz_Parse ./internal/config:Fuzz_Load ./internal/config:Fuzz_rules
FUZZTIME ?= 1m

fuzz: ## Fuzz the amount, import and config parsers, FUZZTIME each (default 1m); an input that fails is kept in the package's testdata/fuzz
	@set -e; for t in $(FUZZ_TARGETS); do \
		echo "fuzz $${t#*:} in $${t%%:*} for $(FUZZTIME)"; \
		$(GO) test -run '^$$' -fuzz "^$${t#*:}\$$" -fuzztime $(FUZZTIME) -fuzzminimizetime 5s "$${t%%:*}"; \
	done

test-scripts: ## Test the scripts CI uses (.github/scripts)
	@for t in .github/scripts/*_test.sh; do $$t || exit 1; done

translations-check: ## Fail when a translated page (README.es.md, docs/config.de.md, …) is behind its English; it says what changed
	@.github/scripts/translations.sh

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

check: lint tidy-check test-scripts translations-check check-coverage smoke ## Most of what CI runs: lint, go.mod check, translations, tests with the coverage floor, smoke test (also try test-race and vuln)

check-coverage: ## Run the tests in a random order with coverage per package, and fail when the total is under COVERAGE_FLOOR
	$(GO) test -shuffle=on -coverprofile=$(COVERFILE) ./...
	@$(GO) tool cover -func=$(COVERFILE) | .github/scripts/coverage-floor.sh $(COVERAGE_FLOOR)

hooks: ## Run the lint and the go.mod check before every git push (.githooks/pre-push; git push --no-verify skips it)
	git config core.hooksPath .githooks

clean: ## Remove build output, the release archives, the coverage profile and the installed cosign
	rm -rf $(BIN) $(COVERFILE) dist bin
