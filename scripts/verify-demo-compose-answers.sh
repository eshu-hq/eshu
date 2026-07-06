#!/usr/bin/env bash
# Proof script for docker-compose.demo.yaml (issue #4742). Failing-test-first:
# this script is RED before docker-compose.demo.yaml exists (no such file, or
# `docker compose config` fails) and GREEN after — the credential-free demo
# stack answers all five specs/demo-first-answers.v1.yaml questions over HTTP,
# then tears down to zero leftover containers/volumes/networks.
#
# Usage: scripts/verify-demo-compose-answers.sh [--keep]
#   --keep   leave the stack running on exit (for debugging a failed run).
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

compose_file="docker-compose.demo.yaml"
project_name="eshu-demo-proof-$$"

: "${ESHU_DEMO_API_PORT:=18080}"
: "${ESHU_DEMO_MCP_PORT:=18091}"
: "${ESHU_DEMO_STACK_TIMEOUT_SECONDS:=1200}"

keep=0
for arg in "$@"; do
	case "${arg}" in
		--keep) keep=1 ;;
		-h | --help)
			sed -n '2,10p' "${BASH_SOURCE[0]}"
			exit 0
			;;
		*)
			echo "verify-demo-compose-answers: unknown argument: ${arg}" >&2
			exit 2
			;;
	esac
done

log() { printf '\n=== %s ===\n' "$*"; }
die() {
	printf 'verify-demo-compose-answers: %s\n' "$*" >&2
	exit 1
}

torn_down=0
teardown() {
	[[ "${torn_down}" -eq 1 ]] && return 0
	torn_down=1
	if [[ "${keep}" -eq 1 ]]; then
		printf '\n[--keep] stack left running: docker compose -p %s -f %s ...\n' "${project_name}" "${compose_file}" >&2
		return 0
	fi
	log "teardown: docker compose down -v --remove-orphans"
	docker compose -p "${project_name}" -f "${compose_file}" down -v --remove-orphans >/dev/null 2>&1 || true
}

cleanup() {
	local status=$?
	if [[ "${status}" -ne 0 ]]; then
		printf '\n=== compose logs (failure) ===\n' >&2
		docker compose -p "${project_name}" -f "${compose_file}" logs --no-color --tail=80 >&2 2>&1 || true
	fi
	teardown
	exit "${status}"
}
trap cleanup EXIT

# ----------------------------------------------------------------------------
# RED: the overlay must exist and render before anything else is meaningful.
# ----------------------------------------------------------------------------
log "RED check: docker-compose.demo.yaml must exist and render"
[[ -f "${compose_file}" ]] || die "RED confirmed: ${compose_file} does not exist"
docker compose -f "${compose_file}" config >/dev/null || die "RED confirmed: ${compose_file} does not render"
echo "GREEN precondition: ${compose_file} exists and renders"

# ----------------------------------------------------------------------------
# Hard gate: no credential-required env anywhere in the demo path.
#
# The compose files are the operator-facing contract: a required (":?") var
# there would force an operator to supply a credential before the stack boots.
# The two demo shell scripts also use ":?" but as internal wiring guards on
# values docker-compose.demo.corpus.yaml always supplies (ESHU_DEMO_CORPUS_REPOS,
# ESHU_POSTGRES_DSN) — defensive "this must have been wired by compose" checks,
# not an operator-supplied credential — so they are intentionally excluded from
# the compose-file gate below and checked separately for credential-shaped vars.
# ----------------------------------------------------------------------------
compose_files=(docker-compose.demo.yaml docker-compose.demo.corpus.yaml docker-compose.demo.runtime.yaml)
demo_files=("${compose_files[@]}" scripts/demo-corpus-staging.sh scripts/demo-corpus-orchestrator.sh)

log "hard gate: no required (\":?\") credential env in the demo compose files"
if rg -n ':\?' "${compose_files[@]}"; then
	die "hard gate failed: found a required (:?) env var in a demo compose file"
fi
echo "hard gate passed: no :? required env vars in compose files"

