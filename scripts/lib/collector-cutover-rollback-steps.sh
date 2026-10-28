#!/usr/bin/env bash
# Step implementations for scripts/collector-cutover-rollback.sh. Sourced, not
# run: it relies on the option variables, command builders, and exit codes the
# main script defines.
#
# Safety model. Every step opens with gates for the steps before it. A gate is a
# read-only check of the real world (pods, claim rows, grants), never a memory
# of what this process did earlier. That makes the script resumable and
# idempotent, and it is what keeps two producers from ever holding authority at
# once: step 2 will not wait before step 1 is confirmed, step 3 will not revoke
# before the drain, and step 4 will not restore before steps 1 to 3 all hold.

# The option variables and exit codes come from the sourcing script.
# shellcheck disable=SC2154,SC2034

# shellcheck source=scripts/lib/collector-cutover-rollback-gates.sh
source "${script_dir}/lib/collector-cutover-rollback-gates.sh"
# shellcheck source=scripts/lib/collector-cutover-rollback-truth.sh
source "${script_dir}/lib/collector-cutover-rollback-truth.sh"

# --- output helpers -------------------------------------------------------------

step_header() { # number title
	printf '\n== STEP %s/6 %s ==\n' "$1" "$2"
}

evidence() { # step text
	printf 'EVIDENCE[step %s]: %s\n' "$1" "$2"
}

step_ok() { # step
	printf 'RESULT[step %s]: ok\n' "$1"
}

refuse() { # step message
	printf 'REFUSED[step %s]: %s\n' "$1" "$2" >&2
	exit "$EX_REFUSED"
}

step_failed() { # step message
	printf 'FAILED[step %s]: %s\n' "$1" "$2" >&2
	exit "$EX_STEP_FAILED"
}

# --- command execution ------------------------------------------------------------

XOUT=""
XRC=0

# exec_cmd prints and runs CMD, keeping its output in XOUT and status in XRC.
exec_cmd() {
	show_cmd
	quiet_cmd
	if [[ -n "$XOUT" ]]; then
		printf '%s\n' "$XOUT" | sed 's/^/    /'
	fi
}

# quiet_cmd runs CMD without echoing it, for polled checks.
quiet_cmd() {
	XRC=0
	XOUT="$("${CMD[@]}" 2>&1)" || XRC=$?
}

# must_exec runs CMD and aborts the step when it fails.
must_exec() { # step description
	exec_cmd
	if [[ "$XRC" -ne 0 ]]; then
		step_failed "$1" "$2 failed with exit ${XRC}; nothing after it was run"
	fi
}

# psql_run runs one read-only SQL file. Connection settings come from the libpq
# environment; no credential is ever placed on the command line.
psql_run() { # sql-name -v name=value ...
	local name="$1"
	shift
	XRC=0
	XOUT="$("$ROLLBACK_PSQL_BIN" -X -A -t -F '|' -v ON_ERROR_STOP=1 "$@" -f "$(sql_file "$name")" 2>&1)" || XRC=$?
	XOUT="$(printf '%s\n' "$XOUT" | awk 'NF')"
}

# wait_until polls fn until it returns 0 or the timeout passes.
wait_until() { # timeout-seconds fn
	local deadline=$((SECONDS + $1))
	while true; do
		if "$2"; then
			return 0
		fi
		if ((SECONDS >= deadline)); then
			return 1
		fi
		sleep "$poll_seconds"
	done
}

# --- steps ----------------------------------------------------------------------------------

# step_1_gitops is the activations-mode replacement for step 1. The GitOps
# removal commit, merged and synced by the operator, is the durable act; this
# step never runs `component disable`, because on this topology that is a
# pod-local write the next pod start undoes. It proves the removal instead, and
# it scales and restarts nothing: the sync already rolled the workloads.
step_1_gitops() {
	step_header 1 "stop new claims for the external producer (activations/GitOps mode)"
	printf 'The removal commit %s in %s replaced the disable. This step proves it; it disables nothing.\n' \
		"$removal_commit" "$gitops_repo"
	build_git_show_removal
	exec_cmd
	if [[ "$XRC" -ne 0 ]]; then
		step_failed 1 "reading the removal commit failed with exit ${XRC}; nothing after it was run"
	fi
	# The registry, not the caller's list, says whether new claims are stopped.
	# A mistyped instance ID is absent from the removal diff while the real one
	# still runs, so refuse here, before anything is waited on.
	if ! registry_clear; then
		[[ "$REGISTRY_COUNT" != "unknown" ]] || step_failed 1 "$REGISTRY_NOTE"
		refuse 1 "$REGISTRY_NOTE"
	fi
	if ! activations_configmap_clear; then
		refuse 1 "$CONFIGMAP_NOTE"
	fi
	confirm_step1_gitops || step_failed 1 "$GITOPS_CONFIRM_NOTE"
	evidence 1 "removal=${removal_commit} worker_pods=0 component_activations=0 coordinator='${GITOPS_CONFIRM_NOTE}'"
	step_ok 1
}

