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
cat "${repo_root}/scripts/lib/test-verify-remote-e2e-finite-completion-fake-docker.sh" >"${fake_bin}/docker"
chmod +x "${fake_bin}/docker"

# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
# the entire heredoc body to a pipe before forking the reader, and macOS's
# 512-byte pipe buffer deadlocks on any body over that size (#5074).
cat "${repo_root}/scripts/lib/test-verify-remote-e2e-finite-completion-fake-curl.sh" >"${fake_bin}/curl"
chmod +x "${fake_bin}/curl"

reset_state() {
  rm -rf "${state_dir}/containers" "${state_dir}/service_ids"
  rm -f "${state_dir}/curl-targets"
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
  while IFS= read -r service; do
    [[ -n "${service}" ]] || continue
    printf '%s\n' "${service}-container" >"${state_dir}/service_ids/${service}"
    printf 'running healthy\n' >"${state_dir}/containers/${service}-container"
  done <"${state_dir}/services"
  cat >"${state_dir}/status-index.json" <<'JSON'
{"terraform_state":{"warning_summary":[]}}
JSON
  cat >"${state_dir}/package-count.json" <<'JSON'
{"total_packages": 3}
JSON
  cat >"${state_dir}/advisory-evidence.json" <<'JSON'
{"count": 1}
JSON
  cat >"${state_dir}/impact-count.json" <<'JSON'
{"total_findings": 2, "affected_findings": 2}
JSON
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

run_verifier() {
  ESHU_REMOTE_E2E_TEST_STATE="${state_dir}" \
    PATH="${fake_bin}:${PATH}" \
    ESHU_REMOTE_E2E_COMPOSE_FILES="docker-compose.remote-e2e.yaml" \
    "${verifier}" >/tmp/eshu-remote-e2e-finite.out 2>/tmp/eshu-remote-e2e-finite.err
}

expect_pass() {
  if ! run_verifier; then
    printf 'expected remote E2E finite verifier to pass\n' >&2
    sed -n '1,180p' /tmp/eshu-remote-e2e-finite.err >&2
    exit 1
  fi
}

expect_fail_with() {
  local pattern="$1"
  if run_verifier; then
    printf 'expected remote E2E finite verifier to fail with %s\n' "${pattern}" >&2
    sed -n '1,180p' /tmp/eshu-remote-e2e-finite.out >&2
    exit 1
  fi
  if ! rg -q "${pattern}" /tmp/eshu-remote-e2e-finite.err; then
    printf 'expected failure output to contain %s\n' "${pattern}" >&2
    sed -n '1,180p' /tmp/eshu-remote-e2e-finite.err >&2
    exit 1
  fi
}

reset_state
cat >"${state_dir}/index-status.json" <<'JSON'
{"status":"healthy","queue":{"outstanding":0,"in_flight":0,"pending":0,"retrying":0,"failed":0,"dead_letter":0},"coordinator":{"run_status_counts":[{"name":"complete","count":8}],"work_item_status_counts":[],"completeness_counts":[]}}
JSON
expect_pass
rg -q 'remote E2E finite completion state:' /tmp/eshu-remote-e2e-finite.out
rg -q 'remote E2E continuous collector polling:' /tmp/eshu-remote-e2e-finite.out

reset_state
cat >"${state_dir}/index-status.json" <<'JSON'
{"status":"progressing","queue":{"outstanding":9,"in_flight":3,"pending":6,"retrying":0,"failed":0,"dead_letter":0},"coordinator":{"run_status_counts":[{"name":"complete","count":8},{"name":"collection_active","count":2},{"name":"reducer_converging","count":1}],"work_item_status_counts":[{"name":"pending","count":6},{"name":"claimed","count":3}],"completeness_counts":[{"name":"pending","count":2}],"active_claims":3}}
JSON
expect_pass
rg -q 'remote E2E continuous collector polling: outstanding=9 in_flight=3 pending=6' /tmp/eshu-remote-e2e-finite.out

reset_state
cat >"${state_dir}/index-status.json" <<'JSON'
{"status":"stalled","queue":{"outstanding":12,"in_flight":0,"pending":12,"retrying":0,"failed":0,"dead_letter":0},"coordinator":{"run_status_counts":[{"name":"collection_pending","count":4}],"work_item_status_counts":[{"name":"pending","count":12}],"completeness_counts":[]}}
JSON
expect_fail_with 'finite completion'

reset_state
cat >"${state_dir}/index-status.json" <<'JSON'
{"status":"progressing","queue":{"outstanding":4,"in_flight":1,"pending":3,"retrying":1,"failed":0,"dead_letter":0},"coordinator":{"run_status_counts":[{"name":"complete","count":8}],"work_item_status_counts":[{"name":"failed_retryable","count":1}],"completeness_counts":[]}}
JSON
expect_fail_with 'finite completion'

printf 'verify-remote-e2e-finite-completion tests passed\n'
