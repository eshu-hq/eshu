#!/usr/bin/env bash
# Moved-base cases for the markdown cap's ledger-growth check (#10248).
# Sourced by scripts/test-verify-markdown-line-cap.sh after its harness
# (new_scratch_repo, write_md_lines, assert_*) exists.
#
# On a pull_request run the job checks out GitHub's merge ref at fetch-depth 2.
# The ledger-growth check read the baseline ledger from origin/$GITHUB_BASE_REF
# fetched at job time. A ledger LOWERING that landed on main after the ref was
# built then read as the PR RAISING the pin ("RAISED ledger pin ... from 550 to
# 600"), a false red on a PR that never touched the ledger. The check now reads
# the ledger at the merge ref's first parent when no explicit base is given.
# shellcheck disable=SC2154 # Parent defines verify_script and the assert_* helpers.

# shellcheck source=scripts/lib/test-merge-ref-fixture.sh
. "${script_root}/lib/test-merge-ref-fixture.sh"

mdcap_mref_pin() { printf '# scratch markdown-line-cap ledger\ngo/internal/thing/README.md\t%s\n' "$1" >"$2/ledger.tsv"; }

# Main lowers the grandfathered file 600 -> 550 and its pin with it, the one
# legal ledger edit.
mdcap_mref_main_shrinks() {
	write_md_lines "$1/go/internal/thing/README.md" 550
	mdcap_mref_pin 550 "$1"
}
# The PR leaves the ledger alone and adds an ungoverned note.
mdcap_mref_pr_touches_a_note() { write_md_lines "$1/notes/pr-note.md" 5; }
# The PR grows the grandfathered file and raises its pin to match.
mdcap_mref_pr_raises_the_pin() {
	write_md_lines "$1/go/internal/thing/README.md" 650
	mdcap_mref_pin 650 "$1"
}
mdcap_mref_branch_raises_the_pin() { mdcap_mref_pr_raises_the_pin "$1"; }

# mdcap_mref_source prints a source repo whose HEAD (B) pins the file at 600.
mdcap_mref_source() {
	local src
	src="$(new_scratch_repo)"
	write_md_lines "${src}/go/internal/thing/README.md" 600
	mdcap_mref_pin 600 "${src}"
	git -C "${src}" add -A >/dev/null 2>&1
	git -C "${src}" commit -q -m "baseline ledger" >/dev/null 2>&1
	printf '%s\n' "${src}"
}

# run_mdcap_mref CLONE [NAME=value ...] runs the real gate over the committed
# clone with every ambient base and event variable cleared, the way the cap job
# runs it (REQUIRE_BASE=1), then the given assignments applied. It does not
# commit: the clone's HEAD is the merge commit under test.
run_mdcap_mref() {
	local clone="$1"
	shift
	MDCAP_OUT="$(env -u MARKDOWN_LINE_CAP_BASE_REF -u GITHUB_EVENT_NAME -u GITHUB_BASE_REF \
		MARKDOWN_LINE_CAP_REPO_ROOT="${clone}" MARKDOWN_LINE_CAP_TSV="${clone}/ledger.tsv" \
		MARKDOWN_LINE_CAP_TSV_REL="ledger.tsv" MARKDOWN_LINE_CAP_REQUIRE_BASE=1 "$@" \
		bash "${verify_script}" --all 2>&1)"
	MDCAP_EXIT=$?
}

run_markdown_merge_ref_cases() {
	local MREF_TMP src moved ctrl viol single
	MREF_TMP="$(mktemp -d)"
	local pr_env=(GITHUB_EVENT_NAME=pull_request GITHUB_BASE_REF=main)

	src="$(mdcap_mref_source)"
	moved="$(mref_build_clone moved "${src}" mdcap_mref_pr_touches_a_note mdcap_mref_main_shrinks)"
	run_mdcap_mref "${moved}" "${pr_env[@]}"
	assert_exit "${MDCAP_EXIT}" 0 "merge ref: a ledger lowering that landed on main is not the PR's raise"
	assert_not_contains "${MDCAP_OUT}" "RAISED ledger pin" "merge ref: no false raised-pin finding"

	run_mdcap_mref "${moved}" "${pr_env[@]}" MARKDOWN_LINE_CAP_BASE_REF=origin/main
	assert_exit "${MDCAP_EXIT}" 1 "merge ref: an explicit base override still wins over the first parent"
	assert_contains "${MDCAP_OUT}" "RAISED ledger pin go/internal/thing/README.md from 550 to 600" \
		"merge ref: the override restores the moved-tip comparison, so the moved fixture is real"

	run_mdcap_mref "${moved}" GITHUB_EVENT_NAME=push GITHUB_BASE_REF=main
	assert_exit "${MDCAP_EXIT}" 1 "merge ref: a push event keeps the origin tip base"

	src="$(mdcap_mref_source)"
	ctrl="$(mref_build_clone ctrl "${src}" mdcap_mref_pr_touches_a_note "")"
	run_mdcap_mref "${ctrl}" "${pr_env[@]}"
	assert_exit "${MDCAP_EXIT}" 0 "merge ref: control with main unmoved stays green"

	src="$(mdcap_mref_source)"
	viol="$(mref_build_clone viol "${src}" mdcap_mref_pr_raises_the_pin mdcap_mref_main_shrinks)"
	run_mdcap_mref "${viol}" "${pr_env[@]}"
	assert_exit "${MDCAP_EXIT}" 1 "merge ref: a PR that raises a pin still fails while main moved"
	assert_contains "${MDCAP_OUT}" "RAISED ledger pin go/internal/thing/README.md from 600 to 650" \
		"merge ref: the raise is measured against the first parent's pin"

	src="$(mdcap_mref_source)"
	single="$(mref_build_linear_clone single "${src}" mdcap_mref_branch_raises_the_pin mdcap_mref_pr_touches_a_note)"
	run_mdcap_mref "${single}" "${pr_env[@]}"
	assert_exit "${MDCAP_EXIT}" 1 "merge ref: a single-parent HEAD does not trust its parent as the base"
	assert_contains "${MDCAP_OUT}" "RAISED ledger pin go/internal/thing/README.md from 600 to 650" \
		"merge ref: the single-parent run measures against the origin base"
	rm -rf "${MREF_TMP}"
}
