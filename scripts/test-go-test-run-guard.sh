#!/usr/bin/env bash
#
# test-go-test-run-guard.sh — TDD/self-test mirror for
# scripts/lib/go-test-run-guard.sh and scripts/go-test-run-guard.sh (#6055).
#
# This is the shared helper every `go test -run <pattern>` gate site in this
# repo now routes through, so its own proof is the load-bearing regression
# test for the whole class-(a) fix: a `-run` pattern matching FEWER tests
# than the caller expects (including the true zero-match case a rename or a
# file move produces) must fail loudly, and a pattern matching enough tests
# must still actually RUN them for real, not just list them.
#
# Credential-free, Docker-free, network-free (it does invoke the real `go`
# toolchain against this repository's own go/internal/mcp package, which is
# already required to build for any other Go gate to run).
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
lib="${repo_root}/scripts/lib/go-test-run-guard.sh"
cli="${repo_root}/scripts/go-test-run-guard.sh"
tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0
TOTAL=0

record_pass() {
	PASS=$((PASS + 1))
	TOTAL=$((TOTAL + 1))
	printf 'ok - %s\n' "$1"
}

record_fail() {
	FAIL=$((FAIL + 1))
	TOTAL=$((TOTAL + 1))
	printf 'not ok - %s\n' "$1"
	if [ -f "${tmp_root}/last-stdout" ]; then
		echo '--- stdout ---'
		head -80 "${tmp_root}/last-stdout"
	fi
}

fail() {
	printf 'test-go-test-run-guard: %s\n' "$*" >&2
	exit 1
}

# ── 1. File presence and syntax ─────────────────────────────────────────────

[[ -f "${lib}" ]] || fail "missing ${lib}"
[[ -f "${cli}" ]] || fail "missing ${cli}"
[[ -x "${cli}" ]] || fail "${cli} must be executable"
bash -n "${lib}" || fail "${lib} has a syntax error"
bash -n "${cli}" || fail "${cli} has a syntax error"

# shellcheck source=scripts/lib/go-test-run-guard.sh
. "${lib}"

# ── 2. A real match at/above the minimum actually RUNS the test ────────────
#
# `^TestReadOnlyTools$` is a real, single-match test in go/internal/mcp
# (also the mcp-schema-drift.yml ReadOnlyTools-count gate's own pin, #6055
# item a). Passing -v proves go_test_run_guard executed the test body (a
# "--- PASS: TestReadOnlyTools" line), not merely `go test -list`'s
# name-only output, which is the exact distinction this guard's pre-check
# must not be mistaken for.
set +e
(cd "${repo_root}/go" && go_test_run_guard 1 '^TestReadOnlyTools$' -- ./internal/mcp/ -v -count=1) \
	>"${tmp_root}/last-stdout" 2>&1
code=$?
set -e
if [ "${code}" -eq 0 ] && rg --fixed-strings --quiet -- '--- PASS: TestReadOnlyTools' "${tmp_root}/last-stdout"; then
	record_pass "real match at minimum runs the test for real (exit 0, PASS line present)"
else
	record_fail "real match at minimum (code=${code}, expected 0 with a PASS line)"
fi

# ── 3. Zero matches (the false-green case a rename or move produces) fails ──
#
# This is the exact BITES scenario: `^TestReadOnlyTools$` renamed or moved
# away leaves a pattern like this one matching nothing. Before #6055, a bare
# `go test -run '<pattern-matching-nothing>' ./internal/mcp/` would exit 0
# printing only `ok`. go_test_run_guard must fail instead.
set +e
(cd "${repo_root}/go" && go_test_run_guard 1 '^TestDoesNotExistAfterAMove6055$' -- ./internal/mcp/ -count=1) \
	>"${tmp_root}/last-stdout" 2>&1
code=$?
set -e
if [ "${code}" -ne 0 ] && rg --fixed-strings --quiet -- 'matched 0 test(s)' "${tmp_root}/last-stdout"; then
	record_pass "zero-match pattern fails loudly instead of exiting 0 (simulated rename/move)"
else
	record_fail "zero-match pattern (code=${code}, expected non-zero with a 'matched 0 test(s)' diagnostic)"
fi

# ── 4. Below-expected-count on a REAL pattern also fails loudly ────────────
#
# A partial regression (one of several expected tests renamed away, but not
# all) is a smaller version of case 3 and must not be masked by "at least
# one test still matched".
set +e
(cd "${repo_root}/go" && go_test_run_guard 2 '^TestReadOnlyTools$' -- ./internal/mcp/ -count=1) \
	>"${tmp_root}/last-stdout" 2>&1
code=$?
set -e
if [ "${code}" -ne 0 ] && rg --fixed-strings --quiet -- 'expected at least 2' "${tmp_root}/last-stdout"; then
	record_pass "below-expected-count on a real pattern fails loudly (matched 1, wanted 2)"
else
	record_fail "below-expected-count (code=${code}, expected non-zero naming the shortfall)"
fi

# ── 5. Missing "--" separator is a usage error, not a silent bad run ───────

set +e
(cd "${repo_root}/go" && go_test_run_guard 1 '^TestReadOnlyTools$' ./internal/mcp/ -count=1) \
	>"${tmp_root}/last-stdout" 2>&1
code=$?
set -e
if [ "${code}" -eq 2 ]; then
	record_pass "missing -- separator is rejected as a usage error"
else
	record_fail "missing -- separator (code=${code}, expected 2)"
fi

# ── 6. The standalone CLI entry point (non-bash callers) works end-to-end ──

set +e
"${cli}" 1 '^TestReadOnlyTools$' -- ./internal/mcp/ -count=1 \
	>"${tmp_root}/last-stdout" 2>&1
code=$?
set -e
if [ "${code}" -eq 0 ]; then
	record_pass "standalone CLI entry point runs the real match to green"
else
	record_fail "standalone CLI entry point (code=${code}, expected 0)"
fi

set +e
"${cli}" 1 '^TestDoesNotExistAfterAMove6055$' -- ./internal/mcp/ -count=1 \
	>"${tmp_root}/last-stdout" 2>&1
code=$?
set -e
if [ "${code}" -ne 0 ]; then
	record_pass "standalone CLI entry point fails loudly on a zero-match pattern"
else
	record_fail "standalone CLI entry point zero-match (code=${code}, expected non-zero)"
fi

printf '\ntest-go-test-run-guard: %d/%d passed\n' "${PASS}" "${TOTAL}"
if [ "${FAIL}" -gt 0 ]; then
	exit 1
fi
exit 0
