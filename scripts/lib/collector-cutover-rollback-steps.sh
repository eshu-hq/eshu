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
# shellcheck disable=SC2154

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
	XOUT="$("$PSQL_BIN" -X -A -t -F '|' -v ON_ERROR_STOP=1 "$@" -f "$(sql_file "$name")" 2>&1)" || XRC=$?
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

# --- predicates (read-only) ----------------------------------------------------------

# worker_pods_gone is true when no component-extension worker pod remains.
worker_pods_gone() {
	build_worker_pods
	quiet_cmd
	[[ "$XRC" -eq 0 ]] || return 1
	[[ -z "$(printf '%s\n' "$XOUT" | awk 'NF')" ]]
}

# DRAIN_CLAIMED and DRAIN_ACTIVE hold the last drain query result.
DRAIN_CLAIMED=""
DRAIN_ACTIVE=""

# read_drain queries the claim table. An unreadable or malformed answer aborts:
# a database that cannot be read must never look like an empty claim table.
read_drain() { # step
	psql_run drain -v "kind=${collector_kind}" -v "instances=${external_instances}"
	if [[ "$XRC" -ne 0 || ! "$XOUT" =~ ^[0-9]+\|[0-9]+$ ]]; then
		step_failed "$1" "could not read the claim table (exit ${XRC}); treating the drain as unknown"
	fi
	DRAIN_CLAIMED="${XOUT%%|*}"
	DRAIN_ACTIVE="${XOUT##*|}"
}

drain_clear() { [[ "$DRAIN_CLAIMED" == "0" && "$DRAIN_ACTIVE" == "0" ]]; }

# drain_poll refreshes the counts for wait_until, reporting each change once.
LAST_DRAIN_REPORT=""
drain_poll() {
	read_drain 2
	local now="${DRAIN_CLAIMED}|${DRAIN_ACTIVE}"
	if [[ "$now" != "$LAST_DRAIN_REPORT" ]]; then
		printf '  claimed_work_items=%s active_claims=%s\n' "$DRAIN_CLAIMED" "$DRAIN_ACTIVE"
		LAST_DRAIN_REPORT="$now"
	fi
	drain_clear
}

# grants_revoked is true when every granted kind shows revoked=true and no live
# twin remains. A missing row is not a revocation. GRANT_NOTE explains a failure.
GRANT_NOTE=""
GRANT_ROWS=""
grants_revoked() {
	build_grants
	quiet_cmd
	GRANT_ROWS="$XOUT"
	if [[ "$XRC" -ne 0 ]]; then
		GRANT_NOTE="listing grants failed with exit ${XRC}"
		return 1
	fi
	local kind verdict
	for kind in "${grant_kinds[@]}"; do
		verdict="$(printf '%s\n' "$XOUT" | awk -F'\t' -v version="$component_version" -v kind="$kind" -v scope="scope=${grant_scope}" '
			function norm(v) { sub(/^v/, "", v); return v }
			$3 == kind && $4 == scope && norm($2) == norm(version) {
				seen = 1
				if ($NF != "revoked=true") live = 1
			}
			END { if (!seen) print "missing"; else if (live) print "live"; else print "revoked" }')"
		if [[ "$verdict" != "revoked" ]]; then
			GRANT_NOTE="grant for kind ${kind} is ${verdict}"
			return 1
		fi
	done
	return 0
}

# in_tree_rolled_out is true when the restored Deployment finished rolling out.
in_tree_rolled_out() {
	build_in_tree_status
	quiet_cmd
	[[ "$XRC" -eq 0 ]]
}

# --- gates (refuse when a precondition does not hold) --------------------------------------

# confirm_step1 returns 0 when new claims are provably stopped: no worker pod
# remains and the coordinator rollout (which re-reads the registry) completed.
CONFIRM_NOTE=""
confirm_step1() {
	build_worker_pods
	show_cmd
	if ! wait_until "$step_timeout" worker_pods_gone; then
		CONFIRM_NOTE="worker pods matching '${worker_selector}' still exist after ${step_timeout}s"
		return 1
	fi
	build_coordinator_status
	show_cmd
	quiet_cmd
	if [[ "$XRC" -ne 0 ]]; then
		CONFIRM_NOTE="coordinator deployment/${coordinator_deployment} rollout did not complete: ${XOUT}"
		return 1
	fi
	CONFIRM_NOTE="${XOUT}"
	return 0
}

