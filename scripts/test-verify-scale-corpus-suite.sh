#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-scale-corpus-suite.sh"
spec="${repo_root}/specs/scale-lab-corpus.v1.yaml"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

expect_pass() {
	local label="$1"
	shift
	if ! "$@" >"${tmp_root}/${label}.out" 2>"${tmp_root}/${label}.err"; then
		printf 'expected %s to pass\n' "${label}" >&2
		sed -n '1,120p' "${tmp_root}/${label}.err" >&2
		exit 1
	fi
}

expect_fail_with() {
	local label="$1"
	local expected="$2"
	shift 2
	if "$@" >"${tmp_root}/${label}.out" 2>"${tmp_root}/${label}.err"; then
		printf 'expected %s to fail\n' "${label}" >&2
		exit 1
	fi
	if ! rg --fixed-strings --quiet -- "${expected}" "${tmp_root}/${label}.err"; then
		printf 'expected %s failure to include %s\n' "${label}" "${expected}" >&2
		sed -n '1,120p' "${tmp_root}/${label}.err" >&2
		exit 1
	fi
}

expect_pass published_contract "${verifier}" --spec "${spec}"

missing_pathological="${tmp_root}/missing-pathological.yaml"
cat >"${missing_pathological}" <<'YAML'
version: scale-lab-corpus/v1
parent_issue: 3169
issue: 3170
gate_status: proposed
corpus_slots:
  - id: smoke/synthetic_contracts
  - id: small/single_repo_multidomain
  - id: medium/representative_20_50
  - id: large/full_corpus_release
domains:
  - id: code_relationships
  - id: supply_chain_evidence
  - id: cloud_iac_runtime_correlation
  - id: docs
  - id: incidents
  - id: observability
privacy_rules:
  - id: no_private_identifiers
  - id: aggregate_public_outputs
  - id: fixture_sanitization
  - id: local_private_manifest_only
metrics:
  - id: fact_rows_per_second
  - id: queue_claim_latency_p95_ms
  - id: reducer_drain_seconds
  - id: graph_write_p95_ms
  - id: api_p95_ms
  - id: mcp_p95_ms
  - id: retry_count
  - id: dead_letter_count
  - id: memory_high_water_mb
  - id: correlation_fanout_candidates_p95
  - id: graph_query_plan_regression_count
thresholds:
  - id: queue_terminal_state
  - id: runtime_no_regression
  - id: query_plan_regression
  - id: privacy_public_evidence
  - id: truth_surface_agreement
acceptance:
  required_before:
    - issue: 3171
    - issue: 3172
    - issue: 3173
YAML
expect_fail_with missing_pathological \
	"missing required corpus slot: pathological/fanout_correlation" \
	"${verifier}" --spec "${missing_pathological}"

private_value="${tmp_root}/private-value.yaml"
cp "${spec}" "${private_value}"
printf '\nprivate_example: "%s://private.example.invalid/resource"\n' "https" >>"${private_value}"
expect_fail_with private_value "spec looks like private data" \
	"${verifier}" --spec "${private_value}"

printf 'verify-scale-corpus-suite tests passed\n'
