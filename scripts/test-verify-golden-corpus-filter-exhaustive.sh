#!/usr/bin/env bash
# Test mirror for scripts/verify-golden-corpus-filter-exhaustive.sh (#5538
# task 4). Unlike scripts/test-verify-golden-corpus-gate.sh, the script under
# test here is itself credential-free and Docker-free (`go list -deps` only),
# so this mirror runs it directly against real repo state instead of only
# asserting structural invariants.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/verify-golden-corpus-filter-exhaustive.sh"
exclusions="${repo_root}/scripts/lib/golden-corpus-filter-exclusions.txt"
workflow="${repo_root}/.github/workflows/golden-corpus-gate.yml"

fail() { printf 'test-verify-golden-corpus-filter-exhaustive: %s\n' "$*" >&2; exit 1; }

[[ -f "${script}" ]] || fail "missing ${script}"
[[ -x "${script}" ]] || fail "verify-golden-corpus-filter-exhaustive.sh must be executable"
[[ "$(wc -l <"${script}" | tr -d '[:space:]')" -lt 500 ]] \
	|| fail "verify-golden-corpus-filter-exhaustive.sh must stay under 500 lines"
bash -n "${script}" || fail "verify-golden-corpus-filter-exhaustive.sh has a syntax error"
[[ -f "${exclusions}" ]] || fail "missing exclusions file: ${exclusions}"
[[ -f "${workflow}" ]] || fail "missing workflow: ${workflow}"

command -v go >/dev/null 2>&1 || fail "go is required to run this mirror"
command -v yq >/dev/null 2>&1 || fail "yq is required to run this mirror"

pass=0
total=0
check() {
	total=$((total + 1))
	if "$@"; then
		pass=$((pass + 1))
	else
		fail "case ${total} failed: $*"
	fi
}

# Case 1: the script forbids `go build` / docker -- this is a `go list` only,
# credential-free gate. A regression here would silently make every PR pay
# for a full build just to check path coverage.
case_no_build_or_docker() {
	! rg --quiet -- 'go build|go run|docker' <(rg --invert-match -- '^\s*#' "${script}")
}
check case_no_build_or_docker

# Case 2: the exclusions file has the documented format -- every non-blank,
# non-comment line carries at least two whitespace-separated fields (a
# package path and a reason), so a malformed entry fails loudly instead of
# silently parsing to an empty reason.
case_exclusions_well_formed() {
	local line pkg reason bad=0
	while IFS= read -r line; do
		[[ -n "${line}" && "${line}" != '#'* ]] || continue
		pkg=""
		reason=""
		read -r pkg reason <<<"${line}"
		if [[ -z "${pkg}" || -z "${reason}" ]]; then
			bad=$((bad + 1))
		fi
	done <"${exclusions}"
	[[ "${bad}" -eq 0 ]]
}
check case_exclusions_well_formed

