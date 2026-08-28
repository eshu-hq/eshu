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

# mdcap_commit_baseline REPO TSV ROWS... writes ROWS to TSV, commits, and
# prints the resulting SHA. The growth check compares against a COMMITTED
# ledger, so a test for it needs a real baseline commit rather than a working
# tree -- that difference is the whole point of the check.
mdcap_commit_baseline() {
	local repo="$1" tsv="$2" row
	shift 2
	printf '# scratch markdown-line-cap ledger\n' > "${tsv}"
	for row in "$@"; do
		printf '%s\n' "${row}" >> "${tsv}"
	done
	git -C "${repo}" add -A >/dev/null 2>&1
	git -C "${repo}" commit -q -m "baseline ledger" --allow-empty >/dev/null 2>&1
	git -C "${repo}" rev-parse HEAD
}

# The hole codex found on #6279: every working-tree check agrees when a change
# adds an over-cap file AND the row pinning it at its own length. count ==
# pinned, the row is not stale, the pin clears the cap -- exit 0. The ledger
# freezes debt that predates the gate, so a row that is not in the baseline is
# a change authorising its own exemption.
test_new_ledger_row_is_rejected() {
	local repo tsv base
	repo="$(new_scratch_repo)"
	write_md_lines "${repo}/go/internal/old/README.md" 600
	tsv="${repo}/ledger.tsv"
	base="$(mdcap_commit_baseline "${repo}" "${tsv}" "go/internal/old/README.md"$'\t'"600")"

	write_md_lines "${repo}/go/internal/new/README.md" 501
	printf '%s\t%s\n' "go/internal/new/README.md" "501" >> "${tsv}"

	MARKDOWN_LINE_CAP_BASE_REF="${base}" MARKDOWN_LINE_CAP_TSV_REL="ledger.tsv" \
		run_mdcap "${repo}" "${tsv}" --all
	assert_exit "${MDCAP_EXIT}" 1 "a self-authored ledger row is rejected"
	assert_contains "${MDCAP_OUT}" "NEW ledger row go/internal/new/README.md pinned at 501" \
		"the finding names the row the change tried to author for itself"
	rm -rf "${repo}"
}

# The same hole reached by re-pinning rather than by adding: a grandfathered
# file grows, and the change raises its pin to match. Working-tree checks see
# count == pinned and agree.
test_raised_ledger_pin_is_rejected() {
	local repo tsv base
	repo="$(new_scratch_repo)"
	write_md_lines "${repo}/go/internal/thing/README.md" 600
	tsv="${repo}/ledger.tsv"
	base="$(mdcap_commit_baseline "${repo}" "${tsv}" "go/internal/thing/README.md"$'\t'"600")"

	write_md_lines "${repo}/go/internal/thing/README.md" 700
	printf '# scratch markdown-line-cap ledger\ngo/internal/thing/README.md\t700\n' > "${tsv}"

	MARKDOWN_LINE_CAP_BASE_REF="${base}" MARKDOWN_LINE_CAP_TSV_REL="ledger.tsv" \
		run_mdcap "${repo}" "${tsv}" --all
	assert_exit "${MDCAP_EXIT}" 1 "raising a pin to cover growth is rejected"
	assert_contains "${MDCAP_OUT}" "RAISED ledger pin go/internal/thing/README.md from 600 to 700" \
		"the finding names both the old pin and the new one"
	rm -rf "${repo}"
}

# Removing a row and lowering a pin are the only legitimate ledger edits, and
# both must stay green or the gate blocks the cleanup it exists to encourage.
test_lowered_ledger_pin_is_accepted() {
	local repo tsv base
	repo="$(new_scratch_repo)"
	write_md_lines "${repo}/go/internal/thing/README.md" 600
	tsv="${repo}/ledger.tsv"
	base="$(mdcap_commit_baseline "${repo}" "${tsv}" "go/internal/thing/README.md"$'\t'"600")"

	write_md_lines "${repo}/go/internal/thing/README.md" 550
	printf '# scratch markdown-line-cap ledger\ngo/internal/thing/README.md\t550\n' > "${tsv}"

	MARKDOWN_LINE_CAP_BASE_REF="${base}" MARKDOWN_LINE_CAP_TSV_REL="ledger.tsv" \
		run_mdcap "${repo}" "${tsv}" --all
	assert_exit "${MDCAP_EXIT}" 0 "lowering a pin toward the cap stays green"
	rm -rf "${repo}"
}

# The commit that INTRODUCES the ledger has no baseline to be measured
# against, and its rows are the baseline. Without this the gate would fail on
# the very change that adds it.
test_ledger_absent_at_baseline_is_accepted() {
	local repo tsv base
	repo="$(new_scratch_repo)"
	write_md_lines "${repo}/go/internal/thing/README.md" 600
	git -C "${repo}" add -A >/dev/null 2>&1
	git -C "${repo}" commit -q -m "no ledger yet" --allow-empty >/dev/null 2>&1
	base="$(git -C "${repo}" rev-parse HEAD)"

	tsv="$(write_ledger "${repo}" "go/internal/thing/README.md"$'\t'"600")"
	MARKDOWN_LINE_CAP_BASE_REF="${base}" MARKDOWN_LINE_CAP_TSV_REL="ledger.tsv" \
		run_mdcap "${repo}" "${tsv}" --all
	assert_exit "${MDCAP_EXIT}" 0 "the commit introducing the ledger is its own baseline"
	rm -rf "${repo}"
}

