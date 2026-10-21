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
# #8673: keep fixture repos out of the host git-ai daemon's Trace2 tracking.
# The daemon keys on its Trace2 socket env; any git process that emits to it
# teaches the daemon the fixture path (fixtures and their gate runs inherit
# this env). GIT_TRACE2_EVENT=0 disables the event stream (same convention as
# test-verify-moved-file-refs.sh); PERF likewise. Teardown retries stay as
# belt-and-braces.
export GIT_TRACE2_EVENT=0 GIT_TRACE2_PERF=0
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

finish() {
	if [ "$failures" -ne 0 ]; then
		printf 'test-verify-gitignore-rg-parity: FAIL %s case(s)\n' "$failures" >&2
		exit 1
	fi
	printf 'test-verify-gitignore-rg-parity: PASS\n'
	exit 0
}

version_at_least() {
	# $1 = have (X.Y.Z), $2 = want (X.Y.Z); exit 0 iff have >= want.
	# Numeric field comparison so 15.10.0 beats 15.2.0. Anything
	# unparseable fails closed: an unknown rg cannot prove census parity.
	local have="$1" want="$2" rest h_major h_minor h_patch w_major w_minor w_patch
	rest="${have#*.}"; h_major="${have%%.*}"
	h_minor="${rest%%.*}"; h_patch="${rest#*.}"
	rest="${want#*.}"; w_major="${want%%.*}"
	w_minor="${rest%%.*}"; w_patch="${rest#*.}"
	case "$h_major$h_minor$h_patch$w_major$w_minor$w_patch" in
		'' | *[!0-9]*) return 1 ;;
	esac
	[ "$h_major" -gt "$w_major" ] && return 0
	[ "$h_major" -lt "$w_major" ] && return 1
	[ "$h_minor" -gt "$w_minor" ] && return 0
	[ "$h_minor" -lt "$w_minor" ] && return 1
	[ "$h_patch" -ge "$w_patch" ]
}

mkshim() {
	# $1 = fake `rg --version` version token (e.g. 14.1.1). Prints a bin dir
	# whose `rg` reports that version and delegates everything else to the
	# real rg, so floor cases run on any host rg.
	local dir real
	dir="$(mktemp -d)"
	printf '%s\n' "$dir" >>"$tmp_list"
	real="$(command -v rg)"
	cat >"$dir/rg" <<EOF
#!/usr/bin/env bash
if [ "\${1:-}" = "--version" ]; then
	printf 'ripgrep %s\n' "$1"
	exit 0
fi
exec "$real" "\$@"
EOF
	chmod +x "$dir/rg"
	printf '%s' "$dir"
}

# rg floor the gate enforces (#7771): the parity walk is only meaningful on
# rg >= 15.2.0, which no longer relativizes anchored parent-.gitignore
# patterns under subdirectory search roots. Below the floor the gate exits 2;
# the parity cases below SKIP there and only the floor pair runs.
rg_floor="15.2.0"
rg_have="$(rg --version 2>/dev/null | awk 'NR==1 {for (i=1; i<=NF; i++) if ($i ~ /^[0-9]+\.[0-9]+\.[0-9]+/) {print $i; exit}}')"
floor_met=0
if [ -n "${rg_have:-}" ] && version_at_least "$rg_have" "$rg_floor"; then
	floor_met=1
fi

# 8. FLOOR RED: a pre-floor rg is refused with exit 2 (the "rg unusable"
# class), naming the found version, the floor, and #7771 -- never a
# misleading parity RED about hidden files.
floor_repo="$(new_repo)"
printf 'floor\n' >"$floor_repo/file.txt"
git -C "$floor_repo" add -A
export ESHU_RG_PARITY_REPO_ROOT="$floor_repo"
shim14="$(mkshim 14.1.1)"
err="$(mktemp)"
rc="$(PATH="$shim14:$PATH" run_gate "$err")"
check "pre-floor rg exits 2" 2 "$rc"
check_contains "floor names the found version" "14.1.1" "$err"
check_contains "floor names the required version" "15.2.0" "$err"
check_contains "floor points at the issue" "#7771" "$err"
rm -f "$err"

# 9. FLOOR GREEN: a floor-version shim proceeds to the walk (exit 0 on a
# clean tree). Passes with and without the gate floor: a GREEN pin.
shim15="$(mkshim 15.2.0)"
err="$(mktemp)"
rc="$(PATH="$shim15:$PATH" run_gate "$err")"
check "floor-version shim proceeds past the floor" 0 "$rc"
rm -f "$err"

