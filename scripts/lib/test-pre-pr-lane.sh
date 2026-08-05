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
#      to fail.
#   4. pre_pr_run_classifier_selfcheck, against stub suites that pass and fail.
#   5. pre_pr_decide_lane end to end, which is the function `make pre-pr`
#      actually calls, and pre_pr_print_lane_banner on the result.
#
# Run with:
#   bash scripts/lib/test-pre-pr-lane.sh
set -uo pipefail

suite_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
for _lib in pre-pr-docs-fastpath pre-pr-lane; do
	if [[ ! -f "${suite_root}/scripts/lib/${_lib}.sh" ]]; then
		echo "test-pre-pr-lane: missing library at ${suite_root}/scripts/lib/${_lib}.sh" >&2
		exit 1
	fi
	# shellcheck source=/dev/null
	source "${suite_root}/scripts/lib/${_lib}.sh"
done

failures=0
cases_run=0
scratch="$(mktemp -d)"
# A case below drops write permission on a directory on purpose; put it back
# before removing the tree, or the cleanup leaves the scratch dir behind.
trap 'chmod -R u+rwX "${scratch}" >/dev/null 2>&1; rm -rf "${scratch}"' EXIT

fail() {
	echo "FAIL $*" >&2
	failures=$((failures + 1))
}

# assert_eq <case-name> <want> <got>
assert_eq() {
	local name="$1" want="$2" got="$3"
	cases_run=$((cases_run + 1))
	if [[ "${got}" != "${want}" ]]; then
		fail "${name}: want '${want}', got '${got}'"
		return
	fi
	echo "PASS ${name}"
}

# assert_contains <case-name> <needle> <haystack>
assert_contains() {
	local name="$1" needle="$2" haystack="$3"
	cases_run=$((cases_run + 1))
	if [[ "${haystack}" != *"${needle}"* ]]; then
		fail "${name}: want output containing '${needle}', got '${haystack}'"
		return
	fi
	echo "PASS ${name}"
}

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

# ─── Layer 2: the state channel ───────────────────────────────────────────

# Never initialized: the library's source-time default has to be a refusal, not
# an empty string, or a caller that skips the init reaches FAST with no record.
assert_eq "uninitialized_state_is_broken" "broken" \
	"$(bash -c 'set -u; source "$1/scripts/lib/pre-pr-lane.sh"; [[ -n "${pre_pr_state_broken}" ]] && echo broken || echo fine' _ "${suite_root}")"

pre_pr_git_state_init
# shellcheck disable=SC2154  # pre_pr_state_dir/_diff_fail_marker are set by the
# sourced library, which is the thing under test here.
assert_eq "state_init_creates_a_directory" "dir" \
	"$([[ -d "${pre_pr_state_dir}" ]] && echo dir || echo missing)"
# shellcheck disable=SC2154  # set by the sourced library.
assert_eq "state_init_marker_sits_in_that_directory" "${pre_pr_state_dir}/diff-failed" \
	"${pre_pr_diff_fail_marker}"
assert_eq "state_init_reports_a_usable_channel" "" "${pre_pr_state_broken}"

# A fixed state-dir path would carry a marker from a previous run into this one,
# and a run that never failed would take the FULL lane forever. Each init gets
# its own directory, so a stale marker cannot reach the next run.
: >"${pre_pr_diff_fail_marker}"
_stale_marker="${pre_pr_diff_fail_marker}"
_first_state_dir="${pre_pr_state_dir}"
pre_pr_git_state_init
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
pre_pr_git_state_init
assert_eq "state_init_without_mktemp_reports_broken" "broken" \
	"$([[ -n "${pre_pr_state_broken}" ]] && echo broken || echo fine)"
assert_eq "state_init_without_mktemp_leaves_no_marker_path" "" "${pre_pr_diff_fail_marker}"

# mktemp succeeding but naming something that is not a directory is the same
# hazard wearing a different hat.
mktemp() { printf '%s\n' "${scratch}/not-a-directory"; }
: >"${scratch}/not-a-directory"
pre_pr_git_state_init
assert_eq "state_init_with_a_non_directory_reports_broken" "broken" \
	"$([[ -n "${pre_pr_state_broken}" ]] && echo broken || echo fine)"
unset -f mktemp

# A directory that exists but rejects writes is the ENOSPC / reaped-tmpdir
# shape: `mktemp -d` succeeded, so the old code believed the channel worked.
pre_pr_git_state_init
chmod 500 "${pre_pr_state_dir}"
pre_pr_git_state_check
assert_eq "state_check_catches_an_unwritable_directory" "broken" \
	"$([[ -n "${pre_pr_state_broken}" ]] && echo broken || echo fine)"
chmod 700 "${pre_pr_state_dir}"
rm -rf "${pre_pr_state_dir}"

# ─── Layer 3: the git boundary ────────────────────────────────────────────

