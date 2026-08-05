#!/usr/bin/env bash
# pre-pr-lane.sh — lane selection and operator banner for scripts/dev/pre-pr.sh
# (#5721).
#
# Sourced, never executed. Depends on scripts/lib/pre-pr-docs-fastpath.sh being
# sourced first (it calls pre_pr_classify_docs_fastpath).
#
# This lives beside the classifier rather than inside pre-pr.sh for two reasons:
# pre-pr.sh is close to the repo's 500-line cap, and the wiring BETWEEN the
# classifier and the gate is where both severe defects of the original fast path
# lived -- a failed git diff read as "nothing changed", and a HEAD~1 fallback
# base read as the branch diff. Wiring that decides which gates get skipped
# belongs somewhere it can be read on its own.

# pre_pr_git_state_init
# Creates the per-run state directory and sets two globals the caller uses:
#   pre_pr_state_dir         temp dir; the caller owns removing it on EXIT
#   pre_pr_diff_fail_marker  exists iff some git_changed_names call failed
#
# The failure record has to be a FILE, not a shell variable. Every changed-path
# collector in pre-pr.sh is called from a command substitution or a process
# substitution, and a variable assigned inside that subshell never reaches the
# parent script.
#
# shellcheck disable=SC2034  # both globals are read by the sourcing caller.
pre_pr_git_state_init() {
	pre_pr_state_dir="$(mktemp -d)"
	pre_pr_diff_fail_marker="${pre_pr_state_dir}/diff-failed"
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
	if [[ ${rc} -ne 0 ]]; then
		: >"${pre_pr_diff_fail_marker}"
		printf 'pre-pr: `git diff --name-only %s` exited %d; its own message is above. The changed-path list is INCOMPLETE.\n' "$*" "${rc}" >&2
	fi
	[[ -n "${out}" ]] && printf '%s\n' "${out}"
	return 0
}

# pre_pr_run_classifier_selfcheck <repo-root>
# Runs the classifier's own table suite and prints its output. Returns the
# suite's exit status.
#
# The classifier decides which gates the current run is allowed to skip, and
# nothing downstream re-checks a FAST verdict: `make pre-pr` writes a per-SHA
# stamp and scripts/dev/prepr-stamp-verify.sh lets the push through on it. The
# suite is hermetic (no Go toolchain, no network) and costs about a second, so
# running it every time is cheaper than any of the outcomes of not running it.
pre_pr_run_classifier_selfcheck() {
	local repo_root="$1"
	printf '\n\033[1m==> docs fast-path classifier self-check\033[0m\n'
	bash "${repo_root}/scripts/lib/test-pre-pr-docs-fastpath.sh"
}

# pre_pr_lane_paths_status <base-status> <base> <diff-failed:0|1> <selfcheck-ok:0|1>
# Prints the status string to hand pre_pr_classify_docs_fastpath: "ok" only when
# the base was trustworthy, every git diff succeeded, and the classifier passed
# its own table. Any other value is an operator-facing reason and forces FULL.
pre_pr_lane_paths_status() {
	local base_status="$1" base="$2" diff_failed="$3" selfcheck_ok="$4"
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
