#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-capability-budget-proof.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

command -v jq >/dev/null 2>&1 || {
	printf 'test-verify-capability-budget-proof: missing required tool: jq\n' >&2
	exit 1
}

write_matrix() {
	local dir="$1"
	mkdir -p "${dir}"
	cat >"${dir}/capability-matrix.v1.yaml" <<'YAML'
capabilities:
  - capability: code_search.exact_symbol
    tools: [find_code]
    profiles:
      production: {status: supported, max_truth_level: exact, required_runtime: deployed_services, p95_latency_ms: 800, max_scope_size: multi_repo_platform}
YAML
}

write_valid_artifact() {
	local file="$1"
	cat >"${file}" <<'JSON'
{
  "schema_version": "capability-budget-proof/v1",
  "status": "pass",
  "run": {
    "issue": 4062,
    "commit": "0123456789abcdef0123456789abcdef01234567",
    "backend": {"kind": "nornicdb", "version": "fixture-v1"}
  },
  "measurements": [{
    "capability": "code_search.exact_symbol",
    "profile": "production",
    "mcp_tools": ["find_code"],
    "corpus_slot": "medium/representative_20_50",
    "backend": {"kind": "nornicdb", "version": "fixture-v1"},
    "latency": {"p50_ms": 120, "p95_ms": 700, "p99_ms": 760},
    "scope": {
      "declared_max_scope_size": "multi_repo_platform",
      "result_scope": "multi_repo_platform",
      "limit_enforced": true,
      "truncation_proof": "limit-plus-one",
      "truncation_invariant": "pass"
    },
    "artifact_handle": "capability-budget-code-search",
    "commit": "0123456789abcdef0123456789abcdef01234567",
    "freshness": {"measured_at": "2026-06-28T00:00:00Z", "expires_at": "2026-07-28T00:00:00Z"},
    "surface_parity": {"status": "pass"},
    "retry_count": 0,
    "dead_letter_count": 0,
    "status": "pass"
  }]
}
JSON
}

expect_pass() {
	local specs="$1"
	local artifact="$2"
	if ! "${verifier}" --specs "${specs}" --artifact "${artifact}" >/tmp/eshu-capability-budget.out 2>/tmp/eshu-capability-budget.err; then
		printf 'expected capability budget verifier to pass\n' >&2
		sed -n '1,120p' /tmp/eshu-capability-budget.err >&2
		exit 1
	fi
}

expect_fail() {
	local specs="$1"
	local artifact="$2"
	local reason="$3"
	if "${verifier}" --specs "${specs}" --artifact "${artifact}" >/tmp/eshu-capability-budget.out 2>/tmp/eshu-capability-budget.err; then
		printf 'expected capability budget verifier to fail\n' >&2
		exit 1
	fi
	if ! rg --fixed-strings --quiet -- "${reason}" /tmp/eshu-capability-budget.out; then
		printf 'expected failure reason "%s"\n' "${reason}" >&2
		sed -n '1,120p' /tmp/eshu-capability-budget.out >&2
		exit 1
	fi
}

specs="${tmp_root}/specs"
write_matrix "${specs}"
valid="${tmp_root}/valid.json"
write_valid_artifact "${valid}"
expect_pass "${specs}" "${valid}"

missing="${tmp_root}/missing.json"
jq '.measurements = []' "${valid}" >"${missing}"
expect_fail "${specs}" "${missing}" "missing_measurement"

missing_latency="${tmp_root}/missing-latency.json"
jq '.measurements[0].latency = {}' "${valid}" >"${missing_latency}"
expect_fail "${specs}" "${missing_latency}" "missing_measurement"

over_budget="${tmp_root}/over-budget.json"
jq '.measurements[0].latency.p95_ms = 801' "${valid}" >"${over_budget}"
expect_fail "${specs}" "${over_budget}" "p95_over_budget"

scope_gap="${tmp_root}/scope-gap.json"
jq '.measurements[0].scope.limit_enforced = false | .measurements[0].scope.truncation_proof = ""' \
	"${valid}" >"${scope_gap}"
expect_fail "${specs}" "${scope_gap}" "scope_not_proven"

surface_disagreement="${tmp_root}/surface-disagreement.json"
jq '
	.measurements[0].api_routes = ["GET /api/v0/code/search"] |
	.measurements[0].surface_parity = {
		"status": "pass",
		"api_p95_ms": 700,
		"mcp_p95_ms": 760,
		"max_delta_ms": 20,
		"proof_handle": "capability-budget-code-search-parity"
	}
' "${valid}" >"${surface_disagreement}"
expect_fail "${specs}" "${surface_disagreement}" "surface_parity_failed"

pass_with_retry="${tmp_root}/pass-with-retry.json"
jq '.measurements[0].retry_count = 1' "${valid}" >"${pass_with_retry}"
expect_fail "${specs}" "${pass_with_retry}" "runtime_invariant_failed"

private_value="${tmp_root}/private-value.json"
jq '.measurements[0].artifact_handle = ("capability-budget-" + "https" + "://private.example.invalid")' \
	"${valid}" >"${private_value}"
expect_fail "${specs}" "${private_value}" "private_data"

"${verifier}" >/tmp/eshu-capability-budget-contract.out
rg --fixed-strings --quiet -- "pass contract=" /tmp/eshu-capability-budget-contract.out

printf 'verify-capability-budget-proof tests passed\n'