step_1() {
	if [[ "$activations_mode" == "true" ]]; then
		step_1_gitops
		return 0
	fi
	step_header 1 "stop new claims for the external producer"
	local instance disabled=""
	local -a externals
	IFS=, read -r -a externals <<<"$external_instances"
	for instance in "${externals[@]}"; do
		build_disable "$instance"
		exec_cmd
		if [[ "$XRC" -eq 0 ]]; then
			disabled+="${instance}=disabled "
		elif [[ "$XOUT" == *"is not enabled for component"* ]]; then
			# "Not enabled" is also what a mistyped ID reads. Continuing is safe only
			# because the registry check below decides whether the component is clear.
			printf '  instance %s is not enabled (already disabled, or the ID is wrong); the registry check below decides\n' "$instance"
			disabled+="${instance}=not-enabled "
		else
			step_failed 1 "disabling instance ${instance} failed with exit ${XRC}; nothing after it was run"
		fi
	done
	# The registry, not the caller's list, says whether new claims are stopped. A
	# mistyped instance ID reads "not enabled" above while the real one still runs,
	# so refuse here, before anything is scaled or restarted.
	if ! registry_clear; then
		[[ "$REGISTRY_COUNT" != "unknown" ]] || step_failed 1 "$REGISTRY_NOTE"
		refuse 1 "$REGISTRY_NOTE"
	fi
	local worker_state="scaled-to-zero"
	build_scale_worker_zero
	exec_cmd
	if [[ "$XRC" -ne 0 ]]; then
		if is_not_found "$XOUT"; then
			# A restore overlay that turns the extension collector off deletes the
			# worker Deployment, so a rerun finds nothing to scale. That is stopped.
			printf '  deployment/%s does not exist; treating the worker as already removed\n' "$worker_deployment"
			worker_state="absent"
		else
			step_failed 1 "scaling the extension worker to zero failed with exit ${XRC}; nothing after it was run"
		fi
	fi
	build_restart_coordinator
	must_exec 1 "restarting the coordinator so it re-reads the component registry"
	confirm_step1 || step_failed 1 "$CONFIRM_NOTE"
	evidence 1 "${disabled}worker=${worker_state} worker_pods=0 component_activations=0 coordinator='${CONFIRM_NOTE}'"
	step_ok 1
}

step_2() {
	step_header 2 "wait for in-flight leases to drain"
	gate_step1 2
	printf 'Waiting up to %ss for claimed work to drain. The coordinator reaper expires stale\n' "$drain_timeout"
	printf 'leases on its own interval; this step only observes and never rewrites claim rows.\n'
	local started=$SECONDS
	LAST_DRAIN_REPORT=""
	if ! wait_until "$drain_timeout" drain_poll; then
		printf 'TIMEOUT[step 2]: %s claimed work item(s) and %s active claim(s) remain after %ss.\n' \
			"$DRAIN_CLAIMED" "$DRAIN_ACTIVE" "$drain_timeout" >&2
		printf 'Do not continue. Check that the coordinator is running its reaper (ESHU_WORKFLOW_COORDINATOR_REAP_INTERVAL)\n' >&2
		printf 'and that no worker for the external instance is still heartbeating, then rerun.\n' >&2
		exit "$EX_TIMEOUT"
	fi
	evidence 2 "claimed_work_items=0 active_claims=0 waited=$((SECONDS - started))s"
	step_ok 2
}

# step_3_gitops is the activations-mode replacement for step 3. No
# `revoke-grant` is executed: the removal deleted the grant source, so zero
# grant rows ARE the proof, guarded by the registry and the removal-commit diff
# so a mistyped producer ID cannot read as revoked.
step_3_gitops() {
	step_header 3 "prove the external producer grants are gone (activations/GitOps mode)"
	gate_step1 3
	gate_step2 3
	build_grants
	show_cmd
	if ! grants_absent; then
		step_failed 3 "${GRANT_NOTE} after the GitOps removal"
	fi
	printf '%s\n' "$GRANT_ROWS" | sed 's/^/    /'
	evidence 3 "no grants remain for ${producer_id}@${component_version}; removal ${removal_commit} deletes the activation"
	step_ok 3
}

