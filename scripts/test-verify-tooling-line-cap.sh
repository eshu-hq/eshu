#!/usr/bin/env bash
# Test mirror and seeded RED/GREEN proof for scripts/verify-tooling-line-cap.sh
# (issue #8221), modelled on scripts/test-verify-markdown-line-cap.sh. It runs
# the REAL CLI entry point against isolated scratch git repos, so every
# assertion proves the production script's behavior; nothing here needs
# Postgres, NornicDB or a Go build.
#
# The three directions that matter are test_planted_501_line_file_is_red,
# test_500_line_file_is_green and test_clean_real_tree_is_green: a gate that
# cannot be shown to fail on a planted violation, and to pass on the clean
# tree, is not a gate. Everything else guards the edges around them.
#
# The scripts-file-cap CI job is pinned by the sourced workflow cases below.
#
# Usage: scripts/test-verify-tooling-line-cap.sh
set -uo pipefail

script_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
verify_script="${script_root}/verify-tooling-line-cap.sh"
real_root="$(cd "${script_root}/.." && pwd)"

pass_count=0
fail_count=0
scratch_dirs=()
trap 'rm -rf "${scratch_dirs[@]:-}"' EXIT

record_pass() { pass_count=$((pass_count + 1)); printf 'PASS: %s\n' "$1"; }
record_fail() {
	fail_count=$((fail_count + 1))
	printf 'FAIL: %s\n' "$1"
	[[ -n "${2:-}" ]] && printf '  %s\n' "$2"
}

# new_scratch_repo prints the path of a throwaway git repo with an empty
# scripts/ dir. --all reads `git ls-files`, so the tree must be a real repo.
new_scratch_repo() {
	local dir
	dir="$(mktemp -d)"
	scratch_dirs+=("${dir}")
	git -C "${dir}" init -q
	git -C "${dir}" config user.email "tooling-line-cap-test@example.invalid"
	git -C "${dir}" config user.name "tooling-line-cap-test"
	mkdir -p "${dir}/scripts/lib"
	printf '%s\n' "${dir}"
}

# write_lines PATH N writes a file of exactly N newline-terminated lines.
write_lines() {
	local path="$1" n="$2" i
	mkdir -p "$(dirname "${path}")"
	: > "${path}"
	for ((i = 1; i <= n; i++)); do
		printf 'line %d\n' "${i}" >> "${path}"
	done
}

# write_lines_unterminated PATH N writes N lines, the last without a newline.
# `wc -l` reports N-1 for it; the gate must count N.
write_lines_unterminated() {
	local path="$1" n="$2" i
	mkdir -p "$(dirname "${path}")"
	: > "${path}"
	for ((i = 1; i < n; i++)); do
		printf 'line %d\n' "${i}" >> "${path}"
	done
	printf 'line %d' "${n}" >> "${path}"
}

# write_ledger REPO ROW... writes a scratch ledger (with a comment line, so the
# comment-skipping path runs every time) and prints its path. Each ROW is
# "path<TAB>count", built by the caller with a literal $'\t'.
write_ledger() {
	local repo="$1" row tsv="${1}/ledger.tsv"
	shift
	printf '# scratch ledger\n' > "${tsv}"
	for row in "$@"; do
		printf '%s\n' "${row}" >> "${tsv}"
	done
	printf '%s\n' "${tsv}"
}

commit_all() {
	git -C "$1" add -A >/dev/null 2>&1
	git -C "$1" commit -q -m "${2:-fixture}" >/dev/null 2>&1
}

# run_gate REPO TSV ARGS... runs the real CLI against a scratch tree. The base
# ref defaults to an unresolvable one so only cases that opt in run the growth
# check. Output goes to $out, the exit status to $rc.
out=""
rc=0
run_gate() {
	local repo="$1" tsv="$2"
	shift 2
	out="$(TOOLING_LINE_CAP_REPO_ROOT="${repo}" TOOLING_LINE_CAP_TSV="${tsv}" \
		TOOLING_LINE_CAP_BASE_REF="${TOOLING_LINE_CAP_BASE_REF:-nonexistent-base}" \
		bash "${verify_script}" "$@" 2>&1)"
	rc=$?
}

