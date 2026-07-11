#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify_remote_e2e_runtime_state.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

fake_bin="${tmp_root}/bin"
state_dir="${tmp_root}/state"
mkdir -p "${fake_bin}" "${state_dir}/containers" "${state_dir}/service_ids"

# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
# the entire heredoc body to a pipe before forking the reader, and macOS's
# 512-byte pipe buffer deadlocks on any body over that size (#5074).
cat "${repo_root}/scripts/lib/test-verify-remote-e2e-runtime-state-hosted-services-fake-docker.sh" >"${fake_bin}/docker"
chmod +x "${fake_bin}/docker"

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

set_service_state() {
  local service="$1"
  local runtime_state="$2"
  local health_state="$3"
  local container_id="${service}-container"
  printf '%s\n' "${container_id}" >"${state_dir}/service_ids/${service}"
  printf '%s %s\n' "${runtime_state}" "${health_state}" >"${state_dir}/containers/${container_id}"
}

while IFS= read -r service; do
  [[ -n "${service}" ]] || continue
  set_service_state "${service}" running healthy
done <"${state_dir}/services"
set_service_state collector-grafana created none

if ESHU_REMOTE_E2E_TEST_STATE="${state_dir}" \
    PATH="${fake_bin}:${PATH}" \
    ESHU_REMOTE_E2E_COMPOSE_FILES="docker-compose.remote-e2e.yaml" \
    ESHU_REMOTE_E2E_COMPOSE_PROFILES="grafana" \
    "${verifier}" >/tmp/eshu-remote-e2e-hosted-services.out 2>/tmp/eshu-remote-e2e-hosted-services.err; then
  printf 'expected rendered hosted collector service to be required by runtime verifier\n' >&2
  sed -n '1,160p' /tmp/eshu-remote-e2e-hosted-services.out >&2
  exit 1
fi

if ! rg -q 'collector-grafana.*created' /tmp/eshu-remote-e2e-hosted-services.err; then
  printf 'expected failure output to identify unhealthy rendered hosted collector\n' >&2
  sed -n '1,160p' /tmp/eshu-remote-e2e-hosted-services.err >&2
  exit 1
fi

printf 'verify-remote-e2e-runtime-state hosted service tests passed\n'
