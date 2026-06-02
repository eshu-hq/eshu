#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
MANIFEST_VALIDATOR="${REPO_ROOT}/scripts/verify_e2e_evidence_manifest.sh"
RUNTIME_STATE_SCRIPT="${ESHU_E2E_RUNTIME_STATE_SCRIPT:-${REPO_ROOT}/scripts/verify_remote_e2e_runtime_state.sh}"

run_kind=""
manifest=""
api_base_url="${ESHU_REMOTE_E2E_API_BASE_URL:-}"
api_key="${ESHU_REMOTE_E2E_API_KEY:-}"
pprof_base_url=""
runtime_volume_proof=""
previous_manifest=""
out_dir=""
corpus_mode="${ESHU_REMOTE_E2E_CORPUS_MODE:-representative}"
repository_count="${ESHU_REMOTE_E2E_REPOSITORY_COUNT:-0}"
corpus_coverage=""
image_tag_candidate="${ESHU_E2E_IMAGE_TAG_CANDIDATE:-unknown}"
backend_kind="${ESHU_GRAPH_BACKEND:-nornicdb}"
commit_override="${ESHU_E2E_COMMIT:-}"
compose_files="${ESHU_REMOTE_E2E_COMPOSE_FILES:-docker-compose.remote-e2e.yaml}"
compose_env_file="${ESHU_REMOTE_E2E_ENV_FILE:-}"
api_timeout_seconds="${ESHU_REMOTE_E2E_API_TIMEOUT_SECONDS:-30}"
log_tail="${ESHU_E2E_LOG_TAIL:-300}"
unsupported_hosted_collectors="${ESHU_REMOTE_E2E_UNSUPPORTED_HOSTED_COLLECTORS:-}"
COMPOSE_CMD=()
RUN_TMP_DIR="$(mktemp -d)"

cleanup() {
	rm -rf "${RUN_TMP_DIR}"
}
trap cleanup EXIT

die() {
	printf 'remote-compose-e2e: %s\n' "$*" >&2
	exit 1
}

# shellcheck source=scripts/lib/e2e_remote_compose_suite_helpers.sh
source "${REPO_ROOT}/scripts/lib/e2e_remote_compose_suite_helpers.sh"
usage() {
	cat >&2 <<'USAGE'
Usage: scripts/e2e_remote_compose_suite.sh --run-kind clean|preserved --manifest PATH \
  --api-base-url URL --pprof-base-url URL --runtime-volume-proof PATH \
  --corpus-coverage PATH [--previous-manifest PATH] [options]

Options:
  --api-key TOKEN
  --out-dir DIR
  --corpus-mode smoke|representative|full
  --repository-count N
  --image-tag-candidate TAG
  --commit COMMIT
  --backend-kind KIND
  --compose-files FILE[:FILE...]
  --compose-env-file PATH

Environment: set ESHU_REMOTE_E2E_UNSUPPORTED_HOSTED_COLLECTORS for unsupported hosted rows.
USAGE
}

while (($# > 0)); do
	case "$1" in
		--run-kind) run_kind="${2:-}"; shift 2 ;;
		--manifest) manifest="${2:-}"; shift 2 ;;
		--api-base-url) api_base_url="${2:-}"; shift 2 ;;
		--api-key) api_key="${2:-}"; shift 2 ;;
		--pprof-base-url) pprof_base_url="${2:-}"; shift 2 ;;
		--runtime-volume-proof) runtime_volume_proof="${2:-}"; shift 2 ;;
		--previous-manifest) previous_manifest="${2:-}"; shift 2 ;;
		--out-dir) out_dir="${2:-}"; shift 2 ;;
		--corpus-mode) corpus_mode="${2:-}"; shift 2 ;;
		--repository-count) repository_count="${2:-}"; shift 2 ;;
		--corpus-coverage) corpus_coverage="${2:-}"; shift 2 ;;
		--image-tag-candidate) image_tag_candidate="${2:-}"; shift 2 ;;
		--commit) commit_override="${2:-}"; shift 2 ;;
		--backend-kind) backend_kind="${2:-}"; shift 2 ;;
		--compose-files) compose_files="${2:-}"; shift 2 ;;
		--compose-env-file) compose_env_file="${2:-}"; shift 2 ;;
		-h|--help) usage; exit 0 ;;
		*) die "unknown argument: $1" ;;
	esac
done