gate_step1() { # for-step
	printf 'GATE before step %s: step 1 (new claims stopped) must be confirmed\n' "$1"
	confirm_step1 || refuse "$1" "step 1 is not confirmed: ${CONFIRM_NOTE}. Do not wait for a drain while the external producer can still claim."
}

gate_step2() { # for-step
	printf 'GATE before step %s: the external instance(s) must hold no claimed work\n' "$1"
	read_drain "$1"
	if ! drain_clear; then
		refuse "$1" "external instance(s) ${external_instances} still hold ${DRAIN_CLAIMED} claimed work item(s) and ${DRAIN_ACTIVE} active claim(s). Two producers must never hold authority for one scope; finish step 2 first."
	fi
	printf '  claimed_work_items=0 active_claims=0\n'
}

gate_step3() { # for-step
	printf 'GATE before step %s: the external producer grants must be revoked\n' "$1"
	if ! grants_revoked; then
		refuse "$1" "${GRANT_NOTE}. Revoke the external producer grant (step 3) before restoring the in-tree producer."
	fi
	printf '  all %s grant(s) revoked\n' "${#grant_kinds[@]}"
}

gate_step4() { # for-step
	printf 'GATE before step %s: the in-tree producer must be restored\n' "$1"
	in_tree_rolled_out || refuse "$1" "deployment/${in_tree_deployment} is not rolled out; restore the in-tree producer (step 4) first."
	printf '  deployment/%s rolled out\n' "$in_tree_deployment"
}

# --- steps ----------------------------------------------------------------------------------

