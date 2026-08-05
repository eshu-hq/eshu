#!/usr/bin/env bash
# test-pre-pr-lane.sh — table-driven test suite for the pre-pr lane wiring
# (#5721).
#
# Sources the real library (scripts/lib/pre-pr-lane.sh). Its sibling suite,
# scripts/lib/test-pre-pr-docs-fastpath.sh, covers the allowlist classifier and
# the base resolver. Neither of those is where the fast path went wrong: both
# severe defects lived in the WIRING between the classifier and the gate -- a
# failed `git diff` read as "nothing changed", and a HEAD~1 fallback base read
# as the branch diff -- and the allowlist suite never crosses into it.
#
# Five layers, one per way the wiring can hand the classifier a lie:
#
#   1. pre_pr_lane_paths_status over its full argument space, asserting the
#      exact string, because every one of those strings is what an operator
#      reads when a run takes the FULL lane.
#   2. The state channel: the marker file is how a subshell tells the parent a
#      git command failed, so a state directory that cannot be written is a run
#      that cannot record its own failures.
#   3. The git boundary: git_changed_names and git_untracked_names against
#      throwaway repositories, including two independent ways for a base diff
#      to fail and one for the untracked listing.
#   4. pre_pr_run_classifier_selfcheck, against stub suites that pass and fail.
#   5. pre_pr_decide_lane end to end, which is the function `make pre-pr`
#      actually calls -- including the three ways the changed-path source it is
#      handed BY NAME can fail to produce a list -- and pre_pr_print_lane_banner
#      on the result.
#
# The environment, the assertions, and the throwaway repositories live in
# scripts/lib/test-pre-pr-lane-fixtures.sh -- the two together crossed the
# repo's 500-line cap, and the cases are the half worth reading in one piece.
#
# Run with:
#   bash scripts/lib/test-pre-pr-lane.sh
set -uo pipefail

suite_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
_fixtures="${suite_root}/scripts/lib/test-pre-pr-lane-fixtures.sh"
if [[ ! -f "${_fixtures}" ]]; then
	echo "test-pre-pr-lane: missing fixtures at ${_fixtures}" >&2
	exit 1
fi
# shellcheck source=scripts/lib/test-pre-pr-lane-fixtures.sh
source "${_fixtures}"

# ─── Layer 1: pre_pr_lane_paths_status ────────────────────────────────────
# The four inputs are independent, so the table is the full 2x2x2x2 product.
# Each row names its own expected output rather than deriving it, so a mutation
# that changes the precedence in the library cannot change what the test wants.

base_not_ok="origin/main does not resolve, so the base fell back to HEAD~1"
diff_msg="a git diff against origin/main failed, so the changed-path list is incomplete"
self_msg="the classifier failed its own self-check above, so its verdict cannot be trusted"
state_msg="the per-run state directory stopped accepting writes during this run"

want_for() {
	case "$1" in
		OK) printf 'ok' ;;
		BASE) printf '%s' "${base_not_ok}" ;;
		DIFF) printf '%s' "${diff_msg}" ;;
		SELF) printf '%s' "${self_msg}" ;;
		STATE) printf '%s' "${state_msg}" ;;
	esac
}

# fields: base-status-token | diff-failed | selfcheck-ok | state-broken-token | want-token
# base-status-token: OK or BASE. state-broken-token: NONE or STATE.
status_cases=(
	"OK|0|1|NONE|OK"
	"OK|0|0|NONE|SELF"
	"OK|1|1|NONE|DIFF"
	"OK|1|0|NONE|DIFF"
	"BASE|0|1|NONE|BASE"
	"BASE|0|0|NONE|BASE"
	"BASE|1|1|NONE|BASE"
	"BASE|1|0|NONE|BASE"
	"OK|0|1|STATE|STATE"
	"OK|0|0|STATE|STATE"
	"OK|1|1|STATE|STATE"
	"OK|1|0|STATE|STATE"
	"BASE|0|1|STATE|STATE"
	"BASE|0|0|STATE|STATE"
	"BASE|1|1|STATE|STATE"
	"BASE|1|0|STATE|STATE"
)

