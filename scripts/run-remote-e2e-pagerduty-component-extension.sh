#!/usr/bin/env bash
set -euo pipefail

# Capture + verify driver for the PagerDuty component-extension remote Compose
# proof. It reads normalized proof artifacts from a running stack where the
# dev.eshu.examples.pagerduty component has been installed, enabled, trusted,
# and processed by collector-component-extension.

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || (cd "$(dirname "$0")/.." && pwd))"

project="${PD_CE_PROOF_PROJECT:-pd-ce-proof}"
collector="${PD_CE_PROOF_COLLECTOR:-${project}-component-extension-collector-1}"
postgres="${PD_CE_PROOF_POSTGRES:-${project}-postgres-1}"
api="${PD_CE_PROOF_API:-${project}-eshu-1}"
mcp="${PD_CE_PROOF_MCP:-${project}-mcp-server-1}"
component_home="${PD_CE_PROOF_COMPONENT_HOME:-/data/.eshu/components}"
component_id="dev.eshu.examples.pagerduty"
instance_id="${PD_CE_PROOF_INSTANCE_ID:-pagerduty-reference}"
artifacts_dir=""

die() {
	printf 'run-remote-e2e-pagerduty-component-extension: %s\n' "$*" >&2
	exit 1
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--artifacts) artifacts_dir="${2:-}"; shift 2 ;;
		-h|--help) sed -n '3,24p' "$0"; exit 0 ;;
		*) die "unknown option: $1" ;;
	esac
done

command -v docker >/dev/null 2>&1 || die "docker is required"
command -v rg >/dev/null 2>&1 || die "rg is required"

if [[ -z "${artifacts_dir}" ]]; then
	artifacts_dir="$(mktemp -d "${TMPDIR:-/tmp}/pd-ce-proof-artifacts.XXXXXX")"
fi
mkdir -p "${artifacts_dir}"

docker inspect "${collector}" >/dev/null 2>&1 || die "collector container not found: ${collector}"
docker inspect "${postgres}" >/dev/null 2>&1 || die "postgres container not found: ${postgres}"
docker inspect "${api}" >/dev/null 2>&1 || die "api container not found: ${api}"
docker inspect "${mcp}" >/dev/null 2>&1 || die "mcp container not found: ${mcp}"

capture_component_inventory() {
	container="$1"
	output="$2"
	docker exec "${container}" sh -c '
		token=""
		while IFS= read -r line; do
			case "$line" in
				ESHU_API_KEY=*) token="${line#ESHU_API_KEY=}" ;;
			esac
		done < /data/.eshu/.env
		[ -n "$token" ] || exit 2
		curl -fsS \
			-H "Authorization: Bearer ${token}" \
			-H "Accept: application/eshu.envelope+json" \
			"http://localhost:8080/api/v0/component-extensions?limit=100"
	' >"${output}"
}

cli_json="$(docker exec "${collector}" eshu component list \
	--component-home "${component_home}" \
	--trust-mode allowlist \
	--allow-id "${component_id}" \
	--allow-publisher eshu-hq \
	--json)"

state_block="$(printf '%s' "${cli_json}" | rg -U -o '"states"\s*:\s*\[[^]]*\]' || true)"
installed=false; enabled=false; trusted=false
printf '%s' "${state_block}" | rg --quiet '"installed"' && installed=true
printf '%s' "${state_block}" | rg --quiet '"enabled"' && enabled=true
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

work_item_row="$(docker exec -i "${postgres}" psql -U eshu -d eshu -tAF'|' \
	-v ON_ERROR_STOP=1 \
	-v instance_id="${instance_id}" \
	-v component_like="${component_id}.%" <<'SQL'
SELECT work_item_id, run_id, source_run_id, generation_id, scope_id, status, current_fencing_token, attempt_count
FROM workflow_work_items AS item
WHERE item.collector_kind='pagerduty'
	AND item.collector_instance_id=:'instance_id'
	AND item.status='completed'
	AND EXISTS (
		SELECT 1
		FROM fact_records AS fact
		WHERE fact.scope_id=item.scope_id
			AND fact.generation_id=item.generation_id
			AND fact.fact_kind LIKE :'component_like'
	)
ORDER BY updated_at DESC, work_item_id DESC
LIMIT 1;
SQL
)"
[[ -n "${work_item_row}" ]] || die "no completed PagerDuty component workflow item with committed facts found for instance ${instance_id}"
IFS='|' read -r work_item_id run_id source_run_id generation_id scope_id workflow_status fencing_token attempt_count <<ROW
${work_item_row}
ROW
[[ -n "${work_item_id}" && -n "${run_id}" && -n "${source_run_id}" && -n "${generation_id}" && -n "${scope_id}" ]] \
	|| die "PagerDuty component workflow item is missing run/source/generation identity"
[[ -n "${fencing_token}" ]] || fencing_token=0
[[ -n "${attempt_count}" ]] || attempt_count=0
{
	printf '{\n  "items": [\n'
	printf '    {"work_item_id": "%s", "run_id": "%s", "source_run_id": "%s", "generation_id": "%s", "collector_kind": "pagerduty", "collector_instance_id": "%s", "state": "%s"}' \
		"${work_item_id}" "${run_id}" "${source_run_id}" "${generation_id}" "${instance_id}" "${workflow_status}"
	printf '\n  ]\n}\n'
} >"${artifacts_dir}/workflow-items.json"

fact_rows="$(docker exec -i "${postgres}" psql -U eshu -d eshu -tAF'|' \
	-v ON_ERROR_STOP=1 \
	-v scope_id="${scope_id}" \
	-v generation_id="${generation_id}" \
	-v component_like="${component_id}.%" <<'SQL'
