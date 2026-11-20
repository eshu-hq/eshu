#!/usr/bin/env bash
#
# ci-gate-leaf-filter-refs.sh — shared dorny filter resolution for
# verify-docs-only-ci-skip.sh and its planner-checks companion.
#
# The three code-gated workflows consume the single generated
# .github/leaf-code-filter.yml (and .github/leaf-lockstep-filter.yml) instead
# of inline copies (#10980). These helpers resolve one dorny step's `filters:`
# value — a shared file reference or a legacy inline block — so every guard
# agrees about what "the workflow's filter" means. Lives here rather than
# inline to keep verify-docs-only-ci-skip.sh under the repo's 500-line file
# cap. Callers supply job_block/step_block (whole-job/step extraction), has,
# ok/bad counters, and repo_root; resolution happens at call time, so source
# order relative to those definitions does not matter.

# The single-source generated leaf filters (#10980). Every code-gated
# workflow's changes steps consume these instead of inline copies.
SHARED_CODE_FILTER=".github/leaf-code-filter.yml"
SHARED_LOCKSTEP_FILTER=".github/leaf-lockstep-filter.yml"

# step_filters_ref <workflow> <step-id> — the `filters:` value of one dorny
# step: `inline` for a `filters: |` block, a repo-relative path for a shared
# `filters: <file>` reference, or empty when the step is missing.
step_filters_ref() {
	local block line
	block="$(step_block "$(job_block "$1" changes)" "$2")"
	[[ -n "${block}" ]] || return 0
	line="$(printf '%s\n' "${block}" | awk '/^[[:space:]]*filters:/{print; exit}')"
	[[ -n "${line}" ]] || return 0
	if [[ "${line}" =~ ^[[:space:]]*filters:[[:space:]]*\| ]]; then
		printf 'inline'
	else
		printf '%s' "${line}" | sed 's/^[[:space:]]*filters:[[:space:]]*//'
	fi
}

# filter_block_for <workflow> <step-id> <key> — the `<key>:` filter block and
# its `- '...'` entries for one dorny step, resolving a shared `filters:`
# file reference as well as inline blocks. Prints nothing when neither
# resolves, so callers fail closed on the empty input instead of passing
# vacuously.
filter_block_for() {
	local wf="$1" step_id="$2" key="$3"
	local ref="${SHARED_CODE_FILTER}"
	[[ "${key}" == lockstep ]] && ref="${SHARED_LOCKSTEP_FILTER}"
	if [[ "$(step_filters_ref "${wf}" "${step_id}")" == "${ref}" ]]; then
		awk -v key="${key}:" '
			$0 == key { grab = 1; print; next }
			grab {
				if ($0 !~ /^[[:space:]]*- /) { exit }
				print
				next
			}
		' "${repo_root}/${ref}"
		return 0
	fi
	# Inline block (pre-#10980 shape, kept for back-compat).
	local block
	block="$(step_block "$(job_block "${wf}" changes)" "${step_id}")"
	[[ -n "${block}" ]] || return 0
	awk -v key="${key}:" '
		$0 ~ "^[[:space:]]*" key "[[:space:]]*$" { grab = 1; print; next }
		grab {
			if ($0 !~ /^[[:space:]]*- /) { exit }
			print
			next
		}
	' <<<"${block}"
}

# code_filter_block <file> — the resolved `code:` filter block for a
# workflow's changes/filter step: the shared single-source file when the
# step consumes it, else the legacy inline block.
code_filter_block() {
	filter_block_for "$1" filter code
}
