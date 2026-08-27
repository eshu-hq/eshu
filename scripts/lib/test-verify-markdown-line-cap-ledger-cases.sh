#!/usr/bin/env bash
# Ledger-integrity and CLI cases for
# scripts/test-verify-markdown-line-cap.sh (issue #6187): the hard fails that
# stop a grandfather row from rotting into a permanent exemption, plus the
# --pin helper and argument handling. Sourced by that driver, which provides
# new_scratch_repo, write_md_lines, write_ledger, run_mdcap and the
# assert_*/record_* helpers. Not runnable on its own.
#
# Sibling of scripts/lib/test-verify-dirgate-naming-exempt-cases.sh, which
# pins the same stale-row rule for the dirgate naming ledger.

# A row whose file was moved, renamed, or deleted must be removed by the
# change that did it. Left behind, it silently pre-authorises an over-cap
# file at that path for whoever recreates it.
test_stale_ledger_row_for_a_missing_file_hard_fails() {
	local repo tsv
	repo="$(new_scratch_repo)"
	tsv="$(write_ledger "${repo}" "go/internal/gone/README.md"$'\t'"900")"
	run_mdcap "${repo}" "${tsv}" --all
	assert_exit "${MDCAP_EXIT}" 1 "ledger row for a missing file hard-fails"
	assert_contains "${MDCAP_OUT}" "STALE ledger row go/internal/gone/README.md -- file no longer exists" \
		"stale-row finding names the path and the reason"
	rm -rf "${repo}"
}

# The #6187 exit path: someone splits a grandfathered file. The row is now
# dead weight and must go in the same change.
test_stale_ledger_row_for_a_file_under_the_cap_hard_fails() {
	local repo tsv
	repo="$(new_scratch_repo)"
	write_md_lines "${repo}/go/internal/thing/README.md" 400
	tsv="$(write_ledger "${repo}" "go/internal/thing/README.md"$'\t'"900")"
	run_mdcap "${repo}" "${tsv}" --all
	assert_exit "${MDCAP_EXIT}" 1 "ledger row for a now-compliant file hard-fails"
	assert_contains "${MDCAP_OUT}" "the file is now 400 lines and no longer needs grandfathering" \
		"the finding tells the author to delete the row"
	rm -rf "${repo}"
}

# A pin at or below the cap grandfathers nothing -- the file would pass
# without it. Accepting one would let a row be written for a compliant file
# and then quietly authorise growth up to the cap boundary it was pinned at.
test_ledger_row_pinned_at_the_cap_is_rejected() {
	local repo tsv
	repo="$(new_scratch_repo)"
	write_md_lines "${repo}/go/internal/thing/README.md" 400
	tsv="$(write_ledger "${repo}" "go/internal/thing/README.md"$'\t'"500")"
	run_mdcap "${repo}" "${tsv}" --all
	assert_exit "${MDCAP_EXIT}" 1 "a row pinned at the cap is rejected"
	assert_contains "${MDCAP_OUT}" "INVALID ledger row go/internal/thing/README.md pinned at 500" \
		"the finding names the invalid pin"
	rm -rf "${repo}"
}

# mdcap_grandfather_lookup returns the FIRST matching row, so a second row
# for the same path is dead text that a reader would reasonably believe is
# in force. Fail rather than shadow it.
test_duplicate_ledger_row_hard_fails() {
	local repo tsv
	repo="$(new_scratch_repo)"
	write_md_lines "${repo}/go/internal/thing/README.md" 901
	tsv="$(write_ledger "${repo}" \
		"go/internal/thing/README.md"$'\t'"900" \
		"go/internal/thing/README.md"$'\t'"2000")"
	run_mdcap "${repo}" "${tsv}" --all
	assert_exit "${MDCAP_EXIT}" 1 "a duplicate row hard-fails"
	assert_contains "${MDCAP_OUT}" "DUPLICATE ledger row go/internal/thing/README.md" \
		"the finding names the duplicated path"
	rm -rf "${repo}"
}

test_malformed_ledger_row_hard_fails() {
	local repo tsv
	repo="$(new_scratch_repo)"
	write_md_lines "${repo}/go/internal/thing/README.md" 900
	tsv="$(write_ledger "${repo}" "go/internal/thing/README.md"$'\t'"nine hundred")"
	run_mdcap "${repo}" "${tsv}" --all
	assert_exit "${MDCAP_EXIT}" 1 "a non-numeric pin hard-fails"
	assert_contains "${MDCAP_OUT}" "MALFORMED ledger row go/internal/thing/README.md" \
		"the finding names the malformed row"
	rm -rf "${repo}"
}

