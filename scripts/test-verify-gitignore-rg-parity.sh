#!/usr/bin/env bash
#
# test-verify-gitignore-rg-parity.sh -- mirror test for
# verify-gitignore-rg-parity.sh.
#
# Reproduces the #7750 miss (an anchored `go/<binary>` line pruning a
# same-named source dir from subdir-rooted rg) in throwaway git repos, and
# pins the exclusions (symlinks, tracked-but-git-ignored files) so the gate
# cannot be quietly widened into a legacy-tree blocker.
set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
gate="${script_dir}/verify-gitignore-rg-parity.sh"
failures=0

tmp_list="$(mktemp)"
cleanup() {
	local d
	while IFS= read -r d; do
		[ -n "$d" ] && rm -rf "$d"
	done <"$tmp_list"
	rm -f "$tmp_list"
}
trap cleanup EXIT

check() {
	# $1 = case name, $2 = expected exit (0|1), $3 = actual exit
	if [ "$2" != "$3" ]; then
		printf 'FAIL %s: exit=%s want=%s\n' "$1" "$3" "$2" >&2
		failures=$((failures + 1))
	else
		printf 'ok   %s\n' "$1"
	fi
}

check_contains() {
	# $1 = case name, $2 = needle, $3 = haystack file
	if rg -q --fixed-strings "$2" "$3"; then
		printf 'ok   %s\n' "$1"
	else
		printf 'FAIL %s: stderr lacks %s\n' "$1" "$2" >&2
		failures=$((failures + 1))
	fi
}

new_repo() {
	local dir
	dir="$(mktemp -d)"
	printf '%s\n' "$dir" >>"$tmp_list"
	git -C "$dir" init -q
	git -C "$dir" config user.email test@example.test
	git -C "$dir" config user.name "Test"
	git -C "$dir" commit -q --allow-empty -m init
	printf '%s\n' "$dir"
}

run_gate() {
	# $1 = stderr capture file; ESHU_RG_PARITY_REPO_ROOT must be exported.
	local rc
	set +e
	bash "$gate" >/dev/null 2>"$1"
	rc=$?
	set -e
	printf '%s' "$rc"
}

# 1. RED: the #7750 shape -- anchored `go/tool` prunes go/cmd/tool/ from a
# go/-rooted search while git still tracks it.
repo="$(new_repo)"
mkdir -p "$repo/go/cmd/tool"
printf 'go/tool\n' >"$repo/.gitignore"
printf 'package tool\n' >"$repo/go/cmd/tool/main.go"
printf 'package cmd\n' >"$repo/go/cmd/other.go"
git -C "$repo" add -A
export ESHU_RG_PARITY_REPO_ROOT="$repo"
err="$(mktemp)"
rc="$(run_gate "$err")"
check "anchored go/binary pruning a source dir is RED" 1 "$rc"
check_contains "RED names the hidden file" "go/cmd/tool/main.go" "$err"
check_contains "RED names the go/ root" "root=go/" "$err"
rm -f "$err"

# 2. GREEN: the bare dir negation re-includes the source dir.
printf 'go/tool\n!tool/\n' >"$repo/.gitignore"
git -C "$repo" add -A
err="$(mktemp)"
rc="$(run_gate "$err")"
check "bare dir negation is GREEN" 0 "$rc"
rm -f "$err"

# 3. RED: the deeper shape -- `go/cmd/tool` relativizes at root go/cmd/.
repo3="$(new_repo)"
mkdir -p "$repo3/go/cmd/other/tool"
printf 'go/cmd/tool\n' >"$repo3/.gitignore"
printf 'package tool\n' >"$repo3/go/cmd/other/tool/deep.go"
git -C "$repo3" add -A
export ESHU_RG_PARITY_REPO_ROOT="$repo3"
err="$(mktemp)"
rc="$(run_gate "$err")"
check "deeper anchored pattern is RED" 1 "$rc"
check_contains "RED names the go/cmd/ root" "root=go/cmd/" "$err"
rm -f "$err"

# 4. GREEN: the deeper shape is fixable the same way.
printf 'go/cmd/tool\n!tool/\n' >"$repo3/.gitignore"
git -C "$repo3" add -A
err="$(mktemp)"
rc="$(run_gate "$err")"
check "deeper negation is GREEN" 0 "$rc"
rm -f "$err"

# 5. GREEN: a genuinely ignored binary alongside the negation stays ignored
# and never fails the gate.
printf 'binary\n' >"$repo/go/tool"
export ESHU_RG_PARITY_REPO_ROOT="$repo"
if git -C "$repo" check-ignore -q go/tool; then
	printf 'ok   ignored binary stays ignored by git\n'
else
	printf 'FAIL ignored binary stays ignored by git\n' >&2
	failures=$((failures + 1))
fi
err="$(mktemp)"
rc="$(run_gate "$err")"
check "ignored binary plus negation is GREEN" 0 "$rc"
rm -f "$err"

# 6. GREEN: tracked symlinks are excluded (rg --files never lists them).
repo6="$(new_repo)"
mkdir -p "$repo6/hooks"
printf 'hook\n' >"$repo6/hooks/run.sh"
ln -s ../hooks "$repo6/link-to-hooks"
git -C "$repo6" add -A
export ESHU_RG_PARITY_REPO_ROOT="$repo6"
err="$(mktemp)"
rc="$(run_gate "$err")"
check "tracked symlink is GREEN" 0 "$rc"
rm -f "$err"

# 7. GREEN: tracked-but-git-ignored files are excluded (both tools agree
# they are hidden; the Dockerfile.test precedent).
repo7="$(new_repo)"
printf '*.test\n' >"$repo7/.gitignore"
printf 'fixture\n' >"$repo7/fixture.docker.test"
printf 'src\n' >"$repo7/src.txt"
git -C "$repo7" add -A
git -C "$repo7" add -f fixture.docker.test
export ESHU_RG_PARITY_REPO_ROOT="$repo7"
err="$(mktemp)"
rc="$(run_gate "$err")"
check "tracked-but-ignored file is GREEN" 0 "$rc"
rm -f "$err"

if [ "$failures" -ne 0 ]; then
	printf 'test-verify-gitignore-rg-parity: FAIL %s case(s)\n' "$failures" >&2
	exit 1
fi
printf 'test-verify-gitignore-rg-parity: PASS\n'
