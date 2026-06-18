#!/usr/bin/env bash
# Focused tests for the Compose-to-Helm runtime parity verifier.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
gate="${repo_root}/scripts/verify-compose-helm-runtime-parity.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

install_fake_tools() {
    local dir="$1"
    mkdir -p "${dir}/_bin"

    cat >"${dir}/_bin/docker" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
args="$*"
if [[ "${args}" == *"docker-compose.remote-e2e.observability.yaml"* && "${args}" == *"config --services"* ]]; then
    cat <<'EOF'
collector-grafana
collector-prometheus-mimir
collector-loki
collector-tempo
EOF
    exit 0
fi
if [[ "${args}" == *"docker-compose.remote-e2e.yaml"* && "${args}" == *"config --services"* ]]; then
    cat <<'EOF'
eshu
mcp-server
ingester
projector
resolution-engine
workflow-coordinator
webhook-listener
collector-terraform-state
collector-oci-registry
collector-package-registry
collector-sbom-attestation
collector-security-alerts
collector-vulnerability-intelligence
collector-aws-cloud
scanner-worker
EOF
    if [ "${ESHU_FAKE_PARITY_MISSING_REMOTE:-0}" = "1" ]; then
        exit 0
    fi
    printf 'collector-confluence\ncollector-jira\ncollector-pagerduty\n'
    exit 0
fi
if [[ "${args}" == *"docker-compose.remote-e2e.yaml config"* ]]; then
    cat <<'EOF'
services:
  eshu:
    healthcheck:
      test: ["CMD", "curl", "-fsS", "http://localhost:8080/healthz"]
    environment:
      ESHU_GRAPH_BACKEND: nornicdb
      ESHU_POSTGRES_DSN: postgresql://eshu:change-me@postgres:5432/eshu
      NEO4J_URI: bolt://nornicdb:7687
      ESHU_PROMETHEUS_METRICS_PORT: "9464"
  mcp-server:
    healthcheck:
      test: ["CMD", "curl", "-fsS", "http://localhost:8080/healthz"]
    environment:
      ESHU_GRAPH_BACKEND: nornicdb
      ESHU_POSTGRES_DSN: postgresql://eshu:change-me@postgres:5432/eshu
      NEO4J_URI: bolt://nornicdb:7687
      ESHU_PROMETHEUS_METRICS_PORT: "9464"
EOF
    exit 0
fi
if [[ "${args}" == *"docker-compose.yaml config --services"* ]]; then
    cat <<'EOF'
nornicdb
postgres
db-migrate
workspace-setup
bootstrap-index
eshu
mcp-server
ingester
resolution-engine
EOF
    exit 0
fi
if [[ "${args}" == *"docker-compose.yaml config"* ]]; then
    cat <<'EOF'
services:
  eshu:
    healthcheck:
      test: ["CMD", "curl", "-fsS", "http://localhost:8080/health"]
    environment:
      ESHU_GRAPH_BACKEND: nornicdb
      ESHU_POSTGRES_DSN: postgresql://eshu:change-me@postgres:5432/eshu
      NEO4J_URI: bolt://nornicdb:7687
      ESHU_PROMETHEUS_METRICS_PORT: "9464"
  mcp-server:
    healthcheck:
      test: ["CMD", "curl", "-fsS", "http://localhost:8080/healthz"]
    environment:
      ESHU_GRAPH_BACKEND: nornicdb
      ESHU_POSTGRES_DSN: postgresql://eshu:change-me@postgres:5432/eshu
      NEO4J_URI: bolt://nornicdb:7687
      ESHU_PROMETHEUS_METRICS_PORT: "9464"
EOF
    exit 0
fi
printf 'unexpected docker args: %s\n' "${args}" >&2
exit 1
SH
    chmod +x "${dir}/_bin/docker"

    cat >"${dir}/_bin/helm" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
cat <<'YAML'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: eshu-api
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: eshu-mcp-server
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: eshu
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: eshu-resolution-engine
---
apiVersion: batch/v1
kind: Job
metadata:
  name: eshu-schema-bootstrap
---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: eshu-api-metrics
  labels:
    app.kubernetes.io/component: api
---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: eshu-mcp-server-metrics
  labels:
    app.kubernetes.io/component: mcp-server
---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: eshu-ingester-metrics
  labels:
    app.kubernetes.io/component: ingester
---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: eshu-resolution-engine-metrics
  labels:
    app.kubernetes.io/component: resolution-engine
---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: eshu-gcp-cloud-collector-metrics
  labels:
    app.kubernetes.io/component: gcp-cloud-collector
YAML
SH
    chmod +x "${dir}/_bin/helm"
}

repo="${tmp_root}/repo"
mkdir -p "${repo}"
cp -R "${repo_root}/deploy" "${repo}/deploy"
cp "${repo_root}/docker-compose.yaml" "${repo}/docker-compose.yaml"
cp "${repo_root}/docker-compose.remote-e2e.yaml" "${repo}/docker-compose.remote-e2e.yaml"
cp "${repo_root}/docker-compose.remote-e2e.observability.yaml" "${repo}/docker-compose.remote-e2e.observability.yaml"
cp "${repo_root}/.env.remote-e2e.example" "${repo}/.env.remote-e2e.example"
install_fake_tools "${repo}"

if ! PATH="${repo}/_bin:${PATH}" "${gate}" --repo-root "${repo}" >"${repo}/pass.out" 2>"${repo}/pass.err"; then
    sed -n '1,160p' "${repo}/pass.err" >&2
    exit 1
fi
rg --quiet 'runtime parity verification passed' "${repo}/pass.out" \
    || { printf 'expected parity verifier to pass complete fake surfaces\n' >&2; exit 1; }
rg --quiet 'core ServiceMonitor coverage: pass' "${repo}/pass.out" \
    || { printf 'expected core ServiceMonitor coverage evidence\n' >&2; exit 1; }

component_service_monitor="${repo}/deploy/helm/eshu/templates/servicemonitor-component-extension-collector.yaml"
component_service_monitor_backup="${repo}/servicemonitor-component-extension-collector.yaml.bak"
mv "${component_service_monitor}" "${component_service_monitor_backup}"
if PATH="${repo}/_bin:${PATH}" "${gate}" --repo-root "${repo}" >"${repo}/missing-sm.out" 2>"${repo}/missing-sm.err"; then
    printf 'expected verifier to fail when component extension ServiceMonitor coverage is missing\n' >&2
    exit 1
fi
rg --quiet 'missing collector ServiceMonitor template coverage: app.kubernetes.io/component: component-extension-collector' "${repo}/missing-sm.err" \
    || { printf 'missing component extension ServiceMonitor coverage was not reported\n' >&2; exit 1; }
mv "${component_service_monitor_backup}" "${component_service_monitor}"

if ESHU_FAKE_PARITY_MISSING_REMOTE=1 PATH="${repo}/_bin:${PATH}" "${gate}" --repo-root "${repo}" >"${repo}/fail.out" 2>"${repo}/fail.err"; then
    printf 'expected verifier to fail when a required remote collector is missing\n' >&2
    exit 1
fi
rg --quiet 'missing profile-expanded remote Compose service: collector-jira' "${repo}/fail.err" \
    || { printf 'missing remote collector failure was not reported\n' >&2; exit 1; }

printf 'Compose-to-Helm runtime parity tests passed\n'
