#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
preflight="${repo_root}/scripts/remote-e2e-observability-preflight.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

run_preflight() {
  "$@" /bin/sh "${preflight}" >"${tmp_root}/preflight.out" 2>"${tmp_root}/preflight.err"
}

expect_pass() {
  if ! run_preflight "$@"; then
    printf 'expected observability preflight to pass\n' >&2
    sed -n '1,120p' "${tmp_root}/preflight.err" >&2
    exit 1
  fi
}

expect_fail_with() {
  local pattern="$1"
  shift
  if run_preflight "$@"; then
    printf 'expected observability preflight to fail with %s\n' "${pattern}" >&2
    sed -n '1,120p' "${tmp_root}/preflight.out" >&2
    exit 1
  fi
  if ! rg -q "${pattern}" "${tmp_root}/preflight.err"; then
    printf 'expected failure output to contain %s\n' "${pattern}" >&2
    sed -n '1,120p' "${tmp_root}/preflight.err" >&2
    exit 1
  fi
}

assert_output_omits() {
  local pattern="$1"
  if rg -q "${pattern}" "${tmp_root}/preflight.out" "${tmp_root}/preflight.err"; then
    printf 'preflight output leaked %s\n' "${pattern}" >&2
    sed -n '1,120p' "${tmp_root}/preflight.out" >&2
    sed -n '1,120p' "${tmp_root}/preflight.err" >&2
    exit 1
  fi
}

grafana_base=(
  env
  ESHU_OBSERVABILITY_COLLECTOR=grafana
  ESHU_OBSERVABILITY_ENABLE_ENV=ESHU_REMOTE_E2E_GRAFANA_ENABLED
  ESHU_OBSERVABILITY_BASE_URL=https://grafana.example.invalid
  ESHU_OBSERVABILITY_BASE_URL_ENV=ESHU_GRAFANA_BASE_URL
  ESHU_OBSERVABILITY_TOKEN_ENV=GRAFANA_TOKEN
)

expect_fail_with \
  'ESHU_REMOTE_E2E_GRAFANA_ENABLED must be true when the grafana profile is selected' \
  "${grafana_base[@]}" ESHU_OBSERVABILITY_ENABLED=false GRAFANA_TOKEN=redacted-token

expect_fail_with \
  'ESHU_GRAFANA_BASE_URL is required when the grafana profile is selected' \
  env \
  ESHU_OBSERVABILITY_COLLECTOR=grafana \
  ESHU_OBSERVABILITY_ENABLE_ENV=ESHU_REMOTE_E2E_GRAFANA_ENABLED \
  ESHU_OBSERVABILITY_ENABLED=true \
  ESHU_OBSERVABILITY_BASE_URL_ENV=ESHU_GRAFANA_BASE_URL \
  ESHU_OBSERVABILITY_TOKEN_ENV=GRAFANA_TOKEN \
  GRAFANA_TOKEN=redacted-token

expect_fail_with \
  'GRAFANA_TOKEN is required when ESHU_OBSERVABILITY_TOKEN_ENV names it' \
  "${grafana_base[@]}" ESHU_OBSERVABILITY_ENABLED=true

expect_pass "${grafana_base[@]}" ESHU_OBSERVABILITY_ENABLED=true GRAFANA_TOKEN=redacted-token
if ! rg -q 'collector=grafana enabled=true target=configured token=required tenant=optional' "${tmp_root}/preflight.out"; then
  printf 'expected grafana proof output\n' >&2
  sed -n '1,120p' "${tmp_root}/preflight.out" >&2
  exit 1
fi
assert_output_omits 'grafana\.example\.invalid|redacted-token'

expect_pass \
  env \
  ESHU_OBSERVABILITY_COLLECTOR=prometheus_mimir \
  ESHU_OBSERVABILITY_ENABLE_ENV=ESHU_REMOTE_E2E_PROMETHEUS_MIMIR_ENABLED \
  ESHU_OBSERVABILITY_ENABLED=true \
  ESHU_OBSERVABILITY_BASE_URL=https://prometheus.example.invalid \
  ESHU_OBSERVABILITY_BASE_URL_ENV=ESHU_PROMETHEUS_MIMIR_BASE_URL
if ! rg -q 'collector=prometheus_mimir enabled=true target=configured token=optional tenant=optional' "${tmp_root}/preflight.out"; then
  printf 'expected prometheus/mimir proof output\n' >&2
  sed -n '1,120p' "${tmp_root}/preflight.out" >&2
  exit 1
fi

expect_pass \
  env \
  ESHU_OBSERVABILITY_COLLECTOR=loki \
  ESHU_OBSERVABILITY_ENABLE_ENV=ESHU_REMOTE_E2E_LOKI_ENABLED \
  ESHU_OBSERVABILITY_ENABLED=true \
  ESHU_OBSERVABILITY_BASE_URL=https://loki.example.invalid \
  ESHU_OBSERVABILITY_BASE_URL_ENV=ESHU_LOKI_BASE_URL \
  ESHU_OBSERVABILITY_TOKEN_ENV=LOKI_TOKEN \
  ESHU_OBSERVABILITY_TENANT_ID_ENV=LOKI_TENANT_ID \
  LOKI_TOKEN=redacted-token \
  LOKI_TENANT_ID=tenant-a
if ! rg -q 'collector=loki enabled=true target=configured token=required tenant=required' "${tmp_root}/preflight.out"; then
  printf 'expected loki proof output\n' >&2
  sed -n '1,120p' "${tmp_root}/preflight.out" >&2
  exit 1
fi

printf 'remote-e2e-observability-preflight tests passed\n'
