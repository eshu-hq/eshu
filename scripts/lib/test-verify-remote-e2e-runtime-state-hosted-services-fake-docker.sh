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
