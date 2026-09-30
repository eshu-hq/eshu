#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
chart="${repo_root}/deploy/helm/eshu"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

expect_fail() {
  local label="$1" values="$2" message="$3"
  if helm template eshu "${chart}" -f "${values}" >"${tmp}/${label}.rendered.yaml" 2>"${tmp}/${label}.err"; then
    printf '%s unexpectedly rendered\n' "${label}" >&2
    exit 1
  fi
  if ! rg -q "${message}" "${tmp}/${label}.err"; then
    printf '%s failed for the wrong reason:\n' "${label}" >&2
    sed -n '1,100p' "${tmp}/${label}.err" >&2
    exit 1
  fi
}

helm template eshu "${chart}" >"${tmp}/default.yaml"
if rg -q 'name: ESHU_POSTGRES_READ_DSN' "${tmp}/default.yaml"; then
  printf 'default render injected a reader DSN\n' >&2
  exit 1
fi

cat >"${tmp}/reader.yaml" <<'YAML'
contentStore:
  secretName: writer-connection
  dsnKey: writer-dsn
api:
  extraEnv:
    - name: ESHU_POSTGRES_READ_DSN
      valueFrom:
        secretKeyRef:
          name: api-reader-connection
          key: reader-dsn
mcpServer:
  extraEnv:
    - name: ESHU_POSTGRES_READ_DSN
      valueFrom:
        secretKeyRef:
          name: mcp-reader-connection
          key: reader-dsn
          optional: false
YAML
helm template eshu "${chart}" -f "${tmp}/reader.yaml" >"${tmp}/reader.yaml.rendered"
for service in api mcp-server; do
  template="templates/deployment.yaml"
  if [[ "${service}" == "mcp-server" ]]; then
    template="templates/deployment-mcp-server.yaml"
  fi
  helm template eshu "${chart}" -f "${tmp}/reader.yaml" --show-only "${template}" >"${tmp}/${service}.yaml"
done
for pair in 'api:api-reader-connection' 'mcp-server:mcp-reader-connection'; do
  service="${pair%%:*}"
  secret="${pair#*:}"
  if ! rg -U -q "name: ESHU_POSTGRES_READ_DSN\n[[:space:]]+valueFrom:\n[[:space:]]+secretKeyRef:\n[[:space:]]+key: reader-dsn\n[[:space:]]+name: ${secret}" "${tmp}/${service}.yaml"; then
    printf '%s reader Secret reference %s missing from render\n' "${service}" "${secret}" >&2
    exit 1
  fi
  if ! rg -U -q 'name: ESHU_POSTGRES_DSN\n[[:space:]]+valueFrom:\n[[:space:]]+secretKeyRef:\n[[:space:]]+name: writer-connection\n[[:space:]]+key: writer-dsn' "${tmp}/${service}.yaml"; then
    printf '%s writer Secret reference changed\n' "${service}" >&2
    exit 1
  fi
done
if [[ "$(rg -c 'name: ESHU_POSTGRES_READ_DSN' "${tmp}/reader.yaml.rendered")" != 2 ]]; then
  printf 'reader DSN injected outside the two serving Deployments\n' >&2
  exit 1
fi

# Ordinary EnvVar sources remain supported; Secret references are recommended
# for credential-bearing DSNs rather than enforced as the only source.
cat >"${tmp}/plain-reader.yaml" <<'YAML'
api:
  extraEnv:
    - name: ESHU_POSTGRES_READ_DSN
      value: host=reader dbname=eshu sslmode=verify-full
YAML
helm template eshu "${chart}" -f "${tmp}/plain-reader.yaml" --show-only templates/deployment.yaml >"${tmp}/plain-reader.rendered.yaml"
rg -U -q 'name: ESHU_POSTGRES_READ_DSN\n[[:space:]]+value: host=reader dbname=eshu sslmode=verify-full' "${tmp}/plain-reader.rendered.yaml"
cat >"${tmp}/configmap-reader.yaml" <<'YAML'
mcpServer:
  extraEnv:
    - name: ESHU_POSTGRES_READ_DSN
      valueFrom:
        configMapKeyRef:
          name: reader-address
          key: dsn
YAML
helm template eshu "${chart}" -f "${tmp}/configmap-reader.yaml" --show-only templates/deployment-mcp-server.yaml >"${tmp}/configmap-reader.rendered.yaml"
rg -U -q 'name: ESHU_POSTGRES_READ_DSN\n[[:space:]]+valueFrom:\n[[:space:]]+configMapKeyRef:\n[[:space:]]+key: dsn\n[[:space:]]+name: reader-address' "${tmp}/configmap-reader.rendered.yaml"

cat >"${tmp}/global-conflict.yaml" <<'YAML'
env:
  ESHU_POSTGRES_READ_DSN: shared-reader
api:
  extraEnv:
    - name: ESHU_POSTGRES_READ_DSN
      valueFrom:
        secretKeyRef:
          name: api-reader-connection
          key: reader-dsn
YAML
expect_fail global-conflict "${tmp}/global-conflict.yaml" 'api.extraEnv ESHU_POSTGRES_READ_DSN conflicts with env'

cat >"${tmp}/service-conflict.yaml" <<'YAML'
mcpServer:
  env:
    ESHU_POSTGRES_READ_DSN: service-reader
  extraEnv:
    - name: ESHU_POSTGRES_READ_DSN
      valueFrom:
        secretKeyRef:
          name: mcp-reader-connection
          key: reader-dsn
YAML
expect_fail service-conflict "${tmp}/service-conflict.yaml" 'mcpServer.extraEnv ESHU_POSTGRES_READ_DSN conflicts with env'

cat >"${tmp}/duplicate.yaml" <<'YAML'
api:
  extraEnv:
    - name: ESHU_POSTGRES_READ_DSN
      valueFrom:
        secretKeyRef:
          name: reader-one
          key: dsn
    - name: ESHU_POSTGRES_READ_DSN
      valueFrom:
        secretKeyRef:
          name: reader-two
          key: dsn
YAML
expect_fail duplicate "${tmp}/duplicate.yaml" 'api.extraEnv repeats ESHU_POSTGRES_READ_DSN'

cat >"${tmp}/optional.yaml" <<'YAML'
mcpServer:
  extraEnv:
    - name: ESHU_POSTGRES_READ_DSN
      valueFrom:
        secretKeyRef:
          name: reader-connection
          key: dsn
          optional: true
YAML
expect_fail optional "${tmp}/optional.yaml" 'mcpServer.extraEnv ESHU_POSTGRES_READ_DSN secretKeyRef.optional=true is not allowed'

cat >"${tmp}/wrong-type.yaml" <<'YAML'
api:
  extraEnv:
    - not-an-env-object
YAML
expect_fail wrong-type "${tmp}/wrong-type.yaml" "at '/api/extraEnv/0': got string, want object"

helm lint "${chart}" -f "${tmp}/reader.yaml" >"${tmp}/lint.out"
printf 'Helm Postgres read env verification passed\n'
