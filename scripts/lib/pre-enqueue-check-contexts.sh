#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
# shellcheck disable=SC2034 # contexts_error is read by the sourcing script.
#
# Required-context and disposition helpers for scripts/dev/pre-enqueue-check.sh
# (#9094). Sourced, not run.
#
# The merge rule reads the ruleset-required set from the gate registry, never
# from a list in the script: `ci-gates contexts --ruleset-only` is the contexts
# the live main ruleset requires, and plain `ci-gates contexts` is every context
# the manifest publishes, including ones the ruleset no longer requires (the
# required-gates-complete aggregate, #9089). The difference is the set whose
# pending state is a note and whose red state needs a disposition.

# ruleset_contexts and manifest_contexts are filled by load_contexts.
ruleset_contexts=()
manifest_contexts=()

# load_contexts <repo-root>: fill ruleset_contexts and manifest_contexts from
# ci-gates. PRE_ENQUEUE_CI_GATES overrides the command (a fake in the self-test),
# split on whitespace with no quoting; PRE_ENQUEUE_REGISTRY the registry path. Returns 1 on any read failure or an
# empty ruleset set: with nothing required the check would pass vacuously.
load_contexts() {
	local root="$1" registry out rc
	local -a cmd
	if [[ -n "${PRE_ENQUEUE_CI_GATES:-}" ]]; then
		read -r -a cmd <<<"${PRE_ENQUEUE_CI_GATES}"
	else
		cmd=(go -C "${root}/go" run ./cmd/ci-gates)
	fi
	registry="${PRE_ENQUEUE_REGISTRY:-${root}/specs/ci-gates.v1.yaml}"
	ruleset_contexts=()
	manifest_contexts=()
	out="$("${cmd[@]}" contexts --registry "${registry}" --ruleset-only 2>/dev/null)"
	rc=$?
	if [[ "${rc}" -ne 0 ]]; then
		contexts_error="ci-gates contexts --ruleset-only exited ${rc}"
		return 1
	fi
	while IFS= read -r line; do
		[[ -z "${line}" ]] || ruleset_contexts+=("${line}")
	done <<<"${out}"
	out="$("${cmd[@]}" contexts --registry "${registry}" 2>/dev/null)"
	rc=$?
	if [[ "${rc}" -ne 0 ]]; then
		contexts_error="ci-gates contexts exited ${rc}"
		return 1
	fi
	while IFS= read -r line; do
		[[ -z "${line}" ]] || manifest_contexts+=("${line}")
	done <<<"${out}"
	if [[ ${#ruleset_contexts[@]} -eq 0 ]]; then
		contexts_error="ci-gates printed no ruleset-required context"
		return 1
	fi
	# A ruleset context the manifest does not list would escape the newest-suite
	# and non-required logic below; list it as a manifest context too.
	local c m found
	for c in "${ruleset_contexts[@]}"; do
		found=0
		for m in ${manifest_contexts[@]+"${manifest_contexts[@]}"}; do
			[[ "${m}" != "${c}" ]] || found=1
		done
		[[ "${found}" -eq 1 ]] || manifest_contexts+=("${c}")
	done
	return 0
}

# json_array <item>...: a JSON array of strings (empty input gives []).
json_array() {
	if [[ $# -eq 0 ]]; then
		echo '[]'
	else
		printf '%s\n' "$@" | jq -R . | jq -sc .
	fi
}

# in_list <needle> <item>...: success when needle is one of the items.
in_list() {
	local needle="$1" item
	shift
	for item in "$@"; do
		[[ "${item}" != "${needle}" ]] || return 0
	done
	return 1
}

# disp_ctx and disp_reason are parallel arrays filled by add_disposition from
# repeated --dispositioned <context>=<issue-or-reason> arguments.
disp_ctx=()
disp_reason=()

# add_disposition <context>=<issue-or-reason>: record one disposition. The value
# splits at the first "=": check names hold spaces and slashes (114 of the 164
# live names, such as "queue-selection / select"), and the reason may hold "=".
# Returns 1 when there is no "=", the context or reason is blank, or the context
# holds a tab, newline or carriage return (the red-state table is tab-separated).
add_disposition() {
	local value="$1" ctx reason
	[[ "${value}" == *=* ]] || return 1
	ctx="${value%%=*}"
	reason="${value#*=}"
	[[ "${ctx}" =~ [^[:space:]] && "${reason}" =~ [^[:space:]] ]] || return 1
	[[ "${ctx}" != *[$'\t\r\n']* ]] || return 1
	disp_ctx+=("${ctx}")
	disp_reason+=("${reason}")
}