for _row in "${status_cases[@]}"; do
	IFS='|' read -r _bs _df _so _sb _want <<<"${_row}"
	case "${_bs}" in OK) _bs_val="ok" ;; *) _bs_val="${base_not_ok}" ;; esac
	case "${_sb}" in NONE) _sb_val="" ;; *) _sb_val="${state_msg}" ;; esac
	assert_eq "paths_status[base=${_bs},diff_failed=${_df},selfcheck=${_so},state=${_sb}]" \
		"$(want_for "${_want}")" \
		"$(pre_pr_lane_paths_status "${_bs_val}" origin/main "${_df}" "${_so}" "${_sb_val}")"
done

# A caller that forgets an argument must not read as "nothing to complain
# about". Under `set -u` a missing $5 would abort mid-function; the arity guard
# turns that into an operator-facing reason and a FULL lane.
arity_msg_prefix="pre-pr called the lane status helper with"
assert_contains "paths_status_rejects_no_arguments" "${arity_msg_prefix} 0 argument(s)" \
	"$(pre_pr_lane_paths_status)"
assert_contains "paths_status_rejects_four_arguments" "${arity_msg_prefix} 4 argument(s)" \
	"$(pre_pr_lane_paths_status ok origin/main 0 1)"
# An EXTRA argument is the same disagreement wearing the other hat: the likely
# shape is one inserted in the middle, which shifts every check onto the wrong
# value while every check still has something to read.
assert_contains "paths_status_rejects_six_arguments" "${arity_msg_prefix} 6 argument(s)" \
	"$(pre_pr_lane_paths_status ok origin/main 0 0 1 "")"

# ─── Layer 2: the state channel ───────────────────────────────────────────

# Never initialized: the library's source-time default has to be a refusal, not
# an empty string, or a caller that skips the init reaches FAST with no record.
assert_eq "uninitialized_state_is_broken" "broken" \
	"$(bash -c 'set -u; source "$1/scripts/lib/pre-pr-lane.sh"; [[ -n "${pre_pr_state_broken}" ]] && echo broken || echo fine' _ "${suite_root}")"

init_state
# shellcheck disable=SC2154  # pre_pr_state_dir/_diff_fail_marker are set by the
# sourced library, which is the thing under test here.
assert_eq "state_init_creates_a_directory" "dir" \
	"$([[ -d "${pre_pr_state_dir}" ]] && echo dir || echo missing)"
# shellcheck disable=SC2154  # set by the sourced library.
assert_eq "state_init_marker_sits_in_that_directory" "${pre_pr_state_dir}/diff-failed" \
	"${pre_pr_diff_fail_marker}"
assert_eq "state_init_reports_a_usable_channel" "" "${pre_pr_state_broken}"

# Init has to leave something DURABLE behind. A probe that cleans up after
# itself proves the channel worked at init and nothing more, and the gap between
# init and the post-collector check is exactly where a tmp reaper does its work.
# shellcheck disable=SC2154  # set by the sourced library.
assert_eq "state_init_leaves_a_durable_sentinel" "present" \
	"$([[ -n "${pre_pr_state_sentinel}" && -e "${pre_pr_state_sentinel}" ]] && echo present || echo absent)"

# The reaped-contents shape: the directory is still there and still writable, so
# re-probing it passes -- but the failure marker that was in it is gone, and a
# marker that was never written and a marker that was deleted read identically.
# Only the sentinel tells them apart.
_reaped_sentinel="${pre_pr_state_sentinel}"
rm -f "${_reaped_sentinel}" "${pre_pr_diff_fail_marker}"
pre_pr_git_state_check
assert_eq "state_check_catches_a_reaped_sentinel" "broken" \
	"$([[ -n "${pre_pr_state_broken}" ]] && echo broken || echo fine)"
assert_contains "state_check_reaped_sentinel_says_why" "reaped mid-run" "${pre_pr_state_broken}"

# A fixed state-dir path would carry a marker from a previous run into this one,
# and a run that never failed would take the FULL lane forever. Each init gets
# its own directory, so a stale marker cannot reach the next run.
: >"${pre_pr_diff_fail_marker}"
_stale_marker="${pre_pr_diff_fail_marker}"
_first_state_dir="${pre_pr_state_dir}"
init_state
assert_eq "state_init_does_not_reuse_the_previous_directory" "different" \
	"$([[ "${pre_pr_state_dir}" != "${_first_state_dir}" ]] && echo different || echo same)"
assert_eq "state_init_does_not_inherit_a_stale_marker" "absent" \
	"$([[ -e "${pre_pr_diff_fail_marker}" ]] && echo present || echo absent)"
