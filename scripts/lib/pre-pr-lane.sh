#!/usr/bin/env bash
# pre-pr-lane.sh — lane selection and operator banner for scripts/dev/pre-pr.sh
# (#5721).
#
# Sourced, never executed. Depends on scripts/lib/pre-pr-docs-fastpath.sh being
# sourced first (it calls pre_pr_classify_docs_fastpath).
#
# This lives beside the classifier rather than inside pre-pr.sh for two reasons.
# The repo caps every file at 500 lines, and `wc -l scripts/dev/pre-pr.sh
# scripts/lib/pre-pr-lane.sh` sums to well past it -- the two halves do not fit
# in one file. That is the mechanical reason. The one that matters more: the
# wiring BETWEEN the classifier and the gate is where both severe defects of the
# original fast path lived -- a failed git diff read as "nothing changed", and a
# HEAD~1 fallback base read as the branch diff. Wiring that decides which gates
# get skipped belongs somewhere it can be read, and tested, on its own.
#
# scripts/lib/test-pre-pr-lane.sh is that test. `make pre-pr` runs it on every
# run via pre_pr_run_classifier_selfcheck.

# Per-run state, declared at source time so a caller that never calls
# pre_pr_git_state_init still fails closed rather than tripping `set -u`:
#   pre_pr_state_dir         temp dir; the caller owns removing it on EXIT
#   pre_pr_diff_fail_marker  exists iff some git collector call failed
#   pre_pr_state_sentinel    the probe file init left behind; see below
#   pre_pr_state_probe_path  the file the LAST probe left behind
#   pre_pr_state_broken      empty iff the state channel is usable; any other
#                            value is an operator-facing reason that forces FULL
#
# shellcheck disable=SC2034  # all five are read by the sourcing caller.
pre_pr_state_dir=""
pre_pr_diff_fail_marker=""
pre_pr_state_sentinel=""
pre_pr_state_probe_path=""
pre_pr_state_broken="pre-pr never initialized its per-run state directory, so a failed git command could not have been recorded"

# Distinguishes one probe file from the next; see pre_pr_state_probe.
pre_pr_state_probe_seq=0

# pre_pr_state_probe <dir>
# Returns 0 only when <dir> is a directory that accepts a NEW file and hands the
# same bytes back, and publishes that file's path in pre_pr_state_probe_path.
#
# Creating the directory is not enough: the failure this whole channel exists to
# record is a git command dying, and a full disk (ENOSPC) makes git die AND
# makes the record of it impossible to write. That conjunction is the one that
# matters, so the write is probed rather than assumed.
#
# Two details of HOW it is probed are load-bearing.
#
# The name is different on every call, so each probe creates a file rather than
# truncating one. A directory that has lost write permission still lets an
# existing file be rewritten, so a probe that reused one fixed name would report
# a healthy channel on a directory that can no longer accept the failure marker.
#
# The file is left behind rather than removed. That is what lets the init probe
# double as a durable sentinel: a tmp reaper that deletes a directory's CONTENTS
# leaves the directory itself writable, so re-probing alone still passes while
# the failure marker that was reaped alongside reads as "no failure recorded".
# pre_pr_git_state_check requires the init sentinel to still be there for
# exactly that case. Both files die with the directory, which the caller removes
# on EXIT.
pre_pr_state_probe() {
	local dir="$1" probe
	pre_pr_state_probe_path=""
	[[ -n "${dir}" && -d "${dir}" ]] || return 1
	pre_pr_state_probe_seq=$((pre_pr_state_probe_seq + 1))
	probe="${dir}/.probe.${pre_pr_state_probe_seq}"
	# The 2>/dev/null comes FIRST: redirections are applied left to right, so a
	# trailing one is too late to catch the shell's own "Permission denied" for
	# the redirect beside it. The caller reports this failure in its own words.
	printf 'pre-pr\n' 2>/dev/null >"${probe}" || return 1
	[[ "$(cat "${probe}" 2>/dev/null)" == "pre-pr" ]] || return 1
	pre_pr_state_probe_path="${probe}"
	return 0
}

