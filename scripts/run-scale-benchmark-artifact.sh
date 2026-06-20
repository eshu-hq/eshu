#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-scale-benchmark-artifact.sh"

artifact=""
measurements=""
thresholds=""
run_kind=""
gate=""
commit_sha=""
backend_kind=""
backend_version=""
corpus_mode=""
corpus_slot=""
repository_count=""
compatibility_status="unsupported"
compatibility_reason=""
compatibility_artifact=""
optimization_claimed="false"
baseline_commit=""
baseline_artifact=""
comparison_result="not_applicable"
results_handle=""
thresholds_handle=""
pprof_handle="scale-benchmark-pprof.txt"
logs_handle="scale-benchmark-logs.txt"
verify_artifact="false"

usage() {
	cat >&2 <<'USAGE'
Usage: scripts/run-scale-benchmark-artifact.sh --artifact PATH --measurements PATH --thresholds PATH [options]

Builds a public-safe scale benchmark artifact from aggregate measurements and
thresholds. Use --verify to validate the produced artifact before returning.
USAGE
}

die() {
	printf 'run-scale-benchmark-artifact: %s\n' "$*" >&2
	exit 1
}

require_tool() {
	command -v "$1" >/dev/null 2>&1 || die "missing required tool: $1"
}

while (($# > 0)); do
	case "$1" in
		--artifact) artifact="${2:-}"; shift 2 ;;
		--measurements) measurements="${2:-}"; shift 2 ;;
		--thresholds) thresholds="${2:-}"; shift 2 ;;
		--run-kind) run_kind="${2:-}"; shift 2 ;;
		--gate) gate="${2:-}"; shift 2 ;;
		--commit) commit_sha="${2:-}"; shift 2 ;;
		--backend-kind) backend_kind="${2:-}"; shift 2 ;;
		--backend-version) backend_version="${2:-}"; shift 2 ;;
		--corpus-mode) corpus_mode="${2:-}"; shift 2 ;;
		--corpus-slot) corpus_slot="${2:-}"; shift 2 ;;
		--repository-count) repository_count="${2:-}"; shift 2 ;;
		--compatibility-status) compatibility_status="${2:-}"; shift 2 ;;
		--compatibility-reason) compatibility_reason="${2:-}"; shift 2 ;;
		--compatibility-artifact) compatibility_artifact="${2:-}"; shift 2 ;;
		--optimization-claimed) optimization_claimed="${2:-}"; shift 2 ;;
		--baseline-commit) baseline_commit="${2:-}"; shift 2 ;;
		--baseline-artifact) baseline_artifact="${2:-}"; shift 2 ;;
		--comparison-result) comparison_result="${2:-}"; shift 2 ;;
		--results-handle) results_handle="${2:-}"; shift 2 ;;
		--thresholds-handle) thresholds_handle="${2:-}"; shift 2 ;;
		--pprof-handle) pprof_handle="${2:-}"; shift 2 ;;
		--logs-handle) logs_handle="${2:-}"; shift 2 ;;
		--verify) verify_artifact="true"; shift ;;
		-h|--help) usage; exit 0 ;;
		*) die "unknown argument: $1" ;;
	esac
done

require_tool jq
require_tool rg

[[ -n "${artifact}" ]] || die "--artifact is required"
[[ -n "${measurements}" ]] || die "--measurements is required"
[[ -n "${thresholds}" ]] || die "--thresholds is required"
[[ -f "${measurements}" ]] || die "measurements file not found: ${measurements}"
[[ -f "${thresholds}" ]] || die "thresholds file not found: ${thresholds}"
jq -e type "${measurements}" >/dev/null || die "measurements must be valid JSON"
jq -e type "${thresholds}" >/dev/null || die "thresholds must be valid JSON"

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
)

private_value_pattern='ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|https?://|/security/dependabot|arn:(aws|aws-us-gov|aws-cn):|/(Users|home|private|var|tmp|Volumes|workspace|workspaces|repos|personal-repos)/|(^|[^0-9])[0-9]{12}([^0-9]|$)|([0-9]{1,3}\.){3}[0-9]{1,3}|[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}'