rm -rf "${_first_state_dir}" "${_stale_marker}"

# mktemp itself failing leaves no directory to write into. The old code kept
# going with an empty state dir and a marker path of "/diff-failed", which no
# unprivileged run can create -- a permanent, silent FAST. A shell function
# shadows the real mktemp here because the condition (a full or missing TMPDIR)
# cannot be produced on a working machine: BSD mktemp falls back to /var/folders
# when TMPDIR points nowhere, so pointing TMPDIR at a missing path proves
# nothing.
mktemp() { return 1; }
init_state
assert_eq "state_init_without_mktemp_reports_broken" "broken" \
	"$([[ -n "${pre_pr_state_broken}" ]] && echo broken || echo fine)"
assert_eq "state_init_without_mktemp_leaves_no_marker_path" "" "${pre_pr_diff_fail_marker}"

# mktemp succeeding but naming something that is not a directory is the same
# hazard wearing a different hat.
# shellcheck disable=SC2154  # scratch is set by the sourced fixtures.
mktemp() { printf '%s\n' "${scratch}/not-a-directory"; }
: >"${scratch}/not-a-directory"
init_state
assert_eq "state_init_with_a_non_directory_reports_broken" "broken" \
	"$([[ -n "${pre_pr_state_broken}" ]] && echo broken || echo fine)"
unset -f mktemp

# A directory that exists but rejects writes is the ENOSPC / reaped-tmpdir
# shape: `mktemp -d` succeeded, so the old code believed the channel worked.
if running_as_root; then
	skip_case "state_check_catches_an_unwritable_directory" "root ignores the mode bits"
else
	init_state
	chmod 500 "${pre_pr_state_dir}"
	pre_pr_git_state_check
	assert_eq "state_check_catches_an_unwritable_directory" "broken" \
		"$([[ -n "${pre_pr_state_broken}" ]] && echo broken || echo fine)"
	chmod 700 "${pre_pr_state_dir}"
	rm -rf "${pre_pr_state_dir}"
fi

# The other half of the ENOSPC conjunction the probe exists for: the write
# REPORTS success and the bytes do not come back. A probe that stops after the
# write is green on exactly this, which is the case where the failure marker
# would also be written, also report success, and also not be there to read.
# `cat` is shadowed because a filesystem that accepts a write and returns
# nothing cannot be produced on a working machine -- the same reason mktemp is
# shadowed above.
init_state
cat() { return 0; }
pre_pr_state_probe "${pre_pr_state_dir}"
assert_eq "state_probe_rejects_a_write_whose_bytes_do_not_come_back" "1" "$?"
unset -f cat
rm -rf "${pre_pr_state_dir}"

# ─── Layer 3: the git boundary ────────────────────────────────────────────
# new_repo and orphan_repo are in the fixtures file.

repo_ok="$(new_repo repo-ok)"
repo_root="${repo_ok}"
init_state
assert_eq "changed_names_reports_the_branch_diff" "docs/page.md" \
	"$(git_changed_names 'origin/main...HEAD')"
assert_eq "successful_diff_leaves_no_marker" "absent" \
	"$([[ -e "${pre_pr_diff_fail_marker}" ]] && echo present || echo absent)"

# Untracked files are invisible to all three `git diff` collectors, and on the
# FAST lane no `go build` compiles them either.
mkdir -p "${repo_ok}/go/internal/newpkg"
printf 'package newpkg\n' >"${repo_ok}/go/internal/newpkg/new.go"
assert_eq "untracked_names_reports_an_unadded_file" "go/internal/newpkg/new.go" \
	"$(git_untracked_names)"
assert_eq "successful_untracked_listing_leaves_no_marker" "absent" \
	"$([[ -e "${pre_pr_diff_fail_marker}" ]] && echo present || echo absent)"
rm -rf "${repo_ok}/go"

# Failure shape 1: no merge base.
repo_orphan="$(orphan_repo repo-orphan)"
repo_root="${repo_orphan}"
init_state
assert_eq "no_merge_base_diff_prints_nothing" "" "$(git_changed_names 'origin/main...HEAD' 2>/dev/null)"
assert_eq "no_merge_base_diff_writes_the_marker" "present" \
	"$([[ -e "${pre_pr_diff_fail_marker}" ]] && echo present || echo absent)"

