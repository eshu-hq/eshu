#!/usr/bin/env bash
# Shared validation for public-safe hosted-growth Postgres proof evidence.

required_fact_families='["collector","parser","search_documents","correlation"]'
required_index_classes='["active_generation","correlation_lookup"]'
required_query_plans='["active_generation_read","correlation_join","retention_changed_since","hot_api_read"]'
required_linked_issues='[3741,3624,3794,3795,3796,3797,3798,3799,3800,3801,3802,3803,3804]'
supported_implications='["none","keep_current_postgres","tune_policy","unchanged","migration_window_required"]'
supported_rationale_classes='["growth_threshold","retention_lag","family_dominance","archive_pressure","below_threshold"]'

validate_hosted_growth_breakpoint_evidence() {
	local input="$1"

	if ! jq -e --argjson required "${required_fact_families}" '
		(.fact_growth.families | type) == "array" and
		([.fact_growth.families[].family] as $names |
			($names | length) == ($required | length) and
			($names | length) == ($names | unique | length) and
			(($required - $names) | length) == 0 and
			(($names - $required) | length) == 0)
	' "${input}" >/dev/null; then
		if ! jq -e '(.fact_growth.families | type) == "array"' "${input}" >/dev/null; then
			die "fact growth families must be an array"
		fi
		missing="$(jq -r --argjson required "${required_fact_families}" '[($required - [.fact_growth.families[]?.family])[]] | join(", ")' "${input}")"
		unknown="$(jq -r --argjson required "${required_fact_families}" '([.fact_growth.families[]?.family] - $required) | join(", ")' "${input}")"
		[[ -z "${missing}" ]] || die "missing required fact growth families: ${missing}"
		[[ -z "${unknown}" ]] || die "unsupported fact growth families: ${unknown}"
		die "fact growth families must be unique and complete"
	fi

	if ! jq -e '
		.fact_growth as $growth |
		$growth.before as $before |
		$growth.after as $after |
		$growth.model_version == "fact_records_growth_v1" and
		($growth.rows_per_second | type == "number" and . > 0) and
		($before.fact_records_rows | type == "number" and . > 0) and
		($after.fact_records_rows | type == "number" and . >= $before.fact_records_rows) and
		($before.index_bytes | type == "number" and . > 0) and
		($after.index_bytes | type == "number" and . >= $before.index_bytes) and
		($before.total_bytes | type == "number" and . >= $before.index_bytes) and
		($after.total_bytes | type == "number" and . >= $after.index_bytes) and
		($before.observed_at | type == "string" and length > 0) and
		($after.observed_at | type == "string" and length > 0) and
		$before.bounded_evidence == true and
		$after.bounded_evidence == true
	' "${input}" >/dev/null; then
		die "fact growth proof must include rows_per_second, before/after fact_records totals, and bounded evidence"
	fi

	if ! jq -e '
		(first(.relations[] | select(.relation == "fact_records")) // null) as $relation |
		$relation != null and
		.fact_growth.after.fact_records_rows == $relation.row_count and
		.fact_growth.after.index_bytes == $relation.index_bytes and
		.fact_growth.after.total_bytes == $relation.total_bytes
	' "${input}" >/dev/null; then
		die "fact growth after totals must match the fact_records relation measurement"
	fi

	if ! jq -e '
		[
			.fact_growth.families[] as $family |
			($family.fact_kind_count | type == "number" and . > 0) and
			($family.before_rows | type == "number" and . >= 0) and
			($family.after_rows | type == "number" and . >= $family.before_rows) and
			($family.after_index_bytes | type == "number" and . > 0) and
			($family.write_amplification_ratio | type == "number" and . > 0) and
			($family.p95_insert_ns | type == "number" and . > 0) and
			$family.bounded_evidence == true
		] | all
	' "${input}" >/dev/null; then
		die "fact growth family proof must include positive counts, write amplification, insert latency, and bounded evidence"
	fi

	if ! jq -e '
		([.fact_growth.families[].after_rows] | add) == .fact_growth.after.fact_records_rows
	' "${input}" >/dev/null; then
		die "fact growth family rows must match fact_records after rows"
	fi

	if ! jq -e --argjson required "${required_index_classes}" '
		(.index_bloat.indexes | type) == "array" and
		(.index_bloat.table_bloat_ratio | type == "number" and . >= 0) and
		(.index_bloat.dead_tuple_bytes | type == "number" and . >= 0) and
		([.index_bloat.indexes[].index_class] as $names |
			($names | length) == ($required | length) and
			($names | length) == ($names | unique | length) and
			(($required - $names) | length) == 0 and
			(($names - $required) | length) == 0) and
		([
			.index_bloat.indexes[] |
			(.size_bytes | type == "number" and . > 0) and
			(.bloat_ratio | type == "number" and . >= 0) and
			(.write_amplification_ratio | type == "number" and . > 0) and
			.bounded_evidence == true
		] | all)
	' "${input}" >/dev/null; then
		if jq -e '
			(.index_bloat.indexes | type) == "array" and
			([.index_bloat.indexes[]?.index_class] as $names |
				($names | length) != ($names | unique | length))
		' "${input}" >/dev/null; then
			die "index bloat classes must be unique and complete"
		fi
		missing="$(jq -r --argjson required "${required_index_classes}" '[($required - [.index_bloat.indexes[]?.index_class])[]] | join(", ")' "${input}")"
		unknown="$(jq -r --argjson required "${required_index_classes}" '([.index_bloat.indexes[]?.index_class] - $required) | join(", ")' "${input}")"
		[[ -z "${missing}" ]] || die "missing required index bloat classes: ${missing}"
		[[ -z "${unknown}" ]] || die "unsupported index bloat classes: ${unknown}"
		die "index bloat classes must be unique and complete"
	fi

	if ! jq -e '
		(.graph_write_pressure.write_p95_ns | type == "number" and . > 0) and
		(.graph_write_pressure.timeout_retries | type == "number" and . >= 0) and
		(.graph_write_pressure.retrying_graph_write_timeout_rows | type == "number" and . >= 0) and
		(.graph_write_pressure.dead_letter_rows | type == "number" and . >= 0) and
		(.graph_write_pressure.p95_group_rows | type == "number" and . > 0) and
		(.graph_write_pressure.observed_at | type == "string" and length > 0) and
		.graph_write_pressure.bounded_evidence == true
	' "${input}" >/dev/null; then
		die "graph-write pressure proof must include write latency, retry rows, timeout retries, and bounded evidence"
	fi

	if ! jq -e --argjson required "${required_query_plans}" '
		(.query_plans | type) == "array" and
		([.query_plans[].query_class] as $names |
			($names | length) == ($required | length) and
			($names | length) == ($names | unique | length) and
			(($required - $names) | length) == 0 and
			(($names - $required) | length) == 0)
	' "${input}" >/dev/null; then
		if ! jq -e '(.query_plans | type) == "array"' "${input}" >/dev/null; then
			die "query plans must be an array"
		fi
		missing="$(jq -r --argjson required "${required_query_plans}" '[($required - [.query_plans[]?.query_class])[]] | join(", ")' "${input}")"
		unknown="$(jq -r --argjson required "${required_query_plans}" '([.query_plans[]?.query_class] - $required) | join(", ")' "${input}")"
		[[ -z "${missing}" ]] || die "missing required query plan classes: ${missing}"
		[[ -z "${unknown}" ]] || die "unsupported query plan classes: ${unknown}"
		die "query plan classes must be unique and complete"
	fi

	if ! jq -e '
		[
			.query_plans[] |
			(.p95_ns | type == "number" and . > 0) and
			(.rows_examined | type == "number" and . > 0) and
			.plan_status == "indexed" and
			.seq_scan == false and
			.spill == false and
			(.observed_at | type == "string" and length > 0) and
			.bounded_evidence == true
		] | all
	' "${input}" >/dev/null; then
		die "query plan proof must be indexed, non-spilling, non-seq-scan, and bounded"
	fi

	if ! jq -e '
		(.retention.superseded_rows | type == "number" and . >= 0) and
		(.retention.oldest_superseded_age_ns | type == "number" and . > 0) and
		(.retention.retention_lag_ns | type == "number" and . >= 0) and
		(.retention.prune_duration_ns | type == "number" and . > 0) and
		(.retention.prune_batch_rows | type == "number" and . > 0) and
		(.retention.archive_required | type == "boolean") and
		.retention.bounded_evidence == true
	' "${input}" >/dev/null; then
		die "retention proof must include lag, prune cost, superseded rows, archive posture, and bounded evidence"
	fi

	validate_hosted_growth_decision "${input}"
}

validate_hosted_growth_decision() {
	local input="$1"

	if ! jq -e --argjson required "${required_linked_issues}" --argjson implications "${supported_implications}" --argjson rationales "${supported_rationale_classes}" '
		(.decision.recommendation | IN("partition", "archive", "split", "retention_tune", "defer")) and
		(.decision.schema_change_required | type == "boolean") and
		(.decision.rationale_class as $v | $rationales | index($v)) and
		(.decision.linked_issues | type == "array") and
		([.decision.linked_issues[] | type == "number"] | all) and
		(.decision.linked_issues | length) == ($required | length) and
		(.decision.linked_issues | length) == (.decision.linked_issues | unique | length) and
		(($required - .decision.linked_issues) | length) == 0 and
		((.decision.linked_issues - $required) | length) == 0 and
		(.decision.migration_implications as $v | $implications | index($v)) and
		(.decision.rollback_implications as $v | $implications | index($v)) and
		(.decision.retention_implications as $v | $implications | index($v)) and
		(.decision.tenant_isolation_implications as $v | $implications | index($v))
	' "${input}" >/dev/null; then
		die "decision proof must choose a supported recommendation, implications, and linked performance work"
	fi

	if ! jq -e '
		def live_queue_rows: .queue_drain.pending_rows + .queue_drain.retry_rows + .queue_drain.claimed_rows;
		def dominant_family_rows: ([.fact_growth.families[].after_rows] | max);
		if .decision.recommendation == "defer" then
			(.decision.schema_change_required == false) and
			(.fact_growth.after.fact_records_rows <= .gate.fact_rows_threshold) and
			(.fact_growth.after.index_bytes <= .gate.index_bytes_threshold) and
			(live_queue_rows <= .gate.queue_rows_threshold) and
			(.queue_drain.oldest_age_ns <= .gate.oldest_queue_age_threshold_ns) and
			(.retention.retention_lag_ns == 0) and
			(.retention.archive_required == false)
		elif .decision.recommendation == "retention_tune" then
			(.decision.schema_change_required == false) and
			(.retention.retention_lag_ns > 0) and
			(.retention.archive_required == false)
		elif .decision.recommendation == "partition" then
			.decision.schema_change_required == true and
			.migration.native_partitioning == true and
			((.fact_growth.after.fact_records_rows > .gate.fact_rows_threshold) or
				(.fact_growth.after.index_bytes > .gate.index_bytes_threshold))
		elif .decision.recommendation == "archive" then
			.decision.schema_change_required == true and
			.retention.archive_required == true and
			.retention.retention_lag_ns > 0
		elif .decision.recommendation == "split" then
			.decision.schema_change_required == true and
			((dominant_family_rows * 2) > .fact_growth.after.fact_records_rows)
		else
			false
		end
	' "${input}" >/dev/null; then
		die "decision recommendation must match measured breakpoint evidence"
	fi
}

validate_hosted_growth_observability() {
	local input="$1"

	if ! jq -e '
		.observability.relation_size == true and
		.observability.index_size == true and
		.observability.read_latency == true and
		.observability.write_latency == true and
		.observability.queue_depth == true and
		.observability.oldest_age == true and
		.observability.retry_count == true and
		.observability.dead_letters == true and
		.observability.stale_rows == true and
		.observability.active_claims == true and
		.observability.migration_duration == true and
		.observability.rollback_status == true
	' "${input}" >/dev/null; then
		die "observability proof must cover required fields as boolean true values"
	fi
}

write_hosted_growth_summary() {
	local input="$1"
	local tmp_json="$2"
	local tmp_markdown="$3"

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
		fact_growth: {
			model_version: .fact_growth.model_version,
			rows_per_second: .fact_growth.rows_per_second,
			before: {
				fact_records_rows: .fact_growth.before.fact_records_rows,
				index_bytes: .fact_growth.before.index_bytes,
				total_bytes: .fact_growth.before.total_bytes,
				observed_at: .fact_growth.before.observed_at
			},
			after: {
				fact_records_rows: .fact_growth.after.fact_records_rows,
				index_bytes: .fact_growth.after.index_bytes,
				total_bytes: .fact_growth.after.total_bytes,
				observed_at: .fact_growth.after.observed_at
			},
			families: [.fact_growth.families[] | {family, fact_kind_count, before_rows, after_rows, after_index_bytes, write_amplification_ratio, p95_insert_ns}]
		},
		index_bloat: {
			table_bloat_ratio: .index_bloat.table_bloat_ratio,
			dead_tuple_bytes: .index_bloat.dead_tuple_bytes,
			indexes: [.index_bloat.indexes[] | {index_class, size_bytes, bloat_ratio, write_amplification_ratio}]
		},
		graph_write_pressure: {
			write_p95_ns: .graph_write_pressure.write_p95_ns,
			timeout_retries: .graph_write_pressure.timeout_retries,
			retrying_graph_write_timeout_rows: .graph_write_pressure.retrying_graph_write_timeout_rows,
			dead_letter_rows: .graph_write_pressure.dead_letter_rows,
			p95_group_rows: .graph_write_pressure.p95_group_rows
		},
		query_plans: [.query_plans[] | {query_class, p95_ns, rows_examined, plan_status, seq_scan, spill}],
		retention: {
			superseded_rows: .retention.superseded_rows,
			oldest_superseded_age_ns: .retention.oldest_superseded_age_ns,
			retention_lag_ns: .retention.retention_lag_ns,
			prune_duration_ns: .retention.prune_duration_ns,
			prune_batch_rows: .retention.prune_batch_rows,
			archive_required: .retention.archive_required
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
			requires_migration_window: .gate.requires_migration_window,
			requires_rollback_artifact: .gate.requires_rollback_artifact
		},
		decision: {
			recommendation: .decision.recommendation,
			schema_change_required: .decision.schema_change_required,
			rationale_class: .decision.rationale_class,
			linked_issues: .decision.linked_issues,
			migration_implications: .decision.migration_implications,
			rollback_implications: .decision.rollback_implications,
			retention_implications: .decision.retention_implications,
			tenant_isolation_implications: .decision.tenant_isolation_implications
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
			"- Fact rows after: \(.fact_growth.after.fact_records_rows)",
			"- Fact rows/sec: \(.fact_growth.rows_per_second)",
			"- Index bloat: table_ratio=\(.index_bloat.table_bloat_ratio), dead_tuple_bytes=\(.index_bloat.dead_tuple_bytes)",
			"- Graph write pressure: p95_ns=\(.graph_write_pressure.write_p95_ns), timeout_retries=\(.graph_write_pressure.timeout_retries)",
			"- Query plans: \([.query_plans[].query_class] | join(","))",
			"- Retention: lag_ns=\(.retention.retention_lag_ns), prune_duration_ns=\(.retention.prune_duration_ns), archive_required=\(.retention.archive_required)",
			"- Migration: rollback=\(.migration.rollback_behavior), scenarios=\([.migration.scenarios[].scenario] | join(","))",
			"- Gate: \(.gate.from_profile) -> \(.gate.to_profile), fact_rows=\(.gate.fact_rows_threshold), queue_rows=\(.gate.queue_rows_threshold)",
			"- Decision: \(.decision.recommendation)",
			"",
			"Raw repositories, hostnames, IPs, paths, DSNs, logs, source payloads, principals, accounts, and credentials remain operator-local."
		] | .[]
	' "${tmp_json}" >"${tmp_markdown}"
}
