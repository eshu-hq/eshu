#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
TARGET_STORY_FILE="${ESHU_REMOTE_E2E_TARGET_STORY_FILE:-}"
API_BASE_URL="${ESHU_REMOTE_E2E_API_BASE_URL:-}"
API_KEY="${ESHU_REMOTE_E2E_API_KEY:-}"
MCP_URL="${ESHU_REMOTE_E2E_MCP_URL:-${ESHU_MCP_URL:-}}"
MCP_TOKEN="${ESHU_REMOTE_E2E_MCP_TOKEN:-${ESHU_MCP_TOKEN:-${API_KEY}}}"
API_TIMEOUT_SECONDS="${ESHU_REMOTE_E2E_API_TIMEOUT_SECONDS:-30}"
COUNTERS_FILE="${ESHU_REMOTE_E2E_BENCHMARK_COUNTERS_FILE:-}"
TMP_DIR="$(mktemp -d)"
artifacts_dir=""

# shellcheck source=scripts/lib/remote_e2e_target_story_common.sh
source "${SCRIPT_DIR}/lib/remote_e2e_target_story_common.sh"
# shellcheck source=scripts/lib/security_intelligence_release_gate_public.sh
source "${SCRIPT_DIR}/lib/security_intelligence_release_gate_public.sh"

cleanup() {
	rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

die() {
	printf '%s\n' "$*" >&2
	exit 1
}

usage() {
	cat <<'USAGE'
Usage: verify-remote-e2e-remediation-benchmark.sh --artifacts <dir>

Runs the target-story proof, API explain readback, MCP explain readback, and
runtime status readback for the remote remediation benchmark. Raw responses stay
temporary; artifacts contain public-safe counts, states, provenance, and command
evidence only.
USAGE
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--artifacts)
			artifacts_dir="${2:-}"
			shift 2
			;;
		--help|-h)
			usage
			exit 0
			;;
		*)
			die "unknown argument: $1"
			;;
	esac
done

[[ -n "${artifacts_dir}" ]] || die "--artifacts <dir> is required"
[[ -n "${TARGET_STORY_FILE}" ]] || die "ESHU_REMOTE_E2E_TARGET_STORY_FILE is required"
[[ -f "${TARGET_STORY_FILE}" ]] || die "target story file not found"
[[ -n "${API_BASE_URL}" ]] || die "ESHU_REMOTE_E2E_API_BASE_URL is required"
[[ -n "${MCP_URL}" ]] || die "ESHU_REMOTE_E2E_MCP_URL is required"
require_tool curl
require_tool git
require_tool jq
require_positive_integer ESHU_REMOTE_E2E_API_TIMEOUT_SECONDS "${API_TIMEOUT_SECONDS}"
mkdir -p "${artifacts_dir}"

api_get_status() {
	local path="$1"
	local output_file="$2"
	local curl_config="${TMP_DIR}/status-curl.conf"
	local -a curl_args=(-fsS)
	printf 'header = "Accept: application/eshu.envelope+json"\n' >"${curl_config}"
	if [[ -n "${API_KEY}" ]]; then
		local escaped_api_key="${API_KEY//\\/\\\\}"
		escaped_api_key="${escaped_api_key//\"/\\\"}"
		printf 'header = "Authorization: Bearer %s"\n' "${escaped_api_key}" >>"${curl_config}"
	fi
	chmod 600 "${curl_config}"
	curl_args+=(-K "${curl_config}" --max-time "${API_TIMEOUT_SECONDS}" "${API_BASE_URL}${path}")
	curl "${curl_args[@]}" >"${output_file}"
}

metric_object() {
	local source_file="$1"
	local filter="$2"
	local output_file="$3"
	jq "${filter}" "${source_file}" >"${output_file}"
	if jq -e 'type == "object" and length > 0' "${output_file}" >/dev/null; then
		jq -c '{state:"reported", values:.}' "${output_file}"
	else
		jq -c -n '{state:"not_reported", values:{}}'
	fi
}

read_counter_source() {
	local index_status="$1"
	local output_file="$2"
	if [[ -n "${COUNTERS_FILE}" ]]; then
		[[ -f "${COUNTERS_FILE}" ]] || die "benchmark counters file not found"
		jq -e . "${COUNTERS_FILE}" >/dev/null || die "benchmark counters file must be valid JSON"
		cp "${COUNTERS_FILE}" "${output_file}"
	else
		cp "${index_status}" "${output_file}"
	fi
}

