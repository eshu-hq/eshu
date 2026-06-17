#!/usr/bin/env bash
# Validate public-safe hosted-growth Postgres proof evidence.

set -euo pipefail

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
	(.proof_id | test("^[A-Za-z0-9._-]+$")) and
	nonempty_string(.generated_at) and
	(.generated_at | test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$")) and
	nonempty_string(.eshu_commit) and
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
	.queue_drain.queue_surface == "reducer" and
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
	(.gate.recommended_action | type == "string" and length > 0) and
	(.gate.operator_status_signal | type == "string" and length > 0) and
	.gate.requires_migration_window == true and
	.gate.requires_rollback_artifact == true
' "${input}" >/dev/null; then
	die "operator gate must target hosted_growth with positive thresholds, status signal, migration window, and rollback artifact"
fi

if ! jq -e '
	[
		.observability.relation_size,
		.observability.index_size,
		.observability.read_latency,
		.observability.write_latency,
		.observability.queue_depth,
		.observability.oldest_age,
		.observability.retry_count,
		.observability.dead_letters,
		.observability.stale_rows,
		.observability.active_claims,
		.observability.migration_duration,
		.observability.rollback_status
	] | all
' "${input}" >/dev/null; then
	die "observability proof must cover relation/index sizes, latency, queue depth, age, retries, dead letters, stale rows, active claims, migration duration, and rollback status"
fi

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

jq '{
	status: "pass",
	schema_version: 1,
	proof_id: .proof_id,
	generated_at: .generated_at,
	eshu_commit: .eshu_commit,
	profile: .profile,
	relation_count: (.relations | length),
	total_row_count: ([.relations[].row_count] | add),
	total_index_bytes: ([.relations[].index_bytes] | add),
	relations: [.relations[] | {relation, row_count, index_bytes, total_bytes, read_p95_ns, write_p95_ns}],
	queue_drain: {
		queue_surface: .queue_drain.queue_surface,
		pending_rows: .queue_drain.pending_rows,
		retry_rows: .queue_drain.retry_rows,
		dead_letter_rows: .queue_drain.dead_letter_rows,
		stale_rows: .queue_drain.stale_rows,
		claimed_rows: .queue_drain.claimed_rows,
		completed_rows: .queue_drain.completed_rows,
		failed_rows: .queue_drain.failed_rows,
		oldest_age_ns: .queue_drain.oldest_age_ns,
		drain_duration_ns: .queue_drain.drain_duration_ns,
		worker_count: .queue_drain.worker_count
	},
	migration: {
		native_partitioning: .migration.native_partitioning,
		active_generation_read_correct: .migration.active_generation_read_correct,
		changed_since_retained_window_correct: .migration.changed_since_retained_window_correct,
		rollback_behavior: .migration.rollback_behavior,
		scenarios: [.migration.scenarios[] | {scenario, status}],
		post_migration_read_p95_ns: .migration.post_migration_read_p95_ns,
		post_migration_write_p95_ns: .migration.post_migration_write_p95_ns,
		post_migration_queue_claim_p95_ns: .migration.post_migration_queue_claim_p95_ns,
		post_migration_queue_drain_duration_ns: .migration.post_migration_queue_drain_duration_ns,
		post_migration_active_generation_rows: .migration.post_migration_active_generation_rows,
		post_migration_changed_since_retained_rows: .migration.post_migration_changed_since_retained_rows,
		post_migration_active_claim_rows_preserved: .migration.post_migration_active_claim_rows_preserved,
		post_migration_retry_rows_preserved: .migration.post_migration_retry_rows_preserved,
		post_migration_dead_letter_rows_preserved: .migration.post_migration_dead_letter_rows_preserved,
		post_migration_stale_rows_classified: .migration.post_migration_stale_rows_classified
	},
	gate: {
		from_profile: .gate.from_profile,
		to_profile: .gate.to_profile,
		fact_rows_threshold: .gate.fact_rows_threshold,
		queue_rows_threshold: .gate.queue_rows_threshold,
		index_bytes_threshold: .gate.index_bytes_threshold,
		oldest_queue_age_threshold_ns: .gate.oldest_queue_age_threshold_ns,
		recommended_action: .gate.recommended_action,
		operator_status_signal: .gate.operator_status_signal,
		requires_migration_window: .gate.requires_migration_window,
		requires_rollback_artifact: .gate.requires_rollback_artifact
	},
	security: {
		secret_scan: .security.secret_scan,
		private_locator_scan: .security.private_locator_scan,
		public_artifact_review: .security.public_artifact_review
	},
	verdict: .verdict,
	failure_class: .failure_class
}' "${input}" >"${tmp_json}"

jq -r '
	[
		"# Hosted-growth Postgres proof",
		"",
		"- Status: \(.status)",
		"- Proof ID: \(.proof_id)",
		"- Generated at: \(.generated_at)",
		"- Profile: \(.profile)",
		"- Relations: \(.relation_count)",
		"- Total rows: \(.total_row_count)",
		"- Total index bytes: \(.total_index_bytes)",
		"- Queue drain: completed=\(.queue_drain.completed_rows), retry=\(.queue_drain.retry_rows), dead_letters=\(.queue_drain.dead_letter_rows), stale=\(.queue_drain.stale_rows)",
		"- Migration: rollback=\(.migration.rollback_behavior), scenarios=\([.migration.scenarios[].scenario] | join(","))",
		"- Gate: \(.gate.from_profile) -> \(.gate.to_profile), fact_rows=\(.gate.fact_rows_threshold), queue_rows=\(.gate.queue_rows_threshold)",
		"",
		"Raw repositories, hostnames, IPs, paths, DSNs, logs, source payloads, principals, accounts, and credentials remain operator-local."
	] | .[]
' "${tmp_json}" >"${tmp_markdown}"

mv "${tmp_json}" "${output_json}"
mv "${tmp_markdown}" "${output_markdown}"
printf 'verify-hosted-growth-postgres-proof: wrote %s and %s\n' "${output_json}" "${output_markdown}"
