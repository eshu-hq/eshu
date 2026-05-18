#!/usr/bin/env bash
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
CHART="${ROOT}/deploy/helm/eshu"
RENDERED="$(mktemp)"

cleanup() {
  rm -f "${RENDERED}"
}
trap cleanup EXIT

helm template eshu "${CHART}" >"${RENDERED}"

require_pattern() {
  local pattern="$1"
  local description="$2"

  if ! rg --quiet "${pattern}" "${RENDERED}"; then
    echo "missing ${description}: ${pattern}" >&2
    exit 1
  fi
}

reject_pattern() {
  local pattern="$1"
  local description="$2"

  if rg --quiet "${pattern}" "${RENDERED}"; then
    echo "unexpected ${description}: ${pattern}" >&2
    exit 1
  fi
}

require_pattern '^kind: Job$' "schema bootstrap Job"
require_pattern 'name: eshu-schema-bootstrap$' "schema bootstrap Job name"
require_pattern '^kind: NetworkPolicy$' "schema bootstrap NetworkPolicy"
require_pattern 'app.kubernetes.io/component: schema-bootstrap' "schema bootstrap component label"
require_pattern '"helm.sh/hook": pre-install,pre-upgrade' "Helm pre-install/pre-upgrade hook"
require_pattern '"helm.sh/hook-delete-policy": before-hook-creation,hook-succeeded' "Helm hook cleanup policy"
require_pattern '/usr/local/bin/eshu-bootstrap-data-plane' "schema bootstrap command"
reject_pattern 'name: db-migrate' "legacy per-workload schema init container"