# ESHU_API_KEY is intentionally excluded: it is Eshu's own self-issued auth
# token (ESHU_AUTO_GENERATE_API_KEY=true generates it internally, the same
# pattern the default docker-compose.yaml eshu/mcp-server services use), not an
# operator-supplied external credential. The provider-shaped names below
# (*_TOKEN, cloud provider keys, third-party *_API_KEY handles such as
# DEEPSEEK_API_KEY or NVD_API_KEY) are the actual credential surface this gate
# protects against.
#
# The gate flags a credential var only when it CONSUMES a value: a host-env
# interpolation (`VAR: ${...}`) or a non-empty literal. A neutralizing
# `VAR: ""` assignment is the opposite of a leak — the demo pins each inherited
# provider var to empty so `extends` cannot pass an operator's credential
# through — so it must NOT trip the gate. The `[^"\n]` after `${` / a non-quote
# first value char is what distinguishes a consuming assignment from `: ""`.
# Comment lines that merely name a credential var (rationale text) are excluded
# by requiring the `VAR:` assignment form, not a bare mention.
cred_names='(_TOKEN|AWS_ACCESS_KEY|AWS_SECRET|AZURE_CLIENT_SECRET|GOOGLE_APPLICATION_CREDENTIALS|GITHUB_TOKEN|PAGERDUTY_API_TOKEN|JIRA_API_TOKEN|DEEPSEEK_API_KEY|NVD_API_KEY)'
# Matches:  <indent>NAME: ${...}   or   <indent>NAME: <non-empty, non-""-literal>
# Does not match:  NAME: ""   or   a comment line containing NAME.
cred_consume_re="^[[:space:]]*[A-Za-z0-9_]*${cred_names}[A-Za-z0-9_]*:[[:space:]]*(\\\$\{|[^\"[:space:]])"
log "hard gate: no *_TOKEN / external-provider *_API_KEY / cloud-credential env consumed by the demo path"
if rg -n "${cred_consume_re}" "${demo_files[@]}"; then
	die "hard gate failed: a credential-shaped env var CONSUMES a value in the demo path (must be pinned to \"\" or absent)"
fi
echo "hard gate passed: no credential-shaped env var consumes a value (neutralized or absent)"

# ----------------------------------------------------------------------------
# GREEN: boot the stack from a clean checkout with zero credential env.
# ----------------------------------------------------------------------------
log "boot demo stack (project ${project_name}) with zero credential env"
docker compose -p "${project_name}" -f "${compose_file}" up --build -d --wait --wait-timeout "${ESHU_DEMO_STACK_TIMEOUT_SECONDS}" \
	|| die "docker compose up did not reach healthy/completed state within ${ESHU_DEMO_STACK_TIMEOUT_SECONDS}s"

api_base="http://localhost:${ESHU_DEMO_API_PORT}"
mcp_base="http://localhost:${ESHU_DEMO_MCP_PORT}"

log "wait for API /readyz"
api_ready=false
for _ in $(seq 1 60); do
	curl -fsS "${api_base}/readyz" >/dev/null 2>&1 && { api_ready=true; break; }
	sleep 2
done
[[ "${api_ready}" == "true" ]] || die "eshu-api /readyz never returned on ${api_base}"

log "wait for MCP /health"
mcp_ready=false
for _ in $(seq 1 60); do
	curl -fsS "${mcp_base}/health" >/dev/null 2>&1 && { mcp_ready=true; break; }
	sleep 2
done
[[ "${mcp_ready}" == "true" ]] || die "eshu-mcp-server /health never returned on ${mcp_base}"

