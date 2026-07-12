#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
spec="${repo_root}/specs/scale-lab-corpus.v1.yaml"

usage() {
	cat >&2 <<'USAGE'
Usage: scripts/verify-scale-corpus-suite.sh [--spec PATH]

Validates the public-safe scale-lab representative corpus contract used before
reducer, graph-write, API, MCP, and correlation performance implementation.
USAGE
}

die() {
	printf 'verify-scale-corpus-suite: %s\n' "$*" >&2
	exit 1
}

require_tool() {
	command -v "$1" >/dev/null 2>&1 || die "missing required tool: $1"
}

while (($# > 0)); do
	case "$1" in
		--spec) spec="${2:-}"; shift 2 ;;
		-h|--help) usage; exit 0 ;;
		*) die "unknown argument: $1" ;;
	esac
done

require_tool rg
[[ -n "${spec}" ]] || die "--spec must not be empty"
[[ -f "${spec}" ]] || die "spec file not found: ${spec}"

private_value_pattern='ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|https?://|arn:(aws|aws-us-gov|aws-cn):|/(Users|home|private|var|tmp|Volumes|workspace|workspaces|repos)/|(^|[^0-9])[0-9]{12}([^0-9]|$)|([0-9]{1,3}\.){3}[0-9]{1,3}|[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}'
if rg --quiet "${private_value_pattern}" "${spec}"; then
	die "spec looks like private data; keep only aggregate counts, public issue numbers, and fixture descriptors"
fi

require_fixed() {
	local label="$1"
	local value="$2"
	if ! rg --fixed-strings --quiet -- "${value}" "${spec}"; then
		die "missing ${label}: ${value}"
	fi
}

require_fixed "version" "version: scale-lab-corpus/v1"
require_fixed "parent epic" "parent_issue: 3169"
require_fixed "owner issue" "issue: 3170"
require_fixed "approval gate" "gate_status: accepted"

required_slots=(
	"smoke/synthetic_contracts"
	"small/single_repo_multidomain"
	"medium/representative_20_50"
	"large/full_corpus_release"
	"pathological/fanout_correlation"
)
for slot in "${required_slots[@]}"; do
	if ! rg --fixed-strings --quiet -- "- id: ${slot}" "${spec}"; then
		die "missing required corpus slot: ${slot}"
	fi
done

required_domains=(
	"code_relationships"
	"supply_chain_evidence"
	"cloud_iac_runtime_correlation"
	"docs"
	"incidents"
	"observability"
)
for domain in "${required_domains[@]}"; do
	if ! rg --fixed-strings --quiet -- "- id: ${domain}" "${spec}"; then
		die "missing required domain: ${domain}"
	fi
done

required_privacy_rules=(
	"no_private_identifiers"
	"aggregate_public_outputs"
	"fixture_sanitization"
	"local_private_manifest_only"
)
for rule in "${required_privacy_rules[@]}"; do
	if ! rg --fixed-strings --quiet -- "- id: ${rule}" "${spec}"; then
		die "missing required privacy rule: ${rule}"
	fi
done

required_metrics=(
	"fact_rows_per_second"
	"queue_claim_latency_p95_ms"
	"reducer_drain_seconds"
	"graph_write_p95_ms"
	"api_p95_ms"
	"mcp_p95_ms"
	"retry_count"
	"dead_letter_count"
	"memory_high_water_mb"
	"correlation_fanout_candidates_p95"
	"graph_query_plan_regression_count"
)
for metric in "${required_metrics[@]}"; do
	if ! rg --fixed-strings --quiet -- "- id: ${metric}" "${spec}"; then
		die "missing required metric: ${metric}"
	fi
done

required_thresholds=(
	"queue_terminal_state"
	"runtime_no_regression"
	"query_plan_regression"
	"privacy_public_evidence"
	"truth_surface_agreement"
)
for threshold in "${required_thresholds[@]}"; do
	if ! rg --fixed-strings --quiet -- "- id: ${threshold}" "${spec}"; then
		die "missing required threshold: ${threshold}"
	fi
done

for issue in 3171 3172 3173; do
	require_fixed "downstream gate issue" "issue: ${issue}"
done

printf 'verify-scale-corpus-suite: pass spec=%s\n' "${spec}"