# Case 3: every exclusion path is spelled go/internal/<name> or go/cmd/<name>
# with no trailing slash or glob -- the exact granularity the script compares
# against the compiled-package set. A "go/internal/foo/**" entry here would
# never match anything the script derives from `go list -deps`.
case_exclusions_no_globs() {
	local line pkg reason bad=0
	while IFS= read -r line; do
		[[ -n "${line}" && "${line}" != '#'* ]] || continue
		pkg=""
		reason=""
		read -r pkg reason <<<"${line}"
		[[ -n "${pkg}" ]] || continue
		if [[ "${pkg}" != go/internal/* && "${pkg}" != go/cmd/* ]] || [[ "${pkg}" == *'*'* ]]; then
			bad=$((bad + 1))
		fi
	done <"${exclusions}"
	[[ "${bad}" -eq 0 ]]
}
check case_exclusions_no_globs

# Case 4: the real repo state passes -- 0 uncovered. This is the main
# regression proof: every package actually compiled into the six binaries is
# covered or excluded today.
case_real_state_passes() {
	local out
	out="$(bash "${script}" 2>&1)" || return 1
	rg --fixed-strings --quiet -- '0 uncovered' <<<"${out}"
}
check case_real_state_passes

# Case 5: deterministic -- running twice against the same inputs produces
# byte-identical stdout (the counts and classification cannot flap).
case_deterministic() {
	local first second
	first="$(bash "${script}" 2>&1)" || return 1
	second="$(bash "${script}" 2>&1)" || return 1
	[[ "${first}" == "${second}" ]]
}
check case_deterministic

# Case 6 (negative): drop one real exclusion entry (go/internal/webhook, a
# package `go list -deps` genuinely compiles into these binaries and that
# carries no workflow path) and confirm the script fails, naming exactly that
# package -- not a different one, and not merely "something failed". Proves
# the check actually classifies packages rather than always passing.
# GOLDEN_CORPUS_FILTER_EXCLUSIONS lets this case point at a scratch copy
# instead of mutating the tracked file.
case_missing_exclusion_fails_naming_it() {
	local scratch out status
	scratch="$(mktemp -t golden-corpus-filter-exclusions-test.XXXXXX)"
	rg --invert-match --fixed-strings -- 'go/internal/webhook' "${exclusions}" >"${scratch}"
	out="$(GOLDEN_CORPUS_FILTER_EXCLUSIONS="${scratch}" bash "${script}" 2>&1)"
	status=$?
	rm -f "${scratch}"
	[[ "${status}" -ne 0 ]] || return 1
	rg --fixed-strings --quiet -- 'go/internal/webhook' <<<"${out}"
}
check case_missing_exclusion_fails_naming_it

# Case 7 (negative): point at a workflow copy with one covering path deleted
# (go/internal/status/**, added by this same change) and confirm the failure
# names that package too -- proves the coverage check (not just the exclusion
# check) actually classifies packages, and exercises the "**" glob-stripping
# path_covers() logic rather than only the literal-match branch.
case_missing_workflow_path_fails_naming_it() {
	local scratch out status
	scratch="$(mktemp -t golden-corpus-filter-workflow-test.XXXXXX)"
	rg --invert-match --fixed-strings -- "- 'go/internal/status/**'" "${workflow}" >"${scratch}"
	out="$(GOLDEN_CORPUS_FILTER_WORKFLOW="${scratch}" bash "${script}" 2>&1)"
	status=$?
	rm -f "${scratch}"
	[[ "${status}" -ne 0 ]] || return 1
	rg --fixed-strings --quiet -- 'go/internal/status' <<<"${out}"
}
check case_missing_workflow_path_fails_naming_it

# Case 8: an unreadable/missing exclusions file fails with a clear message,
# not a bash "unbound variable" or a silent pass.
case_missing_exclusions_file_fails_clearly() {
	local out status
	out="$(GOLDEN_CORPUS_FILTER_EXCLUSIONS="${repo_root}/scripts/lib/does-not-exist-5538.txt" bash "${script}" 2>&1)"
	status=$?
	[[ "${status}" -ne 0 ]] || return 1
	rg --fixed-strings --quiet -- 'missing exclusions file' <<<"${out}"
}
check case_missing_exclusions_file_fails_clearly

# Case 9 (negative + convergence proof, #5877 review): the "NOT REACHABLE:"
# marker on the five search/semantic exclusions must be self-enforcing -- if
# one of them starts being imported from the six gate binaries, the checker
# must fail naming it, not silently keep classifying it as excluded forever
# (the exact class of bug #5877 found in go/internal/boundedset: an entry
# whose reason stopped being true, discovered only by a human re-reading the
# file rather than by the check itself). Blank-import
# go/internal/searchbenchrun from go/cmd/golden-corpus-gate/main.go -- the
# same binary this script derives its compiled set from -- confirm the
# failure names that package, then revert the source file and confirm PASS
# again.
case_not_reachable_becomes_reachable_fails() {
	local main_go="${repo_root}/go/cmd/golden-corpus-gate/main.go"
	local backup out status
	backup="$(mktemp -t golden-corpus-filter-main-go-backup.XXXXXX)"
	cp "${main_go}" "${backup}"

	# Insert a blank import right after the import block's opening line so
	# `go list -deps` picks up the package without touching anything else.
	awk '{print} /^import \($/ && !done {print "\t_ \"github.com/eshu-hq/eshu/go/internal/searchbenchrun\""; done=1}' \
		"${backup}" >"${main_go}"

	out="$(bash "${script}" 2>&1)"
	status=$?
	cp "${backup}" "${main_go}"
	rm -f "${backup}"

	[[ "${status}" -ne 0 ]] || return 1
	rg --fixed-strings --quiet -- 'go/internal/searchbenchrun' <<<"${out}" || return 1

	# Confirm the revert actually restored a clean, passing state -- proves
	# reachability (not some other side effect of the edit) drove the failure.
	out="$(bash "${script}" 2>&1)" || return 1
	rg --fixed-strings --quiet -- '0 uncovered' <<<"${out}"
}
check case_not_reachable_becomes_reachable_fails

printf 'test-verify-golden-corpus-filter-exhaustive: %d/%d cases pass\n' "${pass}" "${total}"
[[ "${pass}" -eq "${total}" ]] || exit 1