assert() {
	local name="$1" want_rc="$2" want_text="${3:-}"
	if [[ "${rc}" -ne "${want_rc}" ]]; then
		record_fail "${name}" "exit ${rc}, want ${want_rc}: ${out}"
	elif [[ -n "${want_text}" && "${out}" != *"${want_text}"* ]]; then
		record_fail "${name}" "output lacks '${want_text}': ${out}"
	else
		record_pass "${name}"
	fi
}

TAB=$'\t'

# shellcheck source=lib/test-verify-tooling-line-cap-workflow-cases.sh
source "${script_root}/lib/test-verify-tooling-line-cap-workflow-cases.sh"

test_planted_501_line_file_is_red() {
	local repo ledger
	repo="$(new_scratch_repo)"
	write_lines "${repo}/scripts/planted.sh" 501
	commit_all "${repo}"
	ledger="$(write_ledger "${repo}")"
	run_gate "${repo}" "${ledger}" --all
	assert "planted 501-line scripts/ file is red" 1 "scripts/planted.sh has 501 lines"
}

test_500_line_file_is_green() {
	local repo ledger
	repo="$(new_scratch_repo)"
	write_lines "${repo}/scripts/at-cap.sh" 500
	commit_all "${repo}"
	ledger="$(write_ledger "${repo}")"
	run_gate "${repo}" "${ledger}" --all
	assert "500-line scripts/ file is green" 0 "evaluated 1 file(s)"
}

test_unterminated_last_line_is_counted() {
	local repo ledger
	repo="$(new_scratch_repo)"
	write_lines_unterminated "${repo}/scripts/unterminated.py" 501
	commit_all "${repo}"
	ledger="$(write_ledger "${repo}")"
	run_gate "${repo}" "${ledger}" --all
	assert "501 lines with no final newline is red" 1 "has 501 lines"
}

test_any_extension_is_governed() {
	local repo ledger
	repo="$(new_scratch_repo)"
	write_lines "${repo}/scripts/lib/table.py" 501
	write_lines "${repo}/scripts/lib/data.tsv" 501
	commit_all "${repo}"
	ledger="$(write_ledger "${repo}")"
	run_gate "${repo}" "${ledger}" --all
	assert "over-cap .py and .tsv under scripts/lib are both red" 1 "scripts/lib/data.tsv has 501 lines"
	[[ "${out}" == *"scripts/lib/table.py has 501 lines"* ]] \
		&& record_pass "over-cap .py is named" || record_fail "over-cap .py is named" "${out}"
}

test_outside_scripts_is_ignored() {
	local repo ledger
	repo="$(new_scratch_repo)"
	write_lines "${repo}/docs/long.md" 900
	write_lines "${repo}/go/long.go" 900
	write_lines "${repo}/scripts/testdata/fixture.txt" 900
	write_lines "${repo}/scripts/.hidden/note.txt" 900
	commit_all "${repo}"
	ledger="$(write_ledger "${repo}")"
	run_gate "${repo}" "${ledger}" --all
	assert "files outside scripts/ and under testdata/hidden are ignored" 0 "evaluated 0 file(s)"
}

test_grandfathered_file_at_pin_is_green() {
	local repo ledger
	repo="$(new_scratch_repo)"
	write_lines "${repo}/scripts/old.sh" 600
	commit_all "${repo}"
	ledger="$(write_ledger "${repo}" "scripts/old.sh${TAB}600")"
	run_gate "${repo}" "${ledger}" --all
	assert "grandfathered file at its pin is green" 0
}

# A ledger row matches a file by exact path. A row for scripts/a.sh.long must
# neither grandfather scripts/a.sh nor collide with a row for scripts/a.sh.
test_ledger_path_prefix_is_exact() {
	local repo ledger
	repo="$(new_scratch_repo)"
	write_lines "${repo}/scripts/a.sh" 600
	write_lines "${repo}/scripts/a.sh.long" 700
	commit_all "${repo}"
	ledger="$(write_ledger "${repo}" "scripts/a.sh.long${TAB}700")"
	run_gate "${repo}" "${ledger}" --all
	assert "a row for scripts/a.sh.long does not grandfather scripts/a.sh" 1 "scripts/a.sh has 600 lines"
	ledger="$(write_ledger "${repo}" "scripts/a.sh${TAB}600" "scripts/a.sh.long${TAB}700")"
	run_gate "${repo}" "${ledger}" --all
	assert "rows for a path and its extension each match their own file" 0
}

