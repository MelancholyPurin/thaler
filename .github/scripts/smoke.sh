#!/usr/bin/env bash
# Smoke test: runs a built binary through the paths that must never break, on
# throwaway databases, and fails at the first one that does.
#
# It checks what the unit tests cannot see from outside: that the binary starts and
# says its version, that data survives a backup and a restore (plain and encrypted),
# that the interface opens and closes, and, when the binary of the previous release
# is given, that a database it wrote opens in this one with the same content, and
# that its backups restore here.
#
# Usage: smoke.sh BINARY [PREVIOUS_BINARY]     (make smoke does this, see the Makefile)
#
# If a deliberate change of the output of `balance` or `list` makes the upgrade
# step fail, that step compares output: change what it compares, not the step.
set -euo pipefail

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
	echo "usage: smoke.sh BINARY [PREVIOUS_BINARY]" >&2
	exit 2
fi
absolute() { (cd "$(dirname "$1")" && printf '%s/%s' "$(pwd)" "$(basename "$1")"); }
new="$(absolute "$1")"
previous=""
[ $# -eq 2 ] && previous="$(absolute "$2")"

root="$(cd "$(dirname "$0")/../.." && pwd)"
csv="$root/internal/importer/testdata/sparkasse_camt_sample.csv"
work="$(mktemp -d "${TMPDIR:-/tmp}/thaler-smoke.XXXXXX")"
trap 'rm -rf "$work"' EXIT
: >"$work/config.toml"
printf 'smoke-passphrase\n' >"$work/passphrase"

steps=0
ok() {
	printf 'ok   %s\n' "$1"
	steps=$((steps + 1))
}
fail() {
	printf 'FAIL %s\n' "$*" >&2
	exit 1
}

# th BINARY DATABASE ARGS...: the binary against a database and the empty config.
th() {
	local bin="$1" db="$2"
	shift 2
	"$bin" --db "$db" --config "$work/config.toml" "$@"
}

# seed BINARY DATABASE: invented data in two accounts, by hand and by import.
seed() {
	th "$1" "$2" cli account add Assets:Bank -c EUR >/dev/null
	th "$1" "$2" cli account add Assets:Savings -c EUR >/dev/null
	th "$1" "$2" cli import --account Assets:Bank "$csv" >/dev/null
	th "$1" "$2" cli add -a Assets:Bank --amount=-12.50 -t Expenses:Groceries -p Smoke -d 2026-10-01 >/dev/null
	th "$1" "$2" cli transfer --from Assets:Bank --to Assets:Savings --amount 50 -d 2026-10-02 >/dev/null
}

# snapshot BINARY DATABASE: what the database holds, as the commands show it.
snapshot() {
	th "$1" "$2" cli balance --all
	th "$1" "$2" cli list
}

# same WHAT WANT GOT: fails with the difference unless the two files are equal.
same() {
	if ! diff -u "$2" "$3" >"$work/diff"; then
		cat "$work/diff" >&2
		fail "$1"
	fi
}

# --- the binary itself -------------------------------------------------------

version="$("$new" --version)" || fail "--version does not run"
case "$version" in
thaler\ ?*) ok "--version says: $version" ;;
*) fail "--version printed '$version', not 'thaler VERSION'" ;;
esac

if "$new" </dev/null >"$work/out" 2>&1; then
	fail "the interface started without a terminal"
fi
grep -q "needs a terminal" "$work/out" || fail "without a terminal it said: $(cat "$work/out")"
ok "refuses to start the interface without a terminal"

# --- data, ledger and backups ------------------------------------------------

db="$work/main/thaler.db"
mkdir -p "$work/main"
seed "$new" "$db"
snapshot "$new" "$db" >"$work/before"
[ -s "$work/before" ] || fail "nothing to compare: the seeded database shows no data"
th "$new" "$db" cli check >/dev/null || fail "the ledger check fails on a fresh database"
ok "creates accounts, imports, adds, transfers; the ledger check passes"

