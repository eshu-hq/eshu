#!/usr/bin/env bash
# Shared validation for the F-9 (#5170) MCP-identity E2E named baseline
# manifest (testdata/golden/auth-mcp-e2e-baseline.json vs the runner's report
# e2e-artifacts/auth-mcp-e2e-report.json). Factored out of
# scripts/verify-auth-mcp-e2e-manifest.sh so the comparison logic is
# unit-testable against fixtures (scripts/test-verify-auth-mcp-e2e-manifest.sh)
# without booting the suite.

auth_mcp_manifest_die() {
	printf 'auth-mcp-e2e-manifest: %s\n' "$*" >&2
	return 1
}

auth_mcp_manifest_require_tools() {
	command -v jq >/dev/null 2>&1 || { auth_mcp_manifest_die "jq is required"; return 1; }
}

# auth_mcp_manifest_validate_baseline_shape checks the committed baseline is a
# well-formed contract (so a corrupted baseline fails loudly rather than
# silently accepting anything).
auth_mcp_manifest_validate_baseline_shape() {
	local baseline="$1"
	jq -e '
		.schema_version == "1" and
		(.max_total_seconds | type == "number" and . > 0) and
		(.steps | type == "array" and length > 0) and
		(.steps | all(.id? != null and .status? != null)) and
		(.denial_outcomes | type == "array" and length >= 3)
	' "${baseline}" >/dev/null 2>&1 \
		|| { auth_mcp_manifest_die "baseline shape is invalid: ${baseline}"; return 1; }
}

# auth_mcp_manifest_compare validates report against baseline:
#   - the report's ordered step-id list EXACTLY equals the baseline's (same
#     ids, same order — catches added, removed, or reordered steps);
#   - each step's status equals the baseline's expected status;
#   - report.totalMs <= baseline.max_total_seconds * 1000.
auth_mcp_manifest_compare() {
	local baseline="$1"
	local report="$2"

	[[ -f "${baseline}" ]] || { auth_mcp_manifest_die "baseline not found: ${baseline}"; return 1; }
	[[ -f "${report}" ]] || { auth_mcp_manifest_die "report not found: ${report}"; return 1; }
	jq -e . "${baseline}" >/dev/null 2>&1 || { auth_mcp_manifest_die "baseline is not valid JSON"; return 1; }
	jq -e . "${report}" >/dev/null 2>&1 || { auth_mcp_manifest_die "report is not valid JSON"; return 1; }
	auth_mcp_manifest_validate_baseline_shape "${baseline}" || return 1

	# Ordered step-id lists must be identical.
	local baseline_ids report_ids
	baseline_ids="$(jq -c '[.steps[].id]' "${baseline}")"
	report_ids="$(jq -c '[.results[].id]' "${report}")"
	if [[ "${baseline_ids}" != "${report_ids}" ]]; then
		auth_mcp_manifest_die "step-id list mismatch (order-sensitive):
  baseline: ${baseline_ids}
  report:   ${report_ids}"
		return 1
	fi

	# Per-step status must match the baseline's expectation.
	local status_mismatch
	status_mismatch="$(jq -r -n --slurpfile b "${baseline}" --slurpfile r "${report}" '
		($b[0].steps | map({key: .id, value: .status}) | from_entries) as $want
		| ($r[0].results | map({key: .id, value: .status}) | from_entries) as $got
		| [ $want | to_entries[] | select($got[.key] != .value)
			| "\(.key): expected \(.value), got \($got[.key] // "<absent>")" ]
		| .[0] // ""
	')"
	if [[ -n "${status_mismatch}" ]]; then
		auth_mcp_manifest_die "step status mismatch: ${status_mismatch}"
		return 1
	fi

	# Runtime bound.
	local within_bound
	within_bound="$(jq -r -n --slurpfile b "${baseline}" --slurpfile r "${report}" '
		($b[0].max_total_seconds * 1000) as $max
		| (($r[0].totalMs // ($max + 1)) <= $max)
	')"
	if [[ "${within_bound}" != "true" ]]; then
		local total_ms max_s
		total_ms="$(jq -r '.totalMs // "<absent>"' "${report}")"
		max_s="$(jq -r '.max_total_seconds' "${baseline}")"
		auth_mcp_manifest_die "runtime bound exceeded: report totalMs=${total_ms} > max_total_seconds=${max_s} (${max_s}000ms)"
		return 1
	fi
}

validate_auth_mcp_e2e_manifest() {
	local baseline="${1:-}"
	local report="${2:-}"
	[[ -n "${baseline}" && -n "${report}" ]] || {
		auth_mcp_manifest_die "usage: validate_auth_mcp_e2e_manifest <baseline.json> <report.json>"
		return 1
	}
	auth_mcp_manifest_require_tools || return 1
	auth_mcp_manifest_compare "${baseline}" "${report}" || return 1
	printf 'auth-mcp-e2e-manifest: pass (%s steps, in order, all matching status, within runtime bound)\n' \
		"$(jq -r '.steps | length' "${baseline}")"
}
