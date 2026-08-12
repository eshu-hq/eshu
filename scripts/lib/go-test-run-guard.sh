#!/usr/bin/env bash
#
# go-test-run-guard.sh — shared helper guarding a non-recursive `go test -run
# <pattern>` pin against the "zero matched tests exits 0" false green (#6055).
#
# `go test -run PATTERN ./pkg` prints `ok  <pkg>  <time>` and exits 0 whether
# PATTERN matched every test it was written for or matched NOTHING at all — a
# test rename or a file move that carries the named test away leaves the pin
# looking exactly like a real pass. go_test_run_guard makes the zero-match (or
# below-expected-count) case fail loudly by counting matches with
# `go test -list` BEFORE running them for real.
#
# Sourced by: scripts/verify-replay-coverage-gate.sh,
# scripts/verify-hosted-governance-proof.sh,
# scripts/verify-ask-eshu-local-proof.sh,
# scripts/verify-hosted-governance-remote-compose-proof.sh,
# scripts/verify-query-plan-profile.sh, scripts/verify-query-plan-regression.sh.
# Also wrapped by scripts/go-test-run-guard.sh, the standalone CLI entry point
# for callers that cannot source a bash function (specs/ci-gates.v1.yaml
# `local.command` strings, GitHub Actions `run:` steps).
#
# Registered as a trigger of the gates that source it AND have a registry
# entry -- today query-plan-regression and replay-coverage-gate (via
# authz-scoped-route-tests) -- mirroring
# scripts/lib/telemetry-coverage-row-check.sh's registration note, so a change
# here re-runs them. The other sourcing scripts
# (verify-hosted-governance-proof.sh, verify-ask-eshu-local-proof.sh,
# verify-hosted-governance-remote-compose-proof.sh, verify-query-plan-profile.sh)
# have no entry in
# specs/ci-gates.v1.yaml and no workflow reference: they are operator scripts
# run by hand, not CI gates, so there is nothing to register them against. If
# one of them ever becomes a gate, add this file to its triggers then.

# go_test_run_guard <min_matches> <run_pattern> -- <go-test-args...>
#
# <go-test-args...> is everything `go test` needs EXCEPT `-run <pattern>` --
# packages first, then flags (e.g. `-tags queryplan_profile_live`,
# `-count=1`). The function inserts `-list <pattern>` for the pre-check and
# `-run <pattern>` for the real run, and must be called from a shell that has
# already `cd`ed to the Go module root and exported any env vars the real
# test run needs (both pre-existing conventions at every call site).
#
# Returns the real `go test -run` invocation's exit status on success, 1 with
# a diagnostic naming the pattern/package/observed-vs-expected count when the
# pre-check finds fewer than min_matches tests, or 2 on a usage error.
go_test_run_guard() {
	local min_matches="$1"
	local pattern="$2"
	shift 2
	if [[ "${1:-}" != "--" ]]; then
		printf 'go_test_run_guard: usage: go_test_run_guard <min_matches> <pattern> -- <go-test-args...>\n' >&2
		return 2
	fi
	shift

	local list_output
	if ! list_output="$(go test -list "${pattern}" "$@" 2>&1)"; then
		printf 'go_test_run_guard: "go test -list %s %s" failed:\n%s\n' "${pattern}" "$*" "${list_output}" >&2
		return 1
	fi

	# Count matched test names from `go test -list` output without a `<<<`
	# here-string feeding a while-read loop (Homebrew bash 5.3.15 deadlocks on
	# that construct past a byte threshold on this repo's dev machines) — write
	# the captured output to a temp file and read from that instead.
	# No `trap ... RETURN` for the cleanup: bash traps are process-global, not
	# function-scoped, so a RETURN trap set here would keep firing on the
	# CALLER's returns after this function exits, referencing a ${list_tmp}
	# that is out of scope by then — an unbound-variable abort under `set -u`.
	# It would also clobber any RETURN trap the caller had already installed.
	# scripts/verify-docs-refs.sh:70-71 documents this same pitfall. Every exit
	# path below removes the file explicitly instead.
	local list_tmp
	list_tmp="$(mktemp)"
	printf '%s\n' "${list_output}" >"${list_tmp}"

	# `go test -list` prints one matched identifier per line, followed by one
	# `ok  <pkg>  <time-or-status>` line per listed package (even when that
	# package matched zero tests) — the exact zero-match signal this guard
	# exists to catch: a pattern matching nothing produces ONLY `ok` lines, and
	# `go test -run` on that same pattern would exit 0 having run nothing.
	# Count identifier lines POSITIVELY. Skipping known noise instead was wrong:
	# a package with no test files at all prints "?   <pkg>  [no test files]",
	# which is neither blank nor `ok`-prefixed, so it was counted as a match --
	# a min_matches=1 caller passed after its only test file moved away, which is
	# the exact false green this guard exists to prevent. Anything that is not a
	# recognised test identifier is now not counted, so a new summary shape from
	# a future toolchain fails closed rather than inflating the count.
	local matched=0
	local line
	while IFS= read -r line; do
		case "${line}" in
			Test*|Benchmark*|Fuzz*|Example*) matched=$((matched + 1)) ;;
		esac
	done <"${list_tmp}"
	rm -f "${list_tmp}"

	if [[ "${matched}" -lt "${min_matches}" ]]; then
		printf 'go_test_run_guard: -run %s matched %d test(s) in "go test %s", expected at least %d. A rename or file move likely broke this pin — re-verify the pattern names real tests and update the expected count.\n' \
			"${pattern}" "${matched}" "$*" "${min_matches}" >&2
		return 1
	fi

	go test -run "${pattern}" "$@"
}
