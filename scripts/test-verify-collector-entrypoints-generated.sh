#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_root="$(mktemp -d)"
trap 'rm -rf "$tmp_root"' EXIT

manifest="${tmp_root}/collector_entrypoints.yaml"
cat >"$manifest" <<'YAML'
schema_version: 1
collectors:
  - command_dir: go/cmd/collector-demo
    runtime_name: collector-demo
    binary_name: eshu-collector-demo
    collector_label: demo collector
    go_name: Demo
    env:
      collector_instances: ESHU_COLLECTOR_INSTANCES_JSON
      instance_id: ESHU_DEMO_COLLECTOR_INSTANCE_ID
      poll_interval: ESHU_DEMO_POLL_INTERVAL
      claim_lease_ttl: ESHU_DEMO_CLAIM_LEASE_TTL
      heartbeat_interval: ESHU_DEMO_HEARTBEAT_INTERVAL
      owner_id: ESHU_DEMO_COLLECTOR_OWNER_ID
      owner_id_const_name: envCollectorOwnerID
    store_name: collector_demo
    claim_id_prefix: demo-claim
    collector_kind_expr: scope.CollectorKind("demo")
    scope_kind: demo
    auth_mode: token_env
    target_list_field: targets
    target_identity_fields: [scope_id]
    target_auth_fields: [token_env]
    source:
      import_path: github.com/eshu-hq/eshu/go/internal/collector/demo
      package_name: demo
      config_type: demo.SourceConfig
      constructor: demo.NewClaimedSource
      config_loader: loadDemoSourceConfig
      config_attacher: attachDemoRuntimeSignals
      runtime_config_type: demoRuntimeConfiguration
YAML

ESHU_COLLECTOR_ENTRYPOINTS_REPO_ROOT="$tmp_root" \
  ESHU_COLLECTOR_ENTRYPOINTS_GO_DIR="${repo_root}/go" \
  ESHU_COLLECTOR_ENTRYPOINTS_MANIFEST="$manifest" \
  "${repo_root}/scripts/generate-collector-entrypoints.sh" >/tmp/eshu-entrypoints-generate.out

ESHU_COLLECTOR_ENTRYPOINTS_REPO_ROOT="$tmp_root" \
  ESHU_COLLECTOR_ENTRYPOINTS_GO_DIR="${repo_root}/go" \
  ESHU_COLLECTOR_ENTRYPOINTS_MANIFEST="$manifest" \
  "${repo_root}/scripts/verify-collector-entrypoints-generated.sh" >/tmp/eshu-entrypoints-verify.out

printf 'package main\n' >"${tmp_root}/go/cmd/collector-demo/main.go"
if ESHU_COLLECTOR_ENTRYPOINTS_REPO_ROOT="$tmp_root" \
  ESHU_COLLECTOR_ENTRYPOINTS_GO_DIR="${repo_root}/go" \
  ESHU_COLLECTOR_ENTRYPOINTS_MANIFEST="$manifest" \
  "${repo_root}/scripts/verify-collector-entrypoints-generated.sh" >/tmp/eshu-entrypoints-stale.out 2>/tmp/eshu-entrypoints-stale.err; then
  printf 'expected stale generated collector entrypoint check to fail\n' >&2
  exit 1
fi

if ! rg -q 'generated file .* is stale' /tmp/eshu-entrypoints-stale.err; then
  printf 'expected stale generated file error, got:\n' >&2
  sed -n '1,120p' /tmp/eshu-entrypoints-stale.err >&2
  exit 1
fi

printf 'verify-collector-entrypoints-generated tests passed\n'