# pre_pr_git_state_init
# Creates the per-run state directory and proves it can carry a record.
#
# The failure record has to be a FILE, not a shell variable. Every changed-path
# collector in pre-pr.sh is called from a command substitution or a process
# substitution, and a variable assigned inside that subshell never reaches the
# parent script. That is also why the directory is proven usable HERE, in the
# parent shell before any subshell runs: if the file channel is broken, no
# collector can tell anyone, and a failed `git diff` would leave nothing behind
# but a stderr line above a green FAST banner.
pre_pr_git_state_init() {
	local dir
	pre_pr_state_dir=""
	pre_pr_diff_fail_marker=""
	pre_pr_state_sentinel=""
	pre_pr_state_broken=""
	dir="$(mktemp -d 2>/dev/null)" || dir=""
	if [[ -z "${dir}" || ! -d "${dir}" ]]; then
		pre_pr_state_broken="mktemp -d produced no state directory, so a failed git command could not have been recorded"
		return 0
	fi
	pre_pr_state_dir="${dir}"
	pre_pr_diff_fail_marker="${dir}/diff-failed"
	if pre_pr_state_probe "${dir}"; then
		pre_pr_state_sentinel="${pre_pr_state_probe_path}"
	else
		pre_pr_state_broken="the per-run state directory ${dir} did not accept a test write, so a failed git command could not have been recorded"
	fi
	return 0
}

# pre_pr_git_state_check
# Re-checks the state channel after the collectors have run, in the parent
# shell. The init probe cannot see a directory that filled up, was reaped, or
# lost write permission mid-run; this one can. The first reason recorded wins,
# because it is the one closest to the cause.
#
# Two different failures, so two checks. A directory that stopped accepting
# writes is caught by re-probing. A directory whose CONTENTS were reaped while
# it stayed writable is not -- the re-probe passes, and the failure marker that
# was reaped alongside reads as "no failure", which is a FAST verdict on a run
# whose record was deleted underneath it. The sentinel init wrote is what spans
# that gap: it is gone if and only if something reaped this directory's files.
pre_pr_git_state_check() {
	[[ -z "${pre_pr_state_broken}" ]] || return 0
	if [[ -z "${pre_pr_state_sentinel}" || ! -e "${pre_pr_state_sentinel}" ]]; then
		pre_pr_state_broken="the sentinel file pre-pr wrote into its per-run state directory is gone, so that directory's contents were reaped mid-run and a failed git command's marker could have gone with it"
		return 0
	fi
	pre_pr_state_probe "${pre_pr_state_dir}" ||
		pre_pr_state_broken="the per-run state directory stopped accepting writes during this run, so a failed git command may not have been recorded"
	return 0
}

# pre_pr_record_git_failure <rc> <description-of-the-command>
# Records a failed git collector where the parent shell can see it, and says so
# on stderr. Never silent: an operator who sees a FULL lane deserves the reason,
# and an operator whose record could not be written deserves that even more.
pre_pr_record_git_failure() {
	local rc="$1" what="$2"
	if [[ -z "${pre_pr_diff_fail_marker}" ]] || ! : 2>/dev/null >"${pre_pr_diff_fail_marker}"; then
		printf 'pre-pr: could not write the git-failure marker; the run falls back to the FULL lane.\n' >&2
	fi
	printf 'pre-pr: `%s` exited %d; its own message is above. The changed-path list is INCOMPLETE.\n' "${what}" "${rc}" >&2
}

