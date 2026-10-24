#!/usr/bin/env bash
# `plan` output for scripts/collector-cutover-rollback.sh. Sourced, not run. It
# prints the same argv the step library executes (both come from the build_*
# functions in the main script), so the printed plan cannot drift from `run`.

# The option variables, command builders, and show_cmd come from the main script.
# shellcheck disable=SC2154

plan_psql_line() { # sql-name description
	printf '  $ %s -X -A -t -F "|" -v ON_ERROR_STOP=1 -v kind=%s ... -f %s   # %s (read-only)\n' \
		"$ROLLBACK_PSQL_BIN" "$collector_kind" "$(sql_file "$1")" "$2"
}

print_plan() {
	local instance kind
	printf 'Rollback plan for collector kind %s\n' "$collector_kind"
	if [[ "$in_tree_direct" == "true" ]]; then
		printf 'External instance(s): %s   In-tree producer: direct collector (deployment/%s), no collector instance\n' \
			"$external_instances" "$in_tree_deployment"
		printf 'Every claim of the kind counts as external; step 5 waits for a generation observed and activated after the window start.\n'
	else
		printf 'External instance(s): %s   In-tree instance(s): %s\n' "$external_instances" "$in_tree_instances"
	fi
	printf 'Nothing below has been executed. "run --yes" performs it in this order.\n\n'

	printf 'STEP 1 stop new claims for the external producer\n'
	local -a externals
	IFS=, read -r -a externals <<<"$external_instances"
	for instance in "${externals[@]}"; do
		build_disable "$instance"
		show_cmd
	done
	build_component_list
	show_cmd
	printf '  (refuses before any scale when the component still has an enabled activation)\n'
	build_scale_worker_zero
	show_cmd
	printf '  (a worker Deployment that is already absent counts as stopped)\n'
	build_restart_coordinator
	show_cmd
	build_coordinator_status
	show_cmd
	build_worker_pods
	show_cmd
	build_worker_replicas
	show_cmd
	printf '  Gate: no worker pod remains, the worker Deployment is at 0 replicas or absent, the\n'
	printf '  component has no enabled activation, and the coordinator rollout is complete.\n'
	printf '  Record: disable result per instance, worker pod count 0, coordinator rollout line.\n\n'

	printf 'STEP 2 wait for in-flight leases to drain (refuses to start before step 1 is confirmed)\n'
	plan_psql_line drain "claimed work items and active claims held by any instance of the kind that is not in-tree (every instance in direct mode)"
	printf '  Gate: 0 claimed work items and 0 active claims. The coordinator reaper expires stale\n'
	printf '  leases on its own interval; this step only waits. It never rewrites claim rows.\n'
	printf '  Record: final counts and the seconds waited.\n\n'

	printf 'STEP 3 revoke the external producer grant (refuses to start before step 2 drained)\n'
	for kind in "${grant_kinds[@]}"; do
		build_revoke "$kind"
		show_cmd
	done
	build_grants
	show_cmd
	printf '  Gate: every grant for %s@%s shows revoked=true.\n' "$producer_id" "$component_version"
	printf '  Record: the grants listing after revocation.\n\n'

	printf 'STEP 4 restore the in-tree producer (refuses unless steps 1 to 3 still hold)\n'
	plan_psql_line drain "re-check the drain gate immediately before restoring"
	plan_psql_line dual "re-check that no overlap exists, which catches a restore applied too early"
	if [[ "$restore_by_gitops" == "true" ]]; then
		printf '  (restore values were applied through GitOps after step 3; the script only scales and waits)\n'
	else
		build_helm_restore
		show_cmd
	fi
	build_scale_in_tree
	show_cmd
	build_in_tree_status
	show_cmd
	if [[ "$in_tree_direct" == "true" ]]; then
		printf '  (a direct collector is restored by scaling its Deployment; the coordinator does not plan it, so it is not restarted again)\n'
		printf '  (the replay window starts at the exact database clock, with no skew margin)\n'
	else
		build_restart_coordinator
		show_cmd
		build_coordinator_status
		show_cmd
	fi
	printf '  Record: restore start time (database clock), helm revision or GitOps commit, rollout lines.\n\n'

	printf 'STEP 5 replay from the last committed generation\n'
	if [[ "$in_tree_direct" == "true" ]]; then
		plan_psql_line direct-commit "scope generations of the kind observed and activated since the restore start"
		printf '  Gate: at least one generation was observed and activated after the window start. A source that did not\n'
		printf '  change mints none, so the step times out (exit 5) until the source changes.\n'
		printf '  Record: the generation count and the latest activation time.\n\n'
	else
		plan_psql_line replay "work items for the in-tree instance(s) updated since the restore start"
		printf '  The restart in step 4 triggers the first planning pass; later passes follow the\n'
		printf '  coordinator reconcile interval. Gate: no pending, claimed, or retryable item remains,\n'
		printf '  at least one completed, none failed terminal.\n'
		printf '  Record: pending/claimed/completed/retryable/terminal counts.\n\n'
	fi

	printf 'STEP 6 verify exact truth against the baseline captured just before step 1\n'
	plan_psql_line dual "dual-authority check: external claims, in-tree claims, overlapping claim intervals"
	plan_psql_line truth "per fact kind: count, stable-key set digest, payload set digest over active generations"
	printf '  Gate: no dual authority, and every fact kind equals the baseline file.\n'
	printf '  On a mismatch, "verify --report-drift" names the keys that moved.\n'
	printf '  Then compare the API and MCP read-truth queries in the runbook.\n'
	printf '  Record: the verify output and the read-truth comparison.\n'
}