step_1() {
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
			printf '  instance %s is already disabled; continuing (idempotent re-run)\n' "$instance"
			disabled+="${instance}=already-disabled "
		else
			step_failed 1 "disabling instance ${instance} failed with exit ${XRC}; nothing after it was run"
		fi
	done
	build_scale_worker_zero
	must_exec 1 "scaling the extension worker to zero"
	build_restart_coordinator
	must_exec 1 "restarting the coordinator so it re-reads the component registry"
	confirm_step1 || step_failed 1 "$CONFIRM_NOTE"
	evidence 1 "${disabled}worker_pods=0 coordinator='${CONFIRM_NOTE}'"
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

step_3() {
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
		psql_run now
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
	build_restart_coordinator
	must_exec 4 "restarting the coordinator so it plans for the restored instance"
	build_coordinator_status
	must_exec 4 "waiting for the coordinator rollout"
	evidence 4 "replay_since='${since}' in_tree='${in_tree_line}' coordinator='${XOUT}'"
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

step_5() {
	step_header 5 "replay from the last committed generation"
	gate_step1 5
	gate_step2 5
	gate_step3 5
	gate_step4 5
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

# --- verify -----------------------------------------------------------------------------------

# read_truth fills XOUT with the per-kind truth rows or aborts.
read_truth() { # step
	psql_run truth -v "kind=${collector_kind}"
	if [[ "$XRC" -ne 0 ]]; then
		step_failed "$1" "could not read fact truth for ${collector_kind}: ${XOUT}"
	fi
}

truth_view() { # file
	if [[ "$keys_only" == "true" ]]; then
		awk -F'|' 'NF { print $1 "|" $2 "|" $3 }' "$1"
	else
		awk 'NF' "$1"
	fi
}

check_authority() { # step
	psql_run dual -v "kind=${collector_kind}" -v "external=${external_instances}" -v "in_tree=${in_tree_instances}"
	if [[ "$XRC" -ne 0 || ! "$XOUT" =~ ^[0-9]+\|[0-9]+\|[0-9]+$ ]]; then
		step_failed "$1" "could not read the claim table for the authority check (exit ${XRC})"
	fi
	local ext tree overlaps
	IFS='|' read -r ext tree overlaps <<<"$XOUT"
	printf '  external_claimed=%s in_tree_claimed=%s overlapping_claim_pairs=%s\n' "$ext" "$tree" "$overlaps"
	if ((ext > 0 && tree > 0)) || ((overlaps > 0)); then
		printf 'DUAL AUTHORITY[step %s]: both producers hold or held authority for one scope.\n' "$1" >&2
		exit "$EX_DUAL_AUTHORITY"
	fi
	if ((ext > 0)); then
		printf 'REFUSED[step %s]: the external instance still holds %s claimed work item(s).\n' "$1" "$ext" >&2
		exit "$EX_REFUSED"
	fi
	evidence "$1" "no dual authority: external_claimed=0 overlapping_claim_pairs=0"
}

compare_truth() { # step
	[[ -s "$baseline_file" ]] || step_failed "$1" "baseline file '${baseline_file}' is missing or empty"
	read_truth "$1"
	local current baseline
	current="$(mktemp)"
	baseline="$(mktemp)"
	printf '%s\n' "$XOUT" >"$current"
	truth_view "$baseline_file" >"$baseline"
	truth_view "$current" >"${current}.view"
	local diff_out diff_rc=0
	diff_out="$(diff -u --label baseline --label current "$baseline" "${current}.view")" || diff_rc=$?
	rm -f "$current" "${current}.view" "$baseline"
	if [[ "$diff_rc" -ne 0 ]]; then
		printf '%s\n' "$diff_out" | sed 's/^/    /'
		printf 'TRUTH MISMATCH[step %s]: fact truth for %s differs from the baseline.\n' "$1" "$collector_kind" >&2
		exit "$EX_TRUTH_MISMATCH"
	fi
	evidence "$1" "fact truth equals baseline ${baseline_file} (keys_only=${keys_only})"
}

# do_verify runs the read-only authority and truth checks. `run` calls it as
# step 6; the verify subcommand calls it on its own.
do_verify() { # step
	local step="$1"
	check_authority "$step"
	if [[ -n "$namespace" && -n "$worker_deployment" ]]; then
		if worker_pods_gone; then
			evidence "$step" "no extension worker pod remains"
		else
			refuse "$step" "extension worker pods still exist"
		fi
	fi
	if [[ -n "$component_home" && -n "$component_id" && ${#grant_kinds[@]} -gt 0 ]]; then
		[[ -n "$producer_id" ]] || producer_id="$component_id"
		[[ -n "$grant_scope" ]] || grant_scope="$collector_kind"
		if grants_revoked; then
			evidence "$step" "external producer grants are revoked"
		else
			refuse "$step" "$GRANT_NOTE"
		fi
	fi
	compare_truth "$step"
}

step_6() {
	step_header 6 "verify exact fact truth against the baseline"
	gate_step1 6
	gate_step2 6
	gate_step3 6
	gate_step4 6
	do_verify 6
	printf 'Now compare the API and MCP read-truth queries in the runbook with the pre-cutover answers.\n'
	step_ok 6
}

capture_baseline() {
	read_truth 6
	[[ -n "$XOUT" ]] || step_failed 6 "no active facts exist for collector kind ${collector_kind}; an empty baseline would prove nothing"
	printf '%s\n' "$XOUT" >"$capture_file"
	printf 'Captured %s fact kind(s) for %s into %s\n' "$(printf '%s\n' "$XOUT" | awk 'END { print NR }')" "$collector_kind" "$capture_file"
	evidence 6 "baseline written to ${capture_file}; keep it with the cutover record"
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
		die "$EX_USAGE" "run needs --baseline FILE from before the cutover (capture it with: verify --capture FILE)"
	fi
	if ((from_step == 5)) && [[ -z "$since" ]]; then
		die "$EX_USAGE" "--since is required with --from-step 5 (use the replay_since value from step 4)"
	fi
	printf 'Rollback of collector kind %s: external %s -> in-tree %s\n' "$collector_kind" "$external_instances" "$in_tree_instances"
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
