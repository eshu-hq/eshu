#!/usr/bin/env bash
# Test mirror and BITES proof for scripts/verify-dirgate.sh (issue #6054).
# Exercises the REAL CLI entry point (not a reimplementation of its logic)
# against isolated scratch git repos, so every assertion here proves the
# actual production script's behavior. No live runtime dependency
# (Postgres/NornicDB/Go build) -- pure filesystem + git + bash.
#
# This file is the slim driver: shared scratch-repo/assertion harness plus
# main(); the test cases themselves live in scripts/lib/test-verify-dirgate-
# *-cases.sh, split by topic to keep every file comfortably under the
# repo's 500-line cap (see generator-script-discipline for the pattern).
# Each case file's functions rely on the harness below (new_scratch_repo,
# run_dirgate, assert_*, record_pass/record_fail) and on dirgate-core.sh's
# functions (e.g. dirgate_digest), both already sourced by the time this
# file sources the case files.
#
# Usage: scripts/test-verify-dirgate.sh
set -uo pipefail

script_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
verify_script="${script_root}/verify-dirgate.sh"
# shellcheck source=lib/dirgate-core.sh
source "${script_root}/lib/dirgate-core.sh"

pass_count=0
fail_count=0

record_pass() { pass_count=$((pass_count + 1)); printf 'PASS: %s\n' "$1"; }
record_fail() {
	fail_count=$((fail_count + 1))
	printf 'FAIL: %s\n' "$1"
	[[ -n "${2:-}" ]] && printf '  %s\n' "$2"
}

# new_scratch_repo creates an isolated git repo with an empty go/ dir and
# prints its path. --all mode needs `git ls-files`, so every scratch tree
# is a real (throwaway) git repo, not just a directory.
new_scratch_repo() {
	local dir
	dir="$(mktemp -d)"
	git -C "${dir}" init -q
	git -C "${dir}" config user.email "dirgate-test@example.invalid"
	git -C "${dir}" config user.name "dirgate-test"
	mkdir -p "${dir}/go"
	printf '%s\n' "${dir}"
}

# write_numbered_files DIR N writes N distinct qualifying .go files.
write_numbered_files() {
	local dir="$1" n="$2" i
	mkdir -p "${dir}"
	for ((i = 0; i < n; i++)); do
		printf 'package fixture\n' > "${dir}/file$(printf '%04d' "${i}").go"
	done
}

# run_dirgate REPO ARGS... runs the real verify-dirgate.sh against a
# scratch repo built by new_scratch_repo, committing its current state
# first (--all reads from git's index, not the working tree). Sets
# DIRGATE_OUT (combined stdout+stderr) and DIRGATE_EXIT.
run_dirgate() {
	local repo="$1"
	shift
	git -C "${repo}" add -A >/dev/null 2>&1
	git -C "${repo}" commit -q -m "scratch fixture" --allow-empty >/dev/null 2>&1
	DIRGATE_OUT="$(DIRGATE_REPO_ROOT="${repo}" DIRGATE_GO_DIR="${repo}/go" \
		DIRGATE_GRANDFATHER_TSV="${DIRGATE_GRANDFATHER_TSV_OVERRIDE:-${script_root}/lib/dirgate-grandfather.tsv}" \
		DIRGATE_NAMING_EXEMPT_TSV="${DIRGATE_NAMING_EXEMPT_TSV_OVERRIDE:-${script_root}/lib/dirgate-naming-exempt.tsv}" \
		bash "${verify_script}" "$@" 2>&1)"
	DIRGATE_EXIT=$?
}

assert_contains() {
	local haystack="$1" needle="$2" label="$3"
	if [[ "${haystack}" == *"${needle}"* ]]; then
		record_pass "${label}"
	else
		record_fail "${label}" "expected to find: ${needle}
got: ${haystack}"
	fi
}

assert_exit() {
	local got="$1" want="$2" label="$3"
	if [[ "${got}" -eq "${want}" ]]; then
		record_pass "${label}"
	else
		record_fail "${label}" "exit ${got}, want ${want}"
	fi
}

empty_grandfather_tsv() {
	local dir="$1"
	printf '# empty scratch grandfather ledger\n' > "${dir}/grandfather.tsv"
	printf '%s\n' "${dir}/grandfather.tsv"
}

empty_naming_exempt_tsv() {
	local dir="$1"
	printf '# empty scratch naming-exempt ledger\n' > "${dir}/naming-exempt.tsv"
	printf '%s\n' "${dir}/naming-exempt.tsv"
}

# shellcheck source=lib/test-verify-dirgate-cap-naming-cases.sh
source "${script_root}/lib/test-verify-dirgate-cap-naming-cases.sh"
# shellcheck source=lib/test-verify-dirgate-grandfather-cases.sh
source "${script_root}/lib/test-verify-dirgate-grandfather-cases.sh"
# shellcheck source=lib/test-verify-dirgate-naming-exempt-cases.sh
source "${script_root}/lib/test-verify-dirgate-naming-exempt-cases.sh"
# shellcheck source=lib/test-verify-dirgate-misc-cases.sh
source "${script_root}/lib/test-verify-dirgate-misc-cases.sh"

main() {
	test_real_tree_is_green
	test_cap_violation_red_and_exits_named
	test_naming_violation_red_names_subpackage
	test_nolint_with_justification_turns_cap_green
	test_bare_nolint_is_rejected
	test_splitting_turns_cap_green
	test_moving_the_file_turns_naming_green
	test_grandfathered_directory
	test_grandfathered_cap_nolint_is_refused
	test_grandfathered_directory_swap_at_same_count_fails
	test_naming_exempt_new_violation_below_pinned_count_is_red
	test_naming_exempt_pinned_file_stays_green
	test_naming_exempt_stale_row_hard_fails
	test_naming_exempt_stale_row_does_not_cover_a_different_file
	test_removable_grandfather_note
	test_qualifying_files_ignores_directory_named_dot_go
	test_naming_subpackages_ignores_directory_named_dot_go
	test_grandfathered_shrink_requires_repin
	test_grandfathered_shrink_below_cap_needs_no_repin
	test_digest_helper

	printf '\ntests passed: %d/%d\n' "${pass_count}" "$((pass_count + fail_count))"
	[[ "${fail_count}" -eq 0 ]]
}

main