# Failure shape 2: a base that resolves and shares history, with one of its tree
# objects removed from the object store. `git diff` exits non-zero here for a
# reason that has nothing to do with merge bases, so it proves the wrapper
# records the STATUS rather than one known message.
repo_broken="$(new_repo repo-broken-tree)"
_tree="$(git -C "${repo_broken}" rev-parse 'origin/main^{tree}')"
rm -f "${repo_broken}/.git/objects/${_tree:0:2}/${_tree:2}"
repo_root="${repo_broken}"
init_state
assert_eq "missing_tree_object_diff_prints_nothing" "" \
	"$(git_changed_names 'origin/main...HEAD' 2>/dev/null)"
assert_eq "missing_tree_object_diff_writes_the_marker" "present" \
	"$([[ -e "${pre_pr_diff_fail_marker}" ]] && echo present || echo absent)"

# Failure shape 3, for the untracked collector rather than the diff one.
# `git ls-files` reads the index, so an unreadable index makes it exit 128 with
# an empty stdout -- the same shape a corrupt .git/info/exclude, an unreadable
# core.excludesFile, or a sparse-checkout error produces. Without the status
# guard that prints nothing, records nothing, leaves the status "ok", and lets
# an untracked .go file ride the FAST lane. A corrupt index is used rather than
# a chmod because it does not depend on the mode bits meaning anything, so this
# case still runs as root.
repo_no_index="$(new_repo repo-no-index)"
printf 'not-an-index' >"${repo_no_index}/.git/index"
repo_root="${repo_no_index}"
init_state
assert_eq "unreadable_index_untracked_listing_prints_nothing" "" \
	"$(git_untracked_names 2>/dev/null)"
assert_eq "unreadable_index_untracked_listing_writes_the_marker" "present" \
	"$([[ -e "${pre_pr_diff_fail_marker}" ]] && echo present || echo absent)"

# ─── Layer 4: pre_pr_run_classifier_selfcheck ─────────────────────────────
# fake_root is in the fixtures file.

root_pass="$(fake_root root-pass 0 0)"
pre_pr_run_classifier_selfcheck "${root_pass}" >/dev/null 2>&1
assert_eq "selfcheck_passes_when_both_suites_pass" "0" "$?"

root_docs_fail="$(fake_root root-docs-fail 1 0)"
pre_pr_run_classifier_selfcheck "${root_docs_fail}" >/dev/null 2>&1
assert_eq "selfcheck_fails_when_the_allowlist_suite_fails" "1" "$?"

root_lane_fail="$(fake_root root-lane-fail 0 1)"
pre_pr_run_classifier_selfcheck "${root_lane_fail}" >/dev/null 2>&1
assert_eq "selfcheck_fails_when_the_lane_suite_fails" "1" "$?"

root_missing="${scratch}/root-missing"
mkdir -p "${root_missing}/scripts/lib"
pre_pr_run_classifier_selfcheck "${root_missing}" >/dev/null 2>&1
assert_eq "selfcheck_fails_when_a_suite_is_missing" "1" "$?"

# ─── Layer 5: pre_pr_decide_lane end to end ───────────────────────────────

# The paths function pre-pr.sh passes is its own changed_all_files; these two
# are the same shape, built on the same wrappers.
paths_from_diff() { git_changed_names 'origin/main...HEAD'; }
paths_from_diff_and_untracked() {
	git_changed_names 'origin/main...HEAD'
	git_untracked_names
}

# decide <fake-root> <repo> <base-status> <paths-fn> — one full decision.
decide() {
	repo_root="$2"
	# shellcheck disable=SC2034  # read by the sourced classifier.
	PRE_PR_FASTPATH_ROOT="$2"
	init_state
	pre_pr_decide_lane "$1" origin/main "$3" "$4" >/dev/null 2>&1
}

# A docs-only branch with everything healthy is the only shape that may be fast.
decide "${root_pass}" "${repo_ok}" ok paths_from_diff
assert_eq "decide_docs_only_branch_is_fast" "fast" "${PRE_PR_FASTPATH_LANE}"
assert_eq "decide_docs_only_branch_status_is_ok" "ok" "${PRE_PR_LANE_PATHS_STATUS}"
assert_eq "decide_docs_only_branch_records_no_diff_failure" "0" "${PRE_PR_LANE_DIFF_FAILED}"
assert_eq "decide_docs_only_branch_collected_the_path" "docs/page.md" \
	"${PRE_PR_LANE_CHANGED_PATHS[*]}"