test_grandfathered_file_that_grows_is_red() {
	local repo ledger
	repo="$(new_scratch_repo)"
	write_lines "${repo}/scripts/old.sh" 601
	commit_all "${repo}"
	ledger="$(write_ledger "${repo}" "scripts/old.sh${TAB}600")"
	run_gate "${repo}" "${ledger}" --all
	assert "grandfathered file that grows past its pin is red" 1 "grew from its grandfathered 600 lines to 601"
}

test_grandfathered_file_that_shrinks_notes_repin() {
	local repo ledger
	repo="$(new_scratch_repo)"
	write_lines "${repo}/scripts/old.sh" 550
	commit_all "${repo}"
	ledger="$(write_ledger "${repo}" "scripts/old.sh${TAB}600")"
	run_gate "${repo}" "${ledger}" --all
	assert "grandfathered file below its pin passes with a re-pin note" 0 "NOTE scripts/old.sh is 550 lines"
}

test_stale_row_for_file_under_cap_is_red() {
	local repo ledger
	repo="$(new_scratch_repo)"
	write_lines "${repo}/scripts/fixed.sh" 400
	commit_all "${repo}"
	ledger="$(write_ledger "${repo}" "scripts/fixed.sh${TAB}600")"
	run_gate "${repo}" "${ledger}" --all
	assert "ledger row for a file now under the cap is stale" 1 "STALE ledger row scripts/fixed.sh"
}

test_stale_row_for_missing_file_is_red() {
	local repo ledger
	repo="$(new_scratch_repo)"
	commit_all "${repo}" 2>/dev/null
	ledger="$(write_ledger "${repo}" "scripts/gone.sh${TAB}600")"
	run_gate "${repo}" "${ledger}" --all
	assert "ledger row for a missing file is stale" 1 "file no longer exists"
}

test_malformed_rows_are_red() {
	local repo ledger
	repo="$(new_scratch_repo)"
	write_lines "${repo}/scripts/old.sh" 901
	commit_all "${repo}"
	ledger="$(write_ledger "${repo}" "scripts/old.sh${TAB}0900")"
	run_gate "${repo}" "${ledger}" --all
	assert "leading-zero pin is rejected, not read as octal" 1 "MALFORMED ledger row scripts/old.sh"
	ledger="$(write_ledger "${repo}" "scripts/old.sh${TAB}nine hundred")"
	run_gate "${repo}" "${ledger}" --all
	assert "non-numeric pin is rejected" 1 "MALFORMED ledger row scripts/old.sh"
}

test_duplicate_and_invalid_rows_are_red() {
	local repo ledger
	repo="$(new_scratch_repo)"
	write_lines "${repo}/scripts/old.sh" 600
	commit_all "${repo}"
	ledger="$(write_ledger "${repo}" "scripts/old.sh${TAB}600" "scripts/old.sh${TAB}650")"
	run_gate "${repo}" "${ledger}" --all
	assert "duplicate ledger row is rejected" 1 "DUPLICATE ledger row scripts/old.sh"
	ledger="$(write_ledger "${repo}" "scripts/old.sh${TAB}500")"
	run_gate "${repo}" "${ledger}" --all
	assert "ledger pin at or below the cap is invalid" 1 "INVALID ledger row scripts/old.sh"
}

# seed_baseline_repo builds a repo whose HEAD commit carries a ledger pinning
# scripts/old.sh at 600, tags it `base`, and prints the repo path. The ledger
# lives in-tree at scripts/lib/tooling-line-cap-grandfather.tsv, which is where
# the growth check reads the baseline from.
seed_baseline_repo() {
	local repo
	repo="$(new_scratch_repo)"
	write_lines "${repo}/scripts/old.sh" 600
	write_lines "${repo}/scripts/other.sh" 700
	printf '# ledger\nscripts/old.sh\t600\nscripts/other.sh\t700\n' \
		> "${repo}/scripts/lib/tooling-line-cap-grandfather.tsv"
	commit_all "${repo}" baseline
	git -C "${repo}" tag base
	printf '%s\n' "${repo}"
}

run_growth() {
	local repo="$1"
	TOOLING_LINE_CAP_BASE_REF=base run_gate "${repo}" \
		"${repo}/scripts/lib/tooling-line-cap-grandfather.tsv" --all
}

