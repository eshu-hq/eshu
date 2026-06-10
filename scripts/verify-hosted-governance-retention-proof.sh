#!/usr/bin/env bash
# Validate public-safe hosted governance retention-state proof evidence.

set -euo pipefail

input=""
output_json=""
output_markdown=""

usage() {
	cat <<USAGE
Usage: $(basename "$0") --input <proof.json> --output-json <summary.json> --output-markdown <summary.md>

The input is an operator-local retention-state proof. Raw policy documents,
tenant identifiers, repositories, source ids, payloads, prompts, provider
responses, backup locators, URLs, hostnames, IPs, and secrets must stay outside
the repository and outside generated summaries.
USAGE
}

die() {
	printf 'verify-hosted-governance-retention-proof: %s\n' "$*" >&2
	exit 1
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--input)
			input="${2:-}"
			shift 2
			;;
		--output-json)
			output_json="${2:-}"
			shift 2
			;;
		--output-markdown)
			output_markdown="${2:-}"
			shift 2
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			die "unknown option: $1"
			;;
	esac
done

[[ -n "${input}" ]] || die "--input is required"
[[ -n "${output_json}" ]] || die "--output-json is required"
[[ -n "${output_markdown}" ]] || die "--output-markdown is required"
[[ -f "${input}" ]] || die "input file not found: ${input}"
command -v jq >/dev/null 2>&1 || die "jq is required"
command -v rg >/dev/null 2>&1 || die "rg is required"

jq -e . "${input}" >/dev/null 2>&1 || die "input must be valid JSON"

forbidden_keys='["url","uri","host","hostname","ip","address","path","file","repository","repo","repo_id","repo_name","tenant","tenant_id","workspace","workspace_id","token","secret","credential","password","dsn","signed_url","backup","backup_locator","raw_policy","policy_body","payload","request","response","prompt","stdout","stderr","transcript","log","logs","principal","source_id","source_identifier"]'
if ! jq -e --argjson forbidden "${forbidden_keys}" '
	[.. | objects | keys[]? | select(. as $key | $forbidden | index($key))]
	| length == 0
' "${input}" >/dev/null; then
	die "input looks like private data; use aggregate retention classes, counts, states, and reason codes only"
fi

if jq -r '.. | strings' "${input}" | rg --quiet --ignore-case \
	'ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|https?://|bolt://|postgres(ql)?://|arn:(aws|aws-us-gov|aws-cn):|/(Users|home|private|var|tmp|Volumes|workspace|workspaces|repos|personal-repos)/|(^|[^0-9])[0-9]{12}([^0-9]|$)|([0-9]{1,3}\.){3}[0-9]{1,3}|[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}|(^|[[:space:]"'\''])[-A-Za-z0-9_.]+/[-A-Za-z0-9_.]+($|[[:space:]"'\'',])'; then
	die "input looks like private data; raw locators, transcripts, and private identifiers are not accepted"
fi

required_scenarios='["configured_retention","local_not_configured","deletion_pending","deletion_complete","graph_rebuild_required"]'
if ! jq -e --argjson required "${required_scenarios}" '
	def names: [.scenarios[]?.name];
	(names | length) == ($required | length) and
	((names - $required) | length) == 0 and
	(($required - names) | length) == 0
' "${input}" >/dev/null; then
	missing="$(jq -r --argjson required "${required_scenarios}" '[($required - [.scenarios[]?.name])[]] | join(", ")' "${input}")"
	unknown="$(jq -r --argjson required "${required_scenarios}" '([.scenarios[]?.name] - $required) | join(", ")' "${input}")"
	[[ -z "${missing}" ]] || die "missing required retention scenarios: ${missing}"
	die "unknown retention scenarios: ${unknown}"
fi

if ! jq -e '
	def nonempty_string($value): ($value | type == "string" and length > 0);
	.schema_version == 1 and
	nonempty_string(.proof_id) and
	(.proof_id | test("^[A-Za-z0-9._-]+$")) and
	nonempty_string(.generated_at) and
	(.generated_at | test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$")) and
	(.scenarios | type == "array") and
	(.security | type == "object")
' "${input}" >/dev/null; then
	die "input root shape must include schema version, proof id, generated timestamp, scenarios, and security object"
fi

if ! jq -e '
	[
		.scenarios[] |
		(.status == "pass") and
		(.retention_mode | IN("metadata_only", "configured", "disabled", "not_configured", "stale", "invalid")) and
		(.deletion_state | IN("not_requested", "pending", "running", "blocked", "repairing_graph", "complete", "failed")) and
		(.reason_codes | type == "array" and length > 0) and
		([.reason_codes[]? | type == "string" and test("^[a-z0-9_]+$")] | all) and
		(.api_status == "pass") and
		(.mcp_status == "pass") and
		(.agreement_status == "pass") and
		(.checked_count | type == "number" and . > 0) and
		(.mismatch_count | type == "number" and . == 0) and
		(.data_class_counts | type == "object") and
		([.data_class_counts[] | type == "number" and . >= 0] | all)
	] | all
' "${input}" >/dev/null; then
	die "retention scenario parity must pass with zero mismatches and bounded retention/deletion state"
fi

if ! jq -e '
	.security.raw_policy_exported == false and
	.security.raw_payload_exported == false and
	.security.secret_scan == "passed" and
	.security.private_locator_scan == "passed" and
	.security.public_artifact_review == "passed"
' "${input}" >/dev/null; then
	die "security review did not pass"
fi

tmp_json="${output_json}.tmp"
tmp_markdown="${output_markdown}.tmp"
mkdir -p "$(dirname "${output_json}")" "$(dirname "${output_markdown}")"

jq '{
	status: "pass",
	schema_version: 1,
	proof_id: .proof_id,
	generated_at: .generated_at,
	scenario_count: (.scenarios | length),
	parity_checked_count: ([.scenarios[].checked_count] | add),
	parity_mismatch_count: ([.scenarios[].mismatch_count] | add),
	scenarios: [
		.scenarios[] | {
			name,
			status,
			retention_mode,
			deletion_state,
			reason_codes,
			checked_count,
			mismatch_count,
			data_class_counts
		}
	],
	security: {
		raw_policy_exported: .security.raw_policy_exported,
		raw_payload_exported: .security.raw_payload_exported,
		secret_scan: .security.secret_scan,
		private_locator_scan: .security.private_locator_scan,
		public_artifact_review: .security.public_artifact_review
	}
}' "${input}" >"${tmp_json}"

jq -r '
	[
		"# Hosted governance retention proof",
		"",
		"- Status: \(.status)",
		"- Proof ID: \(.proof_id)",
		"- Generated at: \(.generated_at)",
		"- Scenarios: \(.scenario_count)",
		"- API/MCP parity: checked=\(.parity_checked_count), mismatches=\(.parity_mismatch_count)",
		"",
		"## Scenarios",
		"",
		(.scenarios[] | "- \(.name): status=\(.status), retention=\(.retention_mode), deletion=\(.deletion_state), reasons=\(.reason_codes | join(",")), checked=\(.checked_count), mismatches=\(.mismatch_count)"),
		"",
		"Raw policies, tenants, repositories, source identifiers, payloads, prompts, provider responses, backup locators, URLs, hostnames, IPs, tokens, and credential handles remain operator-local."
	] | .[]
' "${tmp_json}" >"${tmp_markdown}"

mv "${tmp_json}" "${output_json}"
mv "${tmp_markdown}" "${output_markdown}"
printf 'verify-hosted-governance-retention-proof: wrote %s and %s\n' "${output_json}" "${output_markdown}"