# The round-7 defect, end to end: a base diff that exits 128 prints nothing, and
# nothing-changed used to mean nothing-to-build.
decide "${root_pass}" "${repo_orphan}" ok paths_from_diff
assert_eq "decide_failed_base_diff_is_full" "full" "${PRE_PR_FASTPATH_LANE}"
assert_eq "decide_failed_base_diff_sets_diff_failed" "1" "${PRE_PR_LANE_DIFF_FAILED}"
assert_contains "decide_failed_base_diff_says_why" "a git diff against origin/main failed" \
	"${PRE_PR_FASTPATH_FORCED_FULL_REASON}"

# Same, through the missing-tree-object failure rather than the merge-base one.
decide "${root_pass}" "${repo_broken}" ok paths_from_diff
assert_eq "decide_missing_tree_object_is_full" "full" "${PRE_PR_FASTPATH_LANE}"
assert_eq "decide_missing_tree_object_sets_diff_failed" "1" "${PRE_PR_LANE_DIFF_FAILED}"

# A classifier that cannot pass its own tables may not skip anything, and the
# failure has to reach the lane decision, not just the summary line.
decide "${root_lane_fail}" "${repo_ok}" ok paths_from_diff
assert_eq "decide_failed_selfcheck_is_full" "full" "${PRE_PR_FASTPATH_LANE}"
assert_eq "decide_failed_selfcheck_records_it" "0" "${PRE_PR_LANE_SELFCHECK_OK}"
assert_contains "decide_failed_selfcheck_says_why" "failed its own self-check" \
	"${PRE_PR_FASTPATH_FORCED_FULL_REASON}"

# An untrustworthy base forces FULL even when every git command succeeded.
decide "${root_pass}" "${repo_ok}" "${base_not_ok}" paths_from_diff
assert_eq "decide_untrustworthy_base_is_full" "full" "${PRE_PR_FASTPATH_LANE}"
assert_eq "decide_untrustworthy_base_says_why" "${base_not_ok}" \
	"${PRE_PR_FASTPATH_FORCED_FULL_REASON}"

# The state channel is a precondition, not a convenience. With it broken, the
# marker reads 0 for the same reason a clean run does, so the run must not be
# able to reach FAST -- even though nothing else about it is wrong.
if running_as_root; then
	skip_case "decide_unwritable_state_dir_is_full" "root ignores the mode bits"
	skip_case "decide_unwritable_state_dir_with_failed_diff_is_full" "root ignores the mode bits"
else
	repo_root="${repo_ok}"
	# shellcheck disable=SC2034  # read by the sourced classifier.
	PRE_PR_FASTPATH_ROOT="${repo_ok}"
	init_state
	chmod 500 "${pre_pr_state_dir}"
	pre_pr_decide_lane "${root_pass}" origin/main ok paths_from_diff >/dev/null 2>&1
	assert_eq "decide_unwritable_state_dir_is_full" "full" "${PRE_PR_FASTPATH_LANE}"
	assert_eq "decide_unwritable_state_dir_reports_zero_diff_failures" "0" "${PRE_PR_LANE_DIFF_FAILED}"
	assert_contains "decide_unwritable_state_dir_says_why" "state directory" \
		"${PRE_PR_FASTPATH_FORCED_FULL_REASON}"
	chmod 700 "${pre_pr_state_dir}"

	# The correlated case: a full disk makes the base diff fail AND makes the
	# record of it unwritable. Before the state channel was a precondition this
	# pair classified FAST, on a run that had looked at nothing.
	# shellcheck disable=SC2034  # read by the sourced git wrappers.
	repo_root="${repo_orphan}"
	# shellcheck disable=SC2034  # read by the sourced classifier.
	PRE_PR_FASTPATH_ROOT="${repo_orphan}"
	init_state
	chmod 500 "${pre_pr_state_dir}"
	pre_pr_decide_lane "${root_pass}" origin/main ok paths_from_diff >/dev/null 2>&1
	assert_eq "decide_unwritable_state_dir_with_failed_diff_is_full" "full" "${PRE_PR_FASTPATH_LANE}"
	chmod 700 "${pre_pr_state_dir}"
fi