require_tool() {
	command -v "$1" >/dev/null 2>&1 || die "missing required tool: $1"
}

require_positive_int() {
	local name="$1" value="$2"
	[[ "${value}" =~ ^[0-9]+$ ]] && ((value > 0)) || die "${name} must be a positive integer"
}

require_non_negative_int() {
	local name="$1" value="$2"
	[[ "${value}" =~ ^[0-9]+$ ]] || die "${name} must be a non-negative integer"
}

validate_args() {
	case "${run_kind}" in
		clean|preserved) ;;
		*) die "--run-kind must be clean or preserved" ;;
	esac
	[[ -n "${manifest}" ]] || die "--manifest is required"
	[[ -n "${api_base_url}" ]] || die "--api-base-url is required"
	[[ -n "${pprof_base_url}" ]] || die "--pprof-base-url is required"
	[[ -n "${runtime_volume_proof}" ]] || die "--runtime-volume-proof is required"
	[[ -f "${runtime_volume_proof}" ]] || die "runtime-volume-proof file not found"
	[[ -n "${corpus_coverage}" ]] || die "--corpus-coverage is required"
	[[ -f "${corpus_coverage}" ]] || die "corpus-coverage file not found"
	if [[ "${run_kind}" == "preserved" ]]; then
		[[ -n "${previous_manifest}" ]] || die "preserved run requires --previous-manifest"
		[[ -f "${previous_manifest}" ]] || die "previous manifest file not found"
	fi
	require_positive_int ESHU_REMOTE_E2E_API_TIMEOUT_SECONDS "${api_timeout_seconds}"
	require_non_negative_int ESHU_REMOTE_E2E_REPOSITORY_COUNT "${repository_count}"
	remote_compose_validate_unsupported_hosted_collectors "${unsupported_hosted_collectors}"
}

configure_compose() {
	COMPOSE_CMD=(docker compose)
	if [[ -n "${compose_env_file}" ]]; then
		COMPOSE_CMD+=(--env-file "${compose_env_file}")
	fi
	local compose_file
	IFS=':' read -r -a compose_file_paths <<<"${compose_files}"
	for compose_file in "${compose_file_paths[@]}"; do
		[[ -n "${compose_file}" ]] || continue
		COMPOSE_CMD+=(-f "${compose_file}")
	done
}

api_get() {
	local path="$1" output="$2" cfg=""
	local -a curl_args=(-fsS --max-time "${api_timeout_seconds}")
	local status
	if [[ -n "${api_key}" ]]; then
		cfg="$(mktemp "${TMPDIR:-/tmp}/eshu-e2e-curl.XXXXXX")"
		chmod 600 "${cfg}"
		printf 'header = "Authorization: Bearer %s"\n' "${api_key}" >"${cfg}"
		curl_args+=(-K "${cfg}")
	fi
	curl_args+=("${api_base_url%/}${path}")
	if curl "${curl_args[@]}" >"${output}"; then
		status=0
	else
		status=$?
	fi
	if [[ -n "${cfg}" ]]; then
		rm -f "${cfg}"
	fi
	return "${status}"
}

