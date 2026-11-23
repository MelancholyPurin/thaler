#!/usr/bin/env bash
# Tests the pre-push hook (.githooks/pre-push) with a stand-in for make.
#
# Usage: pre-push_test.sh        (from anywhere)
set -uo pipefail

hook="$(cd "$(dirname "$0")/../.." && pwd)/.githooks/pre-push"
failures=0
zero=0000000000000000000000000000000000000000
sha=1111111111111111111111111111111111111111

# A case is "name | refs pushed | what make does | want hook | want make called with".
# The refs are lines "LOCAL_SHA REMOTE_SHA" joined by ";"; make passes or fails.
cases=(
	"a branch | $sha $zero | passes | pass | lint tidy-check"
	"the lint fails | $sha $zero | fails | fail | lint tidy-check"
	"deleting a branch | $zero $sha | passes | pass | "
	"deleting one branch, pushing another | $zero $sha;$sha $zero | passes | pass | lint tidy-check"
	"nothing to push | | passes | pass | "
)

for c in "${cases[@]}"; do
	IFS='|' read -r name refs makes want wantArgs <<<"$c"
	name=$(echo "$name" | xargs)
	makes=$(echo "$makes" | xargs)
	want=$(echo "$want" | xargs)
	wantArgs=$(echo "$wantArgs" | xargs)
	# given
	dir=$(mktemp -d)
	status=0
	[ "$makes" = fails ] && status=1
	printf '#!/bin/sh\necho "$*" >"%s/args"\nexit %d\n' "$dir" "$status" >"$dir/make"
	chmod +x "$dir/make"
	stdin=""
	IFS=';' read -ra lines <<<"$(echo "$refs" | xargs)"
	for l in "${lines[@]}"; do
		read -r local remote <<<"$l"
		stdin+="refs/heads/x $local refs/heads/x $remote"$'\n'
	done
	# when
	if printf '%s' "$stdin" | MAKE="$dir/make" "$hook" origin https://example.com/x.git >/dev/null 2>&1; then got=pass; else got=fail; fi
	gotArgs=$(cat "$dir/args" 2>/dev/null)
	# then
	if [ "$got" = "$want" ] && [ "$gotArgs" = "$wantArgs" ]; then
		echo "ok    $name"
	else
		echo "FAIL  $name: got $got with make '$gotArgs', want $want with make '$wantArgs'"
		failures=$((failures + 1))
	fi
	rm -rf "$dir"
done

if [ "$failures" -ne 0 ]; then
	echo "$failures case(s) failed"
	exit 1
fi
echo "all cases passed"
