#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
MANIFEST_LIB="${REPO_ROOT}/scripts/lib/e2e_evidence_manifest.sh"

run_kind="${ESHU_REMOTE_E2E_SUITE_RUN_KIND:-}"
manifest_path="${ESHU_REMOTE_E2E_SUITE_MANIFEST:-}"
summary_path="${ESHU_REMOTE_E2E_SUITE_SUMMARY:-}"
volume_proof_path="${ESHU_REMOTE_E2E_SUITE_VOLUME_PROOF:-}"
previous_manifest="${ESHU_REMOTE_E2E_SUITE_PREVIOUS_MANIFEST:-}"
runtime_verifier="${ESHU_REMOTE_E2E_RUNTIME_VERIFIER:-${REPO_ROOT}/scripts/verify_remote_e2e_runtime_state.sh}"
pprof_base_url="${ESHU_REMOTE_E2E_PPROF_BASE_URL:-}"
run_id="${ESHU_REMOTE_E2E_RUN_ID:-}"
commit="${ESHU_REMOTE_E2E_COMMIT:-}"
image_tag_candidate="${ESHU_REMOTE_E2E_IMAGE_TAG_CANDIDATE:-}"
backend_kind="${ESHU_REMOTE_E2E_BACKEND_KIND:-nornicdb}"
backend_digest="${ESHU_REMOTE_E2E_BACKEND_DIGEST:-}"
evidence_dir="${ESHU_REMOTE_E2E_SUITE_EVIDENCE_DIR:-}"
api_base_url="${ESHU_REMOTE_E2E_API_BASE_URL:-}"
log_tail="${ESHU_REMOTE_E2E_LOG_TAIL:-300}"
compose_files="${ESHU_REMOTE_E2E_COMPOSE_FILES:-docker-compose.remote-e2e.yaml}"
compose_env_file="${ESHU_REMOTE_E2E_ENV_FILE:-}"

TMP_DIR="$(mktemp -d)"
cleanup() {
	rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

# shellcheck source=scripts/lib/e2e_evidence_manifest.sh
source "${MANIFEST_LIB}"

usage() {
	printf '%s\n' "Usage: $(basename "$0") --run-kind clean|preserved --manifest <file> --summary <file> --volume-proof <file> --pprof-base-url <url> [options]"
	printf '%s\n' ""
	printf '%s\n' "Options:"
	printf '%s\n' "  --previous-manifest <file>   Required for preserved runs; must be a passing clean manifest."
	printf '%s\n' "  --runtime-verifier <file>    Runtime-state verifier to run first."
	printf '%s\n' "  --api-base-url <url>         Exported to verify_remote_e2e_runtime_state.sh."
	printf '%s\n' "  --run-id <id>                Public-safe synthetic run id."
	printf '%s\n' "  --commit <sha>               Eshu commit under proof."
	printf '%s\n' "  --image-tag-candidate <tag>  Image tag candidate under proof."
	printf '%s\n' "  --backend-kind <kind>        Graph backend kind, default nornicdb."
	printf '%s\n' "  --backend-digest <digest>    Optional backend image digest."
	printf '%s\n' "  --evidence-dir <dir>         Private local log/stats/verifier evidence dir."
	printf '%s\n' "  --log-tail <count>           Compose log tail count, default 300."
}

die() {
	printf 'e2e-remote-compose-suite: %s\n' "$*" >&2
	exit 1
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--run-kind) run_kind="$2"; shift 2 ;;
		--manifest) manifest_path="$2"; shift 2 ;;
		--summary) summary_path="$2"; shift 2 ;;
		--volume-proof) volume_proof_path="$2"; shift 2 ;;
		--previous-manifest) previous_manifest="$2"; shift 2 ;;
		--runtime-verifier) runtime_verifier="$2"; shift 2 ;;
		--pprof-base-url) pprof_base_url="$2"; shift 2 ;;
		--api-base-url) api_base_url="$2"; shift 2 ;;
		--run-id) run_id="$2"; shift 2 ;;
		--commit) commit="$2"; shift 2 ;;
		--image-tag-candidate) image_tag_candidate="$2"; shift 2 ;;
		--backend-kind) backend_kind="$2"; shift 2 ;;
		--backend-digest) backend_digest="$2"; shift 2 ;;
		--evidence-dir) evidence_dir="$2"; shift 2 ;;
		--log-tail) log_tail="$2"; shift 2 ;;
		-h|--help) usage; exit 0 ;;
		*) die "unknown option: $1" ;;
	esac
