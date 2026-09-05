#!/usr/bin/env bash
# Test mirror and BITES proof for scripts/verify-markdown-line-cap.sh
# (issue #6187), modelled directly on scripts/test-verify-dirgate.sh.
# Exercises the REAL CLI entry point (not a reimplementation of its logic)
# against isolated scratch git repos, so every assertion here proves the
# actual production script's behavior. No live runtime dependency
# (Postgres/NornicDB/Go build) -- pure filesystem + git + bash.
#
# #6187's second acceptance criterion is this file: a gate that cannot be
# shown to fail is not a gate. The three directions that matter are
# test_over_cap_file_is_red, test_at_cap_file_is_green, and
# test_grandfathered_file_that_grows_is_red; everything else guards the
# edges around them.
#
# This file is the slim driver: shared scratch-repo/assertion harness plus
# main(); the cases themselves live in scripts/lib/test-verify-markdown-line-
# cap-*-cases.sh, split by topic to keep every file under the repo's 500-line
# cap -- which this gate would be a poor advertisement for exceeding.
#
# Usage: scripts/test-verify-markdown-line-cap.sh
set -uo pipefail

script_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
verify_script="${script_root}/verify-markdown-line-cap.sh"

pass_count=0
fail_count=0

record_pass() { pass_count=$((pass_count + 1)); printf 'PASS: %s\n' "$1"; }
record_fail() {
	fail_count=$((fail_count + 1))
	printf 'FAIL: %s\n' "$1"
	[[ -n "${2:-}" ]] && printf '  %s\n' "$2"
}

# new_scratch_repo creates an isolated git repo with an empty go/ dir and
# prints its path. --all mode needs `git ls-files`, so every scratch tree is
# a real (throwaway) git repo, not just a directory.
new_scratch_repo() {
	local dir
	dir="$(mktemp -d)"
	git -C "${dir}" init -q
	git -C "${dir}" config user.email "markdown-line-cap-test@example.invalid"
	git -C "${dir}" config user.name "markdown-line-cap-test"
	mkdir -p "${dir}/go"
	printf '%s\n' "${dir}"
}

# write_md_lines PATH N writes a Markdown file with exactly N lines, each
# terminated by a newline, creating parent directories as needed.
write_md_lines() {
	local path="$1" n="$2" i
	mkdir -p "$(dirname "${path}")"
	: > "${path}"
	for ((i = 1; i <= n; i++)); do
		printf 'line %d\n' "${i}" >> "${path}"
	done
}

# write_md_lines_unterminated PATH N writes N lines whose LAST line has no
# terminating newline. `wc -l` reports N-1 for such a file and awk's NR
# reports N; the gate must use the awk count, or a file one line over the cap
# reads as exactly at it.
write_md_lines_unterminated() {
	local path="$1" n="$2" i
	mkdir -p "$(dirname "${path}")"
	: > "${path}"
	for ((i = 1; i < n; i++)); do
		printf 'line %d\n' "${i}" >> "${path}"
	done
	printf 'line %d' "${n}" >> "${path}"
}

# write_ledger REPO ROW... writes a scratch ledger (with a comment line, so
# the comment-skipping path is exercised on every run) and prints its path.
# Each ROW is "path<TAB>count" -- callers build them with a literal $'\t'.
write_ledger() {
	local repo="$1" row
	shift
	local tsv="${repo}/ledger.tsv"
	printf '# scratch markdown-line-cap ledger\n' > "${tsv}"
	for row in "$@"; do
		printf '%s\n' "${row}" >> "${tsv}"
	done
	printf '%s\n' "${tsv}"
}

# run_mdcap REPO TSV ARGS... runs the real verify-markdown-line-cap.sh
# against a scratch repo, committing its current state first (--all reads
# from git's index, not the working tree). Sets MDCAP_OUT (combined
# stdout+stderr) and MDCAP_EXIT.
run_mdcap() {
	local repo="$1" tsv="$2"
	shift 2
	git -C "${repo}" add -A >/dev/null 2>&1
	git -C "${repo}" commit -q -m "scratch fixture" --allow-empty >/dev/null 2>&1
	MDCAP_OUT="$(MARKDOWN_LINE_CAP_REPO_ROOT="${repo}" MARKDOWN_LINE_CAP_TSV="${tsv}" \
		bash "${verify_script}" "$@" 2>&1)"
	MDCAP_EXIT=$?
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

assert_not_contains() {
	local haystack="$1" needle="$2" label="$3"
	if [[ "${haystack}" != *"${needle}"* ]]; then
		record_pass "${label}"
	else
		record_fail "${label}" "expected NOT to find: ${needle}
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

# shellcheck source=lib/test-verify-markdown-line-cap-cap-cases.sh
source "${script_root}/lib/test-verify-markdown-line-cap-cap-cases.sh"
# shellcheck source=lib/test-verify-markdown-line-cap-ledger-cases.sh
source "${script_root}/lib/test-verify-markdown-line-cap-ledger-cases.sh"

# shellcheck source=lib/test-verify-markdown-line-cap-scope-cases.sh
source "${script_root}/lib/test-verify-markdown-line-cap-scope-cases.sh"

main() {
	run_markdown_scope_cases
	if [[ "${1:-}" == "--scope-only" ]]; then
		printf 'scope tests: %d passed, %d failed\n' "${pass_count}" "${fail_count}"
		[[ "${fail_count}" -eq 0 ]]
		return $?
	fi
	# The three directions #6187's acceptance criterion names, first.
	test_over_cap_file_is_red
	test_at_cap_file_is_green
	test_grandfathered_file_that_grows_is_red

	test_real_tree_is_green
	test_grandfathered_file_at_its_pin_is_green
	test_grandfathered_shrink_is_green_with_a_note
	test_unterminated_final_line_is_counted
	test_testdata_markdown_is_skipped
	test_hidden_directory_markdown_is_skipped
	test_non_markdown_file_is_skipped
	test_files_mode_catches_an_over_cap_file
	test_files_mode_reports_zero_evaluated_paths
	test_splitting_the_file_turns_it_green

	test_stale_ledger_row_for_a_missing_file_hard_fails
	test_stale_ledger_row_for_a_file_under_the_cap_hard_fails
	test_ledger_row_pinned_at_the_cap_is_rejected
	test_duplicate_ledger_row_hard_fails
	test_malformed_ledger_row_hard_fails
	test_files_mode_verifies_the_ledger_with_no_markdown_paths
	test_hook_env_git_dir_does_not_blind_the_gate
	test_pin_helper_prints_a_row
	test_pin_helper_refuses_a_file_under_the_cap
	test_unknown_mode_exits_two

	test_new_ledger_row_is_rejected
	test_raised_ledger_pin_is_rejected
	test_lowered_ledger_pin_is_accepted
	test_ledger_absent_at_baseline_is_accepted
	test_absent_baseline_ref_is_fetched
	test_unresolvable_baseline_is_red_under_require_base
	test_leading_zero_ledger_pin_is_rejected

	printf '\ntests passed: %d/%d\n' "${pass_count}" "$((pass_count + fail_count))"
	[[ "${fail_count}" -eq 0 ]]
}

main "$@"