queue_summary() {
	local index_status="$1"
	jq -c '
		{
			status: (.status // "unknown"),
			outstanding: (.queue.outstanding // 0),
			pending: (.queue.pending // 0),
			in_flight: (.queue.in_flight // 0),
			retrying: (.queue.retrying // 0),
			failed: (.queue.failed // 0),
			dead_letter: (.queue.dead_letter // 0)
		}
		| . + {
			queue_terminal_ok:
				((.outstanding == 0) and (.pending == 0) and
				 (.in_flight == 0) and (.retrying == 0) and
				 (.failed == 0) and (.dead_letter == 0))
		}
	' "${index_status}"
}

explain_signature() {
	local input="$1"
	jq -c '
		(.data // .) as $data |
		{
			finding_status: ($data.finding.impact_status // ""),
			readiness_state: ($data.readiness.state // ""),
			missing_evidence_count: (($data.readiness.missing_evidence // []) | length),
			owner_state: ($data.remediation_packet.owner.state // ""),
			action_count: (($data.remediation_packet.actions // []) | length)
		}
	' "${input}"
}

scan_artifact_privacy() {
	local file="$1"
	if jq -r '.. | strings' "${file}" 2>/dev/null | rg --quiet \
		'ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|https?://|/security/dependabot|arn:(aws|aws-us-gov|aws-cn):|/(Users|home|private|var|tmp|Volumes|workspace|workspaces|repos|personal-repos)/|(^|[^0-9])[0-9]{12}([^0-9]|$)|([0-9]{1,3}\.){3}[0-9]{1,3}|[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}|(^|[[:space:]"'\''])[-A-Za-z0-9_.]+/[-A-Za-z0-9_.]+($|[[:space:]"'\'',])'; then
		return 1
	fi
	return 0
}

target_repository_id="$(manifest_string '.target_repository_id')"
cve_id="$(manifest_string '.remediation_benchmark.cve_id')"
package_id="$(manifest_string '.remediation_benchmark.package_id')"
image_digest="${ESHU_REMOTE_E2E_IMAGE_DIGEST:-$(manifest_string '.expected_image_digest')}"
[[ -n "${target_repository_id}" ]] || die "target story manifest requires target_repository_id"
[[ -n "${cve_id}" ]] || die "target story manifest requires remediation_benchmark.cve_id"
[[ -n "${package_id}" ]] || die "target story manifest requires remediation_benchmark.package_id"
[[ -n "${image_digest}" ]] || die "target story manifest or ESHU_REMOTE_E2E_IMAGE_DIGEST must provide image digest"

start_epoch="$(date +%s)"
commit_sha="$(git -C "${REPO_ROOT}" rev-parse HEAD)"
target_story_raw="${TMP_DIR}/target-story.log"
target_story_public="${artifacts_dir}/target-story.log"
index_status="${TMP_DIR}/index-status.json"
counter_source="${TMP_DIR}/counter-source.json"
fact_counts_json="${TMP_DIR}/fact-counts.json"
graph_writes_json="${TMP_DIR}/graph-writes.json"
api_explain="${TMP_DIR}/api-impact-explain.json"
mcp_explain="${TMP_DIR}/mcp-impact-explain.json"
summary_json="${artifacts_dir}/summary.json"
summary_md="${artifacts_dir}/summary.md"
transcript="${artifacts_dir}/command-transcript.txt"

if ! ESHU_REMOTE_E2E_API_BASE_URL="${API_BASE_URL}" \
	ESHU_REMOTE_E2E_API_KEY="${API_KEY}" \
	ESHU_REMOTE_E2E_MCP_URL="${MCP_URL}" \
	ESHU_REMOTE_E2E_MCP_TOKEN="${MCP_TOKEN}" \
	ESHU_REMOTE_E2E_API_TIMEOUT_SECONDS="${API_TIMEOUT_SECONDS}" \
	"${SCRIPT_DIR}/verify_remote_e2e_target_story.sh" >"${target_story_raw}" 2>&1; then
	sanitize_public_file "${target_story_raw}" "${target_story_public}"
	die "remote target story proof failed; sanitized log written to ${target_story_public}"
fi
sanitize_public_file "${target_story_raw}" "${target_story_public}"

api_get_status "/index-status" "${index_status}"
read_counter_source "${index_status}" "${counter_source}"
queue_json="$(queue_summary "${index_status}")"
if ! jq -e '.queue_terminal_ok == true' <<<"${queue_json}" >/dev/null; then
	die "remote remediation benchmark queue is not terminal"
fi

fact_counts="$(metric_object "${counter_source}" '.fact_counts // .facts // {}' "${fact_counts_json}")"
graph_writes="$(metric_object "${counter_source}" '.graph_writes // .graph.write_counts // .graph_writes_total // {}' "${graph_writes_json}")"
if ! jq -e '.state == "reported"' <<<"${fact_counts}" >/dev/null; then
	die "remote remediation benchmark requires fact counts from index status or ESHU_REMOTE_E2E_BENCHMARK_COUNTERS_FILE"
fi
if ! jq -e '.state == "reported"' <<<"${graph_writes}" >/dev/null; then
	die "remote remediation benchmark requires graph-write counts from index status or ESHU_REMOTE_E2E_BENCHMARK_COUNTERS_FILE"
fi

repo_query="$(urlencode "${target_repository_id}")"
cve_query="$(urlencode "${cve_id}")"
package_query="$(urlencode "${package_id}")"
explain_path="/supply-chain/impact/explain?cve_id=${cve_query}&package_id=${package_query}&repository_id=${repo_query}"
api_get "${explain_path}" "${api_explain}"
mcp_args="$(jq -n \
	--arg cve_id "${cve_id}" \
	--arg package_id "${package_id}" \
	--arg repository_id "${target_repository_id}" \
	'{cve_id:$cve_id, package_id:$package_id, repository_id:$repository_id}')"
mcp_tool_envelope explain_supply_chain_impact "${mcp_args}" "${mcp_explain}"

api_signature="$(explain_signature "${api_explain}")"
mcp_signature="$(explain_signature "${mcp_explain}")"
api_mcp_parity="fail"
if [[ "${api_signature}" == "${mcp_signature}" ]]; then
	api_mcp_parity="pass"
fi
[[ "${api_mcp_parity}" == "pass" ]] || die "API/MCP explain_supply_chain_impact parity failed"
if ! jq -e '.owner_state != "" and .action_count > 0' <<<"${api_signature}" >/dev/null; then
	die "remediation benchmark did not reach an owner/remediation packet"
fi

end_epoch="$(date +%s)"
wall_time_seconds="$((end_epoch - start_epoch))"
missing_state="$(jq -r '.readiness_state' <<<"${api_signature}")"
missing_count="$(jq -r '.missing_evidence_count' <<<"${api_signature}")"

cat >"${transcript}" <<'TRANSCRIPT'
remote remediation benchmark command transcript
- command: verify_remote_e2e_target_story.sh
- command: GET /supply-chain/impact/explain (CVE, package, and repository anchors redacted)
- command: MCP explain_supply_chain_impact (CVE, package, and repository anchors redacted)
TRANSCRIPT

jq -n \
	--arg commit_sha "${commit_sha}" \
	--arg image_digest "${image_digest}" \
	--arg cve_id "${cve_id}" \
	--argjson wall_time_seconds "${wall_time_seconds}" \
	--argjson queue "${queue_json}" \
	--argjson fact_counts "${fact_counts}" \
	--argjson graph_writes "${graph_writes}" \
	--arg api_mcp_parity "${api_mcp_parity}" \
	--arg missing_state "${missing_state}" \
	--argjson missing_count "${missing_count}" \
	'{
		schema_version: "eshu.remediation_benchmark.v1",
		issue_refs: [3174, 3178, 3129, 3061],
		provenance: {
			commit_sha: $commit_sha,
			image_digest: $image_digest,
			transcript_status: "captured_public_safe"
		},
		timing: { wall_time_seconds: $wall_time_seconds },
		target: {
			cve_id: $cve_id,
			package_anchor_recorded: true,
			repository_anchor_recorded: true
		},
		queue: $queue,
		fact_counts: $fact_counts,
		graph_writes: $graph_writes,
		parity: { api_mcp_parity: $api_mcp_parity },
		missing_evidence: {
			state: $missing_state,
			count: $missing_count
		},
		redaction: {
			private_locator_scan: "passed",
			raw_responses_published: false
		}
	}' >"${summary_json}"

if ! scan_artifact_privacy "${summary_json}"; then
	die "summary artifact looks like private data"
fi

cat >"${summary_md}" <<EOF
# Remote Remediation Benchmark

- Status: pass
- Issues: #3174, #3178, #3129, #3061
- Commit: ${commit_sha}
- Image digest: ${image_digest}
- Wall time seconds: ${wall_time_seconds}
- Queue terminal: $(jq -r '.queue_terminal_ok' <<<"${queue_json}")
- Fact counts: $(jq -r '.state' <<<"${fact_counts}")
- Graph writes: $(jq -r '.state' <<<"${graph_writes}")
- API/MCP parity: ${api_mcp_parity}
- Missing evidence state: ${missing_state}

Raw API, MCP, provider, and command outputs remain operator-local.
EOF

if rg -q 'ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|https?://|arn:(aws|aws-us-gov|aws-cn):|/(Users|home|private|var|tmp|Volumes|workspace|workspaces|repos|personal-repos)/|([0-9]{1,3}\.){3}[0-9]{1,3}|[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}' \
	"${summary_md}" "${transcript}" "${target_story_public}"; then
	die "public benchmark artifacts contain private-shaped values"
fi

printf 'remote remediation benchmark verified: wall_time_seconds=%s queue_terminal_ok=true api_mcp_parity=%s fact_counts=%s graph_writes=%s\n' \
	"${wall_time_seconds}" \
	"${api_mcp_parity}" \
	"$(jq -r '.state' <<<"${fact_counts}")" \
	"$(jq -r '.state' <<<"${graph_writes}")"
