#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
spec="${repo_root}/specs/scale-benchmark-artifact.v1.yaml"
artifact=""

usage() {
	cat >&2 <<'USAGE'
Usage: scripts/verify-scale-benchmark-artifact.sh [--artifact PATH] [--spec PATH]

Validates the public-safe scale benchmark artifact contract for issue #3171.
When --artifact is supplied, validates a benchmark result JSON. Without
--artifact, validates the repository contract spec.
USAGE
}

die() {
	printf 'verify-scale-benchmark-artifact: %s\n' "$*" >&2
	exit 1
}

require_tool() {
	command -v "$1" >/dev/null 2>&1 || die "missing required tool: $1"
}

while (($# > 0)); do
	case "$1" in
		--artifact) artifact="${2:-}"; shift 2 ;;
		--spec) spec="${2:-}"; shift 2 ;;
		-h|--help) usage; exit 0 ;;
		*) die "unknown argument: $1" ;;
	esac
done

require_tool jq
require_tool rg

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

jq_path() {
	local dotted="$1"
	local jq_path=""
	local part
	local -a parts
	IFS='.' read -r -a parts <<<"${dotted}"
	for part in "${parts[@]}"; do
		jq_path="${jq_path}.${part}"
	done
	printf '%s' "${jq_path}"
}

require_json_path() {
	local file="$1"
	local dotted="$2"
	local path
	path="$(jq_path "${dotted}")"
	jq -e "${path} != null" "${file}" >/dev/null \
		|| die "missing required benchmark evidence: ${dotted}"
}

validate_privacy_json() {
	local file="$1"
	local forbidden_keys='[
		"repository","repositories","repository_name","repository_id",
		"repo","repo_name","repo_id","package","packages","package_name",
		"package_id","provider_url","alert_url","installation",
		"provider_repository","url","host","hostname","ip","path","file",
		"token","payload","description","cve_description","transcript",
		"stdout","stderr","request","response","body","account_id","account"
	]'

	jq -e --argjson forbidden "${forbidden_keys}" '
		[.. | objects | keys[]? | select(. as $key | $forbidden | index($key))]
		| length == 0
	' "${file}" >/dev/null \
		|| die "artifact looks like private data; forbidden private-looking keys are not accepted"

	if jq -r '
		paths(strings) as $path
		| select($path != ["run", "commit"])
		| select($path != ["comparison", "baseline_commit"])
		| getpath($path)
	' "${file}" | rg --quiet "${private_value_pattern}"; then
		die "artifact looks like private data; only aggregate counts, status enums, and public issue refs are accepted"
	fi
}

validate_spec() {
	[[ -n "${spec}" ]] || die "--spec must not be empty"
	[[ -f "${spec}" ]] || die "spec file not found: ${spec}"

	if rg --quiet "${private_value_pattern}" "${spec}"; then
		die "spec looks like private data"
	fi

	local required_text=(
		"version: scale-benchmark-artifact/v1"
		"parent_issue: 3169"
		"issue: 3171"
		"schema_version: scale-benchmark-artifact/v1"
		"commit_sha: required"
		"backend_matrix: required"
		"before_after_evidence: required_for_optimization"
	)
	local text
	for text in "${required_text[@]}"; do
		rg --fixed-strings --quiet -- "${text}" "${spec}" \
			|| die "missing contract text: ${text}"
	done

	local metric
	for metric in "${required_metrics[@]}"; do
		rg --fixed-strings --quiet -- "- id: ${metric}" "${spec}" \
			|| die "missing required metric contract: ${metric}"
	done

	printf 'verify-scale-benchmark-artifact: pass spec=%s\n' "${spec}"
}

validate_artifact_shape() {
	local file="$1"
	jq -e '
		.schema_version == "scale-benchmark-artifact/v1" and
		(.status | IN("pass", "partial", "fail")) and
		(.run.kind | IN("baseline", "after", "regression_check")) and
		(.run.issue == 3171) and
		(.run.gate | IN("ci", "remote-compose", "remote-proof")) and
		(.run.metadata_recorded == true) and
		(.run.backend.kind | IN("nornicdb", "neo4j")) and
		(.corpus.contract_version == "scale-lab-corpus/v1") and
		(.corpus.slot | IN("smoke/synthetic_contracts", "small/single_repo_multidomain", "medium/representative_20_50", "large/full_corpus_release", "pathological/fanout_correlation")) and
		(.corpus.mode | IN("smoke", "representative", "full")) and
		(.corpus.repository_count | type == "number" and . >= 0) and
		(.corpus.privacy_status == "public_safe") and
		(.privacy.status == "pass")
	' "${file}" >/dev/null || die "artifact root shape is invalid"

	jq -e '.run.commit | test("^[0-9a-f]{40}$")' "${file}" >/dev/null \
		|| die "run.commit must be a 40-character lowercase commit SHA"
}

validate_required_paths() {
	local file="$1"
	local required_paths=(
		"run.id"
		"run.kind"
		"run.commit"
		"run.issue"
		"run.gate"
		"run.metadata_recorded"
		"run.backend.kind"
		"run.backend.version"
		"corpus.contract_version"
		"corpus.slot"
		"corpus.mode"
		"corpus.repository_count"
		"corpus.privacy_status"
		"backend_matrix.nornicdb.status"
		"backend_matrix.compatibility.status"
		"comparison.optimization_claimed"
		"comparison.result"
		"artifacts.results"
		"artifacts.thresholds"
		"artifacts.pprof"
		"artifacts.logs"
		"observability.pprof_status"
		"observability.logs_status"
		"observability.resource_snapshot_status"
		"privacy.status"
	)
	local dotted
	for dotted in "${required_paths[@]}"; do
		require_json_path "${file}" "${dotted}"
	done

	local metric
	for metric in "${required_metrics[@]}"; do
		require_json_path "${file}" "metrics.${metric}"
	done
}