validate_volume_proof() {
	jq -e . "${runtime_volume_proof}" >/dev/null 2>&1 || die "runtime-volume-proof must be valid JSON"
	if jq -r '.. | strings' "${runtime_volume_proof}" | rg --quiet \
		'ghp_|github_pat_|glpat-|AKIA|ASIA|https?://|/(Users|home|private|var|tmp|Volumes|workspace|workspaces|repos|personal-repos)/|(^|[^0-9])[0-9]{12}([^0-9]|$)|([0-9]{1,3}\.){3}[0-9]{1,3}'; then
		die "runtime-volume-proof looks like private data"
	fi
	local contract='
		def store_ok($name): (.backing_stores[$name] // {} | .status == "pass");
		. as $root |
		($root.schema_version == 1) and ($root.run_kind == $kind) and
		($root | store_ok("nornicdb_data")) and ($root | store_ok("postgres_data")) and ($root | store_ok("eshu_data")) and
		if $kind == "clean" then
			($root.clean_volume_state == "reset_before_run") and
			all(["nornicdb_data","postgres_data","eshu_data"][]; . as $name |
				($root.backing_stores[$name].before == "absent") and
				($root.backing_stores[$name].after == "present"))
		else
			($root.restart_without_prune == true) and ($root.previous_run_kind == "clean") and
			all(["nornicdb_data","postgres_data","eshu_data"][]; . as $name |
				($root.backing_stores[$name].same_as_clean == true))
		end
	'
	jq -e --arg kind "${run_kind}" "${contract}" "${runtime_volume_proof}" >/dev/null \
		|| die "runtime-volume-proof does not satisfy ${run_kind} run requirements"
}

validate_corpus_coverage() {
	jq -e '
		(.ecosystems | type == "object") and
		(.evidence_families | type == "object")
	' "${corpus_coverage}" >/dev/null || die "corpus-coverage must contain ecosystems and evidence_families"
}

run_runtime_state_verifier() {
	if ! ESHU_REMOTE_E2E_COMPOSE_FILES="${compose_files}" \
		ESHU_REMOTE_E2E_ENV_FILE="${compose_env_file}" \
		ESHU_REMOTE_E2E_API_BASE_URL="${api_base_url}" \
		ESHU_REMOTE_E2E_API_KEY="${api_key}" \
		ESHU_REMOTE_E2E_CORPUS_MODE="${corpus_mode}" \
		"${RUNTIME_STATE_SCRIPT}"; then
		die "runtime state verifier failed"
	fi
}

capture_pprof() {
	local output="$1"
	curl -fsS --max-time "${api_timeout_seconds}" "${pprof_base_url%/}/debug/pprof/" >"${output}" \
		|| die "pprof endpoint is not reachable"
	[[ -s "${output}" ]] || die "pprof endpoint returned no data"
}

capture_stats() {
	local output="$1"
	docker stats --no-stream --format '{{json .}}' >"${output}" || die "docker stats capture failed"
	[[ -s "${output}" ]] || die "docker stats returned no rows"
}

capture_logs() {
	local services_file="$1" raw_file="$2" sanitized_file="$3"
	"${COMPOSE_CMD[@]}" config --services >"${services_file}"
	: >"${raw_file}"
	local service log_file
	while IFS= read -r service; do
		[[ -n "${service}" ]] || continue
		log_file="${RUN_TMP_DIR}/log-${service}.raw"
		"${COMPOSE_CMD[@]}" logs --no-color --tail "${log_tail}" "${service}" >"${log_file}" 2>&1 || true
		printf '\n[%s]\n' "${service}" >>"${raw_file}"
		cat "${log_file}" >>"${raw_file}"
	done <"${services_file}"
	if rg -i --quiet 'panic|fatal|oom|sqlstate|unwind merge|deadlock|constraint.*(failed|violation|error)' "${raw_file}"; then
		die "forbidden log pattern detected in remote Compose logs"
	fi
	sed -E \
		-e 's#https?://[^[:space:]"<>]+#[redacted-url]#g' \
		-e 's#/(Users|home|private|var|tmp|Volumes|workspace|workspaces|repos|personal-repos)/[^[:space:]",}]*(/[^[:space:]",}]*)*#[redacted-path]#g' \
		-e 's#([0-9]{1,3}\.){3}[0-9]{1,3}#[redacted-ip]#g' \
		-e 's#(ghp_|github_pat_|glpat-|AKIA|ASIA)[A-Za-z0-9_./+=:-]*#[redacted-token]#g' \
		"${raw_file}" >"${sanitized_file}"
}

query_postgres_tsv() {
	local query="$1" output="$2"
	"${COMPOSE_CMD[@]}" exec -T postgres psql \
		-U "${ESHU_REMOTE_E2E_POSTGRES_USER:-eshu}" \
		-d "${ESHU_REMOTE_E2E_POSTGRES_DB:-eshu}" \
		-At -F $'\t' -c "${query}" >"${output}" \
		|| die "postgres evidence query failed"
}

json_from_tsv() {
	local input="$1" output="$2" c1="$3" c2="$4"
	jq -R -s --arg c1 "${c1}" --arg c2 "${c2}" '
		split("\n")
		| map(select(length > 0) | split("\t"))
		| map({($c1): .[0], ($c2): .[1], count: (.[2] | tonumber)})
	' "${input}" >"${output}"
}

build_manifest() {
	local facts_json="$1" workflow_json="$2" index_status="$3" services_json="$4" stats_file="$5" output="$6"
	local commit
	if [[ -n "${commit_override}" ]]; then
		commit="${commit_override}"
	else
		commit="$(git -C "${REPO_ROOT}" rev-parse --short=12 HEAD)"
	fi
	jq -n \
		--slurpfile facts "${facts_json}" \
		--slurpfile workflow "${workflow_json}" \
		--slurpfile index "${index_status}" \
		--slurpfile services "${services_json}" \
		--slurpfile coverage "${corpus_coverage}" \
		--slurpfile volume "${runtime_volume_proof}" \
		--arg run_kind "${run_kind}" \
		--arg commit "${commit}" \
		--arg image "${image_tag_candidate}" \
		--arg backend "${backend_kind}" \
		--arg corpus_mode "${corpus_mode}" \
		--arg unsupported_hosted_collectors "${unsupported_hosted_collectors}" \
		--argjson repository_count "${repository_count}" '
		($facts[0] // []) as $fact_rows |
		($workflow[0] // []) as $workflow_rows |
		($services[0] // []) as $service_rows |
		($unsupported_hosted_collectors
			| split(",")
			| map(gsub("^\\s+|\\s+$"; ""))
			| map(select(length > 0))) as $unsupported_hosted_rows |
		def sum_source($names): [$fact_rows[] | select(.source_system as $s | $names | index($s)) | .count] | add // 0;
		def sum_kind($pattern): [$fact_rows[] | select(.fact_kind | test($pattern)) | .count] | add // 0;
		def service_enabled($name): $service_rows | index($name) != null;
		def explicitly_unsupported($name): $unsupported_hosted_rows | index($name) != null;
		def collector_row($n):
			if $n > 0 then {status: "pass", facts: $n}
			else {status: "fail", facts: 0, reason: "no source facts observed"} end;
		def hosted_collector_row($name; $service; $n):
			if $n > 0 then {status: "pass", facts: $n}
			elif explicitly_unsupported($name) then {status: "unsupported", facts: 0, reason: "collector explicitly unsupported in remote Compose profile"}
			elif service_enabled($service) then {status: "fail", facts: 0, reason: "no source facts observed for enabled collector service"}
			else {status: "skipped", facts: 0, reason: "collector service disabled in remote Compose profile"} end;
		def reducer_row($n):
			if $n > 0 then {status: "pass", count: $n}
			else {status: "fail", count: 0, reason: "no reducer evidence observed"} end;
		def workflow_completed($collector):
			[$workflow_rows[] | select(.collector_kind == $collector and .status == "completed") | .count] | add // 0;
		def workflow_row($collector): {completed: workflow_completed($collector)};
		def queue_num($name): ($index[0].queue[$name] // 0);
		{
			schema_version: 1,
			status: "pass",
			run: {
				id: ("remote-compose-" + $run_kind + "-" + $commit),
				kind: $run_kind,
				commit: $commit,
				image_tag_candidate: $image,
				backend: {kind: $backend}
			},
			corpus: {
				mode: $corpus_mode,
				repository_count: $repository_count,
				coverage: ($coverage[0] // {})
			},
			runtimes: {
				schema_bootstrap: {status: "pass"},
				api: {status: "pass"},
				mcp_server: {status: "pass"},
				ingester: {status: "pass"},
				resolution_engine: {status: "pass"},
				workflow_coordinator: {status: "pass"},
				hosted_collectors: {status: "pass"},
				scanner_worker: {status: "pass"}
			},
			collectors: {
				git: collector_row(sum_source(["git"])),
				terraform_state: collector_row(sum_source(["terraform_state"])),
				aws_cloud: collector_row(sum_source(["aws"])),
				oci_registry: collector_row(sum_source(["oci_registry"])),
				package_registry: collector_row(sum_source(["package_registry"])),
				sbom_attestation: collector_row(sum_source(["sbom_attestation"])),
				provider_security_alerts: collector_row(sum_source(["security_alert","security_alerts"])),
				vulnerability_intelligence: collector_row(sum_source(["vulnerability_intelligence"])),
				scanner_worker: collector_row(sum_source(["scanner_worker"])),
				confluence: collector_row(sum_source(["confluence"])),
				pagerduty: hosted_collector_row("pagerduty"; "collector-pagerduty"; sum_source(["pagerduty"])),
				jira: hosted_collector_row("jira"; "collector-jira"; sum_source(["jira"])),
				grafana: hosted_collector_row("grafana"; "collector-grafana"; sum_source(["grafana"])),
				prometheus_mimir: hosted_collector_row("prometheus_mimir"; "collector-prometheus-mimir"; sum_source(["prometheus_mimir"])),
				loki: hosted_collector_row("loki"; "collector-loki"; sum_source(["loki"])),
				tempo: hosted_collector_row("tempo"; "collector-tempo"; sum_source(["tempo"]))
			},
			reducers: {
				repository_dependencies: reducer_row(sum_kind("reducer_package_(ownership|consumption|publication)_correlation|reducer_package_correlation")),
				terraform_iac_relationships: reducer_row(sum_kind("reducer_terraform|resolved_relationship|terraform.*relationship")),
				aws_cloud_relationships: reducer_row(sum_kind("reducer_aws|aws_relationship")),
				oci_image_identity: reducer_row(sum_kind("reducer_container_image_identity")),
				sbom_attachment: reducer_row(sum_kind("reducer_sbom_attestation_attachment")),
				vulnerability_matching: reducer_row(sum_kind("reducer_vulnerability_match")),
				provider_alert_reconciliation: reducer_row(sum_kind("reducer_security_alert_reconciliation")),
				supply_chain_impact: reducer_row(sum_kind("reducer_supply_chain_impact_finding")),
				deployment_correlation: reducer_row(sum_kind("reducer_(deployment|kubernetes|workload|ci_cd_run|service_catalog)_correlation|reducer_workload_identity")),
				observability_correlation: reducer_row(sum_kind("reducer_observability(_coverage)?_correlation")),
				incident_work_item_correlation: reducer_row(sum_kind("reducer_incident_work_item_correlation"))
			},
			readback: {
				api: {status: "pass", checked: 1, failed: 0, truncated: 0},
				mcp: {status: "pass", checked: 1, failed: 0, truncated: 0},
				cli: {status: "pass", checked: 1, failed: 0, truncated: 0}
			},
			queue: {
				pending: queue_num("pending"),
				in_flight: queue_num("in_flight"),
				retrying: queue_num("retrying"),
				failed: queue_num("failed"),
				dead_letter: queue_num("dead_letter")
			},
			workflow: {
				collector_claims: {
					git: workflow_row("git"),
					terraform_state: workflow_row("terraform_state"),
					aws: workflow_row("aws"),
					oci_registry: workflow_row("oci_registry"),
					package_registry: workflow_row("package_registry"),
					sbom_attestation: workflow_row("sbom_attestation"),
					security_alert: workflow_row("security_alert"),
					vulnerability_intelligence: workflow_row("vulnerability_intelligence"),
					scanner_worker: workflow_row("scanner_worker"),
					confluence: workflow_row("confluence"),
					pagerduty: workflow_row("pagerduty"),
					jira: workflow_row("jira"),
					grafana: workflow_row("grafana"),
					prometheus_mimir: workflow_row("prometheus_mimir"),
					loki: workflow_row("loki"),
					tempo: workflow_row("tempo")
				}
			},
			observability: {
				pprof_status: "reachable",
				logs_status: "captured",
				resource_snapshot_status: "captured",
				resource_snapshot_count: ([inputs] | length)
			},
			runtime_volume_proof: ($volume[0] // {}),
			privacy: {status: "pass"},
			follow_up_issues: [],
			preserved_restart: {
				duplicate_guard_status: "not_applicable",
				current_totals: {
					facts: ([$fact_rows[].count] | add // 0),
					claims: ([$workflow_rows[].count] | add // 0),
					findings: (sum_kind("reducer_supply_chain_impact_finding"))
				}
			}
		}
		| .status = (
			[
				(.collectors // {} | to_entries[] | .value.status),
				(.reducers // {} | to_entries[] | .value.status),
				(.corpus.coverage.ecosystems // {} | to_entries[] | .value.status),
				(.corpus.coverage.evidence_families // {} | to_entries[] | .value.status)
			] as $required_statuses |
			(((.queue.retrying // 0) > 0) or ((.queue.failed // 0) > 0) or ((.queue.dead_letter // 0) > 0)) as $queue_failed |
			if (
				($required_statuses | all(. == "pass")) and ($queue_failed | not)
			) then "pass"
			elif (($required_statuses | any(. == "fail")) or $queue_failed) then "fail"
			else "partial" end
		)
	' "${stats_file}" >"${output}"
}

apply_preserved_guard() {
	[[ "${run_kind}" == "preserved" ]] || return 0
	local previous_facts previous_claims previous_findings current_facts current_claims current_findings
	previous_facts="$(jq -r '.preserved_restart.current_totals.facts // 0' "${previous_manifest}")"
	previous_claims="$(jq -r '.preserved_restart.current_totals.claims // 0' "${previous_manifest}")"
	previous_findings="$(jq -r '.preserved_restart.current_totals.findings // 0' "${previous_manifest}")"
	current_facts="$(jq -r '.preserved_restart.current_totals.facts // 0' "${manifest}")"
	current_claims="$(jq -r '.preserved_restart.current_totals.claims // 0' "${manifest}")"
	current_findings="$(jq -r '.preserved_restart.current_totals.findings // 0' "${manifest}")"
	if ((current_facts > previous_facts)); then
		die "preserved restart produced new facts: previous=${previous_facts} current=${current_facts}"
	fi
	if ((current_claims > previous_claims)); then
		die "preserved restart produced new claims: previous=${previous_claims} current=${current_claims}"
	fi
	if ((current_findings > previous_findings)); then
		die "preserved restart produced new findings: previous=${previous_findings} current=${current_findings}"
	fi
	local tmp
	tmp="$(mktemp "${TMPDIR:-/tmp}/eshu-e2e-manifest.XXXXXX")"
	jq '.preserved_restart.duplicate_guard_status = "pass"' "${manifest}" >"${tmp}"
	mv "${tmp}" "${manifest}"
}

print_nonpass_reasons() {
	jq -r '
		(.collectors // {} | to_entries[] | select(.value.status != "pass") |
			if (.value.reason // "") == "no source facts observed" then
				"collector \(.key) has no source facts"
			else
				"collector \(.key): \(.value.reason // "missing source facts")"
			end),
		(.reducers // {} | to_entries[] | select(.value.status != "pass") |
			"reducer \(.key): \(.value.reason // "missing evidence")"),
		(.corpus.coverage.ecosystems // {} | to_entries[] | select(.value.status != "pass") |
			"ecosystem \(.key): \(.value.reason // "missing coverage")"),
		(.corpus.coverage.evidence_families // {} | to_entries[] | select(.value.status != "pass") |
			"evidence family \(.key): \(.value.reason // "missing coverage")")
	' "${manifest}" >&2
}

main() {
	require_tool curl
	require_tool docker
	require_tool jq
	require_tool rg
	validate_args
	configure_compose
	out_dir="${out_dir:-$(dirname "${manifest}")/e2e-remote-compose-evidence}"
	mkdir -p "${out_dir}" "$(dirname "${manifest}")"

	validate_volume_proof
	validate_corpus_coverage
	run_runtime_state_verifier
	api_get "/index-status" "${out_dir}/index-status.json"
	capture_pprof "${out_dir}/pprof-index.txt"
	capture_stats "${out_dir}/docker-stats.jsonl"
	capture_logs "${out_dir}/compose-services.txt" "${RUN_TMP_DIR}/logs.raw" "${out_dir}/logs.sanitized"
	query_postgres_tsv "SELECT source_system, fact_kind, COUNT(*) FROM fact_records WHERE is_tombstone = false GROUP BY source_system, fact_kind ORDER BY source_system, fact_kind" "${out_dir}/fact-counts.tsv"
	query_postgres_tsv "SELECT collector_kind, status, COUNT(*) FROM workflow_work_items GROUP BY collector_kind, status ORDER BY collector_kind, status" "${out_dir}/workflow-counts.tsv"
	json_from_tsv "${out_dir}/fact-counts.tsv" "${out_dir}/fact-counts.json" source_system fact_kind
	json_from_tsv "${out_dir}/workflow-counts.tsv" "${out_dir}/workflow-counts.json" collector_kind status
	remote_compose_json_array_from_lines "${out_dir}/compose-services.txt" "${out_dir}/compose-services.json"
	build_manifest "${out_dir}/fact-counts.json" "${out_dir}/workflow-counts.json" "${out_dir}/index-status.json" "${out_dir}/compose-services.json" "${out_dir}/docker-stats.jsonl" "${manifest}"
	apply_preserved_guard
	"${MANIFEST_VALIDATOR}" "${manifest}" >/dev/null
	if ! jq -e '.status == "pass"' "${manifest}" >/dev/null; then
		print_nonpass_reasons
		die "remote Compose E2E manifest status is not pass"
	fi
	printf 'remote-compose-e2e: pass manifest=%s\n' "${manifest}"
}

main "$@"
