#!/usr/bin/env bash
# Fails when the total test coverage is under a floor.
#
# Usage: go tool cover -func=coverage.out | coverage-floor.sh FLOOR
# FLOOR is a percentage (89 or 89.5). It reads the "total:" line that `go tool cover
# -func` ends with; without one there is nothing to judge, which fails too.
set -euo pipefail

if [ $# -ne 1 ]; then
	echo "usage: go tool cover -func=FILE | coverage-floor.sh FLOOR" >&2
	exit 2
fi
floor="$1"

total="$(awk '$1 == "total:" { sub(/%$/, "", $NF); print $NF }')"
if [ -z "$total" ]; then
	echo "coverage: no total in the input, so it cannot be compared with the floor of ${floor}%" >&2
	exit 1
fi
if awk -v t="$total" -v f="$floor" 'BEGIN { exit !(t + 0 < f + 0) }'; then
	echo "coverage: ${total}% is under the floor of ${floor}%: test what you added, or lower COVERAGE_FLOOR in the Makefile and say why" >&2
	exit 1
fi
echo "coverage: ${total}% (floor ${floor}%)"