# new_repo <name> — throwaway repository with one commit on `main`, plus an
# origin/main ref pointing at it and a `feature` branch one docs commit ahead.
new_repo() {
	local dir="${scratch}/$1"
	mkdir -p "${dir}/docs"
	git init -q -b main "${dir}"
	git -C "${dir}" config user.email test@example.invalid
	git -C "${dir}" config user.name test
	: >"${dir}/README.md"
	git -C "${dir}" add README.md
	git -C "${dir}" commit -qm first
	git -C "${dir}" update-ref refs/remotes/origin/main HEAD
	git -C "${dir}" checkout -q -b feature
	: >"${dir}/docs/page.md"
	git -C "${dir}" add docs/page.md
	git -C "${dir}" commit -qm docs
	printf '%s\n' "${dir}"
}

# orphan_repo <name> — origin/main resolves but shares no history with HEAD, so
# `git diff origin/main...HEAD` exits 128 with "fatal: no merge base" and prints
# nothing. This is the shape that used to read as a clean tree.
orphan_repo() {
	local dir
	dir="$(new_repo "$1")"
	git -C "${dir}" checkout -q --orphan detached
	git -C "${dir}" rm -q -rf . >/dev/null 2>&1
	mkdir -p "${dir}/docs"
	: >"${dir}/docs/page.md"
	git -C "${dir}" add docs/page.md
	git -C "${dir}" commit -qm orphan
	printf '%s\n' "${dir}"
}

repo_ok="$(new_repo repo-ok)"
repo_root="${repo_ok}"
pre_pr_git_state_init
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
pre_pr_git_state_init
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
pre_pr_git_state_init
assert_eq "missing_tree_object_diff_prints_nothing" "" \
	"$(git_changed_names 'origin/main...HEAD' 2>/dev/null)"
assert_eq "missing_tree_object_diff_writes_the_marker" "present" \
	"$([[ -e "${pre_pr_diff_fail_marker}" ]] && echo present || echo absent)"

# ─── Layer 4: pre_pr_run_classifier_selfcheck ─────────────────────────────

# fake_root <name> <docs-suite-rc> <lane-suite-rc> — a repo-shaped directory
# holding stub suites, so the self-check's own pass/fail handling is tested
# without running (or recursing into) the real suites.
fake_root() {
	local dir="${scratch}/$1" docs_rc="$2" lane_rc="$3"
	mkdir -p "${dir}/scripts/lib"
	printf '#!/usr/bin/env bash\nexit %s\n' "${docs_rc}" >"${dir}/scripts/lib/test-pre-pr-docs-fastpath.sh"
	printf '#!/usr/bin/env bash\nexit %s\n' "${lane_rc}" >"${dir}/scripts/lib/test-pre-pr-lane.sh"
	printf '%s\n' "${dir}"
}

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
	pre_pr_git_state_init
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
repo_root="${repo_ok}"
# shellcheck disable=SC2034  # read by the sourced classifier.
PRE_PR_FASTPATH_ROOT="${repo_ok}"
pre_pr_git_state_init
chmod 500 "${pre_pr_state_dir}"
pre_pr_decide_lane "${root_pass}" origin/main ok paths_from_diff >/dev/null 2>&1
assert_eq "decide_unwritable_state_dir_is_full" "full" "${PRE_PR_FASTPATH_LANE}"
assert_eq "decide_unwritable_state_dir_reports_zero_diff_failures" "0" "${PRE_PR_LANE_DIFF_FAILED}"
assert_contains "decide_unwritable_state_dir_says_why" "state directory" \
	"${PRE_PR_FASTPATH_FORCED_FULL_REASON}"
chmod 700 "${pre_pr_state_dir}"

# The correlated case: a full disk makes the base diff fail AND makes the record
# of it unwritable. Before the state channel was a precondition this pair
# classified FAST, on a run that had looked at nothing.
# shellcheck disable=SC2034  # read by the sourced git wrappers.
repo_root="${repo_orphan}"
# shellcheck disable=SC2034  # read by the sourced classifier.
PRE_PR_FASTPATH_ROOT="${repo_orphan}"
pre_pr_git_state_init
chmod 500 "${pre_pr_state_dir}"
pre_pr_decide_lane "${root_pass}" origin/main ok paths_from_diff >/dev/null 2>&1
assert_eq "decide_unwritable_state_dir_with_failed_diff_is_full" "full" "${PRE_PR_FASTPATH_LANE}"
chmod 700 "${pre_pr_state_dir}"

# An untracked .go file on an otherwise docs-only branch: no `git diff` sees it,
# and on the FAST lane no `go build` would either.
mkdir -p "${repo_ok}/go/internal/newpkg"
printf 'package newpkg\n' >"${repo_ok}/go/internal/newpkg/new.go"
decide "${root_pass}" "${repo_ok}" ok paths_from_diff_and_untracked
assert_eq "decide_untracked_go_file_is_full" "full" "${PRE_PR_FASTPATH_LANE}"
assert_eq "decide_untracked_go_file_is_the_trigger" "go/internal/newpkg/new.go" \
	"${PRE_PR_FASTPATH_TRIGGERS[*]}"
rm -rf "${repo_ok}/go"

# ─── Layer 5b: the banner the operator reads ──────────────────────────────

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
echo "test-pre-pr-lane: ${cases_run} case(s) run, ${failures} failure(s)"
if [[ "${failures}" -ne 0 ]]; then
	exit 1
fi
echo "test-pre-pr-lane: all tests passed"