# git_changed_names <git-diff-args...>
# Runs `git diff --name-only <args>` and prints the paths it produced. Requires
# ${repo_root} and pre_pr_git_state_init.
#
# The point of this wrapper is the exit status. The old shape piped three
# `git diff` invocations into `sort -u` under `2>/dev/null`, which discarded
# both the status and the message: a base diff that exited 128 with
# "fatal: no merge base" -- routine on the shallow clones used here -- looked
# exactly like a clean tree with nothing changed. Downstream, the docs
# fast-path classifier's "nothing changed, so nothing to build" rule turned
# that into a FAST verdict, skipped every Go lane, and stamped the SHA for
# push. Capture the status directly (never `$?` after a pipe), record the
# failure where the parent shell can see it, and let git's own stderr through.
git_changed_names() {
	local out rc
	out="$(git -C "${repo_root}" diff --name-only "$@")"
	rc=$?
	[[ ${rc} -eq 0 ]] || pre_pr_record_git_failure "${rc}" "git diff --name-only $*"
	[[ -n "${out}" ]] && printf '%s\n' "${out}"
	return 0
}

# git_untracked_names
# Prints files git is not tracking yet and would not otherwise report.
#
# `git diff` against the base, against HEAD, and against the index between them
# cover committed, unstaged-tracked, and staged paths -- and none of the three
# sees a file that was never `git add`ed. That gap did not matter while
# `go build ./...` ran unconditionally, because the build compiles an untracked
# .go file like any other. On the FAST lane it does not run, so a new package
# whose author forgot `git add` would take a green docs-only stamp on a tree
# that does not compile. Untracked paths therefore join the list the LANE
# decision reads, where the allowlist judges them like any other path: an
# untracked .go file is not fast-path-safe, so it forces FULL.
# --exclude-standard honours .gitignore, so build caches and editor droppings
# stay out of it. Only the lane decision reads them; see lane_input_paths in
# scripts/dev/pre-pr.sh for why the push-mirroring gates do not.
git_untracked_names() {
	local out rc
	out="$(git -C "${repo_root}" ls-files --others --exclude-standard)"
	rc=$?
	[[ ${rc} -eq 0 ]] || pre_pr_record_git_failure "${rc}" "git ls-files --others --exclude-standard"
	[[ -n "${out}" ]] && printf '%s\n' "${out}"
	return 0
}

# pre_pr_run_classifier_selfcheck <repo-root>
# Runs the fast path's own test suites and prints their output. Returns 0 only
# when every suite passed.
#
# The classifier decides which gates the current run is allowed to skip, and
# nothing downstream re-checks a FAST verdict: `make pre-pr` writes a per-SHA
# stamp and scripts/dev/prepr-stamp-verify.sh lets the push through on it. Both
# suites are self-contained -- no Go toolchain, no network, and no dependency on
# the developer's git config -- and add a couple of seconds to a run that
# otherwise costs minutes, so running them every time is cheaper than any of the
# outcomes of not running them.
pre_pr_run_classifier_selfcheck() {
	local repo_root="$1" rc=0 suite
	printf '\n\033[1m==> docs fast-path classifier self-check\033[0m\n'
	for suite in test-pre-pr-docs-fastpath test-pre-pr-lane; do
		bash "${repo_root}/scripts/lib/${suite}.sh" || rc=1
	done
	return ${rc}
}

