#!/usr/bin/env bash
# Fails when a translated page was not translated from its English page as it is now.
#
# Each translation (README.es.md, docs/config.de.md) starts with a line that names the
# English it was made from by its git hash:
#
#   <!-- translated from README.md at 1a2b3c4d5e6f -->
#
# When the English changes, its hash does, and this fails until the translation is
# brought up to date and that line names the new hash. `git diff OLD NEW` on the two
# hashes shows what changed in the English, once the new English is staged or committed. A missing translation fails too.
#
# Usage: translations.sh [ROOT]     (default: the repository this script is in)
set -euo pipefail

root="${1:-$(cd "$(dirname "$0")/../.." && pwd)}"
cd "$root"

# The English pages that are translated, and the languages they are translated into.
sources=(README.md docs/config.md)
languages=(es de)

failures=0
fail() {
	echo "FAIL  $*"
	failures=$((failures + 1))
}

for src in "${sources[@]}"; do
	if [ ! -f "$src" ]; then
		fail "$src is missing"
		continue
	fi
	now="$(git hash-object "$src" | cut -c1-12)"
	for lang in "${languages[@]}"; do
		page="${src%.md}.$lang.md"
		if [ ! -f "$page" ]; then
			fail "$page is missing: translate $src into it"
			continue
		fi
		first="$(head -n 1 "$page")"
		was="$(printf '%s\n' "$first" | sed -n "s|^<!-- translated from $src at \([0-9a-f]\{12\}\) -->\$|\1|p")"
		if [ -z "$was" ]; then
			fail "$page does not start with <!-- translated from $src at $now -->"
		elif [ "$was" != "$now" ]; then
			fail "$page was translated from an older $src: bring it up to date (once $src is staged or committed, git diff $was $now shows what changed), then make its first line <!-- translated from $src at $now -->"
		else
			echo "ok    $page"
		fi
	done
done

if [ "$failures" -ne 0 ]; then
	echo "$failures translation(s) behind their English"
	exit 1
fi
