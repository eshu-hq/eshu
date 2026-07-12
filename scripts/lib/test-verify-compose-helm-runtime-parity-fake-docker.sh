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
    printf '%s\n' 'services:
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
      ESHU_PROMETHEUS_METRICS_PORT: "9464"'
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
    printf '%s\n' 'services:
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
      ESHU_PROMETHEUS_METRICS_PORT: "9464"'
    exit 0
fi
printf 'unexpected docker args: %s\n' "${args}" >&2
exit 1
