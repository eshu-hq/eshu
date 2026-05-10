#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHART_DIR="${ROOT_DIR}/deploy/helm/eshu"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

default_render="${TMP_DIR}/default.yaml"
enabled_values="${TMP_DIR}/enabled-values.yaml"
enabled_render="${TMP_DIR}/enabled.yaml"

helm template eshu "${CHART_DIR}" >"${default_render}"
if rg -q "eshu-confluence-collector" "${default_render}"; then
  echo "default render unexpectedly included Confluence collector resources" >&2
  exit 1
fi

cat >"${enabled_values}" <<'YAML'
contentStore:
  dsn: postgresql://eshu:secret@postgres:5432/eshu
neo4j:
  auth:
    secretName: ""
observability:
  prometheus:
    enabled: true
    serviceMonitor:
      enabled: true
confluenceCollector:
  enabled: true
  baseUrl: https://example.atlassian.net/wiki
  spaceId: "196609"
  spaceKey: DEV
  pageLimit: 50
  pollInterval: 30m
  credentials:
    secretName: confluence-collector-credentials
    emailKey: email
    apiTokenKey: api-token
YAML

helm template eshu "${CHART_DIR}" -f "${enabled_values}" >"${enabled_render}"

required_patterns=(
  "name: eshu-confluence-collector"
  "name: eshu-confluence-collector-metrics"
  "app.kubernetes.io/component: confluence-collector"
  "command: \\[\"/usr/local/bin/eshu-collector-confluence\"\\]"
  "name: ESHU_CONFLUENCE_BASE_URL"
  "value: \"https://example.atlassian.net/wiki\""
  "name: ESHU_CONFLUENCE_SPACE_ID"
  "value: \"196609\""
  "name: ESHU_CONFLUENCE_SPACE_KEY"
  "value: \"DEV\""
  "name: ESHU_CONFLUENCE_PAGE_LIMIT"
  "value: \"50\""
  "name: ESHU_CONFLUENCE_POLL_INTERVAL"
  "value: \"30m\""
  "name: ESHU_CONFLUENCE_EMAIL"
  "name: ESHU_CONFLUENCE_API_TOKEN"
  "name: confluence-collector-credentials"
  "key: email"
  "key: api-token"
  "name: ESHU_POSTGRES_DSN"
  "port: metrics"
  "kind: ServiceMonitor"
)

for pattern in "${required_patterns[@]}"; do
  if ! rg -q "${pattern}" "${enabled_render}"; then
    echo "enabled render missing pattern: ${pattern}" >&2
    exit 1
  fi
done

if rg -q "ESHU_CONFLUENCE_ROOT_PAGE_ID" "${enabled_render}"; then
  echo "space-scoped render unexpectedly included root page ID" >&2
  exit 1
fi

