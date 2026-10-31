# Continuous integration

GitHub Actions: `ci.yml` (jobs `changes`, `lint`, `test` on Linux and macOS (the tests, then `make smoke`), `race`, and the gate `Report Result`; Windows is not supported, see `PLT`), `security.yml` (govulncheck), Dependabot weekly. Keep the gate's job name `Report Result`. Tool versions are pinned in the `Makefile`; do not use unpinned `@latest` tools.
