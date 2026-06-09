#!/usr/bin/env bash
# Validate public-safe hosted backup, restore, and graph-rebuild proof evidence.

set -euo pipefail

input=""
output_json=""
output_markdown=""
max_backup_age_seconds="${ESHU_HOSTED_BACKUP_MAX_AGE_SECONDS:-86400}"

usage() {
	cat <<USAGE
Usage: $(basename "$0") --input <proof.json> --output-json <summary.json> --output-markdown <summary.md>

The input is an operator-local aggregate proof. Backup contents, raw restore
logs, private locators, signed URLs, hostnames, IP addresses, repository paths,
and credential handles must stay outside the repository.
USAGE
}

die() {
	printf 'verify-hosted-backup-restore-proof: %s\n' "$*" >&2
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
		--max-backup-age-seconds)
			max_backup_age_seconds="${2:-}"
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
[[ "${max_backup_age_seconds}" =~ ^[0-9]+$ ]] || die "--max-backup-age-seconds must be a non-negative integer"
command -v jq >/dev/null 2>&1 || die "jq is required"
command -v rg >/dev/null 2>&1 || die "rg is required"

jq -e . "${input}" >/dev/null 2>&1 || die "input must be valid JSON"

forbidden_keys='["url","uri","host","hostname","ip","address","path","file","repository","repo","repo_id","repo_name","token","secret","credential","password","dsn","signed_url","payload","request","response","stdout","stderr","transcript","log","logs"]'
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
	(.mode | IN("clean_restore", "graph_only_loss")) and
	(.backup | type == "object") and
	(.restore | type == "object") and
	(.graph_rebuild | type == "object") and
	(.parity | type == "object") and
	(.queue | type == "object") and
	(.readback | type == "object") and
	(.security | type == "object") and
	nonneg_number(.backup.age_seconds) and
	(.backup.checksum_present | type == "boolean") and
	(.backup.encrypted | type == "boolean") and
	nonneg_number(.restore.duration_seconds) and
	nonneg_number(.parity.drift_count) and
	nonneg_number(.queue.pending) and
	nonneg_number(.queue.retrying) and
	nonneg_number(.queue.failed) and
	nonneg_number(.queue.dead_letter)
' "${input}" >/dev/null; then
	die "input root shape must include schema version, generated timestamp, backup, restore, graph rebuild, parity, queue, readback, and security objects"
fi

artifact_handle="$(jq -r '.backup.artifact_handle // ""' "${input}")"
[[ -n "${artifact_handle}" ]] || die "backup artifact handle is required"
[[ "${artifact_handle}" =~ ^[A-Za-z0-9._-]+$ ]] || die "backup artifact handle must be a public-safe opaque handle"

age_seconds="$(jq -r '.backup.age_seconds' "${input}")"
if (( age_seconds > max_backup_age_seconds )); then
	die "backup artifact is stale: age_seconds=${age_seconds} max=${max_backup_age_seconds}"
fi

checksum_present="$(jq -r '.backup.checksum_present' "${input}")"
encrypted="$(jq -r '.backup.encrypted' "${input}")"
[[ "${checksum_present}" == "true" ]] || die "backup artifact checksum proof is required"
[[ "${encrypted}" == "true" ]] || die "backup artifact encryption proof is required"

restore_status="$(jq -r '.restore.status // ""' "${input}")"
restore_failure_class="$(jq -r '.restore.failure_class // ""' "${input}")"
restore_scope="$(jq -r '.restore.target_scope_class // ""' "${input}")"
[[ "${restore_status}" == "succeeded" ]] || die "restore did not succeed: failure_class=${restore_failure_class:-unknown}"
[[ "${restore_failure_class}" == "none" ]] || die "restore failure class must be none for a passing proof"
[[ "${restore_scope}" == "isolated_restore_environment" || "${restore_scope}" == "graph_rebuild_environment" ]] \
	|| die "restore target scope class must be public-safe and operator-scoped"

graph_status="$(jq -r '.graph_rebuild.status // ""' "${input}")"
postgres_preserved="$(jq -r '.graph_rebuild.postgres_preserved' "${input}")"
schema_bootstrap_rerun="$(jq -r '.graph_rebuild.schema_bootstrap_rerun' "${input}")"
projection_replayed="$(jq -r '.graph_rebuild.projection_replayed' "${input}")"
full_recollection_explicit="$(jq -r '.graph_rebuild.full_recollection_explicit' "${input}")"
[[ "${graph_status}" == "succeeded" ]] || die "graph rebuild did not succeed"
[[ "${postgres_preserved}" == "true" || "${full_recollection_explicit}" == "true" ]] \
	|| die "graph rebuild must preserve Postgres unless full source recollection is explicit"
