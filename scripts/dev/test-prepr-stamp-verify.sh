#!/usr/bin/env bash
# Behavioral hermetic test for scripts/dev/prepr-stamp-verify.sh. Unlike
# scripts/dev/test-prove.sh (a static structural mirror, because prove.sh's
# real path needs Docker), this script has no such dependency -- it is pure
# bash file I/O plus `git rev-parse` -- so this test drives the REAL script
# against a synthetic, hermetic git repo and asserts on its actual exit code
# and stderr, not on its source text.
#
# What this pins (#6149 follow-up item 4): the stamp file used to write a
# single ambiguous "deferred=" field that only ever tracked ONE of the two
# ways a stamped run can validate less than "everything" -- a triggered live
# gate whose prerequisite was missing, or a forced ESHU_PREPR_SKIP_LIVE=1.
# The documentation-only fast path skipping the whole static Go lane or the
# race lane was never recorded there at all, only printed to that run's own
# terminal summary -- so a reader consulting only the stamp file later, with
# no access to that terminal output, could not tell "nothing was skipped"
# from "the field that would have said so never tracked this skip class".
# The stamp now writes two distinctly-named fields
# (live_lane_deferred=, fast_path_skipped=), and this script reports both.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
real_script="${repo_root}/scripts/dev/prepr-stamp-verify.sh"

fail() { printf 'test-prepr-stamp-verify: %s\n' "$*" >&2; exit 1; }

[[ -f "${real_script}" ]] || fail "missing ${real_script}"
[[ -x "${real_script}" ]] || fail "prepr-stamp-verify.sh must be executable"
bash -n "${real_script}" || fail "prepr-stamp-verify.sh has a syntax error"

lines="$(wc -l <"${real_script}" | tr -d '[:space:]')"
[[ "${lines}" -lt 500 ]] || fail "prepr-stamp-verify.sh must stay under 500 lines (has ${lines})"

# build_hermetic_repo creates a throwaway git repo at $1/repo with a script
# tree deep enough that repo_root (script_under_test's own
# $(dirname "$0")/../.. resolution) lands on the repo root, then commits one
# file and echoes the resulting HEAD sha.
build_hermetic_repo() {
	local base="$1" repo
	repo="${base}/repo"
	mkdir -p "${repo}/scripts/dev"
	git -C "${repo}" init -q
	git -C "${repo}" config user.email "test@example.com"
	git -C "${repo}" config user.name "test"
	printf 'placeholder\n' >"${repo}/README.md"
	git -C "${repo}" add README.md
	git -C "${repo}" commit -q -m "initial"
	git -C "${repo}" rev-parse HEAD
}

# run_stamp_verify copies script_path into a hermetic repo's own
# scripts/dev/prepr-stamp-verify.sh (so its self-relative repo_root
# resolution lands on the hermetic repo, not this real checkout), writes an
# optional stamp file for HEAD, and runs it with no stdin records -- the
# "invoked by hand" fallback path (PRE_COMMIT_TO_REF unset), which resolves
# to HEAD exactly like every case below needs.
run_stamp_verify() {
	local script_path="$1" stamp_body="${2:-}"
	local tmp head
	tmp="$(mktemp -d)"
	head="$(build_hermetic_repo "${tmp}")"
	cp "${script_path}" "${tmp}/repo/scripts/dev/prepr-stamp-verify.sh"
	chmod +x "${tmp}/repo/scripts/dev/prepr-stamp-verify.sh"
	if [[ -n "${stamp_body}" ]]; then
		mkdir -p "${tmp}/repo/.git/eshu-prepr-stamp"
		printf '%s' "${stamp_body}" >"${tmp}/repo/.git/eshu-prepr-stamp/${head}"
	fi
	local rc=0 out
	out="$(cd "${tmp}/repo" && bash scripts/dev/prepr-stamp-verify.sh </dev/null 2>&1)" || rc=$?
	rm -rf "${tmp}"
	printf '%s\n%s' "${rc}" "${out}"
}

# split_result reads run_stamp_verify's "rc\nout" combined return via a
# nameref-free pattern (portable to bash 3.2, matching this directory's other
# scripts): the first line is rc, everything after the first newline is out.
split_rc() { head -1 <<<"$1"; }
split_out() { tail -n +2 <<<"$1"; }

# ── Case A: no stamp for HEAD at all → blocked, same as before this change ──
result="$(run_stamp_verify "${real_script}" "")"
rc="$(split_rc "${result}")"; out="$(split_out "${result}")"
[[ "${rc}" == "1" ]] || fail "case A: expected exit 1 for an unstamped HEAD, got ${rc} (output: ${out})"
[[ "${out}" == *"is not stamped by make pre-pr"* ]] || fail "case A: expected the unstamped-push message, got: ${out}"

