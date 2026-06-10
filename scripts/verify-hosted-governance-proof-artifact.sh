#!/usr/bin/env bash
# Validate public-safe hosted governance proof evidence.

set -euo pipefail

input=""
output_json=""
output_markdown=""

usage() {
	cat <<USAGE
Usage: $(basename "$0") --input <proof.json> --output-json <summary.json> --output-markdown <summary.md>

The input is an operator-local aggregate proof. Raw tokens, tenant identifiers,
repository names, URLs, hostnames, IP addresses, source payloads, prompts,
provider responses, logs, and private locators must stay outside the repository.
USAGE
}

die() {
	printf 'verify-hosted-governance-proof-artifact: %s\n' "$*" >&2
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

forbidden_keys='["url","uri","host","hostname","ip","address","path","file","repository","repo","repo_id","repo_name","tenant","tenant_id","workspace","workspace_id","token","secret","credential","password","dsn","signed_url","payload","request","response","prompt","stdout","stderr","transcript","log","logs","principal","source_id","source_identifier"]'
if ! jq -e --argjson forbidden "${forbidden_keys}" '
	[.. | objects | keys[]? | select(. as $key | $forbidden | index($key))]
	| length == 0
' "${input}" >/dev/null; then
	die "input looks like private data; use aggregate handles, classes, counts, and status only"
fi

if jq -r '.. | strings' "${input}" | rg --quiet --ignore-case \
	'ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|https?://|bolt://|postgres(ql)?://|arn:(aws|aws-us-gov|aws-cn):|/(Users|home|private|var|tmp|Volumes|workspace|workspaces|repos|personal-repos)/|(^|[^0-9])[0-9]{12}([^0-9]|$)|([0-9]{1,3}\.){3}[0-9]{1,3}|[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}|(^|[[:space:]"'\''])[-A-Za-z0-9_.]+/[-A-Za-z0-9_.]+($|[[:space:]"'\'',])'; then
	die "input looks like private data; raw locators, transcripts, and private identifiers are not accepted"
fi

if ! jq -e '
	def nonempty_string($value): ($value | type == "string" and length > 0);
	def nonneg_number($value): ($value | type == "number" and . >= 0);
	.schema_version == 1 and
	nonempty_string(.proof_id) and
	(.proof_id | test("^[A-Za-z0-9._-]+$")) and
	nonempty_string(.generated_at) and
	(.generated_at | test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$")) and
	(.mode | IN("local", "remote_compose", "kubernetes")) and
	(.auth | type == "object") and
	(.policy | type == "object") and
	(.parity | type == "object") and
	(.redaction | type == "object") and
	(.audit | type == "object") and
	(.proof_gates | type == "object") and
	(.security | type == "object") and
	nonneg_number(.parity.checked_count) and
	nonneg_number(.parity.mismatch_count) and
	nonneg_number(.redaction.canary_count) and
	nonneg_number(.redaction.forbidden_surface_count) and
	nonneg_number(.audit.aggregate_event_count) and
	nonneg_number(.audit.denied_decision_count)
' "${input}" >/dev/null; then
	die "input root shape must include schema version, generated timestamp, auth, policy, parity, redaction, audit, proof_gates, and security objects"
fi

auth_error="$(jq -r '
	select((.auth.unauthenticated_status != "pass") or (.auth.permission_denied_status != "pass") or (.auth.allowed_in_scope_status != "pass"))
	| "unauthenticated=\(.auth.unauthenticated_status // "missing") permission_denied=\(.auth.permission_denied_status // "missing") allowed_in_scope=\(.auth.allowed_in_scope_status // "missing")"
' "${input}")"
[[ -z "${auth_error}" ]] || die "auth proof must pass unauthenticated, permission_denied, and allowed in-scope checks: ${auth_error}"

policy_error="$(jq -r '
	select((.policy.policy_disabled_status != "pass") or (.policy.policy_enforcing_status != "pass") or (.policy.denied_egress_status != "pass"))
	| "policy_disabled=\(.policy.policy_disabled_status // "missing") policy_enforcing=\(.policy.policy_enforcing_status // "missing") denied_egress=\(.policy.denied_egress_status // "missing")"
' "${input}")"
[[ -z "${policy_error}" ]] || die "policy proof must pass disabled, enforcing, and denied-egress checks: ${policy_error}"

if ! jq -e '
	(.policy.reason_classes | type == "array") and
	(.policy.reason_classes | length > 0) and
	([.policy.reason_classes[]? | type == "string" and test("^[a-z0-9_]+$")] | all)
' "${input}" >/dev/null; then
	die "policy reason classes must be public-safe low-cardinality strings"
fi

parity_error="$(jq -r '
	select((.parity.api_status != "pass") or (.parity.mcp_status != "pass") or (.parity.agreement_status != "pass") or (.parity.checked_count <= 0) or (.parity.mismatch_count != 0))
	| "api=\(.parity.api_status // "missing") mcp=\(.parity.mcp_status // "missing") agreement=\(.parity.agreement_status // "missing") checked=\(.parity.checked_count // "missing") mismatches=\(.parity.mismatch_count // "missing")"
' "${input}")"
[[ -z "${parity_error}" ]] || die "API/MCP parity proof must pass with zero mismatches: ${parity_error}"

redaction_error="$(jq -r '
	select((.redaction.status != "pass") or (.redaction.canary_count <= 0) or (.redaction.forbidden_surface_count != 0))
	| "status=\(.redaction.status // "missing") canaries=\(.redaction.canary_count // "missing") forbidden_surfaces=\(.redaction.forbidden_surface_count // "missing")"
' "${input}")"
[[ -z "${redaction_error}" ]] || die "redaction proof must pass with canaries and zero forbidden surfaces: ${redaction_error}"

audit_error="$(jq -r '
	select((.audit.status != "pass") or (.audit.aggregate_event_count <= 0) or (.audit.raw_event_body_exported != false))
	| "status=\(.audit.status // "missing") aggregate_events=\(.audit.aggregate_event_count // "missing") raw_event_body_exported=\(.audit.raw_event_body_exported // "missing")"
' "${input}")"
[[ -z "${audit_error}" ]] || die "audit proof must use aggregate events only: ${audit_error}"

gate_error="$(jq -r '
	select((.proof_gates.local_governance_status != "pass") or (.proof_gates.remote_compose_render_status != "pass") or (.proof_gates.remote_compose_runtime_status != "pass") or (.proof_gates.helm_render_status != "pass"))
	| "local=\(.proof_gates.local_governance_status // "missing") remote_render=\(.proof_gates.remote_compose_render_status // "missing") remote_runtime=\(.proof_gates.remote_compose_runtime_status // "missing") helm=\(.proof_gates.helm_render_status // "missing")"
' "${input}")"
[[ -z "${gate_error}" ]] || die "proof gates must pass local, remote render, remote runtime, and Helm render checks: ${gate_error}"

security_error="$(jq -r '
	select((.security.secret_scan != "passed") or (.security.private_locator_scan != "passed") or (.security.public_artifact_review != "passed"))
	| "secret_scan=\(.security.secret_scan // "missing") private_locator_scan=\(.security.private_locator_scan // "missing") public_artifact_review=\(.security.public_artifact_review // "missing")"
' "${input}")"
[[ -z "${security_error}" ]] || die "security review did not pass: ${security_error}"

tmp_json="${output_json}.tmp"
tmp_markdown="${output_markdown}.tmp"
mkdir -p "$(dirname "${output_json}")" "$(dirname "${output_markdown}")"

jq '{
	status: "pass",
	schema_version: 1,
	proof_id: .proof_id,
	generated_at: .generated_at,
	mode: .mode,
	auth: {
		unauthenticated_status: .auth.unauthenticated_status,
		permission_denied_status: .auth.permission_denied_status,
		allowed_in_scope_status: .auth.allowed_in_scope_status
	},
	policy: {
		policy_disabled_status: .policy.policy_disabled_status,
		policy_enforcing_status: .policy.policy_enforcing_status,
		denied_egress_status: .policy.denied_egress_status,
		reason_classes: .policy.reason_classes
	},
	parity: {
		api_status: .parity.api_status,
		mcp_status: .parity.mcp_status,
		agreement_status: .parity.agreement_status,
		checked_count: .parity.checked_count,
		mismatch_count: .parity.mismatch_count
	},
	redaction: {
		status: .redaction.status,
		canary_count: .redaction.canary_count,
		forbidden_surface_count: .redaction.forbidden_surface_count
	},
	audit: {
		status: .audit.status,
		aggregate_event_count: .audit.aggregate_event_count,
		denied_decision_count: .audit.denied_decision_count,
		raw_event_body_exported: .audit.raw_event_body_exported
	},
	proof_gates: {
		local_governance_status: .proof_gates.local_governance_status,
		remote_compose_render_status: .proof_gates.remote_compose_render_status,
		remote_compose_runtime_status: .proof_gates.remote_compose_runtime_status,
		helm_render_status: .proof_gates.helm_render_status
	},
	security: {
		secret_scan: .security.secret_scan,
		private_locator_scan: .security.private_locator_scan,
		public_artifact_review: .security.public_artifact_review
	}
}' "${input}" >"${tmp_json}"

jq -r '
	[
		"# Hosted governance proof",
		"",
		"- Status: pass",
		"- Proof ID: \(.proof_id)",
		"- Generated at: \(.generated_at)",
		"- Mode: \(.mode)",
		"- Auth: unauthenticated=\(.auth.unauthenticated_status), permission_denied=\(.auth.permission_denied_status), allowed_in_scope=\(.auth.allowed_in_scope_status)",
		"- Policy: disabled=\(.policy.policy_disabled_status), enforcing=\(.policy.policy_enforcing_status), denied_egress=\(.policy.denied_egress_status)",
		"- API/MCP parity: checked=\(.parity.checked_count), mismatches=\(.parity.mismatch_count)",
		"- Redaction: canaries=\(.redaction.canary_count), forbidden_surfaces=\(.redaction.forbidden_surface_count)",
		"- Audit: aggregate_events=\(.audit.aggregate_event_count), denied_decisions=\(.audit.denied_decision_count)",
		"- Proof gates: local=\(.proof_gates.local_governance_status), remote_render=\(.proof_gates.remote_compose_render_status), remote_runtime=\(.proof_gates.remote_compose_runtime_status), helm=\(.proof_gates.helm_render_status)",
		"",
		"Raw tokens, tenants, repositories, source payloads, prompts, provider responses, logs, URLs, hostnames, and private locators remain operator-local."
	] | .[]
' "${input}" >"${tmp_markdown}"

mv "${tmp_json}" "${output_json}"
mv "${tmp_markdown}" "${output_markdown}"
printf 'verify-hosted-governance-proof-artifact: wrote %s and %s\n' "${output_json}" "${output_markdown}"
