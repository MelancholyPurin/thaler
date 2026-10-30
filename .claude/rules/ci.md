# Continuous integration

GitHub Actions: `ci.yml` (jobs `changes`, `lint`, `test`, `race`, and the gate `Report Result`), `security.yml` (govulncheck), Dependabot weekly. Keep the gate's job name `Report Result`. Tool versions are pinned in the `Makefile`; do not use unpinned `@latest` tools.