# pre_pr_lane_paths_status <base-status> <base> <diff-failed:0|1> <selfcheck-ok:0|1> <state-broken-reason>
# Prints the status string to hand pre_pr_classify_docs_fastpath: "ok" only when
# the run could record a failure, the base was trustworthy, every git collector
# succeeded, and the classifier passed its own tables. Any other value is an
# operator-facing reason and forces FULL.
#
# The state-broken check comes first because it invalidates the next one:
# <diff-failed> is read from a marker file, so when the marker could not be
# written it reads 0 for the same reason a clean run does.
pre_pr_lane_paths_status() {
	# Exact arity, not a minimum. An extra argument means the caller and this
	# helper disagree about the argument list, and the most likely shape of that
	# disagreement is an inserted one -- which silently shifts every check below
	# onto the wrong value.
	if [[ $# -ne 5 ]]; then
		printf 'pre-pr called the lane status helper with %d argument(s) instead of 5, so the lane decision has nothing to check\n' "$#"
		return 0
	fi
	local base_status="$1" base="$2" diff_failed="$3" selfcheck_ok="$4" state_broken="$5"
	if [[ -n "${state_broken}" ]]; then
		printf '%s\n' "${state_broken}"
		return 0
	fi
	if [[ "${base_status}" != "ok" ]]; then
		printf '%s\n' "${base_status}"
		return 0
	fi
	if [[ "${diff_failed}" == "1" ]]; then
		printf 'a git diff against %s failed, so the changed-path list is incomplete\n' "${base}"
		return 0
	fi
	if [[ "${selfcheck_ok}" != "1" ]]; then
		printf 'the classifier failed its own self-check above, so its verdict cannot be trusted\n'
		return 0
	fi
	printf 'ok\n'
}

# pre_pr_decide_lane <repo-root> <base> <base-status> <changed-paths-fn>
# The whole lane decision, in one function so it can be driven by a test.
#
# Order matters and is the reason this is not four lines inline in pre-pr.sh:
# the self-check runs first (a classifier that cannot pass its own table has not
# earned the right to skip anything), the collectors run next, the state channel
# is re-probed AFTER them, and only then is the marker read. Reading the marker
# before the collectors ran, or trusting it without re-probing, both produce a
# FAST verdict on a run that failed to look.
#
# Sets, in addition to the PRE_PR_FASTPATH_* globals the classifier sets:
#   PRE_PR_LANE_SELFCHECK_OK        1 when every self-check suite passed
#   PRE_PR_LANE_SELFCHECK_SECONDS   wall time of the self-check
#   PRE_PR_LANE_CHANGED_PATHS       the collected paths, blank lines dropped
#   PRE_PR_LANE_DIFF_FAILED         1 when some collector recorded a failure
#   PRE_PR_LANE_PATHS_STATUS        what was handed to the classifier
#
# shellcheck disable=SC2034  # all five are read by the sourcing caller.
pre_pr_decide_lane() {
	# `base` is deliberately local and deliberately named: bash scopes
	# dynamically, so <changed-paths-fn> sees THIS base when it builds its git
	# arguments. The collectors and the verdict therefore always talk about the
	# same ref, even if a caller's global `base` were to say otherwise.
	local root="$1" base="$2" base_status="$3" paths_fn="$4"
	local start=${SECONDS} p paths_file

	PRE_PR_LANE_SELFCHECK_OK=1
	pre_pr_run_classifier_selfcheck "${root}" || PRE_PR_LANE_SELFCHECK_OK=0
	PRE_PR_LANE_SELFCHECK_SECONDS=$((SECONDS - start))

	# The changed-path source arrives as a NAME, so check that the name resolves
	# before believing what it did or did not print. A typo, a renamed function,
	# or a non-function like `false` runs nothing, prints nothing, and reads as
	# "no path changed" -- round 7's failed-collector defect one level up. The
	# marker cannot save this one: no collector ran to write it.
	if ! declare -F "${paths_fn}" >/dev/null 2>&1; then
		pre_pr_state_broken="pre-pr named '${paths_fn}' as its changed-path source, but no such function exists"
	fi

	# Blank lines are dropped here so a trailing newline never becomes an array
	# element. The classifier rejects an empty path too, but fail-closed (empty
	# => FULL); filtering here keeps a normal run from taking the FULL lane over
	# a blank line, and keeps the array count honest.
	#
	# The collector's output goes through a FILE, not `done < <(fn)`. A process
	# substitution throws the function's exit status away and leaves `$?` holding
	# the status of `read`, so a collector that printed one path and then died
	# looked exactly like a collector that finished. That is the same rule the
	# git wrappers above follow -- capture the status directly -- applied one
	# level up, to the call that invokes them.
	PRE_PR_LANE_CHANGED_PATHS=()
	paths_file="${pre_pr_state_dir}/paths"
	if [[ -n "${pre_pr_state_dir}" ]] && : 2>/dev/null >"${paths_file}"; then
		{ "${paths_fn}" || pre_pr_record_git_failure "$?" "${paths_fn}"; } >"${paths_file}"
		while IFS= read -r p; do
			[[ -n "${p}" ]] && PRE_PR_LANE_CHANGED_PATHS+=("${p}")
		done <"${paths_file}"
	else
		pre_pr_record_git_failure 1 "opening ${paths_file} to capture the changed-path list"
	fi

	pre_pr_git_state_check
	PRE_PR_LANE_DIFF_FAILED=0
	[[ -n "${pre_pr_diff_fail_marker}" && -e "${pre_pr_diff_fail_marker}" ]] && PRE_PR_LANE_DIFF_FAILED=1

	PRE_PR_LANE_PATHS_STATUS="$(pre_pr_lane_paths_status \
		"${base_status}" "${base}" "${PRE_PR_LANE_DIFF_FAILED}" \
		"${PRE_PR_LANE_SELFCHECK_OK}" "${pre_pr_state_broken}")"

	if [[ ${#PRE_PR_LANE_CHANGED_PATHS[@]} -gt 0 ]]; then
		pre_pr_classify_docs_fastpath "${PRE_PR_LANE_PATHS_STATUS}" "${PRE_PR_LANE_CHANGED_PATHS[@]}"
	else
		pre_pr_classify_docs_fastpath "${PRE_PR_LANE_PATHS_STATUS}"
	fi
	return 0
}

# pre_pr_print_lane_banner <base> <path...>
# Prints the FAST/FULL banner for the lane already in PRE_PR_FASTPATH_LANE.
pre_pr_print_lane_banner() {
	local base="$1"
	shift
	if [[ "${PRE_PR_FASTPATH_LANE}" != "fast" ]]; then
		printf '\n\033[1m==> pre-pr lane: FULL\033[0m\n'
		if [[ -n "${PRE_PR_FASTPATH_FORCED_FULL_REASON}" ]]; then
			printf 'the changed-path list could not be trusted, so the lane decision is not being made from it: %s\n' \
				"${PRE_PR_FASTPATH_FORCED_FULL_REASON}"
			printf 'running the full go build/lint/test/race lanes.\n'
			return 0
		fi
		printf 'build-affecting path(s) changed vs %s -- running the full go build/lint/test/race lanes. Triggered by:\n' "${base}"
		local t
		for t in "${PRE_PR_FASTPATH_TRIGGERS[@]}"; do
			printf '  - %s\n' "${t}"
		done
		return 0
	fi

	printf '\n\033[1m==> pre-pr lane: FAST (documentation/specs-only)\033[0m\n'
	printf 'no build-affecting path changed vs %s. Skipping: whole-module go build and go vet, whole-module gofumpt and golangci-lint, the changed-package go test lane, and the race lane.\n' "${base}"

	# An allowlisted path can still live under go/ -- the go:embed'd
	# capabilitycatalog data is the one case. For those the gate registry still
	# selects go-fmt, go-lint, go-vet, go-file-cap and package-docs in the
	# selected-gates step, so "the Go lanes were skipped" would be false. Only
	# build, test and race genuinely skip for them. Say which case this run is
	# rather than printing a saving the operator is not getting.
	local p go_paths=()
	for p in "$@"; do
		case "${p}" in go/*) go_paths+=("${p}") ;; esac
	done
	[[ ${#go_paths[@]} -gt 0 ]] || return 0
	printf 'note: %d changed path(s) live under go/, so the gate registry still selects go-fmt, go-lint, go-vet, go-file-cap and package-docs in the selected-gates step below. For those paths only build, test and race actually skip:\n' \
		"${#go_paths[@]}"
	for p in "${go_paths[@]}"; do
		printf '  - %s\n' "${p}"
	done
}
