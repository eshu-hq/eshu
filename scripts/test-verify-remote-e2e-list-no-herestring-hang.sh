#!/usr/bin/env bash
set -euo pipefail

# Regression guard for the here-string deadlock class (#4718 / same macOS
# pipe-buffer root cause as the #5074 heredoc burn-down).
#
# Homebrew bash >= 5.1 (5.3.15 on Apple Silicon) writes a `<<<` here-string
# body to a pipe before forking the reader. macOS's pipe buffer is 512 bytes,
# so a here-string whose content lands in the (512 B, ~64 KB) window blocks the
# writer forever: 0% CPU, never returns. Linux's 64 KB pipe buffer leaves no
# such window, so CI never sees it and the heredoc-budget gate cannot either
# (it scans `<<EOF` heredocs, not `<<<` here-strings).
#
# Four remote-e2e / governance self-tests captured a verifier's multi-hundred-
# byte `--list` output into `${list_log}` and probed it with
# `rg --fixed-strings --quiet "${needle}" <<<"${list_log}"` inside a loop. Every
# iteration deadlocked under bash 5.3.15. The fix pipes the content with
# `printf '%s\n' "${list_log}" | rg ...` instead. This guard runs each self-test
# under a hard timeout and fails if any of them hangs (or otherwise does not
# pass), so a reverted `<<<` is caught on a macOS developer run rather than
# discovered by hand months later.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

die() {
	printf 'test-verify-remote-e2e-list-no-herestring-hang: %s\n' "$*" >&2
	exit 1
}

# The self-tests that captured a >512-byte `--list` into a `<<<` here-string.
self_tests=(
	test-verify-remote-e2e-component-extension.sh
	test-verify-remote-e2e-component-extension-oci.sh
	test-verify-two-team-governance-proof.sh
	test-verify-k8s-two-team-governance-proof.sh
)

# The deadlock only exists on bash >= 5.1 (the pipe-before-fork here-string
# optimization). Under bash 3.2 (stock macOS /bin/bash) the here-string cannot
# hang, so running the guard there would false-pass. Pick a bug-capable bash
# explicitly — Homebrew's, or PATH's `bash` if it is >= 5.1 — and skip with a
# clear message (never a silent pass) when only 3.2 is available. This is the
# same bash the ci-gates runner steers gate subprocesses to since #5050.
pick_bug_capable_bash() {
	local candidate major minor
	for candidate in /opt/homebrew/bin/bash /usr/local/bin/bash "$(command -v bash 2>/dev/null || true)"; do
		[[ -x "${candidate}" ]] || continue
		# BASH_VERSINFO is only exported to bash children, so ask the candidate.
		major="$("${candidate}" -c 'echo "${BASH_VERSINFO[0]}"' 2>/dev/null || echo 0)"
		minor="$("${candidate}" -c 'echo "${BASH_VERSINFO[1]}"' 2>/dev/null || echo 0)"
		if [[ "${major}" -gt 5 || ( "${major}" -eq 5 && "${minor}" -ge 1 ) ]]; then
			printf '%s' "${candidate}"
			return 0
		fi
	done
	return 1
}

target_bash="$(pick_bug_capable_bash || true)"
if [[ -z "${target_bash}" ]]; then
	printf 'SKIP: no bash >= 5.1 found; the here-string deadlock cannot reproduce under bash 3.2, so this guard has nothing to prove here\n' >&2
	exit 0
fi

# A hard timeout wrapper. Prefer coreutils timeout/gtimeout; fall back to a perl
# SIGALRM so the guard still bites on a stock macOS box without coreutils. The
# alarm timer survives exec and default-terminates the child, so a hang exits
# 128+14=142; timeout exits 124. Either means the self-test was still running at
# the deadline.
# Self-test timeout is generous (they run a verifier against several artifact
# sets); the verifier `--list` pre-flight is short, since a healthy `--list`
# returns in well under a second and a hanging one only needs to be detected.
timeout_secs=30
preflight_secs=8
timeout_tool=""
timeout_is_perl=0
if command -v timeout >/dev/null 2>&1; then
	timeout_tool="timeout"
elif command -v gtimeout >/dev/null 2>&1; then
	timeout_tool="gtimeout"
elif command -v perl >/dev/null 2>&1; then
	timeout_tool="perl"
	timeout_is_perl=1
else
	die "no timeout/gtimeout/perl available to guard the self-tests against a hang"
fi

# run_bounded <seconds> <cmd...> runs the command under a hard deadline and
# returns the command's exit code (124/137/142 signal a deadline hit).
run_bounded() {
	local secs="$1"
	shift
	if [[ "${timeout_is_perl}" -eq 1 ]]; then
		perl -e 'alarm shift; exec @ARGV or exit 127' "${secs}" "$@"
	else
		"${timeout_tool}" "${secs}" "$@"
	fi
}

is_hang_rc() {
	# 124 = coreutils timeout SIGTERM; 137 = SIGKILL; 142 = perl SIGALRM. Any of
	# them means the child was still running at the deadline.
	[[ "$1" -eq 124 || "$1" -eq 137 || "$1" -eq 142 ]]
}

failures=0
passes=0
skips=0
for name in "${self_tests[@]}"; do
	script="${repo_root}/scripts/${name}"
	[[ -f "${script}" ]] || die "missing self-test: ${script}"

	# Each self-test captures its verifier's `--list` output (verifier name is
	# the self-test name without the `test-` prefix) before the here-string
	# probe. If the verifier's own `--list` heredoc still deadlocks (the #5074
	# heredoc class in the verifier, or the #5085 expanded-size blind spot), the
	# self-test hangs at that capture, upstream of the here-string this guard
	# covers. Pre-flight the verifier so the guard attributes that hang to the
	# verifier rather than false-failing the here-string fix; it upgrades to a
	# real PASS automatically once the verifier heredoc is converted.
	verifier="${repo_root}/scripts/${name#test-}"
	if [[ -f "${verifier}" ]]; then
		vrc=0
		run_bounded "${preflight_secs}" "${target_bash}" "${verifier}" --list >/dev/null 2>&1 || vrc=$?
		if is_hang_rc "${vrc}"; then
			printf 'SKIP  %s: its verifier `--list` still hangs (verifier heredoc, #5074/#5085), upstream of the here-string — cannot exercise the fix until that lands\n' \
				"${name}" >&2
			skips=$((skips + 1))
			continue
		fi
	fi

	rc=0
	run_bounded "${timeout_secs}" "${target_bash}" "${script}" >/dev/null 2>&1 || rc=$?
	if is_hang_rc "${rc}"; then
		printf 'FAIL  %s: still running at the %ss deadline (exit %s) — a `<<<` here-string is deadlocking bash 5.1+\n' \
			"${name}" "${timeout_secs}" "${rc}" >&2
		failures=$((failures + 1))
	elif [[ "${rc}" -ne 0 ]]; then
		printf 'FAIL  %s: self-test exited %s (not a hang, but not a pass)\n' "${name}" "${rc}" >&2
		failures=$((failures + 1))
	else
		printf 'PASS  %s (completed, no here-string hang)\n' "${name}"
		passes=$((passes + 1))
	fi
done

if [[ "${failures}" -gt 0 ]]; then
	die "${failures} self-test(s) hung or failed — see above"
fi
if [[ "${passes}" -eq 0 ]]; then
	die "no self-test could exercise the here-string fix (all ${skips} skipped on a hanging verifier) — the guard proved nothing"
fi

printf 'here-string guard: %d passed, %d skipped (verifier heredoc pending), 0 failed\n' \
	"${passes}" "${skips}"