done

require_file() {
	local label="$1"
	local path="$2"
	[[ -n "${path}" ]] || die "${label} is required"
	[[ -f "${path}" ]] || die "${label} file not found: ${path}"
}

require_executable() {
	local label="$1"
	local path="$2"
	[[ -n "${path}" ]] || die "${label} is required"
	[[ -x "${path}" ]] || die "${label} is not executable: ${path}"
}

require_positive_integer() {
	local label="$1"
	local value="$2"
	if [[ ! "${value}" =~ ^[0-9]+$ ]] || (( value <= 0 )); then
		die "${label} must be a positive integer, got ${value}"
	fi
}

require_public_identifier() {
	local label="$1"
	local value="$2"
	[[ -n "${value}" ]] || die "${label} is required"
	if [[ ! "${value}" =~ ^[A-Za-z0-9._:@%+=-]+$ ]]; then
		die "${label} must be a public-safe identifier"
	fi
}

validate_inputs() {
	case "${run_kind}" in
		clean|preserved) ;;
		"") die "--run-kind clean or preserved is required" ;;
		*) die "--run-kind must be clean or preserved, got ${run_kind}" ;;
	esac

	[[ -n "${manifest_path}" ]] || die "--manifest is required"
	require_file "--summary" "${summary_path}"
	require_file "--volume-proof" "${volume_proof_path}"
	require_executable "--runtime-verifier" "${runtime_verifier}"
	[[ -n "${pprof_base_url}" ]] || die "--pprof-base-url is required"
	require_positive_integer "--log-tail" "${log_tail}"

	jq -e . "${summary_path}" >/dev/null 2>&1 || die "--summary must be valid JSON"
	jq -e '.schema_version == 1' "${summary_path}" >/dev/null \
		|| die "--summary schema_version must be 1"
	if ! e2e_manifest_validate_privacy "${summary_path}" >/dev/null 2>"${TMP_DIR}/summary-privacy.err"; then
		die "summary looks like private data; only aggregate status and counts are accepted"
	fi

	if [[ -z "${run_id}" ]]; then
		run_id="remote-compose-${run_kind}-$(date -u +%Y%m%dT%H%M%SZ)"
	fi
	if [[ -z "${commit}" ]]; then
		commit="$(git -C "${REPO_ROOT}" rev-parse HEAD 2>/dev/null || printf 'unknown')"
	fi
	[[ -n "${image_tag_candidate}" ]] || die "--image-tag-candidate is required"
	require_public_identifier "--run-id" "${run_id}"
	require_public_identifier "--commit" "${commit}"
	require_public_identifier "--image-tag-candidate" "${image_tag_candidate}"
	require_public_identifier "--backend-kind" "${backend_kind}"
	if [[ -n "${backend_digest}" ]]; then
		require_public_identifier "--backend-digest" "${backend_digest}"
	fi

	if [[ -z "${evidence_dir}" ]]; then
		evidence_dir="${TMP_DIR}/evidence"
	fi
	mkdir -p "${evidence_dir}"
}