# ── Case B: stamp present, both new fields empty → passes, prints neither ──
result="$(run_stamp_verify "${real_script}" "$(printf 'sha=x\nlive_lane_deferred=\nfast_path_skipped=\n')")"
rc="$(split_rc "${result}")"; out="$(split_out "${result}")"
[[ "${rc}" == "0" ]] || fail "case B: expected exit 0 for a stamp with both fields empty, got ${rc} (output: ${out})"
[[ "${out}" != *"deferred to CI"* ]] || fail "case B: an empty live_lane_deferred must not print the deferred-to-CI message, got: ${out}"
[[ "${out}" != *"fast-path skipped"* ]] || fail "case B: an empty fast_path_skipped must not print the fast-path-skipped message, got: ${out}"

# ── Case C: live_lane_deferred non-empty, fast_path_skipped empty ──────────
# (regression pin for the pre-existing live-lane reporting, now under the
# renamed field)
result="$(run_stamp_verify "${real_script}" "$(printf 'sha=x\nlive_lane_deferred=security\nfast_path_skipped=\n')")"
rc="$(split_rc "${result}")"; out="$(split_out "${result}")"
[[ "${rc}" == "0" ]] || fail "case C: expected exit 0, got ${rc} (output: ${out})"
[[ "${out}" == *"live gates deferred to CI: security"* ]] || fail "case C: expected the live-lane-deferred message naming 'security', got: ${out}"
[[ "${out}" != *"fast-path skipped"* ]] || fail "case C: an empty fast_path_skipped must not print the fast-path-skipped message, got: ${out}"

# ── Case D: fast_path_skipped non-empty, live_lane_deferred empty ──────────
# THE new behavior this item exists to add: a documentation-only fast-path
# skip is now visible from the stamp file alone.
result="$(run_stamp_verify "${real_script}" "$(printf 'sha=x\nlive_lane_deferred=\nfast_path_skipped=race lane (Go changes)\n')")"
rc="$(split_rc "${result}")"; out="$(split_out "${result}")"
[[ "${rc}" == "0" ]] || fail "case D: expected exit 0, got ${rc} (output: ${out})"
[[ "${out}" == *"fast-path skipped locally: race lane (Go changes)"* ]] \
	|| fail "case D: expected the fast-path-skipped message naming the race lane, got: ${out}"
[[ "${out}" != *"deferred to CI"* ]] || fail "case D: an empty live_lane_deferred must not print the deferred-to-CI message, got: ${out}"


# ── Writer-side pin: scripts/dev/pre-pr.sh ──────────────────────────────────
# pre-pr.sh itself needs a live Docker/toolchain environment to run for real
# (the same reason scripts/dev/test-prove.sh mirrors prove.sh statically
# rather than executing it), so its half of this contract is pinned
# structurally: the stamp it writes must carry both renamed/new fields, and
# both silent-skip sites this item exists to close must feed
# fast_path_skipped.
pre_pr_script="${repo_root}/scripts/dev/pre-pr.sh"
[[ -f "${pre_pr_script}" ]] || fail "missing ${pre_pr_script}"
require_prepr() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${pre_pr_script}" || fail "missing ${label} (pre-pr.sh): ${needle}"
}
forbid_prepr() {
	local label="$1" needle="$2"
	if rg --fixed-strings --quiet -- "${needle}" "${pre_pr_script}"; then
		fail "must not contain ${label} (pre-pr.sh): ${needle}"
	fi
}
require_prepr "fast_path_skipped array declared" "fast_path_skipped=()"
require_prepr "stamp write uses the renamed live-lane field, not the old ambiguous one" \
	"printf 'sha=%s\\nlive_lane_deferred=%s\\nfast_path_skipped=%s\\n' \\"
forbid_prepr "the old ambiguous single-field stamp format must not survive" "printf 'sha=%s\\ndeferred=%s\\n'"
# Both silent-skip sites (#6149 follow-up item 4: "two more skip classes are
# also silent") must feed the array the stamp now writes, not just print to
# the terminal-only results[] summary.
require_prepr "fast-lane step-skip loop records into fast_path_skipped" 'fast_path_skipped+=("${pre_pr_skip_name}")'
require_prepr "race-lane fast-path skip records into fast_path_skipped" 'fast_path_skipped+=("race lane (Go changes)")'
# Both loops must still ALSO print to the terminal results[] summary — this
# item adds a second, persisted record; it must not remove the first.
require_prepr "fast-lane step-skip loop still prints to the terminal summary" \
	'results+=("SKIP  ${pre_pr_skip_name} (documentation-only fast path)")'
require_prepr "race-lane fast-path skip still prints to the terminal summary" \
	'results+=("SKIP  race lane (Go changes) (documentation-only fast path)")'

printf 'test-prepr-stamp-verify: pass\n'
