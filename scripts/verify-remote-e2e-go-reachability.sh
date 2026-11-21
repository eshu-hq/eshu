#!/usr/bin/env bash
set -euo pipefail

# Verifier for the go-reachability remote Compose proof (#10031, part of
# eshu-hq/eshu#10002). It checks recorded harness artifacts against the proof
# invariants: the scanner component reads back installed/enabled/trusted
# under a digest-pinned seed image, the scanner_worker work items reached
# terminal success with no retry/failed/dead-letter state, the committed
# vulnerability fact families match the committed expectation exactly, the
# deployed HTTP and MCP readbacks carry the pre-projection
# readiness_unavailable missing_evidence signal (collection scope per
# arbiter verdict b — projected findings with the three recorded verdicts
# belong to #10032), provenance is complete, and
# no secret or host-local material leaks into the proof surfaces.
#
# The expectation lives in the committed
# tests/fixtures/go_reachability_proof/expected.json (module set, levels,
# and scoped warning from the recorded #10030 tuples, wire values mirroring
# the golden snapshot pins) — never in the artifacts dir — so a tampered
# expectation is a visible repo change. The verifier resolves it from the
# repo root and fails closed when it is missing. It operates on an
# artifacts directory so it is deterministic and self-testable; the live
# run (which produces those artifacts from a running Compose stack) is the
# operator/CI gate.

list_only=false
artifacts_dir=""

usage() {
	printf '%s\n' \
		"Usage: $(basename "$0") --artifacts <dir> [--list]" \
		'' \
		'Verifies recorded go-reachability proof artifacts:' \
		'  inventory.json                  scanner component trust readback + digest-pinned seed image' \
		'  api-findings-cve_*.json         deployed HTTP impact-findings envelopes (one per CVE)' \
		'  mcp-findings-cve_*.json         deployed MCP impact-findings envelopes (one per CVE)' \
		'  workflow-items.json             scanner_worker work-item terminal states' \
		'  facts.json                      committed vulnerability fact-family counts' \
		'  provenance.json                 Eshu commit, seed digest, backend, queue terminal state, telemetry' \
		'' \
		'Facts are diffed against the committed' \
		'  tests/fixtures/go_reachability_proof/expected.json.' \
		'' \
		'  --list   print the proof checks without running them'
}

die() {
	printf 'verify-remote-e2e-go-reachability: %s\n' "$*" >&2
	exit 1
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--list) list_only=true; shift ;;
		--artifacts) artifacts_dir="${2:-}"; shift 2 ;;
		-h|--help) usage; exit 0 ;;
		*) die "unknown option: $1" ;;
	esac
done

command -v jq >/dev/null 2>&1 || die "jq is required"
command -v rg >/dev/null 2>&1 || die "rg is required"

readonly component_id="eshu.scanner.go-reachability"
readonly collector_kind="scanner_worker"
# Forbidden material that must never appear in any proof artifact. Host-local
# paths, private key markers, bearer tokens, and raw IPv4 addresses are
# redaction canaries; the proof fails closed if any are present.
readonly forbidden_patterns=(
	'/Users/'
	'/home/'
	'BEGIN [A-Z ]*PRIVATE KEY'
	'[Bb]earer [A-Za-z0-9._-]{8,}'
	'([0-9]{1,3}\.){3}[0-9]{1,3}'
)

print_checks() {
	printf '%s\n' \
		'go-reachability proof checks:' \
		"  1. inventory: ${component_id} reads back installed=true, enabled=true, trusted=true under a digest-pinned seed image" \
		'  2. deployed API/MCP: readbacks carry readiness_unavailable missing_evidence (pre-projection; projected findings belong to #10032)' \
		'  3. workflow: scanner_worker work items terminal success; no retrying/failed/dead-letter' \
		'  4. facts: captured vulnerability fact counts exactly match the committed expected.json' \
		'  5. provenance: records eshu_commit, seed digest, component digest, backend, queue terminal state, telemetry handle, evidence mode' \
		'  6. redaction canary: no host paths, private keys, bearer tokens, or raw IPs in artifacts'
}

if [[ "${list_only}" == true ]]; then
	print_checks
	exit 0
fi

[[ -n "${artifacts_dir}" ]] || die "--artifacts <dir> is required (or use --list)"
[[ -d "${artifacts_dir}" ]] || die "artifacts directory not found: ${artifacts_dir}"

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || (cd "$(dirname "$0")/.." && pwd))"
expected="${repo_root}/tests/fixtures/go_reachability_proof/expected.json"
[[ -f "${expected}" ]] || die "committed expectation not found: ${expected}"

