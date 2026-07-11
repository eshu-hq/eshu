#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify_remote_e2e_runtime_state.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

fake_bin="${tmp_root}/bin"
state_dir="${tmp_root}/state"
mkdir -p "${fake_bin}" "${state_dir}"

# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
# the entire heredoc body to a pipe before forking the reader, and macOS's
# 512-byte pipe buffer deadlocks on any body over that size (#5074).
cp "${repo_root}/scripts/lib/test-verify-remote-e2e-runtime-state-fake-docker.sh" "${fake_bin}/docker"
chmod +x "${fake_bin}/docker"

# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
# the entire heredoc body to a pipe before forking the reader, and macOS's
# 512-byte pipe buffer deadlocks on any body over that size (#5074).
cp "${repo_root}/scripts/lib/test-verify-remote-e2e-runtime-state-fake-curl.sh" "${fake_bin}/curl"
chmod +x "${fake_bin}/curl"

reset_state() {
  rm -rf "${state_dir}/containers" "${state_dir}/service_ids"
  rm -f "${state_dir}/curl-fails" "${state_dir}/curl-targets"
  mkdir -p "${state_dir}/containers" "${state_dir}/service_ids"
  cat >"${state_dir}/services" <<'SERVICES'
eshu
mcp-server
ingester
projector
resolution-engine
workflow-coordinator
collector-terraform-state
collector-oci-registry
collector-package-registry
collector-sbom-attestation
collector-security-alerts
collector-vulnerability-intelligence
collector-aws-cloud
scanner-worker
SERVICES
  printf '%s\n' '{"status":"healthy","queue":{"outstanding":0,"in_flight":0,"pending":0,"retrying":0,"failed":0,"dead_letter":0},"coordinator":{"run_status_counts":[{"name":"complete","count":4}],"completeness_counts":[]}}' >"${state_dir}/index-status.json"
  cat >"${state_dir}/package-count.json" <<'JSON'
{"total_packages": 3}
JSON
  cat >"${state_dir}/status-index.json" <<'JSON'
{"terraform_state":{"warning_summary":[]}}
JSON
  cat >"${state_dir}/advisory-evidence.json" <<'JSON'
{"count": 1, "truncated": false}
JSON
  cat >"${state_dir}/impact-count.json" <<'JSON'
{"total_findings": 2, "affected_findings": 2}
JSON
  printf '%s\n' '{"findings":[],"readiness":{"readiness_state":"unsupported","unsupported_targets":[{"target_kind":"package_registry_metadata","reason":"metadata_too_large","ecosystem":"npm","count":1}]}}' >"${state_dir}/package-registry-gap-readiness.json"
  cat >"${state_dir}/security-alert-count.json" <<'JSON'
{"total_reconciliations": 1}
JSON
  cat >"${state_dir}/sbom-count.json" <<'JSON'
{"total_attachments": 1}
JSON
  cat >"${state_dir}/container-image-count.json" <<'JSON'
{"total_identities": 1}
JSON
}

set_service_state() {
  local service="$1"
  local runtime_state="$2"
  local health_state="$3"
  local container_id="${service}-container"
  printf '%s\n' "${container_id}" >"${state_dir}/service_ids/${service}"
  printf '%s %s\n' "${runtime_state}" "${health_state}" >"${state_dir}/containers/${container_id}"
}

set_all_services_healthy() {
  local service
  while IFS= read -r service; do
    [[ -n "${service}" ]] || continue
    set_service_state "${service}" running healthy
  done <"${state_dir}/services"
}

run_verifier() {
  ESHU_REMOTE_E2E_TEST_STATE="${state_dir}" \
    PATH="${fake_bin}:${PATH}" \
    ESHU_REMOTE_E2E_COMPOSE_FILES="docker-compose.remote-e2e.yaml" \
    "${verifier}" >/tmp/eshu-remote-e2e-runtime.out 2>/tmp/eshu-remote-e2e-runtime.err
}

expect_pass() {
  if ! run_verifier; then
    printf 'expected remote E2E runtime verifier to pass\n' >&2
    sed -n '1,160p' /tmp/eshu-remote-e2e-runtime.err >&2
    exit 1
  fi
}

expect_fail_with() {
  local pattern="$1"
  if run_verifier; then
    printf 'expected remote E2E runtime verifier to fail with %s\n' "${pattern}" >&2
    sed -n '1,160p' /tmp/eshu-remote-e2e-runtime.out >&2
    exit 1
  fi
  if ! rg -q "${pattern}" /tmp/eshu-remote-e2e-runtime.err; then
    printf 'expected failure output to contain %s\n' "${pattern}" >&2
    sed -n '1,160p' /tmp/eshu-remote-e2e-runtime.err >&2
    exit 1
  fi
}

