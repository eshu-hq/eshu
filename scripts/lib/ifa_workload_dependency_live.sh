#!/usr/bin/env bash
# shellcheck shell=bash
# workload_dependency is a maintenance-backed live family. The first drain
# builds workloads while repository dependency projection is still gated.
# Bootstrap maintenance reopens deployment_mapping; the repo_dependency runner
# then writes the exact Repository edges and automatically replays workload
# materialization for the affected repositories. The same drain must therefore
# converge both the three-edge repository prerequisite and the two owned
# Workload DEPENDS_ON edges without a manual reopen.

ifa_workload_dependency_live_drive() {
	local bin_dir="$1" cassette="$2" workers="$3"
	"${bin_dir}/eshu-ifa" drive -cassette "${cassette}" -workers "${workers}"
}

ifa_workload_dependency_live_assert() {
	local bin_dir="$1" expected_edges="$2"
	"${bin_dir}/eshu-ifa" assert-edges -domain workload_dependency -expected "${expected_edges}"
}

ifa_workload_dependency_live_assert_repo_prerequisite() {
	local bin_dir="$1" expected_edges="$2"
	"${bin_dir}/eshu-ifa" assert-edges -domain repo_dependency -expected "${expected_edges}"
}

# The ownership filter is the same one retractWorkloadDependencyEdgesCypher
# uses. Zero here proves the later exact edges cannot be stale state from the
# initial workload drain or a different DEPENDS_ON writer.
ifa_workload_dependency_live_assert_owned_absent() {
	local bin_dir="$1" label="$2" dump_path count
	command -v jq >/dev/null 2>&1 || {
		printf '%s: jq is required for the workload_dependency ownership census\n' "${label}" >&2
		return 1
	}
	dump_path="$(mktemp)" || return 1
	if ! "${bin_dir}/eshu-ifa" graph-dump -out "${dump_path}"; then
		rm -f "${dump_path}"
		return 1
	fi
	count="$(jq '[.edges[] | select(.type == "DEPENDS_ON" and .props.evidence_source == "finalization/workloads")] | length' "${dump_path}")" || {
		rm -f "${dump_path}"
		return 1
	}
	rm -f "${dump_path}"
	[[ "${count}" =~ ^[0-9]+$ && "${count}" -eq 0 ]] || {
		printf '%s: expected zero workload-owned DEPENDS_ON edges before repo_dependency replay, got %q\n' "${label}" "${count}" >&2
		return 1
	}
	printf '%s: confirmed zero workload-owned DEPENDS_ON edges before repo_dependency replay\n' "${label}"
}

# Reopen only this fixture's succeeded workload_materialization row for the
# fault cells' deliberate second execution. The standalone cell relies on the
# repo_dependency runner's automatic replay and never calls this helper.
# Scoping by domain alone would retrigger unrelated work; the count is returned
# from the UPDATE itself and fault callers require exactly one row.
ifa_workload_dependency_live_reopen_materialization() {
	local compose_project="$1" use_compose="$2" dsn="$3" compose_file="$4"
	local result rc=0
	result="$(ifa_det_pg "${compose_project}" "${use_compose}" "${dsn}" \
		"WITH reopened AS (
		   UPDATE fact_work_items
		      SET status = 'pending',
		          attempt_count = 0,
		          container_image_identity_v2_authorized_status = CASE
		              WHEN container_image_identity_v2_required THEN 'pending'
		              ELSE ''
		          END,
		          container_image_identity_v3_authorized_status = CASE
		              WHEN container_image_identity_v3_required THEN 'pending'
		              ELSE ''
		          END,
		          lease_owner = NULL,
		          claim_until = NULL,
		          visible_at = now(),
		          next_attempt_at = NULL,
		          updated_at = now(),
		          reopened_at = now(),
		          failure_class = NULL,
		          failure_message = NULL,
		          failure_details = NULL
		    WHERE stage = 'reducer'
		      AND domain = 'workload_materialization'
		      AND scope_id = 'scope-ifa-workload-dependency-family'
		      AND generation_id = 'gen-ifa-workload-dependency-family-1'
		      AND payload->>'entity_key' = 'repo:repo-ifa-workload-dependency-source'
		      AND status = 'succeeded'
		RETURNING work_item_id, status, attempt_count
		 )
		 SELECT count(*) || '|' ||
		        COALESCE(min(work_item_id), '') || '|' ||
		        COALESCE(min(status), '') || '|' ||
		        COALESCE(min(attempt_count)::text, '')
		   FROM reopened;" "${compose_file}")" || rc=$?
	[[ "${rc}" -eq 0 ]] || return "${rc}"
	printf '%s' "${result}" | tr -d '[:space:]'
}

ifa_workload_dependency_live_validate_work_item_state_args() {
	local work_item_id="$1" attempt_count="$2"
	[[ "${work_item_id}" =~ ^[[:alnum:]_.:/-]+$ ]] || {
		printf 'workload_dependency work_item_id has unsupported characters: %q\n' "${work_item_id}" >&2
		return 1
	}
	[[ "${attempt_count}" =~ ^[0-9]+$ ]] || {
		printf 'workload_dependency attempt_count must be a non-negative integer, got %q\n' "${attempt_count}" >&2
		return 1
	}
}

