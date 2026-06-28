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
			eshu_commit: "0123456789abcdef0123456789abcdef01234567",
			profile: "hosted_growth",
			relations: [
				{relation: "fact_records", row_count: 3200000, index_bytes: 409600000, total_bytes: 1228800000, read_p95_ns: 75000000, write_p95_ns: 95000000, observed_at: "2026-06-17T03:00:00Z", bounded_evidence: true},
				{relation: "fact_work_items", row_count: 180000, index_bytes: 23040000, total_bytes: 69120000, read_p95_ns: 40000000, write_p95_ns: 55000000, observed_at: "2026-06-17T03:00:00Z", bounded_evidence: true},
				{relation: "shared_projection_intents", row_count: 45000, index_bytes: 5760000, total_bytes: 17280000, read_p95_ns: 25000000, write_p95_ns: 35000000, observed_at: "2026-06-17T03:00:00Z", bounded_evidence: true},
				{relation: "shared_projection_acceptance", row_count: 43000, index_bytes: 5504000, total_bytes: 16512000, read_p95_ns: 25000000, write_p95_ns: 35000000, observed_at: "2026-06-17T03:00:00Z", bounded_evidence: true}
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
				worker_count: 8,
				observed_at: "2026-06-17T03:00:00Z",
				bounded_evidence: true
			},
			fact_growth: {
				model_version: "fact_records_growth_v1",
				rows_per_second: 1200,
				before: {fact_records_rows: 2100000, index_bytes: 268435456, total_bytes: 805306368, observed_at: "2026-06-17T02:00:00Z", bounded_evidence: true},
				after: {fact_records_rows: 3200000, index_bytes: 409600000, total_bytes: 1228800000, observed_at: "2026-06-17T03:00:00Z", bounded_evidence: true},
				families: [
					{family: "collector", fact_kind_count: 40, before_rows: 900000, after_rows: 1400000, after_index_bytes: 179200000, write_amplification_ratio: 1.35, p95_insert_ns: 85000000, bounded_evidence: true},
					{family: "parser", fact_kind_count: 17, before_rows: 650000, after_rows: 950000, after_index_bytes: 121600000, write_amplification_ratio: 1.25, p95_insert_ns: 78000000, bounded_evidence: true},
					{family: "search_documents", fact_kind_count: 3, before_rows: 300000, after_rows: 450000, after_index_bytes: 57600000, write_amplification_ratio: 1.18, p95_insert_ns: 70000000, bounded_evidence: true},
					{family: "correlation", fact_kind_count: 8, before_rows: 250000, after_rows: 400000, after_index_bytes: 51200000, write_amplification_ratio: 1.42, p95_insert_ns: 90000000, bounded_evidence: true}
				]
			},
			index_bloat: {
				table_bloat_ratio: 0.18,
				dead_tuple_bytes: 73400320,
				indexes: [
					{index_class: "active_generation", size_bytes: 134217728, bloat_ratio: 0.16, write_amplification_ratio: 1.31, bounded_evidence: true},
					{index_class: "correlation_lookup", size_bytes: 94371840, bloat_ratio: 0.19, write_amplification_ratio: 1.44, bounded_evidence: true}
				]
			},
			graph_write_pressure: {
				write_p95_ns: 125000000,
				timeout_retries: 3,
				retrying_graph_write_timeout_rows: 6,
				dead_letter_rows: 0,
				p95_group_rows: 180,
				observed_at: "2026-06-17T03:00:00Z",
				bounded_evidence: true
			},
			query_plans: [
				{query_class: "active_generation_read", p95_ns: 65000000, rows_examined: 18000, plan_status: "indexed", seq_scan: false, spill: false, observed_at: "2026-06-17T03:00:00Z", bounded_evidence: true},
				{query_class: "correlation_join", p95_ns: 85000000, rows_examined: 22000, plan_status: "indexed", seq_scan: false, spill: false, observed_at: "2026-06-17T03:00:00Z", bounded_evidence: true},
				{query_class: "retention_changed_since", p95_ns: 72000000, rows_examined: 12000, plan_status: "indexed", seq_scan: false, spill: false, observed_at: "2026-06-17T03:00:00Z", bounded_evidence: true},
				{query_class: "hot_api_read", p95_ns: 76000000, rows_examined: 16000, plan_status: "indexed", seq_scan: false, spill: false, observed_at: "2026-06-17T03:00:00Z", bounded_evidence: true}
			],
			retention: {
				superseded_rows: 420000,
				oldest_superseded_age_ns: 172800000000000,
				retention_lag_ns: 3600000000000,
				prune_duration_ns: 48000000000,
				prune_batch_rows: 5000,
				archive_required: false,
				bounded_evidence: true
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
			decision: {
				recommendation: "retention_tune",
				schema_change_required: false,
				rationale_class: "retention_lag",
				linked_issues: [3741, 3624, 3794, 3795, 3796, 3797, 3798, 3799, 3800, 3801, 3802, 3803, 3804],
				migration_implications: "none",
				rollback_implications: "keep_current_postgres",
				retention_implications: "tune_policy",
				tenant_isolation_implications: "unchanged"
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
		.fact_growth.after.fact_records_rows == 3200000 and
		.fact_growth.rows_per_second == 1200 and
		.graph_write_pressure.timeout_retries == 3 and
		.retention.prune_batch_rows == 5000 and
		.decision.recommendation == "retention_tune" and
		.gate.to_profile == "hosted_growth"
	' "${out_json}" >/dev/null
	rg --fixed-strings --quiet 'Hosted-growth Postgres proof' "${out_md}"
	rg --fixed-strings --quiet 'Fact rows after: 3200000' "${out_md}"
	rg --fixed-strings --quiet 'Fact rows/sec: 1200' "${out_md}"
	rg --fixed-strings --quiet 'Decision: retention_tune' "${out_md}"
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

unbounded_relation="${tmp_dir}/unbounded-relation.json"
jq 'del(.relations[].bounded_evidence)' "${safe_input}" >"${unbounded_relation}"
expect_fail unbounded_relation "${unbounded_relation}" "relation proof must include bounded evidence for every measurement"

missing_relation_observed="${tmp_dir}/missing-relation-observed.json"
jq 'del(.relations[].observed_at)' "${safe_input}" >"${missing_relation_observed}"
expect_fail missing_relation_observed "${missing_relation_observed}" "relation proof must include observed_at for every measurement"

workflow_queue="${tmp_dir}/workflow-queue.json"
jq '.queue_drain.queue_surface = "workflow"' "${safe_input}" >"${workflow_queue}"
expect_fail workflow_queue "${workflow_queue}" "queue drain surface must be reducer"

missing_queue_observed="${tmp_dir}/missing-queue-observed.json"
jq 'del(.queue_drain.observed_at)' "${safe_input}" >"${missing_queue_observed}"
expect_fail missing_queue_observed "${missing_queue_observed}" "queue drain proof must include observed_at"

unbounded_queue="${tmp_dir}/unbounded-queue.json"
jq 'del(.queue_drain.bounded_evidence)' "${safe_input}" >"${unbounded_queue}"
expect_fail unbounded_queue "${unbounded_queue}" "queue drain proof must include bounded evidence"

missing_queue="${tmp_dir}/missing-queue.json"
jq '.queue_drain.retry_rows = 0' "${safe_input}" >"${missing_queue}"
expect_fail missing_queue "${missing_queue}" "queue drain must include retry, dead-letter, stale, claimed, and completed rows"

missing_claimed_rows="${tmp_dir}/missing-claimed-rows.json"
jq '.queue_drain.claimed_rows = 0' "${safe_input}" >"${missing_claimed_rows}"
expect_fail missing_claimed_rows "${missing_claimed_rows}" "queue drain must include retry, dead-letter, stale, claimed, and completed rows"

missing_growth="${tmp_dir}/missing-growth.json"
jq 'del(.fact_growth)' "${safe_input}" >"${missing_growth}"
expect_fail missing_growth "${missing_growth}" "fact growth families must be an array"

object_families="${tmp_dir}/object-families.json"
jq '.fact_growth.families = {"family":"collector","after_rows": 1}' "${safe_input}" >"${object_families}"
expect_fail object_families "${object_families}" "fact growth families must be an array"

missing_family="${tmp_dir}/missing-family.json"
jq 'del(.fact_growth.families[] | select(.family == "correlation"))' "${safe_input}" >"${missing_family}"
expect_fail missing_family "${missing_family}" "missing required fact growth families: correlation"

bad_growth_total="${tmp_dir}/bad-growth-total.json"
jq '.fact_growth.after.fact_records_rows = (.relations[] | select(.relation == "fact_records") | .row_count - 1)' "${safe_input}" >"${bad_growth_total}"
expect_fail bad_growth_total "${bad_growth_total}" "fact growth after totals must match the fact_records relation measurement"

family_rows_exceed="${tmp_dir}/family-rows-exceed.json"
jq '(.fact_growth.families[] | select(.family == "collector") | .after_rows) = 5000000' "${safe_input}" >"${family_rows_exceed}"
expect_fail family_rows_exceed "${family_rows_exceed}" "fact growth family rows must match fact_records after rows"

family_rows_under="${tmp_dir}/family-rows-under.json"
jq '(.fact_growth.families[] | select(.family == "collector") | .after_rows) = 1399999' "${safe_input}" >"${family_rows_under}"
expect_fail family_rows_under "${family_rows_under}" "fact growth family rows must match fact_records after rows"

missing_rows_per_second="${tmp_dir}/missing-rows-per-second.json"
jq 'del(.fact_growth.rows_per_second)' "${safe_input}" >"${missing_rows_per_second}"
expect_fail missing_rows_per_second "${missing_rows_per_second}" "fact growth proof must include rows_per_second"

unbounded_family="${tmp_dir}/unbounded-family.json"
jq '(.fact_growth.families[] | select(.family == "collector") | .bounded_evidence) = false' "${safe_input}" >"${unbounded_family}"
expect_fail unbounded_family "${unbounded_family}" "fact growth family proof must include positive counts, write amplification, insert latency, and bounded evidence"

missing_index_bloat="${tmp_dir}/missing-index-bloat.json"
jq 'del(.index_bloat)' "${safe_input}" >"${missing_index_bloat}"
expect_fail missing_index_bloat "${missing_index_bloat}" "missing required index bloat classes: active_generation, correlation_lookup"

missing_index_class="${tmp_dir}/missing-index-class.json"
jq 'del(.index_bloat.indexes[] | select(.index_class == "correlation_lookup"))' "${safe_input}" >"${missing_index_class}"
expect_fail missing_index_class "${missing_index_class}" "missing required index bloat classes: correlation_lookup"

duplicate_index_class="${tmp_dir}/duplicate-index-class.json"
jq '(.index_bloat.indexes[] | select(.index_class == "correlation_lookup") | .index_class) = "active_generation"' "${safe_input}" >"${duplicate_index_class}"
expect_fail duplicate_index_class "${duplicate_index_class}" "index bloat classes must be unique and complete"

missing_graph_pressure="${tmp_dir}/missing-graph-pressure.json"
jq 'del(.graph_write_pressure)' "${safe_input}" >"${missing_graph_pressure}"
expect_fail missing_graph_pressure "${missing_graph_pressure}" "graph-write pressure proof must include write latency, retry rows, timeout retries, and bounded evidence"

missing_plan="${tmp_dir}/missing-plan.json"
jq 'del(.query_plans[] | select(.query_class == "correlation_join"))' "${safe_input}" >"${missing_plan}"
expect_fail missing_plan "${missing_plan}" "missing required query plan classes: correlation_join"

object_query_plans="${tmp_dir}/object-query-plans.json"
jq '.query_plans = {"query_class":"active_generation_read"}' "${safe_input}" >"${object_query_plans}"
expect_fail object_query_plans "${object_query_plans}" "query plans must be an array"

unsupported_plan="${tmp_dir}/unsupported-plan.json"
jq '.query_plans += [{query_class: "unexpected_lookup", p95_ns: 1, rows_examined: 1, plan_status: "indexed", seq_scan: false, spill: false, observed_at: "2026-06-17T03:00:00Z", bounded_evidence: true}]' "${safe_input}" >"${unsupported_plan}"
expect_fail unsupported_plan "${unsupported_plan}" "unsupported query plan classes: unexpected_lookup"

broad_plan="${tmp_dir}/broad-plan.json"
jq '(.query_plans[] | select(.query_class == "hot_api_read") | .seq_scan) = true' "${safe_input}" >"${broad_plan}"
expect_fail broad_plan "${broad_plan}" "query plan proof must be indexed, non-spilling, non-seq-scan, and bounded"

missing_retention="${tmp_dir}/missing-retention.json"
jq 'del(.retention)' "${safe_input}" >"${missing_retention}"
expect_fail missing_retention "${missing_retention}" "retention proof must include lag, prune cost, superseded rows, archive posture, and bounded evidence"

missing_decision="${tmp_dir}/missing-decision.json"
jq 'del(.decision)' "${safe_input}" >"${missing_decision}"
expect_fail missing_decision "${missing_decision}" "decision proof must choose a supported recommendation, implications, and linked performance work"

bad_decision="${tmp_dir}/bad-decision.json"
jq '.decision.recommendation = "defer"' "${safe_input}" >"${bad_decision}"
expect_fail bad_decision "${bad_decision}" "decision recommendation must match measured breakpoint evidence"

defer_with_archive="${tmp_dir}/defer-with-archive.json"
jq '.decision.recommendation = "defer" |
	.decision.schema_change_required = false |
	.fact_growth.after.fact_records_rows = .gate.fact_rows_threshold |
	.fact_growth.before.fact_records_rows = 1000000 |
	.fact_growth.before.index_bytes = 200000000 |
	.fact_growth.before.total_bytes = 600000000 |
	.fact_growth.after.index_bytes = 400000000 |
	.fact_growth.after.total_bytes = 1200000000 |
	(.relations[] | select(.relation == "fact_records") | .row_count) = .gate.fact_rows_threshold |
	(.relations[] | select(.relation == "fact_records") | .index_bytes) = 400000000 |
	(.relations[] | select(.relation == "fact_records") | .total_bytes) = .fact_growth.after.total_bytes |
	(.fact_growth.families[] | select(.family == "collector") | .after_rows) = 800000 |
	(.fact_growth.families[] | select(.family == "parser") | .after_rows) = 600000 |
	(.fact_growth.families[] | select(.family == "search_documents") | .after_rows) = 300000 |
	(.fact_growth.families[] | select(.family == "correlation") | .after_rows) = 300000 |
	(.fact_growth.families[] | select(.family == "collector") | .before_rows) = 600000 |
	(.fact_growth.families[] | select(.family == "parser") | .before_rows) = 400000 |
	(.fact_growth.families[] | select(.family == "search_documents") | .before_rows) = 200000 |
	(.fact_growth.families[] | select(.family == "correlation") | .before_rows) = 200000 |
	.queue_drain.pending_rows = 1 |
	.queue_drain.retry_rows = 1 |
	.queue_drain.claimed_rows = 1 |
	.queue_drain.oldest_age_ns = 1 |
	.retention.retention_lag_ns = 0 |
	.retention.archive_required = true' "${safe_input}" >"${defer_with_archive}"
expect_fail defer_with_archive "${defer_with_archive}" "decision recommendation must match measured breakpoint evidence"

bad_implication="${tmp_dir}/bad-implication.json"
jq '.decision.migration_implications = "move_prod_db"' "${safe_input}" >"${bad_implication}"
expect_fail bad_implication "${bad_implication}" "decision proof must choose a supported recommendation, implications, and linked performance work"

linked_issue_string="${tmp_dir}/linked-issue-string.json"
jq '.decision.linked_issues += ["safecluster"]' "${safe_input}" >"${linked_issue_string}"
expect_fail linked_issue_string "${linked_issue_string}" "decision proof must choose a supported recommendation, implications, and linked performance work"

linked_issue_extra="${tmp_dir}/linked-issue-extra.json"
jq '.decision.linked_issues += [9999]' "${safe_input}" >"${linked_issue_extra}"
expect_fail linked_issue_extra "${linked_issue_extra}" "decision proof must choose a supported recommendation, implications, and linked performance work"

linked_issue_duplicate="${tmp_dir}/linked-issue-duplicate.json"
jq '.decision.linked_issues += [3741]' "${safe_input}" >"${linked_issue_duplicate}"
expect_fail linked_issue_duplicate "${linked_issue_duplicate}" "decision proof must choose a supported recommendation, implications, and linked performance work"

bad_rationale="${tmp_dir}/bad-rationale.json"
jq '.decision.rationale_class = "private_prod_cluster_alpha"' "${safe_input}" >"${bad_rationale}"
expect_fail bad_rationale "${bad_rationale}" "decision proof must choose a supported recommendation, implications, and linked performance work"

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

bad_gate_action="${tmp_dir}/bad-gate-action.json"
jq '.gate.recommended_action = "private_prod_cluster_alpha"' "${safe_input}" >"${bad_gate_action}"
expect_fail bad_gate_action "${bad_gate_action}" "operator gate must target hosted_growth with positive thresholds, public-safe labels, migration window, and rollback artifact"

bad_gate_status="${tmp_dir}/bad-gate-status.json"
jq '.gate.operator_status_signal = "private_prod_cluster_alpha"' "${safe_input}" >"${bad_gate_status}"
expect_fail bad_gate_status "${bad_gate_status}" "operator gate must target hosted_growth with positive thresholds, public-safe labels, migration window, and rollback artifact"

bad_proof_id="${tmp_dir}/bad-proof-id.json"
jq '.proof_id = "private_prod_cluster_alpha"' "${safe_input}" >"${bad_proof_id}"
expect_fail bad_proof_id "${bad_proof_id}" "input root shape must include schema version"

bad_commit="${tmp_dir}/bad-commit.json"
jq '.eshu_commit = "private_prod_cluster_alpha"' "${safe_input}" >"${bad_commit}"
expect_fail bad_commit "${bad_commit}" "input root shape must include schema version"

observability_string="${tmp_dir}/observability-string.json"
jq '.observability.queue_depth = "true"' "${safe_input}" >"${observability_string}"
expect_fail observability_string "${observability_string}" "observability proof must cover required fields as boolean true values"

private_input="${tmp_dir}/private.json"
jq '.proof_id = ("10" + ".0.0.1")' "${safe_input}" >"${private_input}"
expect_fail private "${private_input}" "input looks like private data"

raw_input="${tmp_dir}/raw.json"
jq '.security.private_locator_scan = "failed"' "${safe_input}" >"${raw_input}"
expect_fail raw "${raw_input}" "security review did not pass"

unknown_summary="${tmp_dir}/unknown-summary.json"
jq '.queue_drain.cluster_name = "safecluster" | .gate.cluster_name = "safecluster" | .fact_growth.before.cluster_name = "safecluster" | .decision.extra_label = "safecluster"' "${safe_input}" >"${unknown_summary}"
expect_pass unknown_summary "${unknown_summary}"
unknown_out_json="${tmp_dir}/unknown_summary.summary.json"
if jq -e '.queue_drain.cluster_name? // .gate.cluster_name? // .fact_growth.before.cluster_name? // .decision.extra_label?' "${unknown_out_json}" >/dev/null; then
	printf 'expected summary output to omit unknown aggregate fields\n' >&2
	jq . "${unknown_out_json}" >&2
	exit 1
fi
if jq -e '.gate.recommended_action? // .gate.operator_status_signal?' "${unknown_out_json}" >/dev/null; then
	printf 'expected summary output to omit operator text fields\n' >&2
	jq . "${unknown_out_json}" >&2
	exit 1
fi

printf 'hosted-growth postgres proof tests passed\n'
