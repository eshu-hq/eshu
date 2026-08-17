#!/usr/bin/env bash
# shellcheck disable=SC2034  # Delta collateral constants are consumed by the
# sourced fault-injection delivery library.
# Shared rationale_edges live-gate helpers (#5998). This file is sourced by
# the determinism and fault-injection drivers; callers own strict mode and
# cleanup. The fixture identity below is committed in
# testdata/cassettes/rationale/ifa-rationale-family.json.

ifa_rationale_scope_id="scope-ifa-rationale-family"
ifa_rationale_generation_id="gen-ifa-rationale-family-1"
ifa_rationale_delta_generation_id="gen-ifa-rationale-family-2"
ifa_rationale_repository_id="repository:r_f781caa5"
ifa_rationale_source_run_id="run-ifa-rationale-family-1"
ifa_rationale_expected_tuple="1|1|0|4|3|1|4|0"
ifa_rationale_delta_expected_tuple="1|1|0|1|0|1|1|0"
ifa_rationale_delta_survivor_uid="content-entity:e_763200c9adc3"
ifa_rationale_delta_relative_path="services/payments/charge.py"
ifa_rationale_delta_absolute_path="/repo-rationale/services/payments/charge.py"

# ifa_rationale_drive replays the production-shaped Git cassette into one
# matrix cell. The caller performs its aggregate work-populated guard.
ifa_rationale_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	printf '\n=== %s: drive rationale family cassette (-workers %s) ===\n' "${label}" "${workers}"
	if ! "${bin_dir}/eshu-ifa" drive -cassette "${cassette}" -workers "${workers}" \
		>"${log_dir}/ifa-drive-rationale-${label}.log" 2>&1; then
		tail -40 "${log_dir}/ifa-drive-rationale-${label}.log" >&2 || true
		return 1
	fi
	cat "${log_dir}/ifa-drive-rationale-${label}.log"
}

# ifa_rationale_assert compares the complete three-edge live graph record set,
# including Rationale node, EXPLAINS relationship, and Function target
# properties, against the hand-derived committed expectation.
ifa_rationale_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert rationale materialized records (full-record exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain rationale_edges \
		-expected "${expected_edges}"
}

# ifa_rationale_query_delta_survivor_node proves the delta fixture's one
# required node exists exactly once and byte-for-byte in a canonical graph
# dump. The expected fixture must itself declare the rationale delta
# generation, exactly one surviving edge, and exactly one required node;
# malformed or vacuous inputs fail closed.
ifa_rationale_query_delta_survivor_node() {
	local label="$1" graph_dump="$2" expected_records="$3"
	command -v jq >/dev/null 2>&1 || {
		printf '%s: jq is required for the rationale delta survivor query\n' "${label}" >&2
		return 1
	}
	if ! jq -e --slurpfile expected "${expected_records}" \
		--arg survivor_uid "${ifa_rationale_delta_survivor_uid}" \
		--arg delta_generation "${ifa_rationale_delta_generation_id}" '
		if ($expected | length) != 1
			or (($expected[0] | type) != "object")
			or ($expected[0].generation_id? != $delta_generation)
			or (($expected[0].edges? | type) != "array")
			or (($expected[0].edges | length) != 1)
			or (($expected[0].required_nodes? | type) != "array")
			or (($expected[0].required_nodes | length) != 1) then
			error("delta oracle must declare the rationale delta generation with exactly one edge and one required node")
		elif ((.nodes? | type) != "array") or ((.edges? | type) != "array") then
			error("graph dump must contain nodes and edges arrays")
		else
			$expected[0].required_nodes[0] as $want
			| if (($want.labels? | type) != "array")
				or (($want.props? | type) != "object")
				or ($want.props.uid? != $survivor_uid)
				or ($want.props.generation_id? != $delta_generation) then
				error("required node must be the exact rationale delta Charge survivor")
			else
				[.nodes[] | select(.props.uid? == $survivor_uid)] as $matches
				| if ($matches | length) != 1 then
					error("graph must contain exactly one Charge survivor node")
				elif $matches[0] != $want then
					error("Charge survivor node does not match the expected full record")
				else true
				end
			end
		end
	' "${graph_dump}" >/dev/null; then
		printf '%s: rationale delta survivor-node query failed closed\n' "${label}" >&2
		return 1
	fi
	printf '%s: rationale delta Charge survivor matched the exact delta-generation node record\n' "${label}"
}