# The pre-commit hook fires on an edit to the ledger itself, and such a
# commit passes zero go/**.md paths. Without the unconditional ledger check
# in --files mode that run would evaluate 0 files, exit 0, and read exactly
# like a pass -- so a bad row could be committed through the hook the gate
# owns.
test_files_mode_verifies_the_ledger_with_no_markdown_paths() {
	local repo tsv
	repo="$(new_scratch_repo)"
	tsv="$(write_ledger "${repo}" "go/internal/gone/README.md"$'\t'"900")"
	run_mdcap "${repo}" "${tsv}" --files scripts/lib/markdown-line-cap-grandfather.tsv
	assert_exit "${MDCAP_EXIT}" 1 "--files verifies the ledger even with no Markdown paths"
	assert_contains "${MDCAP_OUT}" "evaluated 0 Markdown file(s) from 1 path(s)" \
		"the run is honest that it evaluated no files"
	assert_contains "${MDCAP_OUT}" "STALE ledger row go/internal/gone/README.md" \
		"the stale row is still caught"
	rm -rf "${repo}"
}

# git exports GIT_DIR to every hook it runs. With an ABSOLUTE GIT_DIR --
# which is what a linked worktree has, and CLAUDE.md mandates all work happen
# in a worktree -- `git rev-parse --show-toplevel` succeeds and returns the
# directory git ran in (<root>/scripts) instead of the work tree, which
# blinded the dirgate gate entirely until it was found. This script computes
# its repo root from BASH_SOURCE for that reason; this case is the pin.
test_hook_env_git_dir_does_not_blind_the_gate() {
	local repo tsv out exit_status
	repo="$(new_scratch_repo)"
	write_md_lines "${repo}/go/internal/thing/README.md" 501
	tsv="$(write_ledger "${repo}")"
	git -C "${repo}" add -A >/dev/null 2>&1
	git -C "${repo}" commit -q -m "scratch fixture" --allow-empty >/dev/null 2>&1
	out="$(GIT_DIR="${repo}/.git" GIT_WORK_TREE="${repo}" \
		MARKDOWN_LINE_CAP_REPO_ROOT="${repo}" MARKDOWN_LINE_CAP_TSV="${tsv}" \
		bash "${verify_script}" --all 2>&1)"
	exit_status=$?
	assert_exit "${exit_status}" 1 "an absolute GIT_DIR in the environment does not blind the gate"
	assert_contains "${out}" "exceeding the 500-line cap" \
		"the finding still fires under hook environment"
	rm -rf "${repo}"
}

test_pin_helper_prints_a_row() {
	local repo tsv
	repo="$(new_scratch_repo)"
	write_md_lines "${repo}/go/internal/thing/README.md" 900
	tsv="$(write_ledger "${repo}")"
	run_mdcap "${repo}" "${tsv}" --pin go/internal/thing/README.md
	assert_exit "${MDCAP_EXIT}" 0 "--pin succeeds for an over-cap file"
	assert_contains "${MDCAP_OUT}" "go/internal/thing/README.md"$'\t'"900" \
		"--pin prints a paste-ready tab-separated row"
	rm -rf "${repo}"
}

# --pin is how a row gets authored, so it must refuse to author a row that
# mdcap_verify_ledger would immediately reject.
test_pin_helper_refuses_a_file_under_the_cap() {
	local repo tsv
	repo="$(new_scratch_repo)"
	write_md_lines "${repo}/go/internal/thing/README.md" 400
	tsv="$(write_ledger "${repo}")"
	run_mdcap "${repo}" "${tsv}" --pin go/internal/thing/README.md
	assert_exit "${MDCAP_EXIT}" 1 "--pin refuses a file that needs no row"
	assert_contains "${MDCAP_OUT}" "is 400 lines, within the 500-line cap -- it needs no ledger row" \
		"--pin explains why it refused"
	rm -rf "${repo}"
}

test_unknown_mode_exits_two() {
	local repo tsv
	repo="$(new_scratch_repo)"
	tsv="$(write_ledger "${repo}")"
	run_mdcap "${repo}" "${tsv}" --everything
	assert_exit "${MDCAP_EXIT}" 2 "an unknown mode exits 2 with usage"
	assert_contains "${MDCAP_OUT}" "usage: verify-markdown-line-cap.sh" "usage is printed"
	rm -rf "${repo}"
}