reset_state
set_all_services_healthy
set_service_state ingester created none
expect_fail_with 'ingester.*created'

reset_state
set_all_services_healthy
set_service_state projector created none
expect_fail_with 'projector.*created'

reset_state
set_all_services_healthy
set_service_state collector-oci-registry running unhealthy
expect_fail_with 'collector-oci-registry.*unhealthy'

reset_state
set_all_services_healthy
set_service_state scanner-worker created none
expect_fail_with 'scanner-worker.*created'

reset_state
set_all_services_healthy
cat >"${state_dir}/index-status.json" <<'JSON'
{
  "status": "stalled",
  "queue": {
    "outstanding": 12,
    "in_flight": 0,
    "pending": 12,
    "retrying": 0,
    "failed": 0,
    "dead_letter": 0
  }
}
JSON
expect_fail_with 'finite completion'

reset_state
set_all_services_healthy
touch "${state_dir}/curl-fails"
expect_fail_with '/api/v0/index-status'

reset_state
set_all_services_healthy
cat >"${state_dir}/index-status.json" <<'JSON'
{
  "status": "healthy",
  "queue": {
    "outstanding": 0,
    "in_flight": 0,
    "pending": 0,
    "retrying": 0,
    "failed": 0,
    "dead_letter": 0
  },
  "coordinator": {
    "run_status_counts": [
      {"name": "complete", "count": 12},
      {"name": "reducer_converging", "count": 12}
    ],
    "completeness_counts": [
      {"name": "pending", "count": 36}
    ]
  }
}
JSON
expect_fail_with 'finite completion'
reset_state
set_all_services_healthy
expect_pass
if ! rg -q 'remote E2E aggregate proof counts: package_registry_packages=3 advisory_evidence=1 impact_findings=2 affected_findings=2 security_alert_reconciliations=1 sbom_attachments=1 container_image_identities=1' /tmp/eshu-remote-e2e-runtime.out; then
  printf 'expected aggregate proof counts in verifier output\n' >&2
  sed -n '1,220p' /tmp/eshu-remote-e2e-runtime.out >&2
  exit 1
fi
reset_state
set_all_services_healthy
# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
# the entire heredoc body to a pipe before forking the reader, and macOS's
# 512-byte pipe buffer deadlocks on any body over that size (#5074).
cat "${repo_root}/scripts/lib/test-verify-remote-e2e-runtime-state-index-status-progressing.json" >"${state_dir}/index-status.json"
export ESHU_REMOTE_E2E_CORPUS_MODE=representative ESHU_REMOTE_E2E_MIN_ADVISORY_EVIDENCE_COUNT=0
expect_pass
rg -q 'remote E2E representative proof safety state:' /tmp/eshu-remote-e2e-runtime.out || { printf 'expected representative proof safety state in verifier output\n' >&2; sed -n '1,260p' /tmp/eshu-remote-e2e-runtime.out >&2; exit 1; }
rg -q 'remote E2E representative background workflow activity:' /tmp/eshu-remote-e2e-runtime.out || { printf 'expected representative background workflow activity in verifier output\n' >&2; sed -n '1,260p' /tmp/eshu-remote-e2e-runtime.out >&2; exit 1; }
unset ESHU_REMOTE_E2E_CORPUS_MODE ESHU_REMOTE_E2E_MIN_ADVISORY_EVIDENCE_COUNT
reset_state
set_all_services_healthy
cat >"${state_dir}/index-status.json" <<'JSON'
{
  "status": "progressing",
  "queue": {
    "outstanding": 25,
    "in_flight": 2,
    "pending": 23,
    "retrying": 0,
    "failed": 0,
    "dead_letter": 0
  },
  "coordinator": {
    "run_status_counts": [
      {"name": "complete", "count": 12}
    ],
    "completeness_counts": []
  }
}
JSON
export ESHU_REMOTE_E2E_CORPUS_MODE=representative
export ESHU_REMOTE_E2E_DERIVED_TARGET_LIMIT=2
expect_fail_with 'representative derived fanout exceeded'
unset ESHU_REMOTE_E2E_DERIVED_TARGET_LIMIT
unset ESHU_REMOTE_E2E_CORPUS_MODE
reset_state
set_all_services_healthy
export ESHU_REMOTE_E2E_CORPUS_MODE=representative
export ESHU_REMOTE_E2E_MIN_ADVISORY_EVIDENCE_COUNT=0
expect_pass
if rg -q '/api/v0/supply-chain/advisories/evidence' "${state_dir}/curl-targets"; then
  printf 'expected representative verifier to skip advisory evidence probe when minimum is zero\n' >&2
  sed -n '1,240p' "${state_dir}/curl-targets" >&2
  exit 1
