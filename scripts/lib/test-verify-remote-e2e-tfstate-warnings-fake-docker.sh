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
  cat "${state_dir}/containers/${1:?container id required}"
  exit 0
fi

if [[ "${1:-}" != "compose" ]]; then
  echo "unexpected docker command: $*" >&2
  exit 2
fi

shift
while (($# > 0)); do
  case "${1}" in
    --env-file|-f|-p|--project-name) shift 2 ;;
    *) break ;;
  esac
done

subcommand="${1:-}"
shift || true
case "${subcommand}" in
  config)
    if [[ "${1:-}" == "--services" ]]; then
      printf '%s\n' \
        eshu mcp-server ingester projector resolution-engine workflow-coordinator \
        collector-terraform-state collector-oci-registry collector-package-registry \
        collector-sbom-attestation collector-security-alerts \
        collector-vulnerability-intelligence collector-aws-cloud scanner-worker
      exit 0
    fi
    echo "unexpected compose config args: $*" >&2
    exit 2
    ;;
  ps)
    service="${@: -1}"
    if [[ -f "${state_dir}/service_ids/${service}" ]]; then
      cat "${state_dir}/service_ids/${service}"
    fi
    ;;
  port)
    printf '0.0.0.0:18080\n'
    ;;
  exec)
    printf 'test-api-key\n'
    ;;
  *)
    echo "unexpected compose subcommand: ${subcommand}" >&2
    exit 2
    ;;
esac