# An untracked .go file on an otherwise docs-only branch: no `git diff` sees it,
# and on the FAST lane no `go build` would either.
mkdir -p "${repo_ok}/go/internal/newpkg"
printf 'package newpkg\n' >"${repo_ok}/go/internal/newpkg/new.go"
decide "${root_pass}" "${repo_ok}" ok paths_from_diff_and_untracked
assert_eq "decide_untracked_go_file_is_full" "full" "${PRE_PR_FASTPATH_LANE}"
assert_eq "decide_untracked_go_file_is_the_trigger" "go/internal/newpkg/new.go" \
	"${PRE_PR_FASTPATH_TRIGGERS[*]}"
rm -rf "${repo_ok}/go"

# ─── Layer 5b: the changed-path source is a NAME ──────────────────────────
# pre-pr.sh hands pre_pr_decide_lane the name of a function, not the paths. Three
# ways that name can fail to produce a path list, and all three used to arrive as
# a silent empty list -- which, with a status of "ok", is the one empty shape the
# classifier is allowed to call FAST. This is round 7's defect one level up: not
# a collector that failed, but a collector that was never called.

paths_fn_that_fails_late() {
	git_changed_names 'origin/main...HEAD'
	return 7
}

# 1. A name that resolves to nothing at all -- a typo, or a function that was
#    renamed out from under the call site.
decide "${root_pass}" "${repo_ok}" ok lane_input_paths_TYPO
assert_eq "decide_unresolvable_paths_fn_is_full" "full" "${PRE_PR_FASTPATH_LANE}"
assert_contains "decide_unresolvable_paths_fn_names_it" "lane_input_paths_TYPO" \
	"${PRE_PR_FASTPATH_FORCED_FULL_REASON}"

# 2. A name that resolves to something runnable that is not a function. `false`
#    is the honest worst case: it exists, it runs, it prints nothing, it fails.
decide "${root_pass}" "${repo_ok}" ok false
assert_eq "decide_non_function_paths_fn_is_full" "full" "${PRE_PR_FASTPATH_LANE}"

# 3. A real function that prints part of the list and then dies. This is the one
#    a process substitution cannot catch: the paths arrive, so nothing looks
#    empty, and the status is discarded on the way out.
decide "${root_pass}" "${repo_ok}" ok paths_fn_that_fails_late
assert_eq "decide_paths_fn_failing_late_is_full" "full" "${PRE_PR_FASTPATH_LANE}"
assert_eq "decide_paths_fn_failing_late_sets_diff_failed" "1" "${PRE_PR_LANE_DIFF_FAILED}"
assert_eq "decide_paths_fn_failing_late_kept_what_it_printed" "docs/page.md" \
	"${PRE_PR_LANE_CHANGED_PATHS[*]}"

# ─── Layer 5c: the banner the operator reads ──────────────────────────────

PRE_PR_FASTPATH_LANE="full"
PRE_PR_FASTPATH_TRIGGERS=()
PRE_PR_FASTPATH_FORCED_FULL_REASON="the base is not trustworthy"
assert_contains "banner_full_prints_the_forced_reason" "the base is not trustworthy" \
	"$(pre_pr_print_lane_banner origin/main)"

PRE_PR_FASTPATH_FORCED_FULL_REASON=""
PRE_PR_FASTPATH_TRIGGERS=("go/internal/query/handler.go" "Makefile")
_banner="$(pre_pr_print_lane_banner origin/main docs/page.md go/internal/query/handler.go Makefile)"
assert_contains "banner_full_lists_the_first_trigger" "- go/internal/query/handler.go" "${_banner}"
assert_contains "banner_full_lists_every_trigger" "- Makefile" "${_banner}"

PRE_PR_FASTPATH_LANE="fast"
PRE_PR_FASTPATH_TRIGGERS=()
assert_contains "banner_fast_names_the_lane" "FAST (documentation/specs-only)" \
	"$(pre_pr_print_lane_banner origin/main docs/page.md)"
assert_contains "banner_fast_warns_when_a_path_is_under_go" \
	"1 changed path(s) live under go/" \
	"$(pre_pr_print_lane_banner origin/main go/internal/capabilitycatalog/data/catalog.generated.json)"

echo ""
# shellcheck disable=SC2154  # cases_run/failures are maintained by the sourced
# fixtures' assertions.
echo "test-pre-pr-lane: ${cases_run} case(s) run, ${failures} failure(s)"
if [[ "${failures}" -ne 0 ]]; then
	exit 1
fi
echo "test-pre-pr-lane: all tests passed"
