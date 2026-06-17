#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-hosted-growth-postgres-proof.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

if [[ ! -f "${verifier}" ]]; then
	printf 'missing verifier: %s\n' "${verifier}" >&2
	exit 1
fi

bash -n "${verifier}"

write_safe_proof() {
	local output="$1"
	jq -n '
		{
			schema_version: 1,
			proof_id: "hosted-growth-postgres-proof-test",
			generated_at: "2026-06-17T03:00:00Z",
			eshu_commit: "commit-2749",
			profile: "hosted_growth",
			relations: [
				{relation: "fact_records", row_count: 3200000, index_bytes: 409600000, total_bytes: 1228800000, read_p95_ns: 75000000, write_p95_ns: 95000000},
				{relation: "fact_work_items", row_count: 180000, index_bytes: 23040000, total_bytes: 69120000, read_p95_ns: 40000000, write_p95_ns: 55000000},
				{relation: "shared_projection_intents", row_count: 45000, index_bytes: 5760000, total_bytes: 17280000, read_p95_ns: 25000000, write_p95_ns: 35000000},
				{relation: "shared_projection_acceptance", row_count: 43000, index_bytes: 5504000, total_bytes: 16512000, read_p95_ns: 25000000, write_p95_ns: 35000000}
			],
			queue_drain: {
				queue_surface: "reducer",
				pending_rows: 4000,
				retry_rows: 12,
				dead_letter_rows: 2,
				stale_rows: 8,
				claimed_rows: 16,
				completed_rows: 3600,
				failed_rows: 3,
				oldest_age_ns: 300000000000,
				drain_duration_ns: 720000000000,
				worker_count: 8
			},
			migration: {
				native_partitioning: false,
				primary_key_includes_partition_key: true,
				unique_constraints_include_partition_key: true,
				active_generation_read_correct: true,
				changed_since_retained_window_correct: true,
				deletes_active_work: false,
				retries_active_work: false,
				rollback_behavior: "keep_current_postgres",
				scenarios: [
					{scenario: "empty_table", status: "passed"},
					{scenario: "large_table", status: "passed"},
					{scenario: "old_generation", status: "passed"},
					{scenario: "stale_rows", status: "passed"},
					{scenario: "active_claim", status: "passed"},
					{scenario: "retry_dead_letter", status: "passed"},
					{scenario: "rollback", status: "passed"}
				],
				post_migration_read_p95_ns: 120000000,
				post_migration_write_p95_ns: 150000000,
				post_migration_queue_claim_p95_ns: 80000000,
				post_migration_queue_drain_duration_ns: 600000000000,
				post_migration_active_generation_rows: 3200000,
				post_migration_changed_since_retained_rows: 55000,
				post_migration_active_claim_rows_preserved: 16,
				post_migration_retry_rows_preserved: 12,
				post_migration_dead_letter_rows_preserved: 2,
				post_migration_stale_rows_classified: 8
			},
			gate: {
				from_profile: "hosted_small",
				to_profile: "hosted_growth",
				fact_rows_threshold: 2000000,
				queue_rows_threshold: 100000,
				index_bytes_threshold: 10737418240,
				oldest_queue_age_threshold_ns: 900000000000,
				recommended_action: "run_hosted_growth_postgres_proof",
				operator_status_signal: "admin_status_relation_queue_summary",
				requires_migration_window: true,
				requires_rollback_artifact: true
			},
			observability: {
				relation_size: true,
				index_size: true,
				read_latency: true,
				write_latency: true,
				queue_depth: true,
				oldest_age: true,
				retry_count: true,
				dead_letters: true,
				stale_rows: true,
				active_claims: true,
				migration_duration: true,
				rollback_status: true
			},
			security: {
				secret_scan: "passed",
				private_locator_scan: "passed",
				public_artifact_review: "passed"
			},
			verdict: "pass",
			failure_class: "none"
		}
	' >"${output}"
}

