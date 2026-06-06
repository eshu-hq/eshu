#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify_remote_e2e_runtime_state.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

fake_bin="${tmp_root}/bin"
state_dir="${tmp_root}/state"
mkdir -p "${fake_bin}" "${state_dir}/containers" "${state_dir}/service_ids"

cat >"${fake_bin}/docker" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

state_dir="${ESHU_REMOTE_E2E_TEST_STATE:?set ESHU_REMOTE_E2E_TEST_STATE}"

if [[ "${1:-}" == "inspect" ]]; then
  shift
  while [[ "${1:-}" == -* ]]; do
    shift
    if [[ "${1:-}" == "{{"* ]]; then
      shift
    fi
  done
  container_id="${1:?container id required}"
  cat "${state_dir}/containers/${container_id}"
  exit 0
fi

if [[ "${1:-}" != "compose" ]]; then
  echo "unexpected docker command: $*" >&2
  exit 2
fi

shift
compose_profiles=""
while (($# > 0)); do
  case "${1}" in
    --env-file|-f|-p|--project-name)
      shift 2
      ;;
    --profile)
      compose_profiles="${compose_profiles} ${2:?profile required}"
      shift 2
      ;;
    *)
      break
      ;;
  esac
done

subcommand="${1:-}"
shift || true
case "${subcommand}" in
  config)
    if [[ "${1:-}" != "--services" ]]; then
      echo "unexpected compose config args: $*" >&2
      exit 2
    fi
    cat "${state_dir}/services"
    for profile in ${compose_profiles}; do
      if [[ "${profile}" == "grafana" ]]; then
        printf 'collector-grafana\n'
      fi
    done
    ;;
  ps)
    quiet=false
    service=""
    while (($# > 0)); do
      case "${1}" in
        -a|--all)
          shift
          ;;
        -q|--quiet)
          quiet=true
          shift
          ;;
        *)
          service="${1}"
          shift
          ;;
      esac
    done
    if [[ "${quiet}" != "true" || -z "${service}" ]]; then
      echo "unexpected compose ps args" >&2
      exit 2
    fi
    if [[ -f "${state_dir}/service_ids/${service}" ]]; then
      cat "${state_dir}/service_ids/${service}"
    fi
    ;;
  *)
    echo "unexpected compose subcommand: ${subcommand}" >&2
    exit 2
    ;;
esac
SH
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