validate_backend_matrix() {
	local file="$1"
	local backend
	for backend in nornicdb compatibility; do
		jq -e --arg backend "${backend}" '
			(.backend_matrix[$backend].status | IN("pass", "fail", "skipped", "unsupported"))
		' "${file}" >/dev/null || die "backend_matrix.${backend}.status is invalid"

		if jq -e --arg backend "${backend}" '.backend_matrix[$backend].status | IN("skipped", "unsupported")' "${file}" >/dev/null; then
			jq -e --arg backend "${backend}" '(.backend_matrix[$backend].reason // "" | length) > 0' "${file}" >/dev/null \
				|| die "backend_matrix.${backend} unsupported requires reason"
		else
			jq -e --arg backend "${backend}" '(.backend_matrix[$backend].artifact // "" | length) > 0' "${file}" >/dev/null \
				|| die "backend_matrix.${backend} pass/fail requires artifact"
		fi
	done

	if jq -e '.status == "pass"' "${file}" >/dev/null; then
		for backend in nornicdb compatibility; do
			jq -e --arg backend "${backend}" '.backend_matrix[$backend].status != "fail"' "${file}" >/dev/null \
				|| die "status pass cannot include failing backend ${backend}"
		done
		jq -e '.backend_matrix.nornicdb.status == "pass"' "${file}" >/dev/null \
			|| die "status pass requires nornicdb backend evidence to pass"
	fi
}

validate_metrics() {
	local file="$1"
	local metric
	for metric in "${required_metrics[@]}"; do
		jq -e --arg metric "${metric}" '
			.metrics[$metric] as $row
			| ($row.stage | type == "string" and length > 0) and
			  ($row.unit | type == "string" and length > 0) and
			  ($row.value | type == "number") and
			  ($row.threshold | type == "number") and
			  ($row.threshold_result | IN("pass", "fail"))
		' "${file}" >/dev/null \
			|| die "metrics.${metric} requires numeric value and threshold"
	done

	if jq -e '.status == "pass"' "${file}" >/dev/null; then
		local failed_metric
		failed_metric="$(jq -r '
			.metrics
			| to_entries
			| map(select(.value.threshold_result != "pass"))
			| .[0].key // ""
		' "${file}")"
		[[ -z "${failed_metric}" ]] || die "status pass cannot include failing metric ${failed_metric}"

		jq -e '.metrics.retry_count.value == 0 and .metrics.dead_letter_count.value == 0' "${file}" >/dev/null \
			|| die "status pass requires zero retry_count and dead_letter_count"
	fi
}

validate_artifact_handles() {
	local file="$1"
	local handle
	for handle in results thresholds pprof logs; do
		jq -e --arg handle "${handle}" '
			.artifacts[$handle] | type == "string" and length > 0
		' "${file}" >/dev/null || die "artifacts.${handle} must be a non-empty string"
	done
}

validate_comparison() {
	local file="$1"
	jq -e '.comparison.optimization_claimed | type == "boolean"' "${file}" >/dev/null \
		|| die "comparison.optimization_claimed must be boolean"

	jq -e '.comparison.result | IN("not_applicable", "no_regression", "improved", "regressed")' "${file}" >/dev/null \
		|| die "comparison.result is invalid"

	if jq -e '.comparison.optimization_claimed == true' "${file}" >/dev/null; then
		jq -e '
			(.comparison.baseline_commit | type == "string" and test("^[0-9a-f]{40}$")) and
			(.comparison.baseline_artifact | type == "string" and length > 0) and
			(.comparison.result | IN("no_regression", "improved", "regressed"))
		' "${file}" >/dev/null || die "optimization claims require before/after evidence"
	fi
}

validate_observability() {
	local file="$1"
	local status
	for status in pprof_status logs_status resource_snapshot_status; do
		jq -e --arg status "${status}" '.observability[$status] | IN("pass", "fail", "skipped")' "${file}" >/dev/null \
			|| die "observability.${status} is invalid"
	done

	if jq -e '.status == "pass"' "${file}" >/dev/null; then
		for status in pprof_status logs_status resource_snapshot_status; do
			jq -e --arg status "${status}" '.observability[$status] != "fail"' "${file}" >/dev/null \
				|| die "status pass cannot include failing observability ${status}"
		done
	fi
}

validate_artifact() {
	[[ -n "${artifact}" ]] || die "--artifact must not be empty"
	[[ -f "${artifact}" ]] || die "artifact file not found: ${artifact}"

	validate_privacy_json "${artifact}"
	validate_required_paths "${artifact}"
	validate_artifact_shape "${artifact}"
	validate_backend_matrix "${artifact}"
	validate_metrics "${artifact}"
	validate_artifact_handles "${artifact}"
	validate_comparison "${artifact}"
	validate_observability "${artifact}"

	printf 'verify-scale-benchmark-artifact: pass artifact=%s\n' "${artifact}"
}

if [[ -n "${artifact}" ]]; then
	validate_artifact
else
	validate_spec
fi