th "$new" "$db" cli backup >/dev/null
plain="$(ls "$work"/main/backups/thaler-*.db | head -n 1)"
th "$new" "$db" cli add -a Assets:Bank --amount=-1 -t Expenses:Smoke -d 2026-10-03 >/dev/null
snapshot "$new" "$db" >"$work/changed"
if diff -q "$work/before" "$work/changed" >/dev/null; then
	fail "adding a transaction changed nothing, so a restore could prove nothing"
fi
th "$new" "$db" cli restore "$plain" >/dev/null
snapshot "$new" "$db" >"$work/after"
same "a restored backup is not the database that was backed up" "$work/before" "$work/after"
ls "$work"/main/backups/replaced/* >/dev/null 2>&1 || fail "the restore did not keep the database it replaced"
ok "a backup restores to what was backed up, and the replaced database is kept"

th "$new" "$db" cli backup --passphrase-file "$work/passphrase" >/dev/null
secret="$(ls "$work"/main/backups/thaler-*.age | head -n 1)"
if grep -aq "SQLite format" "$secret"; then
	fail "the encrypted backup holds a plain SQLite header"
fi
th "$new" "$db" cli add -a Assets:Bank --amount=-2 -t Expenses:Smoke -d 2026-10-04 >/dev/null
th "$new" "$db" cli restore --passphrase-file "$work/passphrase" "$secret" >/dev/null
snapshot "$new" "$db" >"$work/after-encrypted"
same "a restored encrypted backup is not the database that was backed up" "$work/before" "$work/after-encrypted"
ok "an encrypted backup restores to what was backed up"

# --- the interface -----------------------------------------------------------

if command -v tmux >/dev/null 2>&1; then
	session="thaler-smoke-$$"
	THALER_DB="$db" tmux new-session -d -s "$session" -x 100 -y 30 "'$new' --config '$work/config.toml'; echo \$? >'$work/tui-exit'"
	shown=""
	for _ in $(seq 1 50); do
		shown="$(tmux capture-pane -p -t "$session" 2>/dev/null || true)"
		case "$shown" in *Accounts*) break ;; esac
		sleep 0.1
	done
	case "$shown" in
	*Accounts*) ;;
	*)
		tmux kill-session -t "$session" 2>/dev/null || true
		fail "the interface did not show its panels: $shown"
		;;
	esac
	tmux send-keys -t "$session" q
	for _ in $(seq 1 50); do
		[ -f "$work/tui-exit" ] && break
		sleep 0.1
	done
	tmux kill-session -t "$session" 2>/dev/null || true
	[ "$(cat "$work/tui-exit" 2>/dev/null || echo missing)" = "0" ] || fail "the interface did not quit cleanly on q"
	ok "the interface opens, shows its panels and quits on q"
else
	echo "skip the interface (tmux is not installed)"
fi

# --- upgrading from the previous release -------------------------------------

if [ -n "$previous" ]; then
	old="$work/old/thaler.db"
	mkdir -p "$work/old"
	seed "$previous" "$old"
	snapshot "$previous" "$old" >"$work/old-before"
	th "$previous" "$old" cli backup >/dev/null
	oldbackup="$(ls "$work"/old/backups/thaler-*.db | head -n 1)"

	snapshot "$new" "$old" >"$work/old-after"
	same "a database of the previous release shows different data in this one" "$work/old-before" "$work/old-after"
	th "$new" "$old" cli check >/dev/null || fail "the ledger check fails on the upgraded database"
	ok "a database of the previous release opens here with the same data"

	restored="$work/restored/thaler.db"
	mkdir -p "$work/restored"
	th "$new" "$restored" cli restore "$oldbackup" >/dev/null
	snapshot "$new" "$restored" >"$work/restored-after"
	same "a backup of the previous release restores to different data" "$work/old-before" "$work/restored-after"
	ok "a backup of the previous release restores here"
else
	echo "skip the upgrade (no previous release given)"
fi

printf 'smoke: %d steps passed\n' "$steps"
