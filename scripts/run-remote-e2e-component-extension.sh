#!/usr/bin/env bash
set -euo pipefail

# Capture + verify driver for the Scorecard component-extension remote Compose
# proof (#2126, #1923). It reads runtime truth from a running ce-proof stack
# (see docs/public/run-locally/docker-compose.component-extension.yaml), shapes
# it into the three normalized proof artifacts the verifier consumes, then runs
# scripts/verify-remote-e2e-component-extension.sh against them.
#
# The artifacts are normalized on purpose: only the fields the proof asserts are
# emitted (component trust/enablement, workflow terminal state, bounded fact
# family counts). Raw fact payloads and source URIs are never dumped, so the
# verifier's redaction canary holds by construction.
#
# Usage (from repo root, after the stack is up and has reconciled):
#   scripts/run-remote-e2e-component-extension.sh [--artifacts <dir>]
#
# Environment overrides (defaults target the documented ce-proof stack):
#   CE_PROOF_PROJECT      docker compose project name        (default: ce-proof)
#   CE_PROOF_COLLECTOR    collector container name           (default: ${project}-component-extension-collector-1)
#   CE_PROOF_POSTGRES     postgres container name            (default: ${project}-postgres-1)
#   CE_PROOF_COMPONENT_HOME  component home in the collector  (default: /data/.eshu/components)

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || (cd "$(dirname "$0")/.." && pwd))"

project="${CE_PROOF_PROJECT:-ce-proof}"
collector="${CE_PROOF_COLLECTOR:-${project}-component-extension-collector-1}"
postgres="${CE_PROOF_POSTGRES:-${project}-postgres-1}"
component_home="${CE_PROOF_COMPONENT_HOME:-/data/.eshu/components}"
component_id="dev.eshu.examples.scorecard"
artifacts_dir=""

die() {
	printf 'run-remote-e2e-component-extension: %s\n' "$*" >&2
	exit 1
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--artifacts) artifacts_dir="${2:-}"; shift 2 ;;
		-h|--help) sed -n '3,30p' "$0"; exit 0 ;;
		*) die "unknown option: $1" ;;
	esac
done

command -v docker >/dev/null 2>&1 || die "docker is required"
command -v rg >/dev/null 2>&1 || die "rg is required"

if [[ -z "${artifacts_dir}" ]]; then
	artifacts_dir="$(mktemp -d "${TMPDIR:-/tmp}/ce-proof-artifacts.XXXXXX")"
fi
mkdir -p "${artifacts_dir}"

docker inspect "${collector}" >/dev/null 2>&1 || die "collector container not found: ${collector}"
docker inspect "${postgres}" >/dev/null 2>&1 || die "postgres container not found: ${postgres}"

# 1. Inventory: trusted readback from the component CLI, normalized to the
#    installed/enabled/trusted booleans the proof asserts. Trust flags mirror the
#    allowlist policy the coordinator and collector run under.
cli_json="$(docker exec "${collector}" eshu component list \
	--component-home "${component_home}" \
	--trust-mode allowlist \
	--allow-id "${component_id}" \
	--allow-publisher eshu-hq \
	--json)"

state_block="$(printf '%s' "${cli_json}" | rg -U -o '"states"\s*:\s*\[[^]]*\]' || true)"
installed=false; enabled=false
printf '%s' "${state_block}" | rg --quiet '"installed"' && installed=true
printf '%s' "${state_block}" | rg --quiet '"enabled"' && enabled=true
trusted=false
printf '%s' "${cli_json}" | rg --quiet '"verified"\s*:\s*true' && trusted=true
digest="$(printf '%s' "${cli_json}" | rg -o '"manifest_digest"\s*:\s*"[^"]*"' | rg -o 'sha256:[A-Fa-f0-9]+' || echo "")"

cat >"${artifacts_dir}/inventory.json" <<JSON
{
  "component_id": "${component_id}",
  "installed": ${installed},
  "enabled": ${enabled},
  "trusted": ${trusted},
  "manifest_digest": "${digest}"
}
JSON

# 2. Workflow items: terminal state per scorecard component work item. Postgres
#    'completed' maps to the proof's terminal-success state; any non-terminal or
#    failed status is preserved verbatim so the verifier can fail closed.
work_rows="$(docker exec "${postgres}" psql -U eshu -d eshu -tAF'|' -c \
	"SELECT work_item_id, status FROM workflow_work_items WHERE collector_kind='scorecard' ORDER BY work_item_id;")"
{
	printf '{\n  "items": [\n'
	first=true
	while IFS='|' read -r wid status; do
		[[ -n "${wid}" ]] || continue
		[[ "${first}" == true ]] && first=false || printf ',\n'
		printf '    {"work_item_id": "%s", "collector_kind": "scorecard", "state": "%s"}' "${wid}" "${status}"
	done <<<"${work_rows}"
	printf '\n  ]\n}\n'
} >"${artifacts_dir}/workflow-items.json"

# 3. Facts: committed dev.eshu.examples.scorecard.* family counts. Counts only,
#    never payloads, so no source material can leak into the proof surface.
fact_rows="$(docker exec "${postgres}" psql -U eshu -d eshu -tAF'|' -c \
	"SELECT fact_kind, count(*) FROM fact_records WHERE fact_kind LIKE '${component_id}.%' GROUP BY fact_kind ORDER BY fact_kind;")"
{
	printf '{\n'
	first=true
	while IFS='|' read -r kind count; do
		[[ -n "${kind}" ]] || continue
		[[ "${first}" == true ]] && first=false || printf ',\n'
		printf '  "%s": %s' "${kind}" "${count}"
	done <<<"${fact_rows}"
	printf '\n}\n'
} >"${artifacts_dir}/facts.json"

printf 'captured proof artifacts to %s\n' "${artifacts_dir}"
"${repo_root}/scripts/verify-remote-e2e-component-extension.sh" --artifacts "${artifacts_dir}"