[[ "${schema_bootstrap_rerun}" == "true" ]] || die "graph rebuild proof must rerun schema bootstrap"
[[ "${projection_replayed}" == "true" ]] || die "graph rebuild proof must replay projection or equivalent source-derived rebuild work"
if [[ "${full_recollection_explicit}" == "true" && "${restore_scope}" != "isolated_restore_environment" ]]; then
	die "full recollection must be explicit and isolated from graph-only rebuild proof"
fi

parity_status="$(jq -r '.parity.status // ""' "${input}")"
parity_drift="$(jq -r '.parity.drift_count' "${input}")"
[[ "${parity_status}" == "match" && "${parity_drift}" == "0" ]] \
	|| die "restore parity drift is not acceptable: status=${parity_status} drift_count=${parity_drift}"

queue_error="$(jq -r '
	select((.queue.pending != 0) or (.queue.retrying != 0) or (.queue.failed != 0) or (.queue.dead_letter != 0))
	| "pending=\(.queue.pending) retrying=\(.queue.retrying) failed=\(.queue.failed) dead_letter=\(.queue.dead_letter)"
' "${input}")"
[[ -z "${queue_error}" ]] || die "queue terminal state is not zero: ${queue_error}"

readback_error="$(jq -r '
	select((.readback.api_status != "pass") or (.readback.mcp_status != "pass") or (.readback.first_query_status != "pass"))
	| "api=\(.readback.api_status // "missing") mcp=\(.readback.mcp_status // "missing") first_query=\(.readback.first_query_status // "missing")"
' "${input}")"
[[ -z "${readback_error}" ]] || die "API and MCP readback must pass: ${readback_error}"

security_error="$(jq -r '
	select((.security.artifact_contents_platform_owned != true) or (.security.secret_scan != "passed") or (.security.private_locator_scan != "passed"))
	| "artifact_contents_platform_owned=\(.security.artifact_contents_platform_owned) secret_scan=\(.security.secret_scan // "missing") private_locator_scan=\(.security.private_locator_scan // "missing")"
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
	backup: {
		age_seconds: .backup.age_seconds,
		checksum_present: .backup.checksum_present,
		encrypted: .backup.encrypted
	},
	restore: {
		status: .restore.status,
		duration_seconds: .restore.duration_seconds,
		failure_class: .restore.failure_class,
		target_scope_class: .restore.target_scope_class
	},
	graph_rebuild: {
		status: .graph_rebuild.status,
		postgres_preserved: .graph_rebuild.postgres_preserved,
		schema_bootstrap_rerun: .graph_rebuild.schema_bootstrap_rerun,
		projection_replayed: .graph_rebuild.projection_replayed,
		full_recollection_explicit: .graph_rebuild.full_recollection_explicit
	},
	parity: .parity,
	queue: .queue,
	readback: .readback,
	security: .security
}' "${input}" >"${tmp_json}"

jq -r '
	[
		"# Hosted backup and restore proof",
		"",
		"- Status: pass",
		"- Proof ID: \(.proof_id)",
		"- Generated at: \(.generated_at)",
		"- Mode: \(.mode)",
		"- Backup age seconds: \(.backup.age_seconds)",
		"- Restore duration seconds: \(.restore.duration_seconds)",
		"- Restore failure class: \(.restore.failure_class)",
		"- Parity drift count: \(.parity.drift_count)",
		"- Queue terminal state: pending=\(.queue.pending), retrying=\(.queue.retrying), failed=\(.queue.failed), dead_letter=\(.queue.dead_letter)",
		"- API/MCP readback: api=\(.readback.api_status), mcp=\(.readback.mcp_status), first_query=\(.readback.first_query_status)",
		"",
		"Backup contents and private deployment locators remain platform-owned."
	] | .[]
' "${input}" >"${tmp_markdown}"

mv "${tmp_json}" "${output_json}"
mv "${tmp_markdown}" "${output_markdown}"
printf 'verify-hosted-backup-restore-proof: wrote %s and %s\n' "${output_json}" "${output_markdown}"
