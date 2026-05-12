#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHART_DIR="${ROOT_DIR}/deploy/helm/eshu"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

default_render="${TMP_DIR}/default.yaml"
enabled_values="${TMP_DIR}/enabled-values.yaml"
enabled_render="${TMP_DIR}/enabled.yaml"
empty_targets_stderr="${TMP_DIR}/empty-targets.stderr"

helm template eshu "${CHART_DIR}" >"${default_render}"
if rg -q "eshu-oci-registry-collector" "${default_render}"; then
  echo "default render unexpectedly included OCI registry collector resources" >&2
  exit 1
fi

if helm template eshu "${CHART_DIR}" --set ociRegistryCollector.enabled=true >"${TMP_DIR}/empty-targets.yaml" 2>"${empty_targets_stderr}"; then
  echo "enabled render unexpectedly accepted an OCI registry collector without targets" >&2
  exit 1
fi
if ! rg -q "ociRegistryCollector.targets|minItems" "${empty_targets_stderr}"; then
  echo "empty-target render failed without the expected validation message" >&2
  exit 1
fi

cat >"${enabled_values}" <<'YAML'
serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/eshu-oci-registry-collector
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
ociRegistryCollector:
  enabled: true
  instanceId: oci-registry-primary
  pollInterval: 10m
  aws:
    region: us-east-1
  targets:
    - provider: ecr
      registry_id: "123456789012"
      region: us-east-1
      repository: team/api
      references:
        - latest
      tag_limit: 10
    - provider: dockerhub
      repository: library/busybox
      references:
        - latest
      tag_limit: 5
    - provider: jfrog
      base_url: https://artifacts.example.test
      repository_key: docker-local
      repository: team/app
      username_env: JFROG_USERNAME
      password_env: JFROG_PASSWORD
  extraEnv:
    - name: JFROG_USERNAME
      valueFrom:
        secretKeyRef:
          name: jfrog-oci-credentials
          key: username
    - name: JFROG_PASSWORD
      valueFrom:
        secretKeyRef:
          name: jfrog-oci-credentials
          key: password
YAML

helm template eshu "${CHART_DIR}" -f "${enabled_values}" >"${enabled_render}"

required_patterns=(
  "name: eshu-oci-registry-collector"
  "name: eshu-oci-registry-collector-metrics"
  "app.kubernetes.io/component: oci-registry-collector"
  "command: \\[\"/usr/local/bin/eshu-collector-oci-registry\"\\]"
  "name: ESHU_OCI_REGISTRY_COLLECTOR_INSTANCE_ID"
  "value: \"oci-registry-primary\""
  "name: ESHU_OCI_REGISTRY_POLL_INTERVAL"
  "value: \"10m\""
  "name: ESHU_OCI_REGISTRY_TARGETS_JSON"
  "provider.*ecr"
  "provider.*dockerhub"
  "provider.*jfrog"
  "name: AWS_REGION"
  "value: \"us-east-1\""
  "name: AWS_DEFAULT_REGION"
  "name: JFROG_USERNAME"
  "name: JFROG_PASSWORD"
  "name: jfrog-oci-credentials"
  "eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/eshu-oci-registry-collector"
  "name: ESHU_POSTGRES_DSN"
  "port: metrics"
  "kind: ServiceMonitor"
  "initialDelaySeconds: 30"
  "periodSeconds: 30"
  "initialDelaySeconds: 10"
  "periodSeconds: 15"
)

for pattern in "${required_patterns[@]}"; do
  if ! rg -q "${pattern}" "${enabled_render}"; then
    echo "enabled render missing pattern: ${pattern}" >&2
    exit 1
  fi
done

if rg -q "aws_profile" "${enabled_render}"; then
  echo "EKS render unexpectedly included an AWS shared-config profile" >&2
  exit 1
fi

if rg -q -U "app.kubernetes.io/component: oci-registry-collector\\n  annotations:\\nspec:" "${enabled_render}"; then
  echo "metrics service rendered an empty annotations map" >&2
  exit 1
fi
