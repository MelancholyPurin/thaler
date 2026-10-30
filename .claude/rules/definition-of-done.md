# Before you call something done

1. `make check` passes (gofmt, golangci-lint, `go mod tidy` check, script tests, tests in random order). CI also runs `make test-race` and `make vuln`; run `make test-race` after touching anything concurrent (the TUI's background work).
2. New behaviour has tests. When a test guards a safety property, break the code on purpose once (a mutation) and see the test fail; a test that cannot fail proves nothing.
3. Run the real thing at least once for anything with I/O or a UI: build the binary and use a throwaway database (see `data-and-privacy.md`). For the TUI, `tmux` works.
4. User-visible behaviour is documented in `README.md` (feature text, key table, CLI table, `config.example.toml` for config keys, and the `?` help popup for keys).
5. The story is ticked `[x]` in `ROADMAP.md`.