validate_volume_proof() {
	jq -e . "${volume_proof_path}" >/dev/null 2>&1 \
		|| die "--volume-proof must be valid JSON"
	if ! jq -e '
		[.. | objects | keys[] as $key |
			select([
				"volume","volumes","volume_id","volume_ids","volume_name","volume_names",
				"volume_path","mount","mounts","mountpoint","mount_path","host_path",
				"driver","path","paths","file","files","host","hostname","ip","token",
				"payload","raw","body","url","account","account_id"
			] | index($key))]
		| length == 0
	' "${volume_proof_path}" >/dev/null; then
		die "volume proof looks like private data; only aggregate volume status is accepted"
	fi
	if ! e2e_manifest_validate_privacy "${volume_proof_path}" >/dev/null 2>"${TMP_DIR}/volume-privacy.err"; then
		die "volume proof looks like private data; only aggregate volume status is accepted"
	fi

	local contract
	contract='
		def proof_id_ok: (.proof_id | type == "string" and test("^[A-Za-z0-9._-]+$"));
		def store_ok($name): (.backing_stores[$name] // {} | .status == "pass");
		def stores_ok: store_ok("nornicdb_data") and store_ok("postgres_data") and store_ok("eshu_data");
		. as $root |
		($root.schema_version == 1) and ($root | proof_id_ok) and ($root.run_kind == $kind) and ($root | stores_ok) and
		if $kind == "clean" then
			($root.clean_volume_state == "reset_before_run") and
			all(["nornicdb_data","postgres_data","eshu_data"][]; . as $name |
				($root.backing_stores[$name].before == "absent") and
				($root.backing_stores[$name].after == "present"))
		else
			($root.restart_without_prune == true) and
			($root.previous_run_kind == "clean") and
			all(["nornicdb_data","postgres_data","eshu_data"][]; . as $name |
				($root.backing_stores[$name].same_as_clean == true))
		end
	'
	jq -e --arg kind "${run_kind}" "${contract}" "${volume_proof_path}" >/dev/null \
		|| die "volume proof does not satisfy ${run_kind} Compose volume requirements"
}

validate_previous_manifest() {
	if [[ "${run_kind}" != "preserved" ]]; then
		return 0
	fi
	[[ -n "${previous_manifest}" ]] || die "preserved run requires --previous-manifest"
	[[ -f "${previous_manifest}" ]] || die "--previous-manifest file not found: ${previous_manifest}"
	validate_e2e_evidence_manifest "${previous_manifest}" >/dev/null \
		|| die "previous manifest must satisfy the E2E evidence manifest contract"
	jq -e '.status == "pass" and .run.kind == "clean"' "${previous_manifest}" >/dev/null \
		|| die "previous manifest must be a passing clean run"
	jq -e --arg commit "${commit}" --arg image "${image_tag_candidate}" '
		.run.commit == $commit and .run.image_tag_candidate == $image
	' "${previous_manifest}" >/dev/null \
		|| die "previous manifest must prove the same commit and image tag candidate"
}

jq_number() {
	local file="$1"
	local filter="$2"
	jq -r "${filter} // 0" "${file}"
}

require_zero() {
	local label="$1"
	local value="$2"
	if [[ ! "${value}" =~ ^[0-9]+$ ]]; then
		die "preserved ${label} must be a non-negative integer"
	fi
	if (( value != 0 )); then
		die "preserved run has ${label}"
	fi
}

validate_preserved_counters() {
	if [[ "${run_kind}" != "preserved" ]]; then
		return 0
	fi
	require_zero "duplicate claims" "$(jq_number "${summary_path}" '.preserved.duplicate_claims')"
	require_zero "duplicate facts" "$(jq_number "${summary_path}" '.preserved.duplicate_facts')"
	require_zero "duplicate findings" "$(jq_number "${summary_path}" '.preserved.duplicate_findings')"
	require_zero "new dead letters" "$(jq_number "${summary_path}" '.preserved.new_dead_letters')"
}

configure_compose_cmd() {
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

run_runtime_state_verifier() {
	local log_file="${evidence_dir}/remote-runtime-verifier.log"
	if [[ -n "${api_base_url}" ]]; then
		export ESHU_REMOTE_E2E_API_BASE_URL="${api_base_url}"
	fi
	if ! "${runtime_verifier}" >"${log_file}" 2>&1; then
		cat "${log_file}" >&2
		die "remote runtime verifier failed"
	fi
}

capture_compose_logs() {
	local output="${evidence_dir}/compose-logs.txt"
	configure_compose_cmd
	if "${COMPOSE_CMD[@]}" logs --no-color --tail "${log_tail}" >"${output}" 2>&1 && [[ -s "${output}" ]]; then
		printf 'captured'
		return 0
	fi
	printf 'missing'
}

capture_resource_snapshot() {
	local output="${evidence_dir}/docker-stats.json"
	if docker stats --no-stream --format \
		'{"name":"{{.Name}}","cpu":"{{.CPUPerc}}","mem":"{{.MemUsage}}","net":"{{.NetIO}}","block":"{{.BlockIO}}"}' \
		>"${output}" 2>/dev/null && [[ -s "${output}" ]]; then
		if jq -e -s 'length > 0 and all(.[]; ((.cpu // "") | length > 0) and ((.mem // "") | length > 0))' \
			"${output}" >/dev/null 2>&1; then
			printf 'captured'
			return 0
		fi
		printf 'invalid'
		return 0
	fi
	printf 'missing'
}

check_pprof() {
	local output="${evidence_dir}/pprof-index.txt"
	local url="${pprof_base_url%/}/debug/pprof/"
	if curl -fsS -m 5 "${url}" >"${output}" 2>/dev/null && [[ -s "${output}" ]]; then
		printf 'reachable'
		return 0
	fi
	printf 'not_reachable'
}

volume_proof_json() {
	jq -c '{
		status: "pass",
		proof_id: .proof_id,
		run_kind: .run_kind,
		clean_volume_state: (.clean_volume_state // null),
		previous_run_kind: (.previous_run_kind // null),
		restart_without_prune: (.restart_without_prune // null),
		backing_stores: {
			nornicdb_data: (.backing_stores.nornicdb_data // {}),
			postgres_data: (.backing_stores.postgres_data // {}),
			eshu_data: (.backing_stores.eshu_data // {})
		}
	}' "${volume_proof_path}"
}

write_manifest() {
	local pprof_status="$1"
	local logs_status="$2"
	local resource_status="$3"
	local volume_json="$4"
	local output_tmp="${manifest_path}.tmp"
	mkdir -p "$(dirname "${manifest_path}")"
	jq --arg run_id "${run_id}" \
		--arg run_kind "${run_kind}" \
		--arg commit "${commit}" \
		--arg image_tag_candidate "${image_tag_candidate}" \
		--arg backend_kind "${backend_kind}" \
		--arg backend_digest "${backend_digest}" \
		--arg pprof_status "${pprof_status}" \
		--arg logs_status "${logs_status}" \
		--arg resource_status "${resource_status}" \
		--argjson volume_proof "${volume_json}" '
		{
			schema_version: 1,
			status: (.status // "fail"),
			run: {
				id: $run_id,
				kind: $run_kind,
				commit: $commit,
				image_tag_candidate: $image_tag_candidate,
				backend: ({kind: $backend_kind} + (if ($backend_digest | length) > 0 then {digest: $backend_digest} else {} end))
			},
			corpus: .corpus,
			runtimes: .runtimes,
			collectors: .collectors,
			reducers: .reducers,
			readback: .readback,
			queue: .queue,
			observability: {
				pprof_status: $pprof_status,
				logs_status: $logs_status,
				resource_snapshot_status: $resource_status
			},
			privacy: (.privacy // {status: "pass"}),
			follow_up_issues: (.follow_up_issues // []),
			remote_compose: {
				runtime_state_verified: true,
				runtime_state_status: "pass",
				logs_status: $logs_status,
				resource_snapshot_status: $resource_status
			},
			volume_proof: $volume_proof
		}
		+ (if $run_kind == "preserved" then {
			preserved_restart: {
				previous_clean_manifest: "accepted",
				duplicate_claims: (.preserved.duplicate_claims // 0),
				duplicate_facts: (.preserved.duplicate_facts // 0),
				duplicate_findings: (.preserved.duplicate_findings // 0),
				new_dead_letters: (.preserved.new_dead_letters // 0)
			}
		} else {} end)
	' "${summary_path}" >"${output_tmp}"
	validate_e2e_evidence_manifest "${output_tmp}" >/dev/null \
		|| die "generated manifest does not satisfy the E2E evidence contract"
	mv "${output_tmp}" "${manifest_path}"
}

main() {
	e2e_manifest_require_tools >/dev/null
	command -v docker >/dev/null 2>&1 || die "docker is required"
	command -v curl >/dev/null 2>&1 || die "curl is required"
	validate_inputs
	validate_volume_proof
	validate_previous_manifest
	validate_preserved_counters
	run_runtime_state_verifier

	local logs_status resource_status pprof_status volume_json
	logs_status="$(capture_compose_logs)"
	resource_status="$(capture_resource_snapshot)"
	pprof_status="$(check_pprof)"
	if [[ "${logs_status}" != "captured" ]]; then
		die "Compose logs were not captured"
	fi
	if [[ "${resource_status}" != "captured" ]]; then
		die "docker stats CPU/memory snapshot was not captured"
	fi
	if [[ "${pprof_status}" != "reachable" ]]; then
		die "pprof endpoint was not reachable"
	fi
	volume_json="$(volume_proof_json)"
	write_manifest "${pprof_status}" "${logs_status}" "${resource_status}" "${volume_json}"
	printf 'e2e-remote-compose-suite: pass manifest=%s\n' "${manifest_path}"
}

main "$@"
