# Continuous integration

GitHub Actions: `ci.yml` (jobs `changes`, `lint`, `test` on Linux and macOS, `race`, and the gate `Report Result`; `test-windows` runs too but is not in the gate yet, see `PLT-5`), `security.yml` (govulncheck), Dependabot weekly. Keep the gate's job name `Report Result`. Tool versions are pinned in the `Makefile`; do not use unpinned `@latest` tools.