# ifa_rationale_assert_delta_truth combines the exact surviving EXPLAINS record
# assertion with an independent full-record node query for the changed Charge
# Function. It writes a dedicated dump so both assertions precede the caller's
# final determinism/collateral dump.
ifa_rationale_assert_delta_truth() {
	local label="$1" bin_dir="$2" expected_records="$3" graph_dump="$4"
	ifa_rationale_assert "${label}" "${bin_dir}" "${expected_records}" || return 1
	if ! "${bin_dir}/eshu-ifa" graph-dump -out "${graph_dump}"; then
		printf '%s: rationale delta survivor graph-dump failed\n' "${label}" >&2
		return 1
	fi
	ifa_rationale_query_delta_survivor_node "${label}" "${graph_dump}" "${expected_records}"
}

# ifa_rationale_assert_work_counts fails closed unless one rationale fixture
# generation's durable lifecycle equals its generation-specific exact tuple:
#
#   fact total|succeeded|not-succeeded|intent total|upsert|refresh|completed|pending
#   gen-ifa-rationale-family-1: 1|1|0|4|3|1|4|0
#   gen-ifa-rationale-family-2: 1|1|0|1|0|1|1|0
#
# The acceptance row is asserted separately because it is the authoritative
# generation fence, not a queue state. Query failures, blank output, malformed
# output, and any different count all fail.
#
# Args: label compose_project use_compose dsn compose_file generation_id expected_tuple
ifa_rationale_assert_work_counts() {
	local label="$1" compose_project="$2" use_compose="$3" dsn="$4" compose_file="$5"
	local generation_id="$6" expected_tuple="$7"
	local tuple tuple_rc acceptance acceptance_rc
	local tuple_sql acceptance_sql
	case "${generation_id}|${expected_tuple}" in
	"${ifa_rationale_generation_id}|${ifa_rationale_expected_tuple}" | \
		"${ifa_rationale_delta_generation_id}|${ifa_rationale_delta_expected_tuple}") ;;
	*)
		printf '%s: unsupported rationale generation/tuple contract %q/%q\n' \
			"${label}" "${generation_id}" "${expected_tuple}" >&2
		return 1
		;;
	esac

	tuple_sql="SELECT concat_ws('|',
	  (SELECT count(*) FROM fact_work_items
	    WHERE scope_id = '${ifa_rationale_scope_id}'
	      AND generation_id = '${generation_id}'
	      AND stage = 'reducer'
	      AND domain = 'rationale_materialization'),
	  (SELECT count(*) FROM fact_work_items
	    WHERE scope_id = '${ifa_rationale_scope_id}'
	      AND generation_id = '${generation_id}'
	      AND stage = 'reducer'
	      AND domain = 'rationale_materialization'
	      AND status = 'succeeded'),
	  (SELECT count(*) FROM fact_work_items
	    WHERE scope_id = '${ifa_rationale_scope_id}'
	      AND generation_id = '${generation_id}'
	      AND stage = 'reducer'
	      AND domain = 'rationale_materialization'
	      AND status <> 'succeeded'),
	  (SELECT count(*) FROM shared_projection_intents
	    WHERE projection_domain = 'rationale_edges'
	      AND scope_id = '${ifa_rationale_scope_id}'
	      AND generation_id = '${generation_id}'
	      AND repository_id = '${ifa_rationale_repository_id}'
	      AND source_run_id = '${ifa_rationale_source_run_id}'),
	  (SELECT count(*) FROM shared_projection_intents
	    WHERE projection_domain = 'rationale_edges'
	      AND scope_id = '${ifa_rationale_scope_id}'
	      AND generation_id = '${generation_id}'
	      AND repository_id = '${ifa_rationale_repository_id}'
	      AND source_run_id = '${ifa_rationale_source_run_id}'
	      AND payload->>'action' = 'upsert'),
	  (SELECT count(*) FROM shared_projection_intents
	    WHERE projection_domain = 'rationale_edges'
	      AND scope_id = '${ifa_rationale_scope_id}'
	      AND generation_id = '${generation_id}'
	      AND repository_id = '${ifa_rationale_repository_id}'
	      AND source_run_id = '${ifa_rationale_source_run_id}'
	      AND payload->>'action' = 'refresh'),
	  (SELECT count(*) FROM shared_projection_intents
	    WHERE projection_domain = 'rationale_edges'
	      AND scope_id = '${ifa_rationale_scope_id}'
	      AND generation_id = '${generation_id}'
	      AND repository_id = '${ifa_rationale_repository_id}'
	      AND source_run_id = '${ifa_rationale_source_run_id}'
	      AND completed_at IS NOT NULL),
	  (SELECT count(*) FROM shared_projection_intents
	    WHERE projection_domain = 'rationale_edges'
	      AND scope_id = '${ifa_rationale_scope_id}'
	      AND generation_id = '${generation_id}'
	      AND repository_id = '${ifa_rationale_repository_id}'
	      AND source_run_id = '${ifa_rationale_source_run_id}'
	      AND completed_at IS NULL));"

	if tuple="$(ifa_det_pg "${compose_project}" "${use_compose}" "${dsn}" "${tuple_sql}" "${compose_file}")"; then
		tuple_rc=0
	else
		tuple_rc=$?
	fi
	if [[ "${tuple_rc}" -ne 0 ]]; then
		printf '%s: rationale durable-tuple query FAILED (exit %s)\n' "${label}" "${tuple_rc}" >&2
		return "${tuple_rc}"
	fi
	tuple="$(printf '%s' "${tuple}" | tr -d '[:space:]')"
	if [[ -z "${tuple}" ]]; then
		printf '%s: rationale durable-tuple query returned empty output; treat that as unknown\n' "${label}" >&2
		return 1
	fi
	if [[ ! "${tuple}" =~ ^[0-9]+\|[0-9]+\|[0-9]+\|[0-9]+\|[0-9]+\|[0-9]+\|[0-9]+\|[0-9]+$ ]]; then
		printf '%s: rationale durable-tuple query returned malformed output %q\n' "${label}" "${tuple}" >&2
		return 1
	fi
	if [[ "${tuple}" != "${expected_tuple}" ]]; then
		printf '%s: rationale durable tuple = %s, want %s\n' "${label}" "${tuple}" "${expected_tuple}" >&2
		return 1
	fi

	acceptance_sql="SELECT count(*) FROM shared_projection_acceptance
	 WHERE scope_id = '${ifa_rationale_scope_id}'
	   AND acceptance_unit_id = '${ifa_rationale_repository_id}'
	   AND source_run_id = '${ifa_rationale_source_run_id}'
	   AND generation_id = '${generation_id}';"
	if acceptance="$(ifa_det_pg "${compose_project}" "${use_compose}" "${dsn}" "${acceptance_sql}" "${compose_file}")"; then
		acceptance_rc=0
	else
		acceptance_rc=$?
	fi
	if [[ "${acceptance_rc}" -ne 0 ]]; then
		printf '%s: rationale acceptance-count query FAILED (exit %s)\n' "${label}" "${acceptance_rc}" >&2
		return "${acceptance_rc}"
	fi
	acceptance="$(printf '%s' "${acceptance}" | tr -d '[:space:]')"
	if [[ -z "${acceptance}" ]]; then
		printf '%s: rationale acceptance-count query returned empty output; treat that as unknown\n' "${label}" >&2
		return 1
	fi
	if [[ ! "${acceptance}" =~ ^[0-9]+$ ]]; then
		printf '%s: rationale acceptance-count query returned non-numeric output %q\n' "${label}" "${acceptance}" >&2
		return 1
	fi
	if [[ "${acceptance}" != "1" ]]; then
		printf '%s: rationale shared_projection_acceptance rows = %s, want 1\n' "${label}" "${acceptance}" >&2
		return 1
	fi

	printf '%s: rationale generation %s durable tuple %s; accepted generations: 1\n' \
		"${label}" "${generation_id}" "${tuple}"
}
