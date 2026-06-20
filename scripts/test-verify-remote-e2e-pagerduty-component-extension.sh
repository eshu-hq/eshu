#!/usr/bin/env bash
set -euo pipefail

# Self-test for the PagerDuty component-extension proof verifier.

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || (cd "$(dirname "$0")/.." && pwd))"
verifier="${repo_root}/scripts/verify-remote-e2e-pagerduty-component-extension.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}" 2>/dev/null || true' EXIT

die() {
	printf 'test-verify-remote-e2e-pagerduty-component-extension: %s\n' "$*" >&2
	exit 1
}

cat >"${tmp_dir}/inventory.json" <<'JSON'
{"component_id":"dev.eshu.examples.pagerduty","installed":true,"enabled":true,"trusted":true,"manifest_digest":"sha256:1111111111111111111111111111111111111111111111111111111111111111"}
JSON
cat >"${tmp_dir}/workflow-items.json" <<'JSON'
{"items":[{"work_item_id":"pagerduty-reference-1","collector_kind":"pagerduty","state":"completed"}]}
JSON
cat >"${tmp_dir}/facts.json" <<'JSON'
{
  "dev.eshu.examples.pagerduty.incident": 1,
  "dev.eshu.examples.pagerduty.lifecycle_event": 1,
  "dev.eshu.examples.pagerduty.change": 1,
  "dev.eshu.examples.pagerduty.observed_service": 1,
  "dev.eshu.examples.pagerduty.observed_integration": 1,
  "dev.eshu.examples.pagerduty.coverage_warning": 1
}
JSON
cat >"${tmp_dir}/parity.json" <<'JSON'
{
  "fixture_parity": "passed",
  "run_id": "run-pagerduty-reference",
  "source_run_id": "source-run-pagerduty-reference",
  "generation_id": "generation-pagerduty-reference",
  "work_item_id": "work-pagerduty-reference",
  "expected_fact_signature": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
  "extension_fact_signature": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
  "in_tree_fact_count": 6,
  "extension_fact_count": 6
}
JSON
cat >"${tmp_dir}/provenance.json" <<'JSON'
{"eshu_commit":"abc1234","component_digest":"sha256:1111111111111111111111111111111111111111111111111111111111111111","backend":"nornicdb","queue_terminal_state":"completed","metrics_handle":":9464/metrics"}
JSON

missing_readback="${tmp_dir}/missing-readback"
mkdir -p "${missing_readback}"
cp "${tmp_dir}/inventory.json" "${tmp_dir}/workflow-items.json" "${tmp_dir}/facts.json" "${tmp_dir}/parity.json" "${tmp_dir}/provenance.json" "${missing_readback}/"
if "${verifier}" --artifacts "${missing_readback}" >/tmp/pagerduty-component-proof-missing-readback.out 2>/tmp/pagerduty-component-proof-missing-readback.err; then
	die "expected verifier to fail when API/MCP lifecycle artifacts are missing"
fi
rg --quiet 'missing required artifact' /tmp/pagerduty-component-proof-missing-readback.err \
	|| die "missing readback failure was not reported"

cat >"${tmp_dir}/api-inventory.json" <<'JSON'
{"data":{"component_home_configured":true,"components":[{"id":"dev.eshu.examples.pagerduty","verified":true,"states":["installed","enabled","claim_capable"]}]},"error":null}
JSON
cat >"${tmp_dir}/mcp-inventory.json" <<'JSON'
{"data":{"component_home_configured":true,"components":[{"id":"dev.eshu.examples.pagerduty","verified":true,"states":["installed","enabled","claim_capable"]}]},"error":null}
JSON
cat >"${tmp_dir}/disable.json" <<'JSON'
{"schema_version":"eshu.component.cli.v1","command":"disable","status":"disabled","component":{"id":"dev.eshu.examples.pagerduty"},"activation":{"instance_id":"pagerduty-reference"}}
JSON
cat >"${tmp_dir}/post-disable-inventory.json" <<'JSON'
{"data":{"component_home_configured":true,"components":[{"id":"dev.eshu.examples.pagerduty","verified":true,"states":["installed"]}]},"error":null}
JSON
cat >"${tmp_dir}/uninstall.json" <<'JSON'
{"schema_version":"eshu.component.cli.v1","command":"uninstall","status":"uninstalled","component":{"id":"dev.eshu.examples.pagerduty","version":"0.1.0"}}
JSON
cat >"${tmp_dir}/post-uninstall-inventory.json" <<'JSON'
{"data":{"component_home_configured":true,"components":[]},"error":null}
JSON

