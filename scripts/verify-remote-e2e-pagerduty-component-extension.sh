#!/usr/bin/env bash
set -euo pipefail

# Verifier for the PagerDuty component-extension remote Compose proof. It checks
# normalized artifacts captured from a running stack: the reference component is
# installed/enabled/trusted, component workflow rows reached terminal success,
# all PagerDuty reference fact families were committed for the captured
# generation, fixture parity with the in-tree PagerDuty contract is proven by a
# computed fact signature, and proof artifacts contain no private host,
# credential, or network material.

artifacts_dir=""
list_only=false

usage() {
	cat <<USAGE
Usage: $(basename "$0") --artifacts <dir> [--list]

Required artifacts:
  inventory.json        component readback for dev.eshu.examples.pagerduty
  workflow-items.json   component workflow item terminal states
  facts.json            committed dev.eshu.examples.pagerduty.* fact counts
  parity.json           in-tree/reference fixture parity summary
  provenance.json       commit, digest, backend, queue state, telemetry handle
USAGE
}

die() {
	printf 'verify-remote-e2e-pagerduty-component-extension: %s\n' "$*" >&2
	exit 1
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--artifacts) artifacts_dir="${2:-}"; shift 2 ;;
		--list) list_only=true; shift ;;
		-h|--help) usage; exit 0 ;;
		*) die "unknown option: $1" ;;
	esac
done

command -v rg >/dev/null 2>&1 || die "rg is required"

readonly component_id="dev.eshu.examples.pagerduty"
readonly fact_families=(
	"dev.eshu.examples.pagerduty.incident"
	"dev.eshu.examples.pagerduty.lifecycle_event"
	"dev.eshu.examples.pagerduty.change"
	"dev.eshu.examples.pagerduty.observed_service"
	"dev.eshu.examples.pagerduty.observed_integration"
	"dev.eshu.examples.pagerduty.coverage_warning"
)
readonly forbidden_patterns=(
	'/Users/'
	'/home/'
	'BEGIN [A-Z ]*PRIVATE KEY'
	'[Bb]earer [A-Za-z0-9._-]{8,}'
	'([0-9]{1,3}\.){3}[0-9]{1,3}'
)

print_checks() {
	cat <<CHECKS
pagerduty component-extension proof checks:
  1. inventory: ${component_id} reads back installed=true, enabled=true, trusted=true
  2. workflow: pagerduty component workflow item terminal success; no retrying/failed/dead-letter
  3. facts: all ${component_id} fact families have committed counts
  4. parity: reference fixture parity is recorded as passed and expected/extension fact signatures match
  5. provenance: records commit, digest, backend, queue state, and telemetry handle
  6. redaction canary: no host paths, private keys, bearer tokens, or raw IPs in artifacts
CHECKS
}

if [[ "${list_only}" == true ]]; then
	print_checks
	exit 0
fi

[[ -n "${artifacts_dir}" ]] || die "--artifacts <dir> is required (or use --list)"
[[ -d "${artifacts_dir}" ]] || die "artifacts directory not found: ${artifacts_dir}"

inventory="${artifacts_dir}/inventory.json"
workflow_items="${artifacts_dir}/workflow-items.json"
facts="${artifacts_dir}/facts.json"
parity="${artifacts_dir}/parity.json"
provenance="${artifacts_dir}/provenance.json"
for required in "${inventory}" "${workflow_items}" "${facts}" "${parity}" "${provenance}"; do
	[[ -f "${required}" ]] || die "missing required artifact: ${required}"
done

rg --fixed-strings --quiet "\"${component_id}\"" "${inventory}" \
	|| die "inventory missing component ${component_id}"
for state in installed enabled trusted; do
	rg --quiet "\"${state}\"[[:space:]]*:[[:space:]]*true" "${inventory}" \
		|| die "inventory does not show ${state}=true"
done

rg --quiet '"state"[[:space:]]*:[[:space:]]*"(completed|succeeded)"' "${workflow_items}" \
	|| die "no completed/succeeded PagerDuty component workflow item"
if rg --quiet '"state"[[:space:]]*:[[:space:]]*"(retrying|failed|dead_letter|dead-letter)"' "${workflow_items}"; then
	die "PagerDuty component workflow has retrying/failed/dead-letter items"
fi

for family in "${fact_families[@]}"; do
	rg --quiet "\"${family}\"[[:space:]]*:[[:space:]]*[1-9][0-9]*" "${facts}" \
		|| die "missing committed fact family: ${family}"
done

rg --quiet '"fixture_parity"[[:space:]]*:[[:space:]]*"passed"' "${parity}" \
	|| die "PagerDuty fixture parity was not recorded as passed"
for field in run_id source_run_id generation_id work_item_id expected_fact_signature extension_fact_signature; do
	rg --quiet "\"${field}\"[[:space:]]*:[[:space:]]*\"[^\"]+\"" "${parity}" \
		|| die "parity missing or empty field: ${field}"
done
expected_signature="$(rg -o '"expected_fact_signature"[[:space:]]*:[[:space:]]*"sha256:[A-Fa-f0-9]{64}"' "${parity}" | rg -o 'sha256:[A-Fa-f0-9]{64}' || true)"
extension_signature="$(rg -o '"extension_fact_signature"[[:space:]]*:[[:space:]]*"sha256:[A-Fa-f0-9]{64}"' "${parity}" | rg -o 'sha256:[A-Fa-f0-9]{64}' || true)"
[[ -n "${expected_signature}" ]] || die "expected_fact_signature is not a sha256 digest"
[[ -n "${extension_signature}" ]] || die "extension_fact_signature is not a sha256 digest"
[[ "${expected_signature}" == "${extension_signature}" ]] \
	|| die "PagerDuty fixture parity signature mismatch"
rg --quiet '"in_tree_fact_count"[[:space:]]*:[[:space:]]*6' "${parity}" \
	|| die "in-tree PagerDuty fact count mismatch"
rg --quiet '"extension_fact_count"[[:space:]]*:[[:space:]]*6' "${parity}" \
	|| die "extension PagerDuty fact count mismatch"

for field in eshu_commit component_digest backend queue_terminal_state metrics_handle; do
	rg --quiet "\"${field}\"[[:space:]]*:[[:space:]]*\"[^\"]+\"" "${provenance}" \
		|| die "provenance missing or empty field: ${field}"
done
rg --quiet '"component_digest"[[:space:]]*:[[:space:]]*"sha256:[A-Fa-f0-9]{8,}"' "${provenance}" \
	|| die "provenance component_digest is not a sha256 digest"

for artifact in "${inventory}" "${workflow_items}" "${facts}" "${parity}" "${provenance}"; do
	for pattern in "${forbidden_patterns[@]}"; do
		if rg --quiet "${pattern}" "${artifact}"; then
			die "forbidden material matched /${pattern}/ in $(basename "${artifact}")"
		fi
	done
done

printf 'PagerDuty component-extension proof artifacts verified (component=%s)\n' "${component_id}"
