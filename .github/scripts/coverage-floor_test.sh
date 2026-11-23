#!/usr/bin/env bash
# Tests coverage-floor.sh with made-up `go tool cover -func` output.
#
# A wrong pass would let coverage fall without anyone noticing, so the cases lean
# towards totals just under the floor and input with no total at all.
#
# Usage: coverage-floor_test.sh        (from anywhere)
set -uo pipefail

script="$(cd "$(dirname "$0")" && pwd)/coverage-floor.sh"
failures=0

# A case is "name | total line | floor | want", want being pass or fail. An empty
# total line means the input has none.
cases=(
	"over the floor | total:	(statements)	90.9% | 89 | pass"
	"exactly the floor | total:	(statements)	89.0% | 89 | pass"
	"just under the floor | total:	(statements)	88.9% | 89 | fail"
	"far under the floor | total:	(statements)	12.0% | 89 | fail"
	"a floor with decimals, under | total:	(statements)	89.4% | 89.5 | fail"
	"a floor with decimals, over | total:	(statements)	89.6% | 89.5 | pass"
	"a hundred percent | total:	(statements)	100.0% | 89 | pass"
	"nothing covered | total:	(statements)	0.0% | 89 | fail"
	"no total line | | 89 | fail"
)

for c in "${cases[@]}"; do
	IFS='|' read -r name total floor want <<<"$c"
	name=$(echo "$name" | xargs)
	floor=$(echo "$floor" | xargs)
	want=$(echo "$want" | xargs)
	total=$(echo "$total" | sed 's/^ *//; s/ *$//')
	# given
	input="github.com/x/y/a.go:10:	F	100.0%"$'\n'"github.com/x/y/b.go:20:	total	0.0%"
	[ -n "$total" ] && input+=$'\n'"$total"
	# when
	if printf '%s\n' "$input" | "$script" "$floor" >/dev/null 2>&1; then got=pass; else got=fail; fi
	# then
	if [ "$got" = "$want" ]; then
		echo "ok    $name"
	else
		echo "FAIL  $name: got $got, want $want"
		failures=$((failures + 1))
	fi
done

if [ "$failures" -ne 0 ]; then
	echo "$failures case(s) failed"
	exit 1
fi
echo "all cases passed"