test_new_ledger_row_is_rejected() {
	local repo
	repo="$(seed_baseline_repo)"
	write_lines "${repo}/scripts/new-big.sh" 800
	printf 'scripts/new-big.sh\t800\n' >> "${repo}/scripts/lib/tooling-line-cap-grandfather.tsv"
	commit_all "${repo}" "self-exempting row"
	run_growth "${repo}"
	assert "a change cannot add a file together with its own ledger row" 1 "NEW ledger row scripts/new-big.sh"
}

test_raised_pin_is_rejected() {
	local repo
	repo="$(seed_baseline_repo)"
	write_lines "${repo}/scripts/old.sh" 650
	sed -i.bak 's#^scripts/old.sh\t600$#scripts/old.sh\t650#' \
		"${repo}/scripts/lib/tooling-line-cap-grandfather.tsv"
	rm -f "${repo}/scripts/lib/tooling-line-cap-grandfather.tsv.bak"
	commit_all "${repo}" "grow and re-pin"
	run_growth "${repo}"
	assert "a raised pin is the same growth wearing a ledger edit" 1 "RAISED ledger pin scripts/old.sh from 600 to 650"
}

test_lowered_pin_and_removed_row_are_green() {
	local repo
	repo="$(seed_baseline_repo)"
	write_lines "${repo}/scripts/old.sh" 550
	write_lines "${repo}/scripts/other.sh" 300
	printf '# ledger\nscripts/old.sh\t550\n' \
		> "${repo}/scripts/lib/tooling-line-cap-grandfather.tsv"
	commit_all "${repo}" "shrink one, split the other"
	run_growth "${repo}"
	assert "lowering a pin and removing a row are the only legitimate edits" 0
}

test_unresolvable_base_is_note_then_red_when_required() {
	local repo ledger
	repo="$(new_scratch_repo)"
	write_lines "${repo}/scripts/ok.sh" 10
	printf '# ledger\n' > "${repo}/scripts/lib/tooling-line-cap-grandfather.tsv"
	commit_all "${repo}"
	ledger="${repo}/scripts/lib/tooling-line-cap-grandfather.tsv"
	run_gate "${repo}" "${ledger}" --all
	assert "unresolvable baseline is a NOTE locally" 0 "ledger growth not checked"
	out="$(TOOLING_LINE_CAP_REQUIRE_BASE=1 TOOLING_LINE_CAP_REPO_ROOT="${repo}" \
		TOOLING_LINE_CAP_TSV="${ledger}" TOOLING_LINE_CAP_BASE_REF=nonexistent-base \
		bash "${verify_script}" --all 2>&1)"
	rc=$?
	assert "unresolvable baseline is red under REQUIRE_BASE=1" 1 "NO BASELINE"
}

test_files_mode() {
	local repo ledger
	repo="$(new_scratch_repo)"
	write_lines "${repo}/scripts/big.sh" 501
	write_lines "${repo}/scripts/small.sh" 5
	write_lines "${repo}/docs/readme.md" 900
	ledger="$(write_ledger "${repo}")"
	run_gate "${repo}" "${ledger}" --files scripts/small.sh
	assert "--files on an under-cap file is green" 0 "evaluated 1 file(s) from 1 path(s)"
	run_gate "${repo}" "${ledger}" --files scripts/small.sh docs/readme.md
	assert "--files skips an over-cap path outside scripts/" 0 "evaluated 1 file(s) from 2 path(s)"
	run_gate "${repo}" "${ledger}" --files scripts/small.sh scripts/big.sh docs/readme.md
	assert "--files red on the over-cap path, ignoring paths outside scripts/" 1 "evaluated 2 file(s) from 3 path(s)"
	assert "--files names only the scripts/ violation" 1 "scripts/big.sh has 501 lines"
	[[ "${out}" != *"docs/readme.md"* ]] \
		&& record_pass "--files never reports the path outside scripts/" \
		|| record_fail "--files never reports the path outside scripts/" "${out}"
	ledger="$(write_ledger "${repo}" "scripts/gone.sh${TAB}600")"
	run_gate "${repo}" "${ledger}" --files
	assert "--files with no paths still verifies the ledger" 1 "STALE ledger row scripts/gone.sh"
}

