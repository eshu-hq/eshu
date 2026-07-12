#!/usr/bin/env bash
set -euo pipefail

state_dir="${ESHU_REMOTE_COMPOSE_SUITE_TEST_STATE:?set ESHU_REMOTE_COMPOSE_SUITE_TEST_STATE}"

if [[ "${1:-}" == "stats" ]]; then
  cat "${state_dir}/docker-stats.jsonl"
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
    [[ "${1:-}" == "--services" ]] || { echo "unexpected config args: $*" >&2; exit 2; }
    cat "${state_dir}/services"
    ;;
  logs)
    while (($# > 0)); do
      case "${1}" in
        --no-color|--timestamps)
          shift
          ;;
        --tail)
          shift 2
          ;;
        *)
          service="${1}"
          shift
          if [[ -f "${state_dir}/logs/${service}.log" ]]; then
            cat "${state_dir}/logs/${service}.log"
          else
            printf '%s started cleanly\n' "${service}"
          fi
          ;;
      esac
    done
    ;;
  exec)
    while (($# > 0)); do
      case "${1}" in
        -T)
          shift
          ;;
        postgres)
          shift
          ;;
        *)
          break
          ;;
      esac
    done
    query="$*"
    case "${query}" in
      *"fact_records"*)
        cat "${state_dir}/fact-counts.tsv"
        ;;
      *"workflow_work_items"*)
        cat "${state_dir}/workflow-counts.tsv"
        ;;
      *"relationship_evidence_facts"*)
        cat "${state_dir}/reducer-relationship-counts.tsv"
        ;;
      *)
        echo "unexpected postgres query: ${query}" >&2
        exit 2
        ;;
    esac
    ;;
  *)
    echo "unexpected compose subcommand: ${subcommand}" >&2
    exit 2
    ;;
esac
