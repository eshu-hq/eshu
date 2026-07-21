#!/usr/bin/env bash
# MCP-identity E2E sensitivity gate (F-9, issue #5170, design §5 "Sensitivity
# (mutation) proof"). Proves the suite's NEGATIVE credential-less leakage
# module is LIVE — that it would catch a real regression — by recreating the
# mcp-server with the credential-source gate deliberately disabled and asserting
# the module FLIPS from pass to fail (an inverted exit), distinguishing "failed
# ON the mutated gate" from "crashed" via the report's step ids.
#
# Runs the fast standalone `credentialless` module (ESHU_E2E_MCP_MODULE=
# credentialless): no browser, wizard, or shape state — just credential-less
# initialize/tools-list/ping/GET-sse/API probes against a live mcp-server + API.
# Against the real gate every probe 401s (PASS); against the mutated gate
# (docker-compose.e2e.mutation.yaml: ESHU_AUTH_RESOURCE_URI unset +
# ESHU_MCP_ALLOW_UNAUTHENTICATED=true -> dev-mode-open) they 200 (FAIL).
#
# Own isolated Compose project + port block, disjoint from the normal suite's
# eshu-e2e-auth-mcp / 29xxx, so both can run concurrently on one machine.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

# shellcheck source=scripts/lib/auth_mcp_e2e_sensitivity.sh
source "${repo_root}/scripts/lib/auth_mcp_e2e_sensitivity.sh"

project="${ESHU_E2E_SENS_PROJECT_NAME:-eshu-e2e-auth-mcp-sens}"
bind_addr="127.0.0.1"
api_port="${ESHU_E2E_SENS_API_PORT:-29580}"
postgres_port="${ESHU_E2E_SENS_POSTGRES_PORT:-29532}"
postgres_password="change-me"
mcp_port="${ESHU_E2E_SENS_MCP_PORT:-29581}"
nornicdb_http_port="${ESHU_E2E_SENS_NORNICDB_HTTP_PORT:-29574}"
nornicdb_bolt_port="${ESHU_E2E_SENS_NORNICDB_BOLT_PORT:-29587}"
keep_stack="${ESHU_KEEP_COMPOSE_STACK:-false}"

export ESHU_E2E_PROJECT_NAME="$project"
export ESHU_E2E_BIND_ADDR="$bind_addr"
export ESHU_E2E_API_PORT="$api_port"
export ESHU_E2E_POSTGRES_PORT="$postgres_port"
export ESHU_E2E_POSTGRES_PASSWORD="$postgres_password"
export ESHU_E2E_MCP_PORT="$mcp_port"
export ESHU_E2E_NORNICDB_HTTP_PORT="$nornicdb_http_port"
export ESHU_E2E_NORNICDB_BOLT_PORT="$nornicdb_bolt_port"

api_base="http://${bind_addr}:${api_port}"
mcp_base="http://${bind_addr}:${mcp_port}"
report_path="${repo_root}/e2e-artifacts/auth-mcp-e2e-report.json"
step_id="leakage_credentialless_probes_do_not_leak"

base_compose=(-p "$project" -f docker-compose.e2e.yaml)
mutation_compose=(-p "$project" -f docker-compose.e2e.yaml -f docker-compose.e2e.mutation.yaml)

for tool in docker node jq; do
	command -v "$tool" >/dev/null 2>&1 || {
		echo "verify-auth-mcp-e2e-sensitivity: missing required tool: $tool" >&2
		exit 1
	}
done

teardown() {
	if [[ "$keep_stack" == "true" ]]; then
		echo "verify-auth-mcp-e2e-sensitivity: ESHU_KEEP_COMPOSE_STACK=true — leaving stack up (project $project)"
		return
	fi
	echo "verify-auth-mcp-e2e-sensitivity: tearing down project $project"
	docker compose "${base_compose[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
}
trap teardown EXIT

# run_credentialless runs the standalone module against the current stack and
# copies the report to dest (the runner overwrites report_path each run).
run_credentialless() {
	local dest="$1"
	local rc=0
	ESHU_E2E_API_BASE="$api_base" \
		ESHU_E2E_MCP_BASE="$mcp_base" \
		ESHU_E2E_MCP_MODULE="credentialless" \
		node scripts/auth-mcp-e2e-runtime.mjs || rc=$?
	if [[ -f "$report_path" ]]; then
		cp "$report_path" "$dest"
	fi
	return "$rc"
}

echo "verify-auth-mcp-e2e-sensitivity: bringing up the base stack (project $project)"
docker compose "${base_compose[@]}" up -d --build --wait \
	postgres nornicdb db-migrate workspace-setup eshu mcp-server

for endpoint in "${api_base}/healthz" "${mcp_base}/health"; do
	code="$(curl -sS -m 5 -o /dev/null -w '%{http_code}' "${endpoint}" || true)"
	if [[ "$code" != "200" ]]; then
		echo "verify-auth-mcp-e2e-sensitivity: ${endpoint} returned ${code}; stack not ready" >&2
		exit 1
	fi
done

baseline_report="$(mktemp)"
mutated_report="$(mktemp)"
restored_report="$(mktemp)"

echo "verify-auth-mcp-e2e-sensitivity: [1/3] baseline — real gate, expect PASS (exit 0)"
baseline_rc=0
run_credentialless "$baseline_report" || baseline_rc=$?
if [[ "$baseline_rc" -ne 0 ]]; then
	echo "verify-auth-mcp-e2e-sensitivity: baseline FAILED (exit $baseline_rc) against the real gate — the negative module is broken, not the gate" >&2
	exit 1
fi
auth_mcp_sensitivity_assert_step_passed "$baseline_report" "$step_id"
echo "verify-auth-mcp-e2e-sensitivity: baseline PASS confirmed"

echo "verify-auth-mcp-e2e-sensitivity: [2/3] mutate mcp-server (disable credential-source gate) and re-run, expect FAIL (exit != 0)"
docker compose "${mutation_compose[@]}" up -d --no-deps --wait mcp-server
mutated_rc=0
run_credentialless "$mutated_report" || mutated_rc=$?
if [[ "$mutated_rc" -eq 0 ]]; then
	echo "verify-auth-mcp-e2e-sensitivity: MUTATED run PASSED (exit 0) — the negative module did NOT detect the disabled gate; sensitivity NOT proven" >&2
	exit 1
fi
# Inverted exit confirmed non-zero; now prove it failed ON the gate, not a crash.
auth_mcp_sensitivity_assert_step_failed "$mutated_report" "$step_id"
echo "verify-auth-mcp-e2e-sensitivity: mutated run correctly FAILED on the gate (inverted exit $mutated_rc, step '$step_id' = fail)"

echo "verify-auth-mcp-e2e-sensitivity: [3/3] restore mcp-server and re-run, expect PASS again (exit 0)"
docker compose "${base_compose[@]}" up -d --no-deps --wait mcp-server
restored_rc=0
run_credentialless "$restored_report" || restored_rc=$?
if [[ "$restored_rc" -ne 0 ]]; then
	echo "verify-auth-mcp-e2e-sensitivity: RESTORED run FAILED (exit $restored_rc) — the mutation did not cleanly revert" >&2
	exit 1
fi
auth_mcp_sensitivity_assert_step_passed "$restored_report" "$step_id"

rm -f "$baseline_report" "$mutated_report" "$restored_report"
echo "verify-auth-mcp-e2e-sensitivity: PASS — negative module is live (real gate PASS -> mutated FAIL-on-gate -> restored PASS)"
