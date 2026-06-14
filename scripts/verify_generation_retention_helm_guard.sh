#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHART_DIR="${ROOT_DIR}/deploy/helm/eshu"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

write_values() {
  local path="$1"
  shift
  printf '%s\n' "$@" >"${path}"
}

expect_disabled_retention_failure() {
  local label="$1"
  local values="$2"
  local stderr="${TMP_DIR}/${label}.stderr"

  if helm template eshu "${CHART_DIR}" -f "${values}" >"${TMP_DIR}/${label}.rendered.yaml" 2>"${stderr}"; then
    echo "${label} render unexpectedly accepted disabled generation retention" >&2
    exit 1
  fi

  if ! rg -q "ESHU_GENERATION_RETENTION_ENABLED=false is not allowed" "${stderr}"; then
    echo "${label} render failed without the expected generation retention message" >&2
    sed -n '1,160p' "${stderr}" >&2
    exit 1
  fi
}

global_disable_values="${TMP_DIR}/global-disable.yaml"
write_values "${global_disable_values}" \
  'env:' \
  '  ESHU_GENERATION_RETENTION_ENABLED: "false"'
expect_disabled_retention_failure "global-disable" "${global_disable_values}"

resolution_engine_disable_values="${TMP_DIR}/resolution-engine-disable.yaml"
write_values "${resolution_engine_disable_values}" \
  'resolutionEngine:' \
  '  env:' \
  '    ESHU_GENERATION_RETENTION_ENABLED: "False"'
expect_disabled_retention_failure "resolution-engine-disable" "${resolution_engine_disable_values}"

lane_disable_values="${TMP_DIR}/lane-disable.yaml"
write_values "${lane_disable_values}" \
  'resolutionEngine:' \
  '  lanes:' \
  '    - name: critical' \
  '      domains:' \
  '        - deployment_mapping' \
  '      env:' \
  '        ESHU_GENERATION_RETENTION_ENABLED: " FALSE "'
expect_disabled_retention_failure "lane-disable" "${lane_disable_values}"

enabled_values="${TMP_DIR}/enabled.yaml"
write_values "${enabled_values}" \
  'resolutionEngine:' \
  '  env:' \
  '    ESHU_GENERATION_RETENTION_ENABLED: "true"'
helm template eshu "${CHART_DIR}" -f "${enabled_values}" >"${TMP_DIR}/enabled-render.yaml"

if ! rg -q "ESHU_GENERATION_RETENTION_ENABLED" "${TMP_DIR}/enabled-render.yaml"; then
  echo "enabled render did not include the explicit generation retention env override" >&2
  exit 1
fi

printf 'generation retention Helm guard verification passed\n'
