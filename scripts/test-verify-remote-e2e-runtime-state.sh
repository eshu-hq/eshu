#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify_remote_e2e_runtime_state.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

fake_bin="${tmp_root}/bin"
state_dir="${tmp_root}/state"
mkdir -p "${fake_bin}" "${state_dir}"

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
  state_file="${state_dir}/containers/${container_id}"
  if [[ ! -f "${state_file}" ]]; then
    exit 1
  fi
  cat "${state_file}"
  exit 0
fi

if [[ "${1:-}" != "compose" ]]; then
  echo "unexpected docker command: $*" >&2
  exit 2
fi

shift
while (($# > 0)); do
  case "${1}" in
    --env-file|-f|-p|--project-name)
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
    ;;
  ps)
    include_all=false
    quiet=false
    service=""
    while (($# > 0)); do
      case "${1}" in
        -a|--all)
          include_all=true
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
      container_id="$(cat "${state_dir}/service_ids/${service}")"
      runtime_state="$(cut -d " " -f 1 "${state_dir}/containers/${container_id}")"
      if [[ "${include_all}" == "true" || "${runtime_state}" == "running" ]]; then
        printf '%s\n' "${container_id}"
      fi
    fi
    ;;
  port)
    service="${1:?service required}"
    port="${2:?port required}"
    if [[ "${service}:${port}" == "eshu:8080" ]]; then
      printf '0.0.0.0:18080\n'
      exit 0
    fi
    echo "unexpected compose port target: ${service}:${port}" >&2
    exit 2
    ;;
  exec)
    printf 'test-api-key\n'
    ;;
  *)
    echo "unexpected compose subcommand: ${subcommand}" >&2
    exit 2
    ;;
esac
SH
chmod +x "${fake_bin}/docker"

cat >"${fake_bin}/curl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

state_dir="${ESHU_REMOTE_E2E_TEST_STATE:?set ESHU_REMOTE_E2E_TEST_STATE}"
if [[ "$*" != *"/api/v0/index-status"* ]]; then
  echo "unexpected curl target: $*" >&2
  exit 2
fi
if [[ -f "${state_dir}/curl-fails" ]]; then
  exit 7
fi
cat "${state_dir}/index-status.json"
SH
chmod +x "${fake_bin}/curl"

reset_state() {
  rm -rf "${state_dir}/containers" "${state_dir}/service_ids"
  rm -f "${state_dir}/curl-fails"
  mkdir -p "${state_dir}/containers" "${state_dir}/service_ids"
  cat >"${state_dir}/services" <<'SERVICES'
eshu
mcp-server
ingester
resolution-engine
workflow-coordinator
collector-terraform-state
collector-oci-registry
collector-package-registry
collector-aws-cloud
SERVICES
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
  }
}
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
set_service_state collector-oci-registry running unhealthy
expect_fail_with 'collector-oci-registry.*unhealthy'

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
expect_fail_with 'queue completion'

reset_state
set_all_services_healthy
touch "${state_dir}/curl-fails"
expect_fail_with '/api/v0/index-status'

reset_state
set_all_services_healthy
expect_pass

printf 'verify-remote-e2e-runtime-state tests passed\n'