step_3() {
	if [[ "$activations_mode" == "true" ]]; then
		step_3_gitops
		return 0
	fi
	step_header 3 "revoke the external producer grant"
	gate_step1 3
	gate_step2 3
	local kind
	for kind in "${grant_kinds[@]}"; do
		build_revoke "$kind"
		must_exec 3 "revoking the grant for kind ${kind}"
	done
	build_grants
	show_cmd
	if ! grants_revoked; then
		step_failed 3 "${GRANT_NOTE} after revocation"
	fi
	printf '%s\n' "$GRANT_ROWS" | sed 's/^/    /'
	evidence 3 "grants for ${producer_id}@${component_version} revoked for kinds: ${grant_kinds[*]}"
	step_ok 3
}

step_4() {
	step_header 4 "restore the last supported (in-tree) producer"
	gate_step1 4
	gate_step2 4
	gate_step3 4
	if [[ -z "$since" ]]; then
		# A direct producer shares its collector kind with the external producer
		# that just stopped, so a skew margin could count that producer's last
		# commit as the restored one. The exact clock can only be stricter.
		local skew="$ROLLBACK_CLOCK_SKEW_SECONDS"
		[[ "$in_tree_direct" != "true" ]] || skew=0
		psql_run now -v "skew=${skew}"
		[[ "$XRC" -eq 0 && -n "$XOUT" ]] || step_failed 4 "could not read the database clock for the replay window"
		since="$XOUT"
	fi
	printf 'Replay window starts at database time %s\n' "$since"
	if [[ "$restore_by_gitops" == "true" ]]; then
		printf 'Restore values were applied through GitOps after step 3; skipping helm.\n'
	else
		build_helm_restore
		must_exec 4 "applying the rollback values with helm"
	fi
	build_scale_in_tree
	must_exec 4 "scaling the in-tree collector Deployment"
	build_in_tree_status
	must_exec 4 "waiting for the in-tree collector rollout"
	local in_tree_line="$XOUT"
	local coordinator_line="not restarted (a direct producer is not planned by the coordinator)"
	if [[ "$in_tree_direct" != "true" ]]; then
		build_restart_coordinator
		must_exec 4 "restarting the coordinator so it plans for the restored instance"
		build_coordinator_status
		must_exec 4 "waiting for the coordinator rollout"
		coordinator_line="$XOUT"
	fi
	evidence 4 "replay_since='${since}' in_tree='${in_tree_line}' coordinator='${coordinator_line}'"
	step_ok 4
}

REPLAY_COUNTS=""
replay_poll() {
	psql_run replay -v "kind=${collector_kind}" -v "instances=${in_tree_instances}" -v "since=${since}"
	if [[ "$XRC" -ne 0 || ! "$XOUT" =~ ^[0-9]+\|[0-9]+\|[0-9]+\|[0-9]+\|[0-9]+$ ]]; then
		step_failed 5 "could not read replay progress (exit ${XRC})"
	fi
	if [[ "$XOUT" != "$REPLAY_COUNTS" ]]; then
		REPLAY_COUNTS="$XOUT"
		IFS='|' read -r r_pending r_claimed r_completed r_retryable r_terminal <<<"$XOUT"
		printf '  pending=%s claimed=%s completed=%s retryable=%s terminal=%s\n' \
			"$r_pending" "$r_claimed" "$r_completed" "$r_retryable" "$r_terminal"
	fi
	IFS='|' read -r r_pending r_claimed r_completed r_retryable r_terminal <<<"$XOUT"
	if ((r_terminal > 0)); then
		step_failed 5 "${r_terminal} replay work item(s) failed terminally; the in-tree producer did not recover. Inspect them before retrying."
	fi
	((r_pending == 0 && r_claimed == 0 && r_retryable == 0 && r_completed >= 1))
}

# direct_poll is the step 5 predicate for a direct in-tree producer. It has no
# work items to replay, so it passes only when a generation of the kind was
# observed at or after the window start and has been activated. An unreadable
# answer aborts: a failed read never looks like progress.
DIRECT_COMMITS=""
DIRECT_LATEST=""
direct_poll() {
	psql_run direct-commit -v "kind=${collector_kind}" -v "since=${since}"
	if [[ "$XRC" -ne 0 || ! "$XOUT" =~ ^[0-9]+\|[^|]*$ ]]; then
		step_failed 5 "could not read the observed-generation signal (exit ${XRC})"
	fi
	if [[ "$XOUT" != "${DIRECT_COMMITS}|${DIRECT_LATEST}" ]]; then
		DIRECT_COMMITS="${XOUT%%|*}"
		DIRECT_LATEST="${XOUT#*|}"
		printf '  generations_observed_and_activated_since_window_start=%s latest_activated_at=%s\n' "$DIRECT_COMMITS" "${DIRECT_LATEST:-none}"
	fi
	((DIRECT_COMMITS >= 1))
}