inventory="${artifacts_dir}/inventory.json"
workflow_items="${artifacts_dir}/workflow-items.json"
facts="${artifacts_dir}/facts.json"
provenance="${artifacts_dir}/provenance.json"
for required in "${inventory}" "${workflow_items}" "${facts}" "${provenance}"; do
	[[ -f "${required}" ]] || die "missing required artifact: ${required}"
done

cve_ids="$(jq -r '.findings[].cve_id' "${expected}")"
[[ -n "${cve_ids}" ]] || die "expected.json names no findings"
finding_files=()
while IFS= read -r cve_id; do
	suffix="$(printf '%s' "${cve_id}" | tr '[:upper:]-' '[:lower:]_')"
	for surface in api mcp; do
		path="${artifacts_dir}/${surface}-findings-${suffix}.json"
		[[ -f "${path}" ]] || die "missing required artifact: ${path}"
		finding_files+=("${cve_id}:${surface}:${path}")
	done
done <<<"${cve_ids}"

# 1. Inventory: installed, enabled, and trusted for the scanner component,
#    plus a digest-pinned seed image (name@sha256:<64 hex>, no tags).
rg --quiet "\"component_id\"[[:space:]]*:[[:space:]]*\"${component_id}\"" "${inventory}" \
	|| die "inventory is not for component ${component_id}"
for flag in installed enabled trusted; do
	rg --quiet "\"${flag}\"[[:space:]]*:[[:space:]]*true" "${inventory}" \
		|| die "inventory does not show ${flag}=true"
done
rg --quiet '"manifest_digest"[[:space:]]*:[[:space:]]*"sha256:[0-9a-fA-F]{64}"' "${inventory}" \
	|| die "inventory manifest_digest is not a sha256 digest"
seed_image="$(jq -r '.seed_image // empty' "${inventory}")"
[[ "${seed_image}" =~ ^[^@[:space:]]+@sha256:[0-9a-f]{64}$ ]] \
	|| die "inventory seed_image is not a digest-pinned ref"
seed_digest="$(jq -r '.seed_digest // empty' "${inventory}")"
[[ "${seed_digest}" == "sha256:${seed_image##*@sha256:}" ]] \
	|| die "inventory seed_digest does not match the seed_image digest"

# 2+3. Deployed API and MCP readbacks must independently prove every
#    expected finding exactly as the committed expectation pins it.
check_finding_artifact() {
	local cve_id="$1"
	local surface="$2"
	local artifact="$3"
	local spec="$4"

	jq -e '.error == null and (.data | type == "object") and (.data.findings | type == "array") and (.truth | type == "object")' \
		"${artifact}" >/dev/null \
		|| die "${surface} ${cve_id} response is not a positive findings envelope"

	while IFS= read -r field; do
		jq -e --arg f "${field}" '.data | has($f)' "${artifact}" >/dev/null \
			|| die "${surface} ${cve_id} response is missing field: ${field}"
	done < <(jq -r '.required_response_fields[]' <<<"${spec}")

	# Collection scope (arbiter verdict b): an empty findings page is the
	# correct pre-projection state — projected findings belong to #10032.
	# The page must still carry the readiness_unavailable missing_evidence
	# signal; silence without it proves nothing.
	maximum="$(jq -r '.maximum_results' <<<"${spec}")"
	jq -e --argjson hi "${maximum}" \
		'(.data.findings | length) as $n | ($n >= 0 and $n <= $hi)' \
		"${artifact}" >/dev/null \
		|| die "${surface} ${cve_id} result count outside [0, ${maximum}]"
	if jq -e '.data.findings | length == 0' "${artifact}" >/dev/null; then
		jq -e '.data.readiness.readiness_state == "readiness_unavailable"' "${artifact}" >/dev/null \
			|| die "${surface} ${cve_id} empty findings page without readiness_unavailable state"
		jq -e '.data.readiness.missing_evidence | index("readiness_unavailable") != null' "${artifact}" >/dev/null \
			|| die "${surface} ${cve_id} empty findings page without readiness_unavailable missing_evidence"
		return 0
	fi

	while IFS= read -r field; do
		jq -e --arg f "${field}" '.data.findings | all(.[]; has($f))' "${artifact}" >/dev/null \
			|| die "${surface} ${cve_id} finding is missing field: ${field}"
	done < <(jq -r '.result_item_required_fields[]' <<<"${spec}")

	while IFS= read -r entry; do
		local key value_json rest
		key="$(jq -r '.k' <<<"${entry}")"
		value_json="$(jq -c '.v' <<<"${entry}")"
		rest="${key#findings[].}"
		[[ "${rest}" != "${key}" ]] || die "unexpected required_values key (must start with findings[].): ${key}"
		if [[ "${rest}" == *'[]' ]]; then
			local base
			base="${rest%'[]'}"
			jq -e --argjson path "$(jq -c -n --arg p "${base}" '$p | split(".")')" --argjson want "${value_json}" \
				'.data.findings | all(.[]; ((getpath($path) // []) | index($want)) != null)' \
				"${artifact}" >/dev/null \
				|| die "${surface} ${cve_id} required value missing: ${key} == ${value_json}"
		else
			jq -e --argjson path "$(jq -c -n --arg p "${rest}" '$p | split(".")')" --argjson want "${value_json}" \
				'.data.findings | all(.[]; getpath($path) == $want)' \
				"${artifact}" >/dev/null \
				|| die "${surface} ${cve_id} required value mismatch: ${key} == ${value_json}"
		fi
	done < <(jq -c '.required_values | to_entries[] | {k:.key, v:.value}' <<<"${spec}")
}

