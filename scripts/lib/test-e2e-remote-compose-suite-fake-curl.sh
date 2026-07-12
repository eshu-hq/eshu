#!/usr/bin/env bash
set -euo pipefail

state_dir="${ESHU_REMOTE_COMPOSE_SUITE_TEST_STATE:?set ESHU_REMOTE_COMPOSE_SUITE_TEST_STATE}"
printf '%s\n' "$*" >>"${state_dir}/curl-targets"
if [[ "$*" == *"test-api-key"* ]]; then
  echo "curl arguments leaked API key" >&2
  exit 2
fi
if [[ "$*" != *"--max-time"* && "$*" != *"-m"* ]]; then
  echo "curl call is missing timeout" >&2
  exit 2
fi
case "$*" in
  *"/debug/pprof/"*)
    printf 'pprof index\n'
    ;;
  *"/api/v0/index-status"*)
    cat "${state_dir}/index-status.json"
    ;;
  *)
    echo "unexpected curl target: $*" >&2
    exit 2
    ;;
esac