# ----------------------------------------------------------------------------
# Hard gate: the RUNNING container env must be provider-credential-free. The
# file-only grep above is necessary but not sufficient — the demo services
# `extends` the base compose, which MERGES the base env, so an operator's
# exported DEEPSEEK_API_KEY / provider profile / Ask Eshu settings could be
# inherited into the live container even though the demo files themselves
# reference no such var. Assert the actual process env instead.
# ----------------------------------------------------------------------------
log "hard gate: running container env is provider-credential-free"
assert_container_env_clean() {
	local service="$1" var expect actual
	# var:expected-value pairs. Empty expected means the var must be empty/unset.
	for pair in \
		"DEEPSEEK_API_KEY:" \
		"ESHU_SEMANTIC_PROVIDER_PROFILES_JSON:" \
		"ESHU_SEMANTIC_EXTRACTION_POLICY_JSON:" \
		"ESHU_SEMANTIC_SEARCH_PROVIDER_PROFILE_ID:" \
		"ESHU_ASK_ENABLED:false" \
		"ESHU_ASK_NARRATION_ENABLED:false"; do
		var="${pair%%:*}"
		expect="${pair#*:}"
		# printenv exits non-zero when the var is unset; treat that as empty.
		actual="$(docker compose -p "${project_name}" -f "${compose_file}" exec -T "${service}" printenv "${var}" 2>/dev/null || true)"
		actual="$(printf '%s' "${actual}" | tr -d '\r\n')"
		if [[ "${actual}" != "${expect}" ]]; then
			die "container env leak: ${service} ${var}=${actual:-<empty>}, want '${expect:-<empty>}' (demo must not inherit operator provider credentials)"
		fi
	done
	echo "${service}: provider env clean (no credentials, ask disabled)"
}
assert_container_env_clean eshu
assert_container_env_clean mcp-server

# call_mcp_tool posts a tools/call JSON-RPC request to /mcp/message with NO
# auth header and prints the tool's answer object as JSON. MCP tools return a
# canonical envelope { data, truth, error } in structuredContent; this unwraps
# to `data` (the answer body) so callers assert on the answer fields directly.
# A tool that returns a bare object (no envelope) is passed through unchanged.
# No Authorization header is sent — the demo serves reads open, and the answers
# coming back are the evidence that open posture works end to end.
call_mcp_tool() {
	local tool_name="$1" arguments_json="$2"
	local response
	response="$(curl -fsS -X POST "${mcp_base}/mcp/message" \
		-H 'Content-Type: application/json' \
		-d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"${tool_name}\",\"arguments\":${arguments_json}}}")"
	python3 - "$response" <<'PYEOF'
import json
import sys

doc = json.loads(sys.argv[1])
if doc.get("error"):
	sys.stderr.write(f"tools/call rpc error: {doc['error']}\n")
	sys.exit(1)
result = doc.get("result") or {}
structured = result.get("structuredContent")
if structured is None:
	for entry in result.get("content", []):
		if entry.get("type") == "text":
			structured = json.loads(entry["text"])
			break
if structured is None:
	sys.stderr.write("tools/call: no structuredContent or text content\n")
	sys.exit(1)
if result.get("isError") or (isinstance(structured, dict) and structured.get("error")):
	sys.stderr.write(f"tools/call: tool reported error: {json.dumps(structured)[:400]}\n")
	sys.exit(1)
# Unwrap the canonical { data, truth, error } envelope to the answer body.
if isinstance(structured, dict) and "data" in structured and "truth" in structured:
	structured = structured["data"]
print(json.dumps(structured))
PYEOF
}

# assert_fields_present checks each field name is a top-level JSON key in body.
assert_fields_present() {
	local label="$1" body="$2"
	shift 2
	python3 - "$label" "$body" "$@" <<'PYEOF'
import json
import sys

label = sys.argv[1]
doc = json.loads(sys.argv[2])
fields = sys.argv[3:]
if not isinstance(doc, dict):
	sys.stderr.write(f"{label}: response is not a JSON object ({type(doc).__name__})\n")
	sys.exit(1)
missing = [f for f in fields if f not in doc]
if missing:
	sys.stderr.write(f"{label}: missing required fields {missing} in response keys {sorted(doc.keys())}\n")
	sys.exit(1)
print(f"{label}: all required fields present ({fields})")
PYEOF
}

# json_count prints the integer `count` field of a JSON object body.
json_count() { python3 -c 'import json,sys; print(json.loads(sys.argv[1]).get("count",0))' "$1"; }

