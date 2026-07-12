#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
MANIFEST_VALIDATOR="${REPO_ROOT}/scripts/verify_e2e_evidence_manifest.sh"
MANIFEST_LIB="${REPO_ROOT}/scripts/lib/e2e_remote_compose_manifest.sh"
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
readback_proof="${ESHU_REMOTE_E2E_READBACK_PROOF:-}"
image_tag_candidate="${ESHU_E2E_IMAGE_TAG_CANDIDATE:-unknown}"
backend_kind="${ESHU_GRAPH_BACKEND:-nornicdb}"
commit_override="${ESHU_E2E_COMMIT:-}"
compose_files="${ESHU_REMOTE_E2E_COMPOSE_FILES:-docker-compose.remote-e2e.yaml}"
compose_env_file="${ESHU_REMOTE_E2E_ENV_FILE:-}"
api_timeout_seconds="${ESHU_REMOTE_E2E_API_TIMEOUT_SECONDS:-30}"
log_tail="${ESHU_E2E_LOG_TAIL:-300}"
unsupported_hosted_collectors="${ESHU_REMOTE_E2E_UNSUPPORTED_HOSTED_COLLECTORS:-}"
unsupported_reducers="${ESHU_REMOTE_E2E_UNSUPPORTED_REDUCERS:-}"
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
# shellcheck source=scripts/lib/e2e_remote_compose_manifest.sh
source "${MANIFEST_LIB}"

usage() {
	# Delivered from a sibling data file, not a heredoc: Homebrew bash >= 5.1
	# writes an entire heredoc body to a pipe before forking the reader, and
	# macOS's 512-byte pipe buffer deadlocks on this ~1.2KB body (#5074). The
	# body is fully static (was a quoted <<'USAGE', no shell expansion), so
	# the file is byte-identical to the original heredoc body.
	cat >&2 "${REPO_ROOT}/scripts/lib/e2e_remote_compose_suite-usage.txt"
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
		--readback-proof) readback_proof="${2:-}"; shift 2 ;;
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

validate_unsupported_reducers() {
	local invalid
	invalid="$(jq -nr --arg raw "${unsupported_reducers}" '
		def trim: gsub("^\\s+|\\s+$"; "");
		[
			"repository_dependencies",
			"terraform_iac_relationships",
			"aws_cloud_relationships",
			"oci_image_identity",
			"sbom_attachment",
			"vulnerability_matching",
			"provider_alert_reconciliation",
			"supply_chain_impact",
			"deployment_correlation",
			"observability_correlation",
			"incident_work_item_correlation"
		] as $allowed |
		$raw
		| split(",")
		| map(trim)
		| map(select(length > 0))
		| map(. as $name | select(($allowed | index($name)) | not))
		| .[0] // ""
	')"
	[[ -z "${invalid}" ]] || die "unsupported reducer row is invalid: ${invalid}"
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
	validate_unsupported_reducers
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
		((.schema_version // 1) == 1) and
		(.ecosystems | type == "object") and
		(.evidence_families | type == "object") and
		(.repository_count | type == "number" and . >= 0)
	' "${corpus_coverage}" >/dev/null || die "corpus-coverage must contain schema_version, repository_count, ecosystems, and evidence_families"
	local coverage_mode coverage_repository_count
	coverage_mode="$(jq -r '.mode // ""' "${corpus_coverage}")"
	[[ -z "${coverage_mode}" || "${coverage_mode}" == "${corpus_mode}" ]] \
		|| die "corpus-coverage mode must match --corpus-mode"
	coverage_repository_count="$(jq -r '.repository_count' "${corpus_coverage}")"
	[[ "${coverage_repository_count}" == "${repository_count}" ]] \
		|| die "corpus-coverage repository_count must match --repository-count"
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

main() {
	local readback_manifest_proof relationship_query
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
	if [[ -n "${readback_proof}" ]]; then
		validate_readback_proof "${readback_proof}"
		readback_manifest_proof="${readback_proof}"
	else
		readback_manifest_proof="${RUN_TMP_DIR}/readback-proof-missing.json"
		write_missing_readback_proof "${readback_manifest_proof}"
	fi
	run_runtime_state_verifier
	api_get "/index-status" "${out_dir}/index-status.json"
	capture_pprof "${out_dir}/pprof-index.txt"
	capture_stats "${out_dir}/docker-stats.jsonl"
	capture_logs "${out_dir}/compose-services.txt" "${RUN_TMP_DIR}/logs.raw" "${out_dir}/logs.sanitized"
	query_postgres_tsv "SELECT source_system, fact_kind, COUNT(*) FROM fact_records WHERE is_tombstone = false GROUP BY source_system, fact_kind ORDER BY source_system, fact_kind" "${out_dir}/fact-counts.tsv"
	query_postgres_tsv "SELECT collector_kind, status, COUNT(*) FROM workflow_work_items GROUP BY collector_kind, status ORDER BY collector_kind, status" "${out_dir}/workflow-counts.tsv"
	# Delivered from a sibling data file, not a heredoc: Homebrew bash >= 5.1
	# writes an entire heredoc body to a pipe before forking the reader, and
	# macOS's 512-byte pipe buffer deadlocks on this ~936B body (#5074). The
	# body is fully static (was a quoted <<'SQL', no shell expansion), so the
	# file is byte-identical to the original heredoc body.
	relationship_query="$(<"${REPO_ROOT}/scripts/lib/e2e_remote_compose_suite-relationship-query.sql")"
	query_postgres_tsv "${relationship_query}" "${out_dir}/reducer-relationship-counts.tsv"
	json_from_tsv "${out_dir}/fact-counts.tsv" "${out_dir}/fact-counts.json" source_system fact_kind
	json_from_tsv "${out_dir}/workflow-counts.tsv" "${out_dir}/workflow-counts.json" collector_kind status
	json_reducer_counts_from_tsv "${out_dir}/reducer-relationship-counts.tsv" "${out_dir}/reducer-relationship-counts.json"
	json_array_from_lines "${out_dir}/compose-services.txt" "${out_dir}/compose-services.json"
	build_manifest "${out_dir}/fact-counts.json" "${out_dir}/workflow-counts.json" "${out_dir}/reducer-relationship-counts.json" "${out_dir}/index-status.json" "${out_dir}/compose-services.json" "${out_dir}/docker-stats.jsonl" "${readback_manifest_proof}" "${manifest}"
	apply_preserved_guard
	"${MANIFEST_VALIDATOR}" "${manifest}" >/dev/null
	if ! jq -e '.status == "pass"' "${manifest}" >/dev/null; then
		print_nonpass_reasons
		die "remote Compose E2E manifest status is not pass"
	fi
	printf 'remote-compose-e2e: pass manifest=%s\n' "${manifest}"
}

main "$@"
