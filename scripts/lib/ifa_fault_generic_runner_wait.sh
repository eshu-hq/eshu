#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# The wait_stage=runner non-vacuity predicate (split out of
# ifa_fault_generic_cells.sh -- see that file's header for why the split).
#
# UNPROVEN: no family in ifa_family_registry.sh declares wait_stage=runner
# today (handles_route/runs_in/invokes_cloud_action would need it, per that
# registry file's own note on code_call_materialization's second-stage
# domains). This has never run against a live stack. Built for completeness
# because Task 2 requires it declared now; do NOT report it as proven until a
# real wait_stage=runner family exercises it.
#
# Sourced by ifa_fault_generic_cells.sh, which also supplies the driver-owned
# globals this file reads (log_dir is not needed here; only ifa_det_pg from
# ifa_fault_injection_common.sh).

# ifa_fault_wait_for_claimed_projection_intent mirrors
# ifa_fault_wait_for_claimed's polling shape (ifa_fault_injection_common.sh)
# for the SECOND-stage shared-projection queue instead of the first-stage
# fact_work_items claim. shared_projection_intents
# (migrations/008_shared_projection_intents.sql) has no separate lease/
# claim-owner column the way fact_work_items does -- "claimed" here is read
# as "a row exists under this projection_domain with completed_at IS NULL",
# the strongest non-vacuity signal this schema exposes.
ifa_fault_wait_for_claimed_projection_intent() {
	local compose_project="$1" use_compose="$2" dsn="$3" compose_file="$4"
	local budget="${5:-60}" domain="$6"
	if [[ ! "${budget}" =~ ^[1-9][0-9]*$ ]]; then
		echo "ifa_fault_wait_for_claimed_projection_intent: budget must be a positive integer, got ${budget}" >&2
		return 1
	fi
	if [[ ! "${domain}" =~ ^[a-z0-9_]+$ ]]; then
		echo "ifa_fault_wait_for_claimed_projection_intent: domain must match ^[a-z0-9_]+$, got ${domain}" >&2
		return 1
	fi
	local count
	count="$(ifa_det_pg "${compose_project}" "${use_compose}" "${dsn}" \
		"CREATE OR REPLACE FUNCTION pg_temp.ifa_wait_for_claimed_intent(wait_seconds integer)
		 RETURNS integer LANGUAGE plpgsql AS \$\$
		 DECLARE
		   observed integer;
		   deadline timestamptz := clock_timestamp() + make_interval(secs => wait_seconds);
		 BEGIN
		   LOOP
		     SELECT count(*) INTO observed
		       FROM shared_projection_intents
		      WHERE projection_domain = '${domain}' AND completed_at IS NULL;
		     IF observed > 0 THEN
		       RETURN observed;
		     END IF;
		     EXIT WHEN clock_timestamp() >= deadline;
		     PERFORM pg_sleep(0.001);
		   END LOOP;
		   RETURN 0;
		 END
		 \$\$;
		 SELECT pg_temp.ifa_wait_for_claimed_intent(${budget});" \
		"${compose_file}" | tail -n 1 | tr -d '[:space:]')"
	if [[ -n "${count}" && "${count}" -gt 0 ]]; then
		printf '%s' "${count}"
		return 0
	fi
	echo "ifa_fault_wait_for_claimed_projection_intent: no uncompleted shared_projection_intents row for projection_domain=${domain} appeared within ${budget}s" >&2
	return 1
}
