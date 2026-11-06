#!/usr/bin/env bash
#
# Regression guard for eshu-hq/eshu#10282: the pre-commit go-lint hook failed on
# the pristine tree while `go test` stayed green, so lanes had to commit with
# SKIP=go-lint.
#
# THE BUG THIS EXISTS TO CATCH
#
# go-sitter-forest/perl v1.9.9 does not compile as C23, and GCC 15+ on
# glibc 2.43 defaults to C23, so a cold Go build cache fails with
# "bsearch.h:72:7: error: expected identifier or '(' before '_Generic'" (#9066).
# Every developer entrypoint carries the -std=gnu17 workaround except the
# scanner arms of scripts/dev/precommit-go.sh: the source plus
# eshu_export_cgo_cflags call lived only inside the `surface` arm's subshell,
# so `lint`, `lint-all`, `fmt`, and `gosec` ran with the host default C
# dialect. The hook additionally pins GOTOOLCHAIN to go/go.mod, giving it a
# different (cold) build cache from the developer's default toolchain, so it
# always recompiled the cgo package and always hit the dialect error — while
# `go test` passed on a warm shared cache. The failure surfaced as typecheck
# diagnostics, which misdirected the first diagnosis toward golangci-lint /
# stdlib version drift; no binary change was ever involved.
#
# WHAT THIS GUARD ASSERTS, AND WHY THIS SHAPE
#
# It drives the REAL precommit-go.sh (not a copy of its logic) and asserts the
# observable that actually moves: CGO_CFLAGS is EXPORTED in the script's
# process environment, with exactly -std=gnu17, before any arm runs. The probe
# uses the `cache-paths` arm, which performs no compilation, installs no tool,
# and needs no network. `env` is a child process, so a match proves the value
# reaches scanners through inheritance rather than sitting in an unexported
# shell variable. Case 0 is the negative control: without sourcing the script
# the variable must be absent, so a pass cannot come from the harness.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/dev/precommit-go.sh"
[ -f "${script}" ] || { printf 'test-precommit-go-cgo-cflags: missing %s\n' "${script}" >&2; exit 1; }

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

failures=0
fail() {
	printf 'test-precommit-go-cgo-cflags: FAIL: %s\n' "$*" >&2
	failures=$((failures + 1))
}
pass() { printf 'test-precommit-go-cgo-cflags: ok: %s\n' "$*"; }

# probe_env prints the process environment of the real script after its
# top-level exports have run, with CGO_CFLAGS scrubbed from the input.
probe_env() {
	env -u CGO_CFLAGS bash -c "source '${script}' cache-paths >/dev/null 2>&1; env"
}

# count_matches prints how many lines of $2 match pattern $1 (rg --count
# prints nothing and exits 1 on no match, so a bare substitution would feed
# an empty string into the integer comparison below).
count_matches() {
	local n
	n="$(rg --count --no-filename -- "$1" "$2" || true)"
	printf '%s' "${n:-0}"
}

# ---------------------------------------------------------------------------
# Case 0 — negative control: the harness starts with no CGO_CFLAGS.
# ---------------------------------------------------------------------------
if env -u CGO_CFLAGS bash -c 'env' | rg --quiet -- '^CGO_CFLAGS='; then
	fail "control: CGO_CFLAGS is already exported in the test environment — a pass below could come from the harness, not the script"
else
	pass "control: CGO_CFLAGS starts unset"
fi

# ---------------------------------------------------------------------------
# Case 1 — the script exports CGO_CFLAGS with exactly -std=gnu17.
# ---------------------------------------------------------------------------
probe_env >"${tmp_dir}/probe.env"
cgo_lines="$(count_matches '^CGO_CFLAGS=' "${tmp_dir}/probe.env")"
if [ "${cgo_lines}" -ne 1 ]; then
	fail "expected exactly 1 exported CGO_CFLAGS line after running precommit-go.sh, found ${cgo_lines} — the top-level eshu_export_cgo_cflags call is missing or not exported"
else
	value="$(rg --no-line-number --no-filename -- '^CGO_CFLAGS=' "${tmp_dir}/probe.env")"
	case "${value}" in
	*-std=gnu17*)
		rest="${value//-std=gnu17/}"
		case "${rest}" in
		*-std=*)
			fail "exported ${value} carries a second -std= besides gnu17 — the C dialect must be exactly one -std=gnu17"
			;;
		*)
			pass "precommit-go.sh exports CGO_CFLAGS with -std=gnu17 to every arm"
			;;
		esac
		;;
	*)
		fail "exported ${value} has no -std=gnu17 — scanner arms compile cgo with the host default dialect (#9066)"
		;;
	esac
fi

if [ "${failures}" -ne 0 ]; then
	printf 'test-precommit-go-cgo-cflags: %d failure(s)\n' "${failures}" >&2
	exit 1
fi
printf 'test-precommit-go-cgo-cflags: all checks passed\n'