test_pin_mode() {
	local repo ledger
	repo="$(new_scratch_repo)"
	write_lines "${repo}/scripts/big.sh" 777
	write_lines "${repo}/scripts/small.sh" 5
	ledger="$(write_ledger "${repo}")"
	run_gate "${repo}" "${ledger}" --pin scripts/big.sh
	assert "--pin prints the measured row" 0 "scripts/big.sh${TAB}777"
	run_gate "${repo}" "${ledger}" --pin scripts/small.sh
	assert "--pin refuses a file within the cap" 1 "needs no ledger row"
	run_gate "${repo}" "${ledger}" --pin scripts/missing.sh
	assert "--pin refuses a missing file" 1 "no such file"
	run_gate "${repo}" "${ledger}" --bogus
	assert "unknown mode exits 2" 2 "usage:"
}

# A pre-commit hook runs with GIT_DIR exported, absolute in every linked
# worktree. The CLI derives the repo root from its own location, so the gate
# must still walk the real scripts/ tree (hundreds of files) rather than
# evaluating nothing and exiting 0.
test_hook_env_git_dir_does_not_blind_the_gate() {
	local git_dir evaluated
	git_dir="$(git -C "${real_root}" rev-parse --absolute-git-dir)"
	out="$(GIT_DIR="${git_dir}" TOOLING_LINE_CAP_BASE_REF=nonexistent-base \
		bash "${verify_script}" --all 2>&1)"
	rc=$?
	evaluated="$(printf '%s\n' "${out}" | sed -n 's/.*evaluated \([0-9][0-9]*\) file(s) under scripts\/.*/\1/p')"
	if [[ "${rc}" -eq 0 && -n "${evaluated}" && "${evaluated}" -gt 100 ]]; then
		record_pass "exported GIT_DIR does not blind the gate (evaluated ${evaluated})"
	else
		record_fail "exported GIT_DIR does not blind the gate" "exit ${rc}, evaluated '${evaluated}': ${out}"
	fi
}

# The clean-tree half of the RED/GREEN pair: the real scripts/ tree and the
# committed ledger pass.
test_clean_real_tree_is_green() {
	out="$(TOOLING_LINE_CAP_BASE_REF=nonexistent-base bash "${verify_script}" --all 2>&1)"
	rc=$?
	assert "clean real tree passes with the committed ledger" 0 "evaluated"
}

# The gate's own sources must satisfy the cap it enforces.
test_gate_sources_are_under_the_cap() {
	local path bad=""
	for path in scripts/verify-tooling-line-cap.sh scripts/test-verify-tooling-line-cap.sh \
		scripts/lib/tooling-line-cap-core.sh scripts/lib/test-verify-tooling-line-cap-workflow-cases.sh; do
		[[ -f "${real_root}/${path}" ]] || { bad="${bad} ${path}(missing)"; continue; }
		if (( $(awk 'END { print NR }' "${real_root}/${path}") > 500 )); then
			bad="${bad} ${path}"
		fi
	done
	[[ -z "${bad}" ]] \
		&& record_pass "gate sources are within their own cap" \
		|| record_fail "gate sources are within their own cap" "over:${bad}"
}

main() {
	test_planted_501_line_file_is_red
	test_500_line_file_is_green
	test_unterminated_last_line_is_counted
	test_any_extension_is_governed
	test_outside_scripts_is_ignored
	test_grandfathered_file_at_pin_is_green
	test_ledger_path_prefix_is_exact
	test_grandfathered_file_that_grows_is_red
	test_grandfathered_file_that_shrinks_notes_repin
	test_stale_row_for_file_under_cap_is_red
	test_stale_row_for_missing_file_is_red
	test_malformed_rows_are_red
	test_duplicate_and_invalid_rows_are_red
	test_new_ledger_row_is_rejected
	test_raised_pin_is_rejected
	test_lowered_pin_and_removed_row_are_green
	test_unresolvable_base_is_note_then_red_when_required
	test_files_mode
	test_pin_mode
	test_hook_env_git_dir_does_not_blind_the_gate
	test_workflow_job_is_wired
	test_clean_real_tree_is_green
	test_gate_sources_are_under_the_cap
	printf '\n%d passed, %d failed\n' "${pass_count}" "${fail_count}"
	[[ "${fail_count}" -eq 0 ]]
}

main "$@"