# Q1 and Q3 assert the answer the manifest's playbooks drive, via their MCP
# parity tools (get_service_story, get_incident_context) — the manifest records
# these as the parity surfaces, and the playbook resolve endpoint returns the
# call plan rather than the answer. The answer fields are the manifest's
# required_response_fields for each question.
log "Q1 (code_to_deployment): get_service_story for workload:api-svc"
q1_body="$(call_mcp_tool get_service_story '{"workload_id":"workload:api-svc"}')"
assert_fields_present "Q1" "${q1_body}" answer_metadata answer_packet api_surface ci_cd_evidence code_to_runtime_trace deployment_evidence deployment_lanes deployment_overview service_identity service_name

log "Q2 (deployment_to_cloud_resource): list_kubernetes_correlations for supply-chain-demo"
q2_body="$(call_mcp_tool list_kubernetes_correlations '{"cluster_id":"supply-chain-demo"}')"
assert_fields_present "Q2" "${q2_body}" correlations count limit truncated
q2_count="$(json_count "${q2_body}")"
[[ "${q2_count}" -ge 1 ]] || die "Q2 failed: count=${q2_count}, want >= 1 (rc-4 workload->image correlation did not converge)"
echo "Q2: count=${q2_count} (rc-4 present)"

log "Q3 (incident_to_service): get_incident_context for PSCD1"
q3_body="$(call_mcp_tool get_incident_context '{"incident_id":"PSCD1"}')"
assert_fields_present "Q3" "${q3_body}" answer_metadata answer_packet evidence_path incident related_changes timeline

log "Q4 (cross-repo dependency): list_package_registry_correlations for github.com/acme/lib-common"
q4_body="$(call_mcp_tool list_package_registry_correlations '{"package_id":"github.com/acme/lib-common"}')"
assert_fields_present "Q4" "${q4_body}" collector_readiness correlations count
q4_count="$(json_count "${q4_body}")"
[[ "${q4_count}" -ge 1 ]] || die "Q4 failed: count=${q4_count}, want >= 1 (rc-3 cross-repo DEPENDS_ON did not converge)"
echo "Q4: count=${q4_count} (rc-3 present)"

log "Q5 (observability_to_workload): GET /api/v0/observability/coverage/correlations?provider=tempo"
q5_body="$(curl -fsS "${api_base}/api/v0/observability/coverage/correlations?provider=tempo&limit=50")"
assert_fields_present "Q5" "${q5_body}" correlations count limit truncated
q5_count="$(json_count "${q5_body}")"
[[ "${q5_count}" -ge 1 ]] || die "Q5 failed: count=${q5_count}, want >= 1 (tempo coverage correlation did not converge)"
echo "Q5: count=${q5_count}"

log "PASS: all five demo-first-answers questions answered correctly over HTTP"

# ----------------------------------------------------------------------------
# Teardown assertion: down -v --remove-orphans must leave zero leftovers.
# ----------------------------------------------------------------------------
if [[ "${keep}" -eq 0 ]]; then
	log "teardown: docker compose down -v --remove-orphans"
	docker compose -p "${project_name}" -f "${compose_file}" down -v --remove-orphans
	torn_down=1

	log "hard gate: zero leftover containers/volumes/networks for project ${project_name}"
	leftover_containers="$(docker ps -a --filter "label=com.docker.compose.project=${project_name}" -q)"
	leftover_volumes="$(docker volume ls --filter "label=com.docker.compose.project=${project_name}" -q)"
	leftover_networks="$(docker network ls --filter "label=com.docker.compose.project=${project_name}" -q)"
	[[ -z "${leftover_containers}" ]] || die "teardown left containers: ${leftover_containers}"
	[[ -z "${leftover_volumes}" ]] || die "teardown left volumes: ${leftover_volumes}"
	[[ -z "${leftover_networks}" ]] || die "teardown left networks: ${leftover_networks}"
	echo "teardown verified clean: zero containers, volumes, networks for ${project_name}"
fi

log "PASS: verify-demo-compose-answers green end to end"