SELECT fact_kind, count(*)
FROM fact_records
WHERE scope_id=:'scope_id' AND generation_id=:'generation_id' AND fact_kind LIKE :'component_like'
GROUP BY fact_kind
ORDER BY fact_kind;
SQL
)"
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

extension_fact_count="$(printf '%s\n' "${fact_rows}" | rg -c '^dev\.eshu\.examples\.pagerduty\.' || true)"
[[ -n "${extension_fact_count}" ]] || extension_fact_count=0
actual_fact_json="$(docker exec -i "${postgres}" psql -U eshu -d eshu -tA \
	-v ON_ERROR_STOP=1 \
	-v scope_id="${scope_id}" \
	-v generation_id="${generation_id}" \
	-v component_like="${component_id}.%" <<'SQL'
SELECT COALESCE(
	jsonb_agg(
		jsonb_build_object(
			'kind', fact_kind,
			'schema_version', schema_version,
			'stable_key', stable_fact_key,
			'source_confidence', source_confidence,
			'source_ref', jsonb_build_object(
				'source_system', source_system,
				'scope_id', scope_id,
				'generation_id', generation_id,
				'fact_key', source_fact_key,
				'uri', COALESCE(source_uri, ''),
				'record_id', COALESCE(source_record_id, '')
			),
			'payload', payload
		)
		ORDER BY fact_kind, stable_fact_key
	)::text,
	'[]'
)
FROM fact_records
WHERE scope_id=:'scope_id' AND generation_id=:'generation_id' AND fact_kind LIKE :'component_like';
SQL
)"
extension_fact_signature="$(printf '%s' "${actual_fact_json}" | docker exec -i "${collector}" pagerduty-reference --proof-digest-json)"
expected_request="$(cat <<JSON
{
  "protocol_version": "collector-sdk/v1alpha1",
  "claim": {
    "component_id": "${component_id}",
    "instance_id": "${instance_id}",
    "collector_kind": "pagerduty",
    "source_system": "pagerduty",
    "scope": {
      "id": "${scope_id}",
      "kind": "pagerduty_account"
    },
    "source_run_id": "${source_run_id}",
    "generation_id": "${generation_id}",
    "work_item_id": "${work_item_id}",
    "fencing_token": "${fencing_token}",
    "attempt": ${attempt_count},
    "deadline": "2026-06-14T15:00:00Z",
    "config_handle": "component-config:${instance_id}"
  },
  "contract": {
    "protocol_version": "collector-sdk/v1alpha1"
  },
  "config": {
    "source": {
      "input": "/opt/pagerduty/testdata/complete.json"
    }
  }
}
JSON
)"
expected_fact_signature="$(printf '%s' "${expected_request}" | docker exec -i "${collector}" pagerduty-reference --sdk-stdio --proof-digest)"
fixture_parity="failed"
if [[ "${extension_fact_signature}" == "${expected_fact_signature}" ]]; then
	fixture_parity="passed"
fi
cat >"${artifacts_dir}/parity.json" <<JSON
{
  "fixture_parity": "${fixture_parity}",
  "run_id": "${run_id}",
  "source_run_id": "${source_run_id}",
  "generation_id": "${generation_id}",
  "work_item_id": "${work_item_id}",
  "expected_fact_signature": "${expected_fact_signature}",
  "extension_fact_signature": "${extension_fact_signature}",
  "in_tree_fact_count": 6,
  "extension_fact_count": ${extension_fact_count}
}
JSON

eshu_commit="$(git -C "${repo_root}" rev-parse --short HEAD 2>/dev/null || echo "unknown")"
backend="$(docker exec "${collector}" printenv ESHU_GRAPH_BACKEND 2>/dev/null || echo "nornicdb")"
queue_state="$(docker exec -i "${postgres}" psql -U eshu -d eshu -tA \
	-v ON_ERROR_STOP=1 \
	-v instance_id="${instance_id}" \
	-v generation_id="${generation_id}" 2>/dev/null <<'SQL' | tr -d '[:space:]'
SELECT string_agg(DISTINCT status, ',' ORDER BY status)
FROM workflow_work_items
WHERE collector_kind='pagerduty' AND collector_instance_id=:'instance_id' AND generation_id=:'generation_id';
SQL
)"
[[ -n "${queue_state}" ]] || queue_state="none"
cat >"${artifacts_dir}/provenance.json" <<JSON
{
  "eshu_commit": "${eshu_commit}",
  "component_id": "${component_id}",
  "component_instance_id": "${instance_id}",
  "component_digest": "${digest}",
  "backend": "${backend}",
  "queue_terminal_state": "${queue_state}",
  "metrics_handle": ":9464/metrics",
  "source_evidence_only": true
}
JSON

capture_component_inventory "${api}" "${artifacts_dir}/api-inventory.json"
capture_component_inventory "${mcp}" "${artifacts_dir}/mcp-inventory.json"

docker exec "${collector}" eshu component disable "${component_id}" \
	--component-home "${component_home}" \
	--instance "${instance_id}" \
	--json >"${artifacts_dir}/disable.json"
capture_component_inventory "${api}" "${artifacts_dir}/post-disable-inventory.json"

docker exec "${collector}" eshu component uninstall "${component_id}" \
	--component-home "${component_home}" \
	--version 0.1.0 \
	--json >"${artifacts_dir}/uninstall.json"
capture_component_inventory "${api}" "${artifacts_dir}/post-uninstall-inventory.json"

printf 'captured PagerDuty component-extension proof artifacts to %s\n' "${artifacts_dir}"
"${repo_root}/scripts/verify-remote-e2e-pagerduty-component-extension.sh" --artifacts "${artifacts_dir}"
