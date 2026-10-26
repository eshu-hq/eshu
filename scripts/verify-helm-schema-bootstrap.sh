#!/usr/bin/env bash
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
CHART="${ROOT}/deploy/helm/eshu"
RENDERED="$(mktemp)"
RENDERED_ARGO="$(mktemp)"

cleanup() {
  rm -f "${RENDERED}" "${RENDERED_ARGO}"
}
trap cleanup EXIT

helm template eshu "${CHART}" >"${RENDERED}"
helm template eshu "${CHART}" \
  --set schemaBootstrap.useHelmHooks=false \
  --set schemaBootstrap.argocdPreSync.enabled=true >"${RENDERED_ARGO}"

require_pattern() {
  local pattern="$1"
  local description="$2"
  local file="${3:-${RENDERED}}"

  if ! rg --quiet "${pattern}" "${file}"; then
    echo "missing ${description}: ${pattern}" >&2
    exit 1
  fi
}

reject_pattern() {
  local pattern="$1"
  local description="$2"
  local file="${3:-${RENDERED}}"

  if rg --quiet "${pattern}" "${file}"; then
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
reject_pattern 'argocd.argoproj.io/hook' "ArgoCD hook in default Helm render"

# Issue #9201: the same Job doubles as an ArgoCD PreSync hook behind the
# argocdPreSync switch (default off, enabled in the QA overlay first).
require_pattern '"argocd.argoproj.io/hook": PreSync' "ArgoCD PreSync hook" "${RENDERED_ARGO}"
require_pattern '"argocd.argoproj.io/sync-wave": "-10"' "ArgoCD negative sync wave" "${RENDERED_ARGO}"
require_pattern '"argocd.argoproj.io/hook-delete-policy": "BeforeHookCreation"' "ArgoCD hook delete policy" "${RENDERED_ARGO}"
require_pattern '/usr/local/bin/eshu-bootstrap-data-plane' "schema bootstrap command" "${RENDERED_ARGO}"
reject_pattern '"helm.sh/hook"' "Helm hook in ArgoCD render" "${RENDERED_ARGO}"

# The two hook families are mutually exclusive: ArgoCD also processes
# helm.sh/hook annotations, so both on would double-register the Job.
if helm template eshu "${CHART}" \
  --set schemaBootstrap.argocdPreSync.enabled=true >/dev/null 2>&1; then
  echo "expected helm template to fail with helm and ArgoCD hooks both enabled" >&2
  exit 1
fi
