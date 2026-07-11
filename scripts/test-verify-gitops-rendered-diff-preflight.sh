#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-gitops-rendered-diff-preflight.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

die() {
	printf 'gitops-rendered-diff-preflight-test: %s\n' "$*" >&2
	exit 1
}

require_tool() {
	local tool="$1"
	if ! command -v "${tool}" >/dev/null 2>&1; then
		die "${tool} is required"
	fi
}

run_preflight() {
	local output="$1"
	shift
	"${verifier}" "$@" >"${output}" 2>"${output}.err"
}

expect_pass() {
	local output="$1"
	shift
	if ! run_preflight "${output}" "$@"; then
		cat "${output}.err" >&2
		die "expected preflight to pass"
	fi
}

expect_fail() {
	local output="$1"
	shift
	if run_preflight "${output}" "$@"; then
		cat "${output}" >&2
		die "expected preflight to fail"
	fi
}

require_output() {
	local output="$1" pattern="$2" description="$3"
	if ! rg -q "${pattern}" "${output}" "${output}.err"; then
		cat "${output}" >&2
		cat "${output}.err" >&2
		die "missing ${description}: ${pattern}"
	fi
}

reject_output() {
	local output="$1" pattern="$2" description="$3"
	if rg -q "${pattern}" "${output}" "${output}.err"; then
		cat "${output}" >&2
		cat "${output}.err" >&2
		die "unexpected ${description}: ${pattern}"
	fi
}

require_tool helm
require_tool rg

valid_values="${tmp_root}/valid-values.yaml"
# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
# the entire heredoc body to a pipe before forking the reader, and macOS's
# 512-byte pipe buffer deadlocks on any body over that size (#5074).
cat "${repo_root}/scripts/lib/test-verify-gitops-rendered-diff-preflight-valid-values.yaml" >"${valid_values}"

pass_output="${tmp_root}/pass.out"
expect_pass "${pass_output}" --values "${valid_values}"
require_output "${pass_output}" 'gitops rendered-diff preflight passed' "pass banner"
require_output "${pass_output}" 'Deployment/eshu-api' "API resource summary"
require_output "${pass_output}" 'Deployment/eshu-mcp-server' "MCP resource summary"
require_output "${pass_output}" 'StatefulSet/eshu' "ingester resource summary"
require_output "${pass_output}" 'Deployment/eshu-resolution-engine' "resolution-engine resource summary"
require_output "${pass_output}" 'ServiceMonitor/eshu-api' "ServiceMonitor summary"
require_output "${pass_output}" 'postgres=configured' "Postgres dependency summary"
require_output "${pass_output}" 'graph=configured' "graph dependency summary"
reject_output "${pass_output}" 'postgresql://|Bearer' "secret-bearing output"

base_output="${tmp_root}/base.out"
expect_fail "${base_output}" --overlay deploy/argocd/base
require_output "${base_output}" 'placeholder value rendered: replace-me' "placeholder failure"

latest_values="${tmp_root}/latest-values.yaml"
cat >"${latest_values}" <<'YAML'
image:
  tag: latest
YAML
latest_output="${tmp_root}/latest.out"
expect_fail "${latest_output}" --values "${valid_values}" --values "${latest_values}"
require_output "${latest_output}" 'unpinned image tag latest' "latest image failure"

exposure_values="${tmp_root}/bad-exposure.yaml"
cat >"${exposure_values}" <<'YAML'
exposure:
  ingress:
    enabled: true
  gateway:
    enabled: true
YAML
exposure_output="${tmp_root}/bad-exposure.out"
expect_fail "${exposure_output}" --values "${valid_values}" --values "${exposure_values}"
require_output "${exposure_output}" 'exposure.ingress.enabled and exposure.gateway.enabled cannot both be true' "Helm exposure failure"

nornicdb_values="${tmp_root}/bad-nornicdb.yaml"
cat >"${nornicdb_values}" <<'YAML'
nornicdb:
  enabled: true
schemaBootstrap:
  useHelmHooks: true
YAML
nornicdb_output="${tmp_root}/bad-nornicdb.out"
expect_fail "${nornicdb_output}" --values "${valid_values}" --values "${nornicdb_values}"
require_output "${nornicdb_output}" 'schemaBootstrap.useHelmHooks=true requires the graph backend to exist before Helm pre-install hooks run' "schema bootstrap ordering failure"

collector_values="${tmp_root}/bad-collector.yaml"
cat >"${collector_values}" <<'YAML'
securityAlertCollector:
  enabled: true
  instanceId: security-alert-primary
  collectorInstances:
    - collector_kind: security_alert
      instance_id: security-alert-primary
      mode: claim_driven
      enabled: true
      claims_enabled: true
YAML
collector_output="${tmp_root}/bad-collector.out"
expect_fail "${collector_output}" --values "${valid_values}" --values "${collector_values}"
require_output "${collector_output}" 'workflowCoordinator.enabled=true is required when claim-driven collectors are enabled' "coordinator consistency failure"

printf 'verify-gitops-rendered-diff-preflight tests passed\n'
