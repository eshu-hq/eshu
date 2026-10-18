#!/usr/bin/env bash
# verify-gitignore-rg-parity.sh -- every tracked file must be visible to rg.
#
# Regression gate for #7750: anchored .gitignore lines of the form
# `go/<binary>` relativize to bare basenames when ripgrep runs rooted at a
# subdirectory, silently pruning same-named source directories (3,352 tracked
# files under go/ invisible to `rg ... go/` on rg 14.1.1 at 825d9335634e, while
# `git check-ignore` correctly reported them as tracked; the count grows
# with the tree). A new `go/<binary>` line whose basename collides with a
# source directory would reintroduce the prune with exit code 0 and no
# warning. The prune needs a relativizing rg: 14.1.1 does, 15.2.0 does not
# (upstream fixed the relativization), so this gate enforces a 15.2.0 floor
# and CI pins checksum-verified 15.2.0 via
# scripts/ci/install-apt-packages.sh. The `.claude/*` ignored tree (#7771)
# pruned 32 tracked files from a `.claude/` search root on 14.1.1 with no
# working negation spelling; on the floor rg the existing negations apply
# and the root is covered like every other root.
#
# The check walks every search root where an anchored pattern can relativize:
# the repo root, every top-level directory, and every second-level directory
# under go/ (the `go/<binary>` family relativizes at root `go/`; a deeper
# `go/<sub>/<binary>` shape would relativize at root `go/<sub>/`). Depth
# boundary: a three-level anchored pattern (go/a/b/bin) would relativize at
# go/a/b/, which is never walked, so the gate would stay GREEN on that prune
# -- likewise for anchored lines in nested .gitignore files below go/*/. No
# such pattern exists today; extend the walk if one is ever added. At each
# root, every path `git ls-files` reports must also appear in
# `rg --files --hidden`. Only the tracked-but-hidden direction is compared:
# untracked files (including genuinely ignored build outputs) never fail the
# gate. The gate asserts rg/git agreement: tracked symlinks (git lists them,
# `rg --files` never does) and tracked-but-git-ignored paths (force-added
# fixtures) are excluded from the tracked side, because both tools agree
# those are hidden. Only the relativized-pattern prune -- files git shows
# and rg hides -- fails the gate.
#
# Usage:
#   scripts/verify-gitignore-rg-parity.sh
#
# Exit codes:
#   0 -- every tracked file is visible to rg from every search root.
#   1 -- at least one root hides tracked files from rg; details on stderr.
#   2 -- rg is not installed, or is below the 15.2.0 floor.
set -euo pipefail

repo_root="${ESHU_RG_PARITY_REPO_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"
cd "$repo_root"

command -v rg >/dev/null 2>&1 || {
	printf 'verify-gitignore-rg-parity: rg not found on PATH\n' >&2
	exit 2
}

# rg minimum-version floor (#7771): the parity walk below is only meaningful
# on rg >= 15.2.0, which no longer relativizes anchored parent-.gitignore
# patterns under subdirectory search roots (14.1.1 does, hiding tracked files
# with exit 0). Refusing an old rg with exit 2 names the true cause; without
# this floor, un-skipping .claude/ would turn every pre-15.2.0 rg into a
# misleading "tracked files hidden" parity RED.
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

rg_floor="15.2.0"
rg_have="$(rg --version 2>/dev/null | awk 'NR==1 {for (i=1; i<=NF; i++) if ($i ~ /^[0-9]+\.[0-9]+\.[0-9]+/) {print $i; exit}}')"
if [ -z "${rg_have:-}" ] || ! version_at_least "$rg_have" "$rg_floor"; then
	printf 'verify-gitignore-rg-parity: rg %s is below the %s floor (#7771); census searches need rg >= %s\n' "${rg_have:-unknown}" "$rg_floor" "$rg_floor" >&2
	exit 2
fi

failures=0
roots_checked=0

tracked_visible() {
	# $1 = search root relative to the repo root, or empty for the repo root.
	# Prints the tracked paths git itself does not hide: symlinks are dropped
	# (`git ls-files` lists them but `rg --files` never does, e.g. the
	# .claude/skills and .codex/skills links), then `git ls-files -c -i`
	# removes tracked-but-ignored paths (force-added fixtures such as
	# tests/fixtures/correlation_dsl/multi-dockerfile-repo/Dockerfile.test,
	# which matches `*.test`). `git check-ignore` cannot do this step: it
	# does not report tracked files (proven: same-depth untracked
	# probe2.test matches, tracked Dockerfile.test does not). What remains
	# is the set rg must agree is visible: the gate asserts rg/git
	# agreement, and only the relativized-pattern prune disagrees.
	# Tab-split keeps paths with spaces intact.
	local root="$1" all ignored
	if [ -z "$root" ]; then
		all="$(git ls-files -s | awk -F'\t' '$1 !~ /^120000/ {print $2}' | sort)"
		ignored="$(git ls-files -c -i --exclude-standard | sort)"
	else
		all="$(git ls-files -s -- "$root" | awk -F'\t' '$1 !~ /^120000/ {print $2}' | sort)"
		ignored="$(git ls-files -c -i --exclude-standard -- "$root" | sort)"
	fi
	comm -23 <(printf '%s\n' "$all") <(printf '%s\n' "$ignored")
}

check_root() {
	# $1 = search root relative to the repo root, or empty for the repo root.
	local root="$1" missing count sample label
	if [ -z "$root" ]; then
		label="."
		missing="$(comm -13 <(rg --files --hidden 2>/dev/null | sort) <(tracked_visible "" | sort))"
	else
		label="$root"
		missing="$(comm -13 <(rg --files --hidden "$root" 2>/dev/null | sort) <(tracked_visible "$root" | sort))"
	fi
	if [ -n "$missing" ]; then
		count="$(printf '%s\n' "$missing" | wc -l)"
		# awk, not head: head exits after 20 lines and SIGPIPEs printf,
		# which fails the pipeline under pipefail; awk drains stdin.
		sample="$(printf '%s\n' "$missing" | awk 'NR <= 20')"
		printf 'verify-gitignore-rg-parity: RED root=%s tracked-hidden-from-rg=%s\n' "$label" "$count" >&2
		printf '%s\n' "$sample" >&2
		failures=$((failures + 1))
	fi
	roots_checked=$((roots_checked + 1))
}

check_root ""
for d in */; do
	[ -e "$d" ] || continue
	check_root "$d"
done
for d in .*/; do
	case "$d" in
		./ | ../ | .git/) continue ;;
	esac
	[ -e "$d" ] || continue
	check_root "$d"
done
if [ -d go ]; then
	for d in go/*/; do
		[ -e "$d" ] || continue
		check_root "$d"
	done
fi

if [ "$failures" -ne 0 ]; then
	printf 'verify-gitignore-rg-parity: FAIL %s root(s) hide tracked files from rg (%s roots checked)\n' "$failures" "$roots_checked" >&2
	exit 1
fi
printf 'verify-gitignore-rg-parity: PASS every tracked file visible to rg (%s roots checked)\n' "$roots_checked"
