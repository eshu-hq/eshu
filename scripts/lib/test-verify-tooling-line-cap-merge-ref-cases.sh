#!/usr/bin/env bash
# Moved-base cases for the tooling cap's ledger-growth check (#10248). Sourced
# by scripts/test-verify-tooling-line-cap.sh after its harness (new_scratch_repo,
# write_lines, commit_all, assert, out/rc) exists.
#
# On a pull_request run the job checks out GitHub's merge ref at fetch-depth 2.
# The growth check read the baseline ledger from origin/$GITHUB_BASE_REF fetched
# at job time, so a ledger lowering that landed on main after the ref was built
# read as the PR raising the pin: a false red on a PR that never touched the
# ledger. It now reads the ledger at the merge ref's first parent unless
# TOOLING_LINE_CAP_BASE_REF is set.
# shellcheck disable=SC2154 # Parent defines script_root, verify_script, out, rc.

# shellcheck source=scripts/lib/test-merge-ref-fixture.sh
. "${script_root}/lib/test-merge-ref-fixture.sh"

slcap_mref_ledger_rel="scripts/lib/tooling-line-cap-grandfather.tsv"

slcap_mref_pin() {
	printf '# ledger\nscripts/old.sh\t%s\n' "$1" >"$2/${slcap_mref_ledger_rel}"
}
# Main lowers the grandfathered file 600 -> 550 and its pin with it.
slcap_mref_main_shrinks() {
	write_lines "$1/scripts/old.sh" 550
	slcap_mref_pin 550 "$1"
}
# The PR leaves the ledger alone and adds a small script.
slcap_mref_pr_touches_a_script() { write_lines "$1/scripts/pr-note.sh" 5; }
# The PR grows the grandfathered file and raises its pin to match.
slcap_mref_pr_raises_the_pin() {
	write_lines "$1/scripts/old.sh" 650
	slcap_mref_pin 650 "$1"
}

slcap_mref_source() {
	local src
	src="$(new_scratch_repo)"
	write_lines "${src}/scripts/old.sh" 600
	slcap_mref_pin 600 "${src}"
	commit_all "${src}" baseline
	printf '%s\n' "${src}"
}

# run_slcap_mref CLONE [NAME=value ...] runs the real gate over the committed
# clone with ambient base and event variables cleared, the way the cap job runs
# it (REQUIRE_BASE=1). It does not commit: HEAD is the merge commit under test.
run_slcap_mref() {
	local clone="$1"
	shift
	out="$(env -u TOOLING_LINE_CAP_BASE_REF -u GITHUB_EVENT_NAME -u GITHUB_BASE_REF \
		TOOLING_LINE_CAP_REPO_ROOT="${clone}" \
		TOOLING_LINE_CAP_TSV="${clone}/${slcap_mref_ledger_rel}" \
		TOOLING_LINE_CAP_REQUIRE_BASE=1 "$@" bash "${verify_script}" --all 2>&1)"
	rc=$?
}

run_tooling_merge_ref_cases() {
	local MREF_TMP src moved ctrl viol single
	MREF_TMP="$(mktemp -d)"
	local pr_env=(GITHUB_EVENT_NAME=pull_request GITHUB_BASE_REF=main)
	local raised_550="RAISED ledger pin scripts/old.sh from 550 to 600"
	local raised_600="RAISED ledger pin scripts/old.sh from 600 to 650"

	src="$(slcap_mref_source)"
	moved="$(mref_build_clone moved "${src}" slcap_mref_pr_touches_a_script slcap_mref_main_shrinks)"
	run_slcap_mref "${moved}" "${pr_env[@]}"
	assert "merge ref: a ledger lowering that landed on main is not the PR's raise" 0
	[[ "${out}" != *"RAISED ledger pin"* ]] \
		&& record_pass "merge ref: no false raised-pin finding" \
		|| record_fail "merge ref: no false raised-pin finding" "${out}"

	run_slcap_mref "${moved}" "${pr_env[@]}" TOOLING_LINE_CAP_BASE_REF=origin/main
	assert "merge ref: an explicit base override still wins and restores the moved-tip read" 1 "${raised_550}"

	run_slcap_mref "${moved}" GITHUB_EVENT_NAME=push GITHUB_BASE_REF=main
	assert "merge ref: a push event keeps the origin tip base" 1 "${raised_550}"

	src="$(slcap_mref_source)"
	ctrl="$(mref_build_clone ctrl "${src}" slcap_mref_pr_touches_a_script "")"
	run_slcap_mref "${ctrl}" "${pr_env[@]}"
	assert "merge ref: control with main unmoved stays green" 0

	src="$(slcap_mref_source)"
	viol="$(mref_build_clone viol "${src}" slcap_mref_pr_raises_the_pin slcap_mref_main_shrinks)"
	run_slcap_mref "${viol}" "${pr_env[@]}"
	assert "merge ref: a PR that raises a pin still fails, measured against the first parent" 1 "${raised_600}"

	src="$(slcap_mref_source)"
	single="$(mref_build_linear_clone single "${src}" slcap_mref_pr_raises_the_pin slcap_mref_pr_touches_a_script)"
	run_slcap_mref "${single}" "${pr_env[@]}"
	assert "merge ref: a single-parent HEAD does not trust its parent as the base" 1 "${raised_600}"
	rm -rf "${MREF_TMP}"
}