ifa_workload_dependency_live_wait_for_claimed_attempt() {
	local compose_project="$1" use_compose="$2" dsn="$3" compose_file="$4"
	local budget="$5" work_item_id="$6" attempt_count="$7" observed
	[[ "${budget}" =~ ^[1-9][0-9]*$ ]] || {
		printf 'workload_dependency claimed-attempt budget must be a positive integer, got %q\n' "${budget}" >&2
		return 1
	}
	ifa_workload_dependency_live_validate_work_item_state_args "${work_item_id}" "${attempt_count}" || return 1
	observed="$(ifa_det_pg "${compose_project}" "${use_compose}" "${dsn}" \
		"CREATE OR REPLACE FUNCTION pg_temp.ifa_wait_for_workload_dependency_claim(wait_seconds integer)
		 RETURNS integer LANGUAGE plpgsql AS \$\$
		 DECLARE
		   matched integer;
		   deadline timestamptz := clock_timestamp() + make_interval(secs => wait_seconds);
		 BEGIN
		   LOOP
		     SELECT count(*) INTO matched
		       FROM fact_work_items
		      WHERE work_item_id = '${work_item_id}'
		        AND stage = 'reducer'
		        AND domain = 'workload_materialization'
		        AND scope_id = 'scope-ifa-workload-dependency-family'
		        AND generation_id = 'gen-ifa-workload-dependency-family-1'
		        AND payload->>'entity_key' = 'repo:repo-ifa-workload-dependency-source'
		        AND status IN ('claimed', 'running')
		        AND attempt_count = ${attempt_count};
		     IF matched = 1 THEN
		       RETURN matched;
		     END IF;
		     EXIT WHEN clock_timestamp() >= deadline;
		     PERFORM pg_sleep(0.001);
		   END LOOP;
		   RETURN 0;
		 END
		 \$\$;
		 SELECT pg_temp.ifa_wait_for_workload_dependency_claim(${budget});" \
		"${compose_file}" | tail -n 1 | tr -d '[:space:]')" || return 1
	[[ "${observed}" == "1" ]] || {
		printf 'workload_dependency exact row %q did not reach claimed/running at attempt %s within %ss\n' \
			"${work_item_id}" "${attempt_count}" "${budget}" >&2
		return 1
	}
	printf '%s' "${observed}"
}

ifa_workload_dependency_live_assert_work_item_state() {
	local compose_project="$1" use_compose="$2" dsn="$3" compose_file="$4"
	local work_item_id="$5" status="$6" attempt_count="$7" matched
	ifa_workload_dependency_live_validate_work_item_state_args "${work_item_id}" "${attempt_count}" || return 1
	[[ "${status}" =~ ^(pending|claimed|running|retrying|succeeded|dead_letter)$ ]] || {
		printf 'workload_dependency status has unsupported value: %q\n' "${status}" >&2
		return 1
	}
	matched="$(ifa_det_pg "${compose_project}" "${use_compose}" "${dsn}" \
		"SELECT count(*)
		   FROM fact_work_items
		  WHERE work_item_id = '${work_item_id}'
		    AND stage = 'reducer'
		    AND domain = 'workload_materialization'
		    AND scope_id = 'scope-ifa-workload-dependency-family'
		    AND generation_id = 'gen-ifa-workload-dependency-family-1'
		    AND payload->>'entity_key' = 'repo:repo-ifa-workload-dependency-source'
		    AND status = '${status}'
		    AND attempt_count = ${attempt_count};" "${compose_file}" | tr -d '[:space:]')" || return 1
	[[ "${matched}" == "1" ]] || {
		printf 'workload_dependency exact row %q is not %s at attempt %s (matched %q)\n' \
			"${work_item_id}" "${status}" "${attempt_count}" "${matched}" >&2
		return 1
	}
	printf '%s' "${matched}"
}

ifa_workload_dependency_live_drain() {
	local label="$1" bin_dir="$2" log_dir="$3" timeout="$4"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-workload-dependency-${label}" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-workload-dependency-${label}" reducer_pid "${bin_dir}/eshu-reducer"
	"${bin_dir}/eshu-golden-corpus-gate" -phase=drains \
		-snapshot=testdata/golden/e2e-20repo-snapshot.json -drain-timeout="${timeout}" || return 1
	ifa_det_stop_join_untrack_bg_pid "${projector_pid}" TERM || return 1
	ifa_det_stop_join_untrack_bg_pid "${reducer_pid}" TERM || return 1
}

# Args: bin cassette workload-expected repo-expected workers log timeout.
# The caller owns the fresh stack and schema lifecycle so this proof runs once
# inside each N cell and its exact graph truth enters that cell's digest.
ifa_workload_dependency_live_run_matrix_cell() {
	local bin_dir="$1" cassette="$2" expected_edges="$3" repo_expected="$4"
	local workers="$5" log_dir="$6" timeout="$7"
	ifa_workload_dependency_live_drive "${bin_dir}" "${cassette}" "${workers}" || return 1
	ifa_workload_dependency_live_drain pre "${bin_dir}" "${log_dir}" "${timeout}" || return 1
	ifa_workload_dependency_live_assert_owned_absent "${bin_dir}" pre-maintenance || return 1
	ifa_repo_dependency_live_run_maintenance_pass workload-dependency "${bin_dir}" "${log_dir}" || return 1
	ifa_workload_dependency_live_drain repo "${bin_dir}" "${log_dir}" "${timeout}" || return 1
	ifa_workload_dependency_live_assert_repo_prerequisite "${bin_dir}" "${repo_expected}" || return 1
	ifa_workload_dependency_live_assert "${bin_dir}" "${expected_edges}" || return 1
}
