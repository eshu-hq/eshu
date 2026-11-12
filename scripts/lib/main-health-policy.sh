#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# main-health-policy.sh -- the gh-free helpers of scripts/ci/main-health.sh:
# policy derivation from the repo's own files (required workflows from the
# watcher's wake list, their workflow_run files, the sentinel from the
# registry's required-status workflow, registry gate blocking-ness, the
# ruleset workflow) and the text sanitisers for log lines. Split out to keep
# the watcher under the repo's 500-line file cap. It makes no GitHub call;
# the watcher's mirror (scripts/lib/test-main-health-workflow.sh) fails if
# one appears here.
#
# Sourced by scripts/ci/main-health.sh; expects repo_root, watcher_yml and
# registry_yml to be set.

# required_workflows prints the display names of every workflow whose verdict
# on main counts, one per line: the watcher's own wake list. The wake list is
# the judged list (`workflow_run` cannot take a computed list), and the static
# mirror fails when the workflow file drifts from this derivation.
required_workflows() {
	yq '.on.workflow_run.workflows[]' "${watcher_yml}" | awk 'NF && $0 != "null"' | sort -u
}

# sentinel_workflow prints the display name of the sentinel workflow that
# anchors the source-has-a-verdict pending check: the registry's
# required-status workflow file (specs/ci-gates.v1.yaml
# required_status_checks[].workflow, the Go umbrellas' workflow) resolved
# through that file's own `.name`. Prints nothing when underivable; the
# watcher fails closed on that.
sentinel_workflow() {
	local wf f
	wf="$(yq '.required_status_checks[] | select(.workflow != null) | .workflow' "${registry_yml}" | head -1)"
	[[ -n "${wf:-}" && "${wf}" != "null" ]] || return 0
	f="${repo_root}/.github/workflows/${wf}"
	[[ -f "${f}" ]] || return 0
	yq '.name' "${f}" | awk 'NF && $0 != "null"'
}

# workflow_run_files prints the file name of every required workflow whose
# `on:` declares a workflow_run trigger. Their workflow_run runs are listed per
# workflow file: a repo-wide workflow_run listing for one commit is dominated
# by hundreds of Required Gates publisher runs.
workflow_run_files() {
	local files=("${repo_root}"/.github/workflows/*.yml "${repo_root}"/.github/workflows/*.yaml)
	local existing=() f
	for f in "${files[@]}"; do [[ -f "${f}" ]] && existing+=("${f}"); done
	[[ ${#existing[@]} -gt 0 ]] || return 0
	# The tab joins name and file through strenv, not a "\t" literal in the
	# expression: yq < 4.49 does not interpret string escapes (#8354), so the
	# literal made this list empty and the watcher skipped every workflow_run
	# listing.
	TAB=$'\t' yq -N 'select((.on | tag) == "!!map" and (.on | has("workflow_run"))) | .name + strenv(TAB) + filename' "${existing[@]}" |
		awk -F'\t' 'NR == FNR { want[$0] = 1; next } ($1 in want) { n = split($2, p, "/"); print p[n] }' \
			<(required_workflows) -
}

# blocking_gates prints the registry's gates as a JSON array of
# {workflow, job, checks, blocking}; a gate without `blocking: false` is
# blocking.
blocking_gates() {
	yq -o=json -I=0 '[.gates[]? | {"workflow": .ci.workflow, "job": .ci.job,
		"checks": (.ci.check_names // []), "blocking": (.blocking != false)}]' "${registry_yml}"
}

# ruleset_workflow_file prints the file name of the workflow that owns the
# scheduled ruleset verification: the workflow whose jobs contain
# verify-live-ruleset.
ruleset_workflow_file() {
	local f
	for f in "${repo_root}"/.github/workflows/*.yml "${repo_root}"/.github/workflows/*.yaml; do
		[[ -f "${f}" ]] || continue
		if [[ "$(yq '.jobs // {} | has("verify-live-ruleset")' "${f}")" == "true" ]]; then
			basename "${f}"
			return 0
		fi
	done
	return 0
}

# clean makes untrusted log text safe to embed in an issue: control characters
# and ANSI removed, no @mentions, no backticks, bounded length.
clean() {
	sed -e $'s/\x1b\\[[0-9;]*[A-Za-z]//g' | tr -d '\000-\010\013-\037' |
		awk '{ gsub(/@/, "(at)"); gsub(/`/, "\x27"); print }' | cut -c1-300
}

# verdict_of prints the single most useful line from a job log: the first Go
# test failure or panic if there is one, else the last line before the first
# Actions error annotation, else that annotation.
verdict_of() {
	sed -E 's/^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+Z //' |
		awk '
			/^(--- FAIL:|FAIL[ \t]|FAIL$|panic: )/ { if (!fail) fail = $0 }
			/^##\[error\]/ { if (!err) { err = $0; sub(/^##\[error\]/, "", err); before = prev } }
			NF && !/^##\[/ { prev = $0 }
			END {
				if (fail) print fail
				else if (err ~ /^Process completed with exit code/ && before != "") print before
				else if (err != "") print err
			}' | clean
}
