#!/usr/bin/env bash
set -euo pipefail

summary=""
measurements=""

usage() {
	cat >&2 <<'USAGE'
Usage: scripts/run-scale-benchmark-measurements.sh --summary PATH --measurements PATH

Builds the public-safe measurement input consumed by
scripts/run-scale-benchmark-artifact.sh from aggregate runtime summary JSON.
The summary must already come from a representative or remote-proof run; this
script only validates and normalizes aggregate measurements.
USAGE
}

die() {
	printf 'run-scale-benchmark-measurements: %s\n' "$*" >&2
	exit 1
}

require_tool() {
	command -v "$1" >/dev/null 2>&1 || die "missing required tool: $1"
}

while (($# > 0)); do
	case "$1" in
		--summary) summary="${2:-}"; shift 2 ;;
		--measurements) measurements="${2:-}"; shift 2 ;;
		-h|--help) usage; exit 0 ;;
		*) die "unknown argument: $1" ;;
	esac
done

require_tool jq
require_tool rg

[[ -n "${summary}" ]] || die "--summary is required"
[[ -n "${measurements}" ]] || die "--measurements is required"
[[ -f "${summary}" ]] || die "summary file not found: ${summary}"
jq -e type "${summary}" >/dev/null || die "summary must be valid JSON"

private_value_pattern='ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|https?://|/security/dependabot|arn:(aws|aws-us-gov|aws-cn):|/(Users|home|private|var|tmp|Volumes|workspace|workspaces|repos|personal-repos)/|(^|[^0-9])[0-9]{12}([^0-9]|$)|([0-9]{1,3}\.){3}[0-9]{1,3}|[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}'

if jq -r 'paths as $path | ($path[] | strings), (getpath($path) | select(type == "string"))' "${summary}" |
	rg --quiet "${private_value_pattern}"; then
	die "summary looks like private data"
fi

require_number() {
	local jq_expr="$1"
	local label="$2"
	jq -er "${jq_expr} | numbers" "${summary}" 2>/dev/null \
		|| die "missing required numeric field: ${label}"
}

require_positive_number() {
	local jq_expr="$1"
	local label="$2"
	local value
	value="$(require_number "${jq_expr}" "${label}")"
	jq -en --argjson value "${value}" '$value > 0' >/dev/null \
		|| die "${label} must be greater than zero"
	printf '%s' "${value}"
}

require_sample_p95() {
	local jq_expr="$1"
	local label="$2"
	jq -e "${jq_expr} | type == \"array\" and length > 0 and all(.[]; type == \"number\" and . >= 0)" "${summary}" >/dev/null \
		|| die "missing required non-negative numeric sample array: ${label}"
	jq -r "${jq_expr} | sort | .[((length * 95 / 100 | ceil) - 1)]" "${summary}"
}

fact_rows="$(require_number '.ingestion.fact_rows' 'ingestion.fact_rows')"
elapsed_seconds="$(require_positive_number '.ingestion.elapsed_seconds' 'ingestion.elapsed_seconds')"
queue_claim_latency_p95_ms="$(require_sample_p95 '.queue.claim_latency_ms_samples' 'queue.claim_latency_ms_samples')"
graph_write_p95_ms="$(require_sample_p95 '.graph.write_latency_ms_samples' 'graph.write_latency_ms_samples')"
api_p95_ms="$(require_sample_p95 '.api.latency_ms_samples' 'api.latency_ms_samples')"
mcp_p95_ms="$(require_sample_p95 '.mcp.latency_ms_samples' 'mcp.latency_ms_samples')"
reducer_drain_seconds="$(require_number '.reducer.drain_seconds' 'reducer.drain_seconds')"
retry_count="$(require_number '.queue.retry_count' 'queue.retry_count')"
dead_letter_count="$(require_number '.queue.dead_letter_count' 'queue.dead_letter_count')"
memory_high_water_mb="$(require_number '.runtime.memory_high_water_mb' 'runtime.memory_high_water_mb')"

fact_rows_per_second="$(jq -n --argjson rows "${fact_rows}" --argjson seconds "${elapsed_seconds}" '$rows / $seconds')"
backend_matrix="$(jq -c '.backend_matrix // {}' "${summary}")"
observability="$(jq -c '.observability // {}' "${summary}")"

mkdir -p "$(dirname "${measurements}")"
jq -n \
	--argjson fact_rows_per_second "${fact_rows_per_second}" \
	--argjson queue_claim_latency_p95_ms "${queue_claim_latency_p95_ms}" \
	--argjson reducer_drain_seconds "${reducer_drain_seconds}" \
	--argjson graph_write_p95_ms "${graph_write_p95_ms}" \
	--argjson api_p95_ms "${api_p95_ms}" \
	--argjson mcp_p95_ms "${mcp_p95_ms}" \
	--argjson retry_count "${retry_count}" \
	--argjson dead_letter_count "${dead_letter_count}" \
	--argjson memory_high_water_mb "${memory_high_water_mb}" \
	--argjson backend_matrix "${backend_matrix}" \
	--argjson observability "${observability}" \
	'{
		metrics: {
			fact_rows_per_second: $fact_rows_per_second,
			queue_claim_latency_p95_ms: $queue_claim_latency_p95_ms,
			reducer_drain_seconds: $reducer_drain_seconds,
			graph_write_p95_ms: $graph_write_p95_ms,
			api_p95_ms: $api_p95_ms,
			mcp_p95_ms: $mcp_p95_ms,
			retry_count: $retry_count,
			dead_letter_count: $dead_letter_count,
			memory_high_water_mb: $memory_high_water_mb
		},
		backend_matrix: $backend_matrix,
		observability: $observability
	}' >"${measurements}"

printf 'run-scale-benchmark-measurements: wrote measurements=%s\n' "${measurements}"