expect_pass() {
	local label="$1"
	local input="$2"
	local out_json="${tmp_dir}/${label}.summary.json"
	local out_md="${tmp_dir}/${label}.summary.md"
	if ! "${verifier}" --input "${input}" --output-json "${out_json}" --output-markdown "${out_md}" >"${tmp_dir}/${label}.out" 2>"${tmp_dir}/${label}.err"; then
		printf 'expected %s to pass\n' "${label}" >&2
		sed -n '1,120p' "${tmp_dir}/${label}.err" >&2
		exit 1
	fi
	jq -e '
		.status == "pass" and
		.proof_id == "hosted-growth-postgres-proof-test" and
		.relation_count == 4 and
		.total_row_count == 3468000 and
		.queue_drain.retry_rows == 12 and
		.gate.to_profile == "hosted_growth"
	' "${out_json}" >/dev/null
	rg --fixed-strings --quiet 'Hosted-growth Postgres proof' "${out_md}"
	rg --fixed-strings --quiet 'Raw repositories, hostnames, IPs' "${out_md}"
}

expect_fail() {
	local label="$1"
	local input="$2"
	local expected="$3"
	local out_json="${tmp_dir}/${label}.summary.json"
	local out_md="${tmp_dir}/${label}.summary.md"
	if "${verifier}" --input "${input}" --output-json "${out_json}" --output-markdown "${out_md}" >"${tmp_dir}/${label}.out" 2>"${tmp_dir}/${label}.err"; then
		printf 'expected %s to fail\n' "${label}" >&2
		exit 1
	fi
	rg --fixed-strings --quiet -- "${expected}" "${tmp_dir}/${label}.err" || {
		printf 'expected %s failure to include %s\n' "${label}" "${expected}" >&2
		sed -n '1,120p' "${tmp_dir}/${label}.err" >&2
		exit 1
	}
}

safe_input="${tmp_dir}/safe.json"
write_safe_proof "${safe_input}"
expect_pass safe "${safe_input}"

missing_relation="${tmp_dir}/missing-relation.json"
jq 'del(.relations[] | select(.relation == "fact_records"))' "${safe_input}" >"${missing_relation}"
expect_fail missing_relation "${missing_relation}" "missing required relations: fact_records"

missing_queue="${tmp_dir}/missing-queue.json"
jq '.queue_drain.retry_rows = 0' "${safe_input}" >"${missing_queue}"
expect_fail missing_queue "${missing_queue}" "queue drain must include retry, dead-letter, stale, claimed, and completed rows"

missing_scenario="${tmp_dir}/missing-scenario.json"
jq 'del(.migration.scenarios[] | select(.scenario == "rollback"))' "${safe_input}" >"${missing_scenario}"
expect_fail missing_scenario "${missing_scenario}" "missing required migration scenarios: rollback"

unsafe_partition="${tmp_dir}/unsafe-partition.json"
jq '.migration.native_partitioning = true | .migration.unique_constraints_include_partition_key = false' "${safe_input}" >"${unsafe_partition}"
expect_fail unsafe_partition "${unsafe_partition}" "native partitioning proof must include partition keys in primary and unique constraints"

active_mutation="${tmp_dir}/active-mutation.json"
jq '.migration.deletes_active_work = true' "${safe_input}" >"${active_mutation}"
expect_fail active_mutation "${active_mutation}" "migration must preserve active and retrying work"

bad_gate="${tmp_dir}/bad-gate.json"
jq '.gate.to_profile = "hosted_small"' "${safe_input}" >"${bad_gate}"
expect_fail bad_gate "${bad_gate}" "operator gate must target hosted_growth"

private_input="${tmp_dir}/private.json"
jq '.proof_id = ("10" + ".0.0.1")' "${safe_input}" >"${private_input}"
expect_fail private "${private_input}" "input looks like private data"

raw_input="${tmp_dir}/raw.json"
jq '.security.private_locator_scan = "failed"' "${safe_input}" >"${raw_input}"
expect_fail raw "${raw_input}" "security review did not pass"

unknown_summary="${tmp_dir}/unknown-summary.json"
jq '.queue_drain.cluster_name = "safecluster" | .gate.cluster_name = "safecluster"' "${safe_input}" >"${unknown_summary}"
expect_pass unknown_summary "${unknown_summary}"
unknown_out_json="${tmp_dir}/unknown_summary.summary.json"
if jq -e '.queue_drain.cluster_name? // .gate.cluster_name?' "${unknown_out_json}" >/dev/null; then
	printf 'expected summary output to omit unknown queue/gate fields\n' >&2
	jq . "${unknown_out_json}" >&2
	exit 1
fi

printf 'hosted-growth postgres proof tests passed\n'
