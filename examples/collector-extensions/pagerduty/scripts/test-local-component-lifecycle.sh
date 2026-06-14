#!/usr/bin/env bash
set -euo pipefail

repo_root="${ESHU_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(git rev-parse --show-toplevel)"
fi

package_dir="$repo_root/examples/collector-extensions/pagerduty"
component_home="$(mktemp -d)"
output_dir="$(mktemp -d)"
cleanup() {
  rm -rf "$component_home" "$output_dir"
}
trap cleanup EXIT

run_eshu() {
  go -C "$repo_root/go" run ./cmd/eshu "$@"
}

run_eshu component inspect "$package_dir/manifest.yaml" --json >"$output_dir/inspect.json"
rg -q '"status": "inspected"' "$output_dir/inspect.json"
rg -q '"id": "dev.eshu.examples.pagerduty"' "$output_dir/inspect.json"

run_eshu component verify "$package_dir/manifest.yaml" \
  --trust-mode allowlist \
  --allow-id dev.eshu.examples.pagerduty \
  --allow-publisher eshu-hq \
  --json >"$output_dir/verify.json"
rg -q '"status": "verified"' "$output_dir/verify.json"

run_eshu component conform "$package_dir/manifest.yaml" \
  --fixture "$package_dir/testdata/fixtures/complete-result.json" \
  --mode fixture \
  --json >"$output_dir/conform.json"
rg -q '"status": "passed"' "$output_dir/conform.json"

run_eshu component install "$package_dir/manifest.yaml" \
  --component-home "$component_home" \
  --trust-mode allowlist \
  --allow-id dev.eshu.examples.pagerduty \
  --allow-publisher eshu-hq \
  --json >"$output_dir/install.json"
rg -q '"status": "installed"' "$output_dir/install.json"

run_eshu component enable dev.eshu.examples.pagerduty \
  --component-home "$component_home" \
  --instance pagerduty-reference-local \
  --mode scheduled \
  --claims \
  --config "$package_dir/config.example.yaml" \
  --json >"$output_dir/enable.json"
rg -q '"status": "enabled"' "$output_dir/enable.json"

run_eshu component list \
  --component-home "$component_home" \
  --json >"$output_dir/list.json"
rg -q '"status": "listed"' "$output_dir/list.json"
rg -q '"claim_capable"' "$output_dir/list.json"