"${verifier}" --artifacts "${tmp_dir}" >/tmp/pagerduty-component-proof-pass.out
rg --quiet 'PagerDuty component-extension proof artifacts verified' /tmp/pagerduty-component-proof-pass.out \
	|| die "expected verifier pass output"

missing_family="${tmp_dir}/missing-family"
mkdir -p "${missing_family}"
cp "${tmp_dir}/inventory.json" "${tmp_dir}/api-inventory.json" "${tmp_dir}/mcp-inventory.json" \
	"${tmp_dir}/workflow-items.json" "${tmp_dir}/parity.json" "${tmp_dir}/provenance.json" \
	"${tmp_dir}/disable.json" "${tmp_dir}/post-disable-inventory.json" "${tmp_dir}/uninstall.json" \
	"${tmp_dir}/post-uninstall-inventory.json" "${missing_family}/"
cat >"${missing_family}/facts.json" <<'JSON'
{"dev.eshu.examples.pagerduty.incident": 1}
JSON
if "${verifier}" --artifacts "${missing_family}" >/tmp/pagerduty-component-proof-fail.out 2>/tmp/pagerduty-component-proof-fail.err; then
	die "expected verifier to fail when PagerDuty fact families are missing"
fi
rg --quiet 'missing committed fact family' /tmp/pagerduty-component-proof-fail.err \
	|| die "missing family failure was not reported"

signature_mismatch="${tmp_dir}/signature-mismatch"
mkdir -p "${signature_mismatch}"
cp "${tmp_dir}/inventory.json" "${tmp_dir}/api-inventory.json" "${tmp_dir}/mcp-inventory.json" \
	"${tmp_dir}/workflow-items.json" "${tmp_dir}/facts.json" "${tmp_dir}/provenance.json" \
	"${tmp_dir}/disable.json" "${tmp_dir}/post-disable-inventory.json" "${tmp_dir}/uninstall.json" \
	"${tmp_dir}/post-uninstall-inventory.json" "${signature_mismatch}/"
cat >"${signature_mismatch}/parity.json" <<'JSON'
{
  "fixture_parity": "passed",
  "run_id": "run-pagerduty-reference",
  "source_run_id": "source-run-pagerduty-reference",
  "generation_id": "generation-pagerduty-reference",
  "work_item_id": "work-pagerduty-reference",
  "expected_fact_signature": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
  "extension_fact_signature": "sha256:2222222222222222222222222222222222222222222222222222222222222222",
  "in_tree_fact_count": 6,
  "extension_fact_count": 6
}
JSON
if "${verifier}" --artifacts "${signature_mismatch}" >/tmp/pagerduty-component-proof-sig.out 2>/tmp/pagerduty-component-proof-sig.err; then
	die "expected verifier to fail when PagerDuty parity signatures differ"
fi
rg --quiet 'fixture parity signature mismatch' /tmp/pagerduty-component-proof-sig.err \
	|| die "signature mismatch failure was not reported"

leaky="${tmp_dir}/leaky"
mkdir -p "${leaky}"
cp "${tmp_dir}/inventory.json" "${tmp_dir}/api-inventory.json" "${tmp_dir}/mcp-inventory.json" \
	"${tmp_dir}/workflow-items.json" "${tmp_dir}/facts.json" "${tmp_dir}/parity.json" \
	"${tmp_dir}/provenance.json" "${tmp_dir}/disable.json" "${tmp_dir}/post-disable-inventory.json" \
	"${tmp_dir}/uninstall.json" "${tmp_dir}/post-uninstall-inventory.json" "${leaky}/"
cat >"${leaky}/parity.json" <<'JSON'
{
  "fixture_parity": "passed",
  "run_id": "run-pagerduty-reference",
  "source_run_id": "source-run-pagerduty-reference",
  "generation_id": "generation-pagerduty-reference",
  "work_item_id": "work-pagerduty-reference",
  "expected_fact_signature": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
  "extension_fact_signature": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
  "in_tree_fact_count": 6,
  "extension_fact_count": 6,
  "leak": "Bearer exampletoken123"
}
JSON
if "${verifier}" --artifacts "${leaky}" >/tmp/pagerduty-component-proof-leak.out 2>/tmp/pagerduty-component-proof-leak.err; then
	die "expected verifier to fail when proof artifacts contain forbidden material"
fi
rg --quiet 'forbidden material matched' /tmp/pagerduty-component-proof-leak.err \
	|| die "forbidden material failure was not reported"

printf 'PagerDuty component-extension proof verifier self-test passed\n'