step_5_direct() {
	printf 'Waiting up to %ss for the restored direct collector to observe a generation after %s and for it to be activated.\n' "$replay_timeout" "$since"
	printf 'It has no work items to replay; the signal is a scope generation observed and activated after the window start.\n'
	DIRECT_COMMITS=""
	DIRECT_LATEST=""
	if ! wait_until "$replay_timeout" direct_poll; then
		printf 'TIMEOUT[step 5]: no generation was observed and activated for kind %s after %s within %ss.\n' \
			"$collector_kind" "$since" "$replay_timeout" >&2
		printf 'A source that did not change mints no new generation; change the source, or check the collector logs.\n' >&2
		exit "$EX_TIMEOUT"
	fi
	evidence 5 "since='${since}' direct_observed_generations=${DIRECT_COMMITS} latest_activated_at=${DIRECT_LATEST}"
}

step_5() {
	step_header 5 "replay from the last committed generation"
	gate_step1 5
	gate_step2 5
	gate_step3 5
	gate_step4 5
	if [[ "$in_tree_direct" == "true" ]]; then
		step_5_direct
		step_ok 5
		return 0
	fi
	printf 'Waiting up to %ss for the restored producer to re-plan and finish from window start %s.\n' "$replay_timeout" "$since"
	printf 'The coordinator restart in step 4 triggers the first planning pass; later passes follow\n'
	printf 'ESHU_WORKFLOW_COORDINATOR_RECONCILE_INTERVAL.\n'
	REPLAY_COUNTS=""
	if ! wait_until "$replay_timeout" replay_poll; then
		printf 'TIMEOUT[step 5]: replay did not finish within %ss (last counts %s).\n' "$replay_timeout" "$REPLAY_COUNTS" >&2
		exit "$EX_TIMEOUT"
	fi
	evidence 5 "since='${since}' counts(pending|claimed|completed|retryable|terminal)=${REPLAY_COUNTS}"
	step_ok 5
}

step_6() {
	step_header 6 "verify exact fact truth against the baseline"
	gate_step1 6
	gate_step2 6
	gate_step3 6
	gate_step4 6
	do_verify 6
	printf 'Now compare the API and MCP read-truth queries in the runbook with the answers from before the rollback.\n'
	step_ok 6
}

# --- subcommands --------------------------------------------------------------------------------

cmd_verify() {
	if [[ -n "$capture_file" ]]; then
		capture_baseline
		return 0
	fi
	[[ -n "$baseline_file" ]] || die "$EX_USAGE" "verify needs --baseline FILE (compare) or --capture FILE (write)"
	printf '== VERIFY (read-only) ==\n'
	do_verify 6
	printf 'VERIFY OK\n'
}

cmd_run() {
	[[ "$assume_yes" == "true" ]] || die "$EX_USAGE" "run changes live workloads and grants. Review 'plan', then pass --yes."
	if ((through_step >= 6)) && [[ ! -s "$baseline_file" ]]; then
		die "$EX_USAGE" "run needs --baseline FILE captured just before step 1 (capture it with: verify --capture FILE --capture-keys)"
	fi
	if ((through_step >= 6)); then
		# Check the format now, read-only, so a stale baseline cannot first fail at
		# step 6 after the in-tree producer was already restored.
		local problem
		if problem="$(baseline_format_problem)"; then
			die "$EX_USAGE" "$problem"
		fi
	fi
	if ((from_step == 5)) && [[ -z "$since" ]]; then
		die "$EX_USAGE" "--since is required with --from-step 5 (use the replay_since value from step 4)"
	fi
	printf 'Rollback of collector kind %s: external %s -> in-tree %s\n' "$collector_kind" "$external_instances" \
		"${in_tree_instances:-direct collector (deployment/${in_tree_deployment})}"
	local step
	for ((step = from_step; step <= through_step; step++)); do
		"step_${step}"
	done
	if ((through_step >= 6)); then
		printf '\nROLLBACK COMPLETE: the in-tree producer holds authority and truth matches the baseline.\n'
		printf 'Record every EVIDENCE line above in the cutover issue.\n'
	else
		printf '\nSTOPPED after step %s (--through-step). Resume with --from-step %s.\n' "$through_step" "$((through_step + 1))"
	fi
}
