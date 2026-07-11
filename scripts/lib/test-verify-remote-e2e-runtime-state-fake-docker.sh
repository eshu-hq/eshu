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