validate_aggregate_input() {
	local file="$1"
	local label="$2"
	if jq -r 'paths(strings) as $path | getpath($path)' "${file}" | rg --quiet "${private_value_pattern}"; then
		die "${label} looks like private data"
	fi
}

metric_stage() {
	case "$1" in
		fact_rows_per_second) printf 'ingestion' ;;
		queue_claim_latency_p95_ms|retry_count|dead_letter_count) printf 'queue' ;;
		reducer_drain_seconds) printf 'reducer' ;;
		graph_write_p95_ms) printf 'graph' ;;
		api_p95_ms) printf 'api' ;;
		mcp_p95_ms) printf 'mcp' ;;
		memory_high_water_mb) printf 'runtime' ;;
		*) die "unknown metric: $1" ;;
	esac
}

metric_unit() {
	case "$1" in
		fact_rows_per_second) printf 'rows_per_second' ;;
		queue_claim_latency_p95_ms|graph_write_p95_ms|api_p95_ms|mcp_p95_ms) printf 'milliseconds' ;;
		reducer_drain_seconds) printf 'seconds' ;;
		retry_count|dead_letter_count) printf 'count' ;;
		memory_high_water_mb) printf 'mebibytes' ;;
		*) die "unknown metric: $1" ;;
	esac
}

require_number() {
	local file="$1"
	local jq_expr="$2"
	local message="$3"
	jq -er "${jq_expr} | numbers" "${file}" 2>/dev/null || die "${message}"
}

require_string_enum() {
	local file="$1"
	local jq_expr="$2"
	local message="$3"
	jq -er "${jq_expr} | strings" "${file}" 2>/dev/null || die "${message}"
}

json_string_or_default() {
	local file="$1"
	local jq_expr="$2"
	local default="$3"
	jq -er "${jq_expr} | strings" "${file}" 2>/dev/null || printf '%s' "${default}"
}

sanitize_path_handle() {
	local value="$1"
	value="${value##*/}"
	[[ -n "${value}" ]] || die "artifact handle must not be empty"
	if rg --quiet "${private_value_pattern}" <<<"${value}"; then
		die "artifact handle looks like private data"
	fi
	printf '%s' "${value}"
}

