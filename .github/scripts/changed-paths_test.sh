#!/usr/bin/env bash
# Tests changed-paths.sh against throwaway git repositories.
#
# Each case names the files a change touches and what the script must answer. A
# wrong "false" would silently skip a check that matters, so the cases lean towards
# the files that must NOT be skipped.
#
# Usage: changed-paths_test.sh        (from anywhere)
set -uo pipefail

script="$(cd "$(dirname "$0")" && pwd)/changed-paths.sh"
failures=0

# A case is "name | change | want".
#   change is a space-separated list of steps:
#     +path  adds or changes a file     -path  deletes it     >old:new  renames it
#     none   changes nothing
#   Underscores in a path stand for spaces.
cases=(
	"README only | +README.md | false"
	"README and roadmap | +README.md +ROADMAP.md | false"
	"a markdown file deep in the tree | +internal/tui/NOTES.md | false"
	"a document under docs | +docs/guide.txt | false"
	"the licence | +LICENSE | false"
	"gitignore | +.gitignore | false"
	"dependabot config | +.github/dependabot.yml | false"
	"a name with spaces, markdown | +release_notes.md | false"
	"deleting the README | -README.md | false"
	"a Go file | +internal/core/new.go | true"
	"a Go file and the README | +internal/core/new.go +README.md | true"
	"a name with spaces, Go | +internal/core/my_file.go | true"
	"go.mod | +go.mod | true"
	"go.sum | +go.sum | true"
	"the Makefile | +Makefile | true"
	"the lint configuration | +.golangci.yml | true"
	"the embedded example config | +config.example.toml | true"
	"an embedded migration | +internal/core/dbmigrations/20261101_x.up.sql | true"
	"a test fixture | +internal/importer/testdata/sample.csv | true"
	"a workflow | +.github/workflows/ci.yml | true"
	"the filter script | +.github/scripts/changed-paths.sh | true"
	"a file nobody thought of | +something.unknown | true"
	"deleting a Go file | -main.go | true"
	"moving a Go file to a document | >main.go:main.md | true"
	"moving a document to a Go file | >README.md:readme.go | true"
	"nothing changed | none | true"
)

# Cases about the base itself, not the files.
base_cases=(
	"no base given||true"
	"the all-zero base of a new branch|0000000000000000000000000000000000000000|true"
	"a base that is not in the repository|1111111111111111111111111111111111111111|true"
)

run_in_repo() { # run_in_repo CHANGE -> prints the answer line, sets $base
	local change=$1
	dir=$(mktemp -d)
	(
		cd "$dir" || exit 1
		git init -q . && git config user.email t@example.com && git config user.name t
		mkdir -p internal/core
		echo base >README.md
		echo base >main.go
		git add -A && git commit -qm base
		git rev-parse HEAD >"$dir.base"
		for step in $change; do
			case "$step" in
			none) ;;
			+*)
				path=${step#+}
				path=${path//_/ }
				mkdir -p "$(dirname "$path")"
				echo "changed $RANDOM" >"$path"
				;;
			-*)
				git rm -q -- "$(echo "${step#-}" | tr _ ' ')"
				;;
			\>*)
				from=${step#>}
				from=${from%%:*}
				to=${step##*:}
				git mv -- "$from" "$to"
				;;
			esac
		done
		git add -A
		git commit -qm change --allow-empty
	)
}

check() { # check NAME GOT WANT
	if [ "$2" = "$3" ]; then
		echo "ok    $1"
	else
		echo "FAIL  $1: got go=$2, want go=$3"
		failures=$((failures + 1))
	fi
}

for c in "${cases[@]}"; do
	IFS='|' read -r name change want <<<"$c"
	name=$(echo "$name" | xargs)
	want=$(echo "$want" | xargs)
	# given
	run_in_repo "$(echo "$change" | xargs)"
	# when
	got=$(cd "$dir" && "$script" "$(cat "$dir.base")" 2>/dev/null)
	# then
	check "$name" "${got#go=}" "$want"
	rm -rf "$dir" "$dir.base"
done

for c in "${base_cases[@]}"; do
	IFS='|' read -r name base want <<<"$c"
	# given
	run_in_repo "+README.md"
	# when
	got=$(cd "$dir" && "$script" "$base" 2>/dev/null)
	# then
	check "$name" "${got#go=}" "$want"
	rm -rf "$dir" "$dir.base"
done

if [ "$failures" -ne 0 ]; then
	echo "$failures case(s) failed"
	exit 1
fi
echo "all cases passed"
