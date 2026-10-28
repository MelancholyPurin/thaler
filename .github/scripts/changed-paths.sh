#!/usr/bin/env bash
# Says whether the Go checks (lint, tests, vulnerability scan) have to run for a change.
#
# Usage: changed-paths.sh BASE   (run inside a clone that has BASE and HEAD)
# Prints "go=true" or "go=false" on stdout, for $GITHUB_OUTPUT, and the reason on stderr.
#
# It is an allowlist: the checks are skipped only when EVERY changed file is one that
# cannot affect the Go build. Any other file, including one nobody thought of, runs
# them. That includes files that are not .go but are still compiled in (the embedded
# config.example.toml and the migrations), the Makefile, the lint configuration and the
# workflows themselves. Whenever the answer cannot be worked out, it is "true".
set -euo pipefail

base="${1:-}"

answer() { # answer VALUE REASON
	echo "go=$1"
	echo "go=$1: $2" >&2
	exit 0
}

# Files that cannot change what the Go checks see.
skippable() {
	case "$1" in
	*.md | LICENSE | .gitignore | .github/dependabot.yml | docs/*) return 0 ;;
	esac
	return 1
}

zero="0000000000000000000000000000000000000000"
if [ -z "$base" ] || [ "$base" = "$zero" ]; then
	answer true "no base to compare with (a schedule, a manual run or a new branch), so everything runs"
fi
if ! git cat-file -e "${base}^{commit}" 2>/dev/null; then
	answer true "the base commit ${base} is not available, so everything runs"
fi

changed=0
while IFS= read -r -d '' file; do
	changed=$((changed + 1))
	if ! skippable "$file"; then
		answer true "$file needs the checks"
	fi
done < <(git diff --name-only --no-renames -z "$base" HEAD)

if [ "$changed" -eq 0 ]; then
	answer true "no changed files found, so everything runs"
fi
answer false "only documentation and similar files changed ($changed)"
