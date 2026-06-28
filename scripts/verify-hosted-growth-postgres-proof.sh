#!/usr/bin/env bash
# Validate public-safe hosted-growth Postgres proof evidence.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
source "${repo_root}/scripts/lib/hosted_growth_postgres_proof.sh"

input=""
output_json=""
output_markdown=""

usage() {
	cat <<USAGE
Usage: $(basename "$0") --input <proof.json> --output-json <summary.json> --output-markdown <summary.md>

The input is an operator-local hosted-growth Postgres proof. Raw repositories,
hostnames, IPs, paths, DSNs, logs, source payloads, principals, accounts, and
credentials must stay outside the repository and outside generated summaries.
USAGE
}

die() {
	printf 'verify-hosted-growth-postgres-proof: %s\n' "$*" >&2
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

forbidden_keys='["url","uri","host","hostname","ip","address","path","file","repository","repo","repo_id","repo_name","tenant","tenant_id","workspace","workspace_id","account","account_id","token","secret","credential","password","dsn","signed_url","payload","request","response","prompt","stdout","stderr","transcript","log","logs","principal","source_id","source_identifier"]'
if ! jq -e --argjson forbidden "${forbidden_keys}" '
	[.. | objects | keys[]? | select(. as $key | $forbidden | index($key))]
	| length == 0
' "${input}" >/dev/null; then
	die "input looks like private data; use aggregate relation names, counts, states, and low-cardinality classes only"
fi

if jq -r '.. | strings' "${input}" | rg --quiet --ignore-case \
	'g(hp_|ithub_pat_)|glpat-|A(KIA|SIA)|xox[baprs]-|https?://|bolt://|postgres(ql)?://|arn:(aws|aws-us-gov|aws-cn):|/(Users|home|private|var|tmp|Volumes|workspace|workspaces|repos|personal-repos)/|(^|[^0-9])[0-9]{12}([^0-9]|$)|([0-9]{1,3}\.){3}[0-9]{1,3}|[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}|(^|[[:space:]"'\''])[-A-Za-z0-9_.]+/[-A-Za-z0-9_.]+($|[[:space:]"'\'',])'; then
	die "input looks like private data; raw locators, transcripts, and private identifiers are not accepted"
fi

required_relations='["fact_records","fact_work_items","shared_projection_intents","shared_projection_acceptance"]'
if ! jq -e --argjson required "${required_relations}" '
	def names: [.relations[]?.relation];
	(names | length) == ($required | length) and
	((names - $required) | length) == 0 and
	(($required - names) | length) == 0
' "${input}" >/dev/null; then
	missing="$(jq -r --argjson required "${required_relations}" '[($required - [.relations[]?.relation])[]] | join(", ")' "${input}")"
	unknown="$(jq -r --argjson required "${required_relations}" '([.relations[]?.relation] - $required) | join(", ")' "${input}")"
	[[ -z "${missing}" ]] || die "missing required relations: ${missing}"
	die "unknown relations: ${unknown}"
fi

required_scenarios='["empty_table","large_table","old_generation","stale_rows","active_claim","retry_dead_letter","rollback"]'
if ! jq -e --argjson required "${required_scenarios}" '
	def names: [.migration.scenarios[]?.scenario];
	(names | length) == ($required | length) and
	((names - $required) | length) == 0 and
	(($required - names) | length) == 0
' "${input}" >/dev/null; then
	missing="$(jq -r --argjson required "${required_scenarios}" '[($required - [.migration.scenarios[]?.scenario])[]] | join(", ")' "${input}")"
	unknown="$(jq -r --argjson required "${required_scenarios}" '([.migration.scenarios[]?.scenario] - $required) | join(", ")' "${input}")"
	[[ -z "${missing}" ]] || die "missing required migration scenarios: ${missing}"
	die "unknown migration scenarios: ${unknown}"
fi

if ! jq -e '
	def nonempty_string($value): ($value | type == "string" and length > 0);
	.schema_version == 1 and
	nonempty_string(.proof_id) and
	(.proof_id | test("^hosted-growth-postgres-proof-(test|[0-9]{8}T[0-9]{6}Z)$")) and
	nonempty_string(.generated_at) and
	(.generated_at | test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$")) and
	nonempty_string(.eshu_commit) and
	(.eshu_commit | test("^[0-9a-f]{7,40}$")) and
	.profile == "hosted_growth" and
	(.relations | type == "array") and
	(.queue_drain | type == "object") and
	(.migration | type == "object") and
	(.gate | type == "object") and
	(.observability | type == "object") and
	(.security | type == "object") and
	.verdict == "pass" and
	.failure_class == "none"
' "${input}" >/dev/null; then
	die "input root shape must include schema version, proof id, generated timestamp, eshu commit, hosted_growth profile, relations, queue_drain, migration, gate, observability, security, pass verdict, and none failure class"
fi

if ! jq -e '
	[
		.relations[] as $relation |
		($relation.row_count | type == "number" and . > 0) and
		($relation.index_bytes | type == "number" and . > 0) and
		($relation.total_bytes | type == "number" and . >= $relation.index_bytes) and
		($relation.read_p95_ns | type == "number" and . > 0) and
		($relation.write_p95_ns | type == "number" and . > 0)
	] | all
' "${input}" >/dev/null; then
	die "relation proof must include positive row_count, index_bytes, total_bytes, read_p95_ns, and write_p95_ns"
fi

if ! jq -e '
	[.relations[] | (.observed_at | type == "string" and length > 0)] | all
' "${input}" >/dev/null; then
	die "relation proof must include observed_at for every measurement"
fi

if ! jq -e '
	[.relations[] | .bounded_evidence == true] | all
' "${input}" >/dev/null; then
	die "relation proof must include bounded evidence for every measurement"
fi

if ! jq -e '.queue_drain.queue_surface == "reducer"' "${input}" >/dev/null; then
	die "queue drain surface must be reducer"
fi

if ! jq -e '
	(.queue_drain.pending_rows | type == "number" and . >= 0) and
	(.queue_drain.retry_rows | type == "number" and . > 0) and
	(.queue_drain.dead_letter_rows | type == "number" and . > 0) and
	(.queue_drain.stale_rows | type == "number" and . > 0) and
	(.queue_drain.claimed_rows | type == "number" and . > 0) and
	(.queue_drain.completed_rows | type == "number" and . > 0) and
	(.queue_drain.failed_rows | type == "number" and . >= 0) and
	(.queue_drain.oldest_age_ns | type == "number" and . > 0) and
	(.queue_drain.drain_duration_ns | type == "number" and . > 0) and
	(.queue_drain.worker_count | type == "number" and . > 0)
' "${input}" >/dev/null; then
	die "queue drain must include retry, dead-letter, stale, claimed, and completed rows plus positive age, duration, and worker count"
fi

if ! jq -e '(.queue_drain.observed_at | type == "string" and length > 0)' "${input}" >/dev/null; then
	die "queue drain proof must include observed_at"
fi

if ! jq -e '.queue_drain.bounded_evidence == true' "${input}" >/dev/null; then
	die "queue drain proof must include bounded evidence"
fi

validate_hosted_growth_breakpoint_evidence "${input}"

if ! jq -e '
	(.migration.native_partitioning == false or
		(.migration.primary_key_includes_partition_key == true and .migration.unique_constraints_include_partition_key == true)) and
	.migration.active_generation_read_correct == true and
	.migration.changed_since_retained_window_correct == true and
	.migration.deletes_active_work == false and
	.migration.retries_active_work == false and
	(.migration.rollback_behavior | IN("keep_current_postgres", "discard_candidate", "fail_closed")) and
	([.migration.scenarios[] | .status == "passed"] | all) and
	(.migration.post_migration_read_p95_ns | type == "number" and . > 0) and
	(.migration.post_migration_write_p95_ns | type == "number" and . > 0) and
	(.migration.post_migration_queue_claim_p95_ns | type == "number" and . > 0) and
	(.migration.post_migration_queue_drain_duration_ns | type == "number" and . > 0) and
	(.migration.post_migration_active_generation_rows | type == "number" and . > 0) and
	(.migration.post_migration_changed_since_retained_rows | type == "number" and . > 0) and
	(.migration.post_migration_active_claim_rows_preserved | type == "number" and . > 0) and
	(.migration.post_migration_retry_rows_preserved | type == "number" and . > 0) and
	(.migration.post_migration_dead_letter_rows_preserved | type == "number" and . > 0) and
	(.migration.post_migration_stale_rows_classified | type == "number" and . > 0)
' "${input}" >/dev/null; then
	if jq -e '.migration.native_partitioning == true and (.migration.primary_key_includes_partition_key != true or .migration.unique_constraints_include_partition_key != true)' "${input}" >/dev/null; then
		die "native partitioning proof must include partition keys in primary and unique constraints"
	fi
	if jq -e '.migration.deletes_active_work == true or .migration.retries_active_work == true' "${input}" >/dev/null; then
		die "migration must preserve active and retrying work"
	fi
	die "migration proof must pass with active-generation, changed-since, rollback, latency, row-preservation, and queue-drain evidence"
fi

if ! jq -e '
	.gate.from_profile == "hosted_small" and
	.gate.to_profile == "hosted_growth" and
	(.gate.fact_rows_threshold | type == "number" and . > 0) and
	(.gate.queue_rows_threshold | type == "number" and . > 0) and
	(.gate.index_bytes_threshold | type == "number" and . > 0) and
	(.gate.oldest_queue_age_threshold_ns | type == "number" and . > 0) and
	.gate.recommended_action == "run_hosted_growth_postgres_proof" and
	.gate.operator_status_signal == "admin_status_relation_queue_summary" and
	.gate.requires_migration_window == true and
	.gate.requires_rollback_artifact == true
' "${input}" >/dev/null; then
	die "operator gate must target hosted_growth with positive thresholds, public-safe labels, migration window, and rollback artifact"
fi

validate_hosted_growth_observability "${input}"

if ! jq -e '
	.security.secret_scan == "passed" and
	.security.private_locator_scan == "passed" and
	.security.public_artifact_review == "passed"
' "${input}" >/dev/null; then
	die "security review did not pass"
fi

tmp_json="${output_json}.tmp"
tmp_markdown="${output_markdown}.tmp"
mkdir -p "$(dirname "${output_json}")" "$(dirname "${output_markdown}")"

write_hosted_growth_summary "${input}" "${tmp_json}" "${tmp_markdown}"

mv "${tmp_json}" "${output_json}"
mv "${tmp_markdown}" "${output_markdown}"
printf 'verify-hosted-growth-postgres-proof: wrote %s and %s\n' "${output_json}" "${output_markdown}"
