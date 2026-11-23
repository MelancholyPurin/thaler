#!/usr/bin/env bash
# Tests translations.sh against throwaway git repositories.
#
# A wrong pass would let a translation fall behind its English without anyone
# noticing, so most cases are ways of being behind.
#
# Usage: translations_test.sh        (from anywhere)
set -uo pipefail

script="$(cd "$(dirname "$0")" && pwd)/translations.sh"
failures=0

# A case is "name | how the translations are written | then | want".
#   stamp   every translation names the hash of its English
#   then    a step after that: none, english (README.md changes), drop (README.de.md
#           is removed), unstamp (README.es.md loses its first line), other (README.es.md
#           names docs/config.md), short (README.es.md names a hash cut short)
cases=(
	"all up to date | stamp | none | pass"
	"the English changed | stamp | english | fail"
	"a translation is missing | stamp | drop | fail"
	"a translation does not say what it was made from | stamp | unstamp | fail"
	"a translation names another page | stamp | other | fail"
	"a translation names a hash cut short | stamp | short | fail"
)

hash() { git hash-object "$1" | cut -c1-12; }

for c in "${cases[@]}"; do
	IFS='|' read -r name _ step want <<<"$c"
	name=$(echo "$name" | xargs)
	step=$(echo "$step" | xargs)
	want=$(echo "$want" | xargs)
	# given
	dir=$(mktemp -d)
	(
		cd "$dir" || exit 1
		git init -q . && mkdir docs
		echo "# English" >README.md
		echo "# Config" >docs/config.md
		for src in README.md docs/config.md; do
			for lang in es de; do
				printf '<!-- translated from %s at %s -->\n# %s\n' "$src" "$(hash "$src")" "$lang" >"${src%.md}.$lang.md"
			done
		done
		case "$step" in
		english) echo "more" >>README.md ;;
		drop) rm README.de.md ;;
		unstamp) sed -i.bak 1d README.es.md && rm README.es.md.bak ;;
		other) printf '<!-- translated from docs/config.md at %s -->\n' "$(hash README.md)" >README.es.md ;;
		short) printf '<!-- translated from README.md at %s -->\n' "$(hash README.md | cut -c1-8)" >README.es.md ;;
		esac
	)
	# when
	if "$script" "$dir" >/dev/null 2>&1; then got=pass; else got=fail; fi
	# then
	if [ "$got" = "$want" ]; then
		echo "ok    $name"
	else
		echo "FAIL  $name: got $got, want $want"
		failures=$((failures + 1))
	fi
	rm -rf "$dir"
done

if [ "$failures" -ne 0 ]; then
	echo "$failures case(s) failed"
	exit 1
fi
echo "all cases passed"