if [ "$floor_met" != "1" ]; then
	printf 'SKIP parity cases need rg >= %s, have %s\n' "$rg_floor" "${rg_have:-unknown}"
	printf 'SKIP anchored go/binary pruning a source dir is RED\n'
	printf 'SKIP bare dir negation is GREEN\n'
	printf 'SKIP deeper anchored pattern is RED\n'
	printf 'SKIP deeper negation is GREEN\n'
	printf 'SKIP ignored binary plus negation is GREEN\n'
	printf 'SKIP tracked symlink is GREEN\n'
	printf 'SKIP tracked-but-ignored file is GREEN\n'
	printf 'SKIP .claude/ tree without seed is GREEN\n'
	printf 'SKIP .rgignore seed under .claude/ is RED\n'
	printf 'SKIP seed removal under .claude/ is GREEN\n'
	finish
fi

# Capability probe: RED cases 1 and 3 need an rg that relativizes anchored
# parent-.gitignore patterns under subdirectory search roots (rg 14.1.1
# does; rg 15.2.0 does not -- upstream fixed the relativization). The gate
# enforces a 15.2.0 floor (#7771), so on any rg the gate accepts these cases
# permanently SKIP; case 11 carries the shipping-rg RED coverage instead.
# The probe asks rg directly (not via the gate) so a broken gate that always
# exits 0 cannot flip the RED cases to SKIP and mask its own regression.
probe_repo="$(new_repo)"
mkdir -p "$probe_repo/go/cmd/tool"
printf 'go/tool\n' >"$probe_repo/.gitignore"
printf 'package tool\n' >"$probe_repo/go/cmd/tool/main.go"
git -C "$probe_repo" add -A
red_supported=0
if (cd "$probe_repo" && rg --files --hidden go/ 2>/dev/null | rg -q 'go/cmd/tool/main\.go'); then
	printf 'SKIP RED cases 1+3 need relativizing rg, have %s\n' "$(rg --version | awk 'NR==1')"
else
	red_supported=1
fi

# 1. RED: the #7750 shape -- anchored `go/tool` prunes go/cmd/tool/ from a
# go/-rooted search while git still tracks it.
repo="$(new_repo)"
mkdir -p "$repo/go/cmd/tool"
printf 'go/tool\n' >"$repo/.gitignore"
printf 'package tool\n' >"$repo/go/cmd/tool/main.go"
printf 'package cmd\n' >"$repo/go/cmd/other.go"
git -C "$repo" add -A
export ESHU_RG_PARITY_REPO_ROOT="$repo"
if [ "$red_supported" = "1" ]; then
	err="$(mktemp)"
	rc="$(run_gate "$err")"
	check "anchored go/binary pruning a source dir is RED" 1 "$rc"
	check_contains "RED names the hidden file" "go/cmd/tool/main.go" "$err"
	check_contains "RED names the go/ root" "root=go/" "$err"
	rm -f "$err"
else
	printf 'SKIP anchored go/binary pruning a source dir is RED\n'
fi

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
if [ "$red_supported" = "1" ]; then
	err="$(mktemp)"
	rc="$(run_gate "$err")"
	check "deeper anchored pattern is RED" 1 "$rc"
	check_contains "RED names the go/cmd/ root" "root=go/cmd/" "$err"
	rm -f "$err"
else
	printf 'SKIP deeper anchored pattern is RED\n'
fi

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

# 10. GREEN: the .claude/ root from #7771 is walked -- the ignored-tree shape
# (.claude/* plus dir negations) with all tracked files visible.
claude_repo="$(new_repo)"
mkdir -p "$claude_repo/.claude/sub"
printf '.claude/*\n!.claude/sub/\n!.claude/sub/**\n' >"$claude_repo/.gitignore"
printf 'keep\n' >"$claude_repo/.claude/sub/keep.md"
git -C "$claude_repo" add -A
export ESHU_RG_PARITY_REPO_ROOT="$claude_repo"
err="$(mktemp)"
rc="$(run_gate "$err")"
check ".claude/ tree without seed is GREEN" 0 "$rc"
rm -f "$err"

# 11. RED: a committed .rgignore naming a tracked file hides it from rg
# while git still lists it, so the gate fires on the shipping rg itself.
# This replaces the relativization RED coverage of cases 1 and 3, which the
# 15.2.0 floor permanently SKIPs. Removing the seed returns GREEN.
printf 'keep.md\n' >"$claude_repo/.rgignore"
err="$(mktemp)"
rc="$(run_gate "$err")"
check ".rgignore seed under .claude/ is RED" 1 "$rc"
check_contains "RED names the .claude/ root" "root=.claude/" "$err"
rm -f "$err"
rm -f "$claude_repo/.rgignore"
err="$(mktemp)"
rc="$(run_gate "$err")"
check "seed removal under .claude/ is GREEN" 0 "$rc"
rm -f "$err"

finish