fi
if ! rg -q 'remote E2E aggregate proof count advisory_evidence skipped: minimum=0' /tmp/eshu-remote-e2e-runtime.out; then
  printf 'expected representative verifier to report skipped advisory evidence probe\n' >&2
  exit 1
fi
unset ESHU_REMOTE_E2E_MIN_ADVISORY_EVIDENCE_COUNT
unset ESHU_REMOTE_E2E_CORPUS_MODE
reset_state
set_all_services_healthy
cat >"${state_dir}/index-status.json" <<'JSON'
{
  "status": "progressing",
  "queue": {
    "outstanding": 8,
    "in_flight": 1,
    "pending": 6,
    "retrying": 1,
    "failed": 0,
    "dead_letter": 0
  },
  "coordinator": {
    "run_status_counts": [
      {"name": "complete", "count": 12}
    ],
    "completeness_counts": []
  }
}
JSON
export ESHU_REMOTE_E2E_CORPUS_MODE=representative
expect_fail_with 'representative runtime safety'
unset ESHU_REMOTE_E2E_CORPUS_MODE
reset_state
set_all_services_healthy
cat >"${state_dir}/index-status.json" <<'JSON'
{
  "status": "healthy",
  "queue": {
    "outstanding": 0,
    "in_flight": 0,
    "pending": 0,
    "retrying": 0,
    "failed": 0,
    "dead_letter": 0
  },
  "coordinator": {
    "run_status_counts": [
      {"name": "complete", "count": 12},
      {"name": "failed", "count": 1}
    ],
    "completeness_counts": [
      {"name": "blocked", "count": 1}
    ]
  }
}
JSON
export ESHU_REMOTE_E2E_CORPUS_MODE=representative
expect_fail_with 'representative runtime safety'
unset ESHU_REMOTE_E2E_CORPUS_MODE
reset_state
set_all_services_healthy
export ESHU_REMOTE_E2E_PACKAGE_REGISTRY_GAP_PACKAGE_ID='npm://registry.npmjs.org/oversized'
expect_pass
if ! rg -q 'package_registry_metadata_too_large_gaps=1' /tmp/eshu-remote-e2e-runtime.out; then
  printf 'expected package-registry metadata too-large gap proof in verifier output\n' >&2
  sed -n '1,240p' /tmp/eshu-remote-e2e-runtime.out >&2
  exit 1
fi
if rg -q 'npm://registry.npmjs.org/oversized' /tmp/eshu-remote-e2e-runtime.out; then
  printf 'package-registry gap proof leaked package_id\n' >&2
  sed -n '1,240p' /tmp/eshu-remote-e2e-runtime.out >&2
  exit 1
fi
unset ESHU_REMOTE_E2E_PACKAGE_REGISTRY_GAP_PACKAGE_ID
reset_state
set_all_services_healthy
cat >"${state_dir}/package-registry-gap-readiness.json" <<'JSON'
{
  "findings": [],
  "readiness": {
    "readiness_state": "ready_zero_findings",
    "unsupported_targets": []
  }
}
JSON
export ESHU_REMOTE_E2E_PACKAGE_REGISTRY_GAP_PACKAGE_ID='npm://registry.npmjs.org/oversized'
expect_fail_with 'package_registry_metadata_too_large_gaps=0 below required minimum 1'
unset ESHU_REMOTE_E2E_PACKAGE_REGISTRY_GAP_PACKAGE_ID
reset_state
set_all_services_healthy
cat >"${state_dir}/package-count.json" <<'JSON'
{"total_packages": 0}
JSON
export ESHU_REMOTE_E2E_CORPUS_MODE=representative ESHU_REMOTE_E2E_MIN_ADVISORY_EVIDENCE_COUNT=0
expect_fail_with 'package_registry_packages=0 below required minimum 1'
unset ESHU_REMOTE_E2E_CORPUS_MODE ESHU_REMOTE_E2E_MIN_ADVISORY_EVIDENCE_COUNT
printf 'verify-remote-e2e-runtime-state tests passed\n'