sanitize_explicit_handle() {
	local value="$1"
	[[ -n "${value}" ]] || die "artifact handle must not be empty"
	if [[ "${value}" == */* ]] || rg --quiet "${private_value_pattern}" <<<"${value}"; then
		die "artifact handle looks like private data"
	fi
	printf '%s' "${value}"
}

validate_aggregate_input "${measurements}" "measurements"
validate_aggregate_input "${thresholds}" "thresholds"

[[ "${commit_sha}" =~ ^[0-9a-f]{40}$ ]] || die "run.commit must be a 40-character lowercase commit SHA"
[[ "${run_kind}" =~ ^(baseline|after|regression_check)$ ]] || die "run.kind is invalid"
[[ "${gate}" =~ ^(ci|remote-compose|remote-proof)$ ]] || die "run.gate is invalid"
[[ "${backend_kind}" =~ ^(nornicdb|neo4j)$ ]] || die "run.backend.kind is invalid"
[[ -n "${backend_version}" ]] || die "run.backend.version is required"
[[ "${corpus_slot}" =~ ^(smoke/synthetic_contracts|small/single_repo_multidomain|medium/representative_20_50|large/full_corpus_release|pathological/fanout_correlation)$ ]] \
	|| die "corpus.slot is invalid"
[[ "${corpus_mode}" =~ ^(smoke|representative|full)$ ]] || die "corpus.mode is invalid"
[[ "${optimization_claimed}" == "true" || "${optimization_claimed}" == "false" ]] \
	|| die "--optimization-claimed must be true or false"

if [[ "${optimization_claimed}" == "true" ]]; then
	[[ "${baseline_commit}" =~ ^[0-9a-f]{40}$ ]] \
		|| die "comparison.baseline_commit must be a 40-character lowercase commit SHA"
	[[ -n "${baseline_artifact}" ]] || die "optimization claims require --baseline-artifact"
	[[ "${comparison_result}" =~ ^(no_regression|improved|regressed)$ ]] \
		|| die "optimization claims require no_regression, improved, or regressed comparison result"
else
	baseline_commit=""
	baseline_artifact=""
	comparison_result="not_applicable"
fi

if [[ -z "${results_handle}" ]]; then
	results_handle="$(sanitize_path_handle "${measurements}")"
else
	results_handle="$(sanitize_explicit_handle "${results_handle}")"
fi
if [[ -z "${thresholds_handle}" ]]; then
	thresholds_handle="$(sanitize_path_handle "${thresholds}")"
else
	thresholds_handle="$(sanitize_explicit_handle "${thresholds_handle}")"
fi
pprof_handle="$(sanitize_explicit_handle "${pprof_handle}")"
logs_handle="$(sanitize_explicit_handle "${logs_handle}")"

metrics_json="$(jq -n '{}')"
artifact_status="pass"
for metric in "${required_metrics[@]}"; do
	value="$(require_number "${measurements}" ".metrics[\"${metric}\"]" "missing required measurement: metrics.${metric}")"
	threshold="$(require_number "${thresholds}" ".metrics[\"${metric}\"].threshold" "missing required threshold: metrics.${metric}.threshold")"
	direction="$(require_string_enum "${thresholds}" ".metrics[\"${metric}\"].direction" "missing required threshold direction: metrics.${metric}.direction")"
	[[ "${direction}" == "min" || "${direction}" == "max" ]] \
		|| die "metrics.${metric}.direction must be min or max"

	if jq -en --arg direction "${direction}" --argjson value "${value}" --argjson threshold "${threshold}" '
		if $direction == "min" then $value >= $threshold else $value <= $threshold end
	' >/dev/null; then
		threshold_result="pass"
	else
		threshold_result="fail"
		artifact_status="fail"
	fi

	metrics_json="$(jq \
		--arg metric "${metric}" \
		--arg stage "$(metric_stage "${metric}")" \
		--arg unit "$(metric_unit "${metric}")" \
		--argjson value "${value}" \
		--argjson threshold "${threshold}" \
		--arg threshold_result "${threshold_result}" \
		'.[$metric] = {
			stage: $stage,
			unit: $unit,
			value: $value,
			threshold: $threshold,
			threshold_result: $threshold_result
		}' <<<"${metrics_json}")"
done

nornicdb_status="$(json_string_or_default "${measurements}" '.backend_matrix.nornicdb.status' 'fail')"
nornicdb_artifact="$(json_string_or_default "${measurements}" '.backend_matrix.nornicdb.artifact' '')"
nornicdb_reason="$(json_string_or_default "${measurements}" '.backend_matrix.nornicdb.reason' '')"
[[ "${nornicdb_status}" =~ ^(pass|fail|skipped|unsupported)$ ]] \
	|| die "backend_matrix.nornicdb.status is invalid"
if [[ "${nornicdb_status}" == "pass" || "${nornicdb_status}" == "fail" ]]; then
	[[ -n "${nornicdb_artifact}" ]] || die "backend_matrix.nornicdb pass/fail requires artifact"
	nornicdb_artifact="$(sanitize_explicit_handle "${nornicdb_artifact}")"
else
	[[ -n "${nornicdb_reason}" ]] || die "backend_matrix.nornicdb unsupported requires reason"
fi
[[ "${nornicdb_status}" == "pass" ]] || artifact_status="fail"

[[ "${compatibility_status}" =~ ^(pass|fail|skipped|unsupported)$ ]] \
	|| die "backend_matrix.compatibility.status is invalid"
if [[ "${compatibility_status}" == "pass" || "${compatibility_status}" == "fail" ]]; then
	[[ -n "${compatibility_artifact}" ]] || die "backend_matrix.compatibility pass/fail requires artifact"
	compatibility_artifact="$(sanitize_explicit_handle "${compatibility_artifact}")"
else
	[[ -n "${compatibility_reason}" ]] || die "backend_matrix.compatibility unsupported requires reason"
fi
[[ "${compatibility_status}" != "fail" ]] || artifact_status="fail"

pprof_status="$(json_string_or_default "${measurements}" '.observability.pprof_status' 'skipped')"
logs_status="$(json_string_or_default "${measurements}" '.observability.logs_status' 'skipped')"
resource_snapshot_status="$(json_string_or_default "${measurements}" '.observability.resource_snapshot_status' 'skipped')"
for observability_status in "${pprof_status}" "${logs_status}" "${resource_snapshot_status}"; do
	[[ "${observability_status}" =~ ^(pass|fail|skipped)$ ]] || die "observability status is invalid"
	[[ "${observability_status}" != "fail" ]] || artifact_status="fail"
done

repository_count_number="$(jq -en --argjson count "${repository_count}" '$count | numbers' 2>/dev/null)" \
	|| die "--repository-count must be numeric"

mkdir -p "$(dirname "${artifact}")"
jq -n \
	--arg status "${artifact_status}" \
	--arg run_id "scale-bench-${commit_sha:0:8}" \
	--arg run_kind "${run_kind}" \
	--arg commit "${commit_sha}" \
	--arg gate "${gate}" \
	--arg backend_kind "${backend_kind}" \
	--arg backend_version "${backend_version}" \
	--arg corpus_slot "${corpus_slot}" \
	--arg corpus_mode "${corpus_mode}" \
	--argjson repository_count "${repository_count_number}" \
	--arg nornicdb_status "${nornicdb_status}" \
	--arg nornicdb_artifact "${nornicdb_artifact}" \
	--arg nornicdb_reason "${nornicdb_reason}" \
	--arg compatibility_status "${compatibility_status}" \
	--arg compatibility_artifact "${compatibility_artifact}" \
	--arg compatibility_reason "${compatibility_reason}" \
	--argjson optimization_claimed "${optimization_claimed}" \
	--arg baseline_commit "${baseline_commit}" \
	--arg baseline_artifact "${baseline_artifact}" \
	--arg comparison_result "${comparison_result}" \
	--arg results_handle "${results_handle}" \
	--arg thresholds_handle "${thresholds_handle}" \
	--arg pprof_handle "${pprof_handle}" \
	--arg logs_handle "${logs_handle}" \
	--arg pprof_status "${pprof_status}" \
	--arg logs_status "${logs_status}" \
	--arg resource_snapshot_status "${resource_snapshot_status}" \
	--argjson metrics "${metrics_json}" \
	'{
		schema_version: "scale-benchmark-artifact/v1",
		status: $status,
		run: {
			id: $run_id,
			kind: $run_kind,
			commit: $commit,
			issue: 3171,
			gate: $gate,
			metadata_recorded: true,
			backend: {kind: $backend_kind, version: $backend_version}
		},
		corpus: {
			contract_version: "scale-lab-corpus/v1",
			slot: $corpus_slot,
			mode: $corpus_mode,
			repository_count: $repository_count,
			privacy_status: "public_safe"
		},
		backend_matrix: {
			nornicdb: {status: $nornicdb_status, artifact: $nornicdb_artifact, reason: $nornicdb_reason},
			compatibility: {status: $compatibility_status, artifact: $compatibility_artifact, reason: $compatibility_reason}
		},
		comparison: {
			optimization_claimed: $optimization_claimed,
			baseline_commit: (if $baseline_commit == "" then null else $baseline_commit end),
			baseline_artifact: (if $baseline_artifact == "" then null else $baseline_artifact end),
			result: $comparison_result
		},
		artifacts: {
			results: $results_handle,
			thresholds: $thresholds_handle,
			pprof: $pprof_handle,
			logs: $logs_handle
		},
		metrics: $metrics,
		observability: {
			pprof_status: $pprof_status,
			logs_status: $logs_status,
			resource_snapshot_status: $resource_snapshot_status
		},
		privacy: {status: "pass"}
	}' >"${artifact}"

if [[ "${verify_artifact}" == "true" ]]; then
	"${verifier}" --artifact "${artifact}"
fi

printf 'run-scale-benchmark-artifact: wrote artifact=%s status=%s\n' "${artifact}" "${artifact_status}"