# The false green codex found on #6279: test.yml's verify-contracts job checks
# out with `fetch-depth: 2`, so the runner has no refs/remotes/origin/main and
# the growth check resolved no baseline -- NOTE, exit 0, backstop gone, reads
# like a pass. This builds exactly that shape: a clone whose remote-tracking
# ref for the base branch has been deleted, carrying a self-authored ledger
# row. The gate must fetch its own baseline and go red.
test_absent_baseline_ref_is_fetched() {
	local upstream clone tsv
	upstream="$(new_scratch_repo)"
	write_md_lines "${upstream}/go/internal/old/README.md" 600
	printf '# scratch markdown-line-cap ledger\ngo/internal/old/README.md\t600\n' \
		> "${upstream}/ledger.tsv"
	git -C "${upstream}" add -A >/dev/null 2>&1
	git -C "${upstream}" commit -q -m "baseline ledger" >/dev/null 2>&1
	git -C "${upstream}" branch -M main >/dev/null 2>&1

	clone="$(mktemp -d)/clone"
	git clone -q "${upstream}" "${clone}" >/dev/null 2>&1
	git -C "${clone}" config user.email "markdown-line-cap-test@example.invalid"
	git -C "${clone}" config user.name "markdown-line-cap-test"
	# The verify-contracts checkout shape: the base branch has no
	# remote-tracking ref of its own.
	git -C "${clone}" update-ref -d refs/remotes/origin/main >/dev/null 2>&1
	git -C "${clone}" remote set-head origin -d >/dev/null 2>&1

	write_md_lines "${clone}/go/internal/new/README.md" 501
	tsv="${clone}/ledger.tsv"
	printf '%s\t%s\n' "go/internal/new/README.md" "501" >> "${tsv}"

	MARKDOWN_LINE_CAP_BASE_REF="origin/main" MARKDOWN_LINE_CAP_TSV_REL="ledger.tsv" \
		run_mdcap "${clone}" "${tsv}" --all
	assert_exit "${MDCAP_EXIT}" 1 "a missing base ref is fetched rather than skipped"
	assert_contains "${MDCAP_OUT}" "NEW ledger row go/internal/new/README.md pinned at 501" \
		"the fetched baseline still catches the self-authored row"
	assert_not_contains "${MDCAP_OUT}" "ledger growth not checked" \
		"the growth check does not report itself skipped once the ref is fetchable"
	rm -rf "${upstream}" "${clone}"
}

# When the baseline genuinely cannot be reached -- no origin remote, or no
# network -- a local run says so and carries on. CI passes
# MARKDOWN_LINE_CAP_REQUIRE_BASE=1 instead, because a gate that could not run
# is not a gate that passed, and a false green on this backstop is how a
# change authorises its own exemption.
test_unresolvable_baseline_is_red_under_require_base() {
	local repo tsv
	repo="$(new_scratch_repo)"
	write_md_lines "${repo}/go/internal/thing/README.md" 600
	tsv="$(write_ledger "${repo}" "go/internal/thing/README.md"$'\t'"600")"

	MARKDOWN_LINE_CAP_BASE_REF="origin/main" MARKDOWN_LINE_CAP_TSV_REL="ledger.tsv" \
		MARKDOWN_LINE_CAP_REQUIRE_BASE="1" run_mdcap "${repo}" "${tsv}" --all
	assert_exit "${MDCAP_EXIT}" 1 "an unfetchable baseline fails under REQUIRE_BASE"
	assert_contains "${MDCAP_OUT}" "NO BASELINE -- origin/main does not resolve" \
		"the failure names the ref it could not reach"

	MARKDOWN_LINE_CAP_BASE_REF="origin/main" MARKDOWN_LINE_CAP_TSV_REL="ledger.tsv" \
		run_mdcap "${repo}" "${tsv}" --all
	assert_exit "${MDCAP_EXIT}" 0 "the same repo stays green without REQUIRE_BASE"
	assert_contains "${MDCAP_OUT}" "NOTE ledger growth not checked" \
		"the local run says the check could not run"
	rm -rf "${repo}"
}

# A digits-only pin is not enough: bash reads a leading zero as octal, so
# "0900" aborts the comparison with "value too great for base". The abort makes
# the arithmetic FALSE and, inside a negated condition, the growth goes
# unreported -- a 901-line file exited 0 with the gate believing it checked.
test_leading_zero_ledger_pin_is_rejected() {
	local repo tsv
	repo="$(new_scratch_repo)"
	write_md_lines "${repo}/go/internal/thing/README.md" 901
	tsv="$(write_ledger "${repo}" "go/internal/thing/README.md"$'\t'"0900")"
	run_mdcap "${repo}" "${tsv}" --all
	assert_exit "${MDCAP_EXIT}" 1 "a leading-zero pin is rejected rather than silently disabling the check"
	assert_contains "${MDCAP_OUT}" "MALFORMED ledger row go/internal/thing/README.md" \
		"the finding names the malformed row"
	assert_contains "${MDCAP_OUT}" "got 0900" \
		"the finding quotes the noncanonical value back"
	rm -rf "${repo}"
}
