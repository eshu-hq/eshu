#!/usr/bin/env bash
set -euo pipefail

repo_root="${ESHU_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(git rev-parse --show-toplevel)"
fi

source_dir="$repo_root/examples/collector-extensions/pagerduty"
index_path="$source_dir/community-extension-index.draft.yaml"
work_root="$(mktemp -d)"
output_dir="$(mktemp -d)"
cleanup() {
  rm -rf "$work_root" "$output_dir"
}
trap cleanup EXIT

package_dir="$work_root/examples/collector-extensions/pagerduty"
sdk_dir="$work_root/sdk/go/collector"
mkdir -p "$package_dir" "$sdk_dir"
cp -R "$source_dir"/. "$package_dir"/
cp -R "$repo_root/sdk/go/collector"/. "$sdk_dir"/

run_eshu() {
  go -C "$repo_root/go" run ./cmd/eshu "$@"
}

go -C "$package_dir" test ./...

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

run_eshu component index verify "$index_path" --json >"$output_dir/index.json"
rg -q '"status": "verified"' "$output_dir/index.json"
rg -q '"valid": true' "$output_dir/index.json"

printf 'pagerduty marketplace readiness verification passed\n'
