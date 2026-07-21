#!/usr/bin/env bash
# refresh-auth-mcp-e2e-baseline.sh — recapture the F-9 (#5170) MCP-identity
# E2E named baseline (testdata/golden/auth-mcp-e2e-baseline.json) from a real
# green suite run.
#
# Run this after an intentional change to the suite's step set (a step added,
# removed, renamed, or reordered), then REVIEW THE DIFF before committing — a
# baseline bump is a reviewed claim that the new step list/order is the expected
# contract. The human-curated fields (description, denial_outcomes,
# max_total_seconds, run.backend.kind, baseline_id) are PRESERVED; only the
# ordered steps[] list (ids + statuses) is recaptured from the fresh report.
#
# By default it runs the full suite to produce the report; pass --report <path>
# to fold an existing report instead (e.g. e2e-artifacts/auth-mcp-e2e-report.json
# from a run you just did).
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

baseline="${repo_root}/testdata/golden/auth-mcp-e2e-baseline.json"
report="${repo_root}/e2e-artifacts/auth-mcp-e2e-report.json"
run_suite=true

while [[ $# -gt 0 ]]; do
	case "$1" in
	--report)
		report="${2:?--report requires a path}"
		run_suite=false
		shift 2
		;;
	*)
		printf 'refresh-auth-mcp-e2e-baseline: unknown argument: %s\n' "$1" >&2
		exit 1
		;;
	esac
done

die() {
	printf 'refresh-auth-mcp-e2e-baseline: %s\n' "$*" >&2
	exit 1
}

command -v jq >/dev/null 2>&1 || die "missing required tool: jq"
[[ -f "${baseline}" ]] || die "baseline not found: ${baseline}"

if [[ "${run_suite}" == "true" ]]; then
	printf 'refresh-auth-mcp-e2e-baseline: running the full suite to capture a fresh report...\n' >&2
	bash "${repo_root}/scripts/run-auth-mcp-e2e.sh"
fi

[[ -f "${report}" ]] || die "report not found: ${report}"
jq -e . "${report}" >/dev/null 2>&1 || die "report is not valid JSON: ${report}"

# Refuse to fold a report that contains any failed step — the baseline is a
# KNOWN-GOOD contract, never captured from a red run.
if jq -e '[.results[] | select(.status != "pass")] | length > 0' "${report}" >/dev/null; then
	die "report contains non-pass steps; refusing to capture a baseline from a red run"
fi

# Fold the fresh ordered steps (id + status) into the baseline, preserving
# every curated field.
updated="$(jq --slurpfile r "${report}" '
	.steps = ($r[0].results | map({id: .id, status: .status}))
' "${baseline}")" || die "failed to merge the report into the baseline"

printf '%s\n' "${updated}" >"${baseline}"
printf 'refresh-auth-mcp-e2e-baseline: updated %s (%s steps) — review the diff before committing\n' \
	"${baseline}" "$(jq -r '.steps | length' "${baseline}")" >&2