while IFS= read -r item; do
	cve_id="${item%%:*}"
	rest="${item#*:}"
	surface="${rest%%:*}"
	path="${rest#*:}"
	spec="$(jq -c --arg c "${cve_id}" '.findings[] | select(.cve_id == $c)' "${expected}")"
	check_finding_artifact "${cve_id}" "${surface}" "${path}" "${spec}"
done < <(printf '%s\n' "${finding_files[@]}")

# 4. Workflow: terminal success for the scanner_worker items, no
#    retrying/failed/dead-letter anywhere.
jq -e --arg kind "${collector_kind}" \
	'(.items | type == "array") and (any(.items[]; .collector_kind == $kind and (.state == "completed" or .state == "succeeded")))' \
	"${workflow_items}" >/dev/null \
	|| die "no completed/succeeded ${collector_kind} work item"
if rg --quiet '"state"[[:space:]]*:[[:space:]]*"(retrying|failed|dead_letter|dead-letter)"' "${workflow_items}"; then
	die "scanner_worker workflow has retrying/failed/dead-letter items"
fi

# 5. Facts: captured counts must exactly match the committed expectation —
#    no missing family, no extra family, no drifted count.
if ! diff <(jq -S . "${facts}") <(jq -S '.fact_counts' "${expected}") >/dev/null; then
	die "facts do not match expected.json fact_counts"
fi

# 6. Provenance: every reproducibility/audit field must be present and
#    non-empty, the seed image digest-pinned, and the run marked as
#    evidence mode.
for field in eshu_commit component_digest core_version backend queue_terminal_state metrics_handle; do
	rg --quiet "\"${field}\"[[:space:]]*:[[:space:]]*\"[^\"]+\"" "${provenance}" \
		|| die "provenance missing or empty field: ${field}"
done
rg --quiet '"eshu_commit"[[:space:]]*:[[:space:]]*"unknown"' "${provenance}" \
	&& die "provenance eshu_commit is unknown (capture ran outside a checkout)"
rg --quiet '"component_digest"[[:space:]]*:[[:space:]]*"sha256:[0-9A-Fa-f]{64}"' "${provenance}" \
	|| die "provenance component_digest is not a sha256 digest"
jq -e '.evidence_mode == true' "${provenance}" >/dev/null \
	|| die "provenance does not mark evidence_mode=true"
# Single-backend proof (arbiter amendment to #10031): the read surface is
# Neo4j-pinned, so the run must record neo4j as-run — never nornicdb.
jq -e '.backend == "neo4j"' "${provenance}" >/dev/null \
	|| die "provenance backend is not neo4j (single-backend proof required)"

# 7. Redaction canary across every artifact.
for artifact in "${inventory}" "${workflow_items}" "${facts}" "${provenance}" "${artifacts_dir}"/api-findings-*.json "${artifacts_dir}"/mcp-findings-*.json; do
	for pattern in "${forbidden_patterns[@]}"; do
		if rg --quiet "${pattern}" "${artifact}"; then
			die "forbidden material matched /${pattern}/ in $(basename "${artifact}")"
		fi
	done
done

printf 'go-reachability proof artifacts verified (component=%s)\n' "${component_id}"
