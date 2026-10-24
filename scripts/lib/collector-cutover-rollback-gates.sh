#!/usr/bin/env bash
# Read-only predicates and the refusal gates for scripts/collector-cutover-
# rollback.sh. Sourced by the step library, which supplies exec_cmd, quiet_cmd,
# psql_run, wait_until, refuse, step_failed, and the XOUT/XRC result variables.
#
# A predicate reads the real world (pods, claim rows, the registry, grants) and
# never remembers what this process did earlier, so every gate can be re-run on
# a resume. A predicate that cannot read its source answers "not satisfied": a
# failed or unreadable check never looks like a clear one.

# The option variables, command builders, and exit codes come from the main script;
# GRANT_ROWS and LAST_DRAIN_REPORT are read by the step library.
# shellcheck disable=SC2154,SC2034

# --- predicates ---------------------------------------------------------------

# worker_stopped is true when the extension worker is provably stopped: no pod
# matches the selector AND the Deployment is at zero replicas or absent. The
# second condition matters because a selector that matches nothing (custom pod
# labels, the wrong namespace) would otherwise pass while a worker still runs.
POD_NOTE=""
worker_pods_gone() {
	build_worker_pods
	quiet_cmd
	if [[ "$XRC" -ne 0 ]]; then
		POD_NOTE="listing worker pods failed with exit ${XRC}: ${XOUT}"
		return 1
	fi
	# `-o name` prints one "pod/<name>" line per pod. Other output (kubectl's "No
	# resources found" notice goes to stderr, which quiet_cmd merges) is not a pod.
	if [[ -n "$(printf '%s\n' "$XOUT" | awk '/^pod\//')" ]]; then
		POD_NOTE="worker pod(s) still exist"
		return 1
	fi
	build_worker_replicas
	quiet_cmd
	if [[ "$XRC" -eq 0 ]]; then
		if [[ "$XOUT" != "0" ]]; then
			POD_NOTE="deployment/${worker_deployment} still wants '${XOUT}' replica(s), so no pod matching the selector does not prove it stopped"
			return 1
		fi
	elif ! is_not_found "$XOUT"; then
		POD_NOTE="reading deployment/${worker_deployment} failed with exit ${XRC}: ${XOUT}"
		return 1
	fi
	POD_NOTE=""
	return 0
}

# is_not_found is true when kubectl output reports a missing object.
is_not_found() { [[ "$1" == *"(NotFound)"* ]]; }

# DRAIN_CLAIMED and DRAIN_ACTIVE hold the last drain query result.
DRAIN_CLAIMED=""
DRAIN_ACTIVE=""

# read_drain queries the claim table. An unreadable or malformed answer aborts:
# a database that cannot be read must never look like an empty claim table.
read_drain() { # step
	psql_run drain -v "kind=${collector_kind}" -v "in_tree=${in_tree_instances}"
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

# read_registry sets REGISTRY_COUNT to how many activations of the component
# remain enabled, summed over its installed versions, or "unknown" when the
# listing is unreadable or malformed. Only `eshu component list`'s tab-separated
# "activations=N" column is read.
REGISTRY_NOTE=""
REGISTRY_COUNT=""
read_registry() {
	build_component_list
	quiet_cmd
	if [[ "$XRC" -ne 0 ]]; then
		REGISTRY_NOTE="listing the component registry failed with exit ${XRC}: ${XOUT}"
		REGISTRY_COUNT="unknown"
		return 0
	fi
	REGISTRY_NOTE=""
	REGISTRY_COUNT="$(printf '%s\n' "$XOUT" | awk -F'\t' -v id="$component_id" '
		$1 == id {
			if ($NF ~ /^activations=[0-9]+$/) { sub(/^activations=/, "", $NF); total += $NF } else { bad = 1 }
		}
		END { if (bad) print "unknown"; else print total + 0 }')"
}

# registry_clear is true when the component has no enabled activation. A
# mistyped --external-instance leaves the real activation enabled, so this
# check, not the caller's list, decides whether step 1 reached its goal.
registry_clear() {
	read_registry
	if [[ "$REGISTRY_COUNT" == "0" ]]; then
		return 0
	fi
	if [[ "$REGISTRY_COUNT" == "unknown" ]]; then
		REGISTRY_NOTE="${REGISTRY_NOTE:-the registry listing for ${component_id} could not be parsed}"
	else
		REGISTRY_NOTE="component ${component_id} still has ${REGISTRY_COUNT} enabled activation(s) after disabling ${external_instances}; the instance ID may be mistyped or the component may run more instances than you named. List them with 'eshu component list' and pass every one"
	fi
	return 1
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

# read_direct_running sets DIRECT_LIVE to yes, no, or unknown for the direct
# collector Deployment. It is running when spec.replicas or status.replicas is
# above zero, so a pod still stopping after a scale-down counts. An unreadable,
# missing, or malformed answer is unknown, and callers treat unknown as running:
# a mistyped Deployment name must never read as "no producer is live".
DIRECT_LIVE="unknown"
DIRECT_NOTE=""
read_direct_running() {
	build_in_tree_replicas
	quiet_cmd
	DIRECT_NOTE=""
	if [[ "$XRC" -ne 0 || ! "$XOUT" =~ ^[0-9]+\|[0-9]*$ ]]; then
		DIRECT_LIVE="unknown"
		DIRECT_NOTE="deployment/${in_tree_deployment} could not be read (exit ${XRC}): $(printf '%s' "$XOUT" | head -c 200 | tr '\n' ' ')"
		return 0
	fi
	local desired="${XOUT%%|*}" observed="${XOUT##*|}"
	if ((10#${desired} > 0 || 10#${observed:-0} > 0)); then
		DIRECT_LIVE="yes"
	else
		DIRECT_LIVE="no"
	fi
}

# --- gates (refuse when a precondition does not hold) --------------------------

# confirm_step1 returns 0 when new claims are provably stopped: the component has
# no enabled activation, the worker is stopped, and the coordinator rollout
# (which re-reads the registry) completed.
CONFIRM_NOTE=""
confirm_step1() {
	if ! registry_clear; then
		CONFIRM_NOTE="$REGISTRY_NOTE"
		return 1
	fi
	build_worker_pods
	show_cmd
	if ! wait_until "$step_timeout" worker_pods_gone; then
		CONFIRM_NOTE="the worker is not confirmed stopped after ${step_timeout}s${POD_NOTE:+ ($POD_NOTE)}"
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
	printf 'GATE before step %s: no non-in-tree instance of the kind may hold claimed work\n' "$1"
	read_drain "$1"
	if ! drain_clear; then
		refuse "$1" "instance(s) of kind ${collector_kind} other than the in-tree set still hold ${DRAIN_CLAIMED} claimed work item(s) and ${DRAIN_ACTIVE} active claim(s). Two producers must never hold authority for one scope; finish step 2 first."
	fi
	printf '  claimed_work_items=0 active_claims=0\n'
}

# gate_step3 also runs the dual-authority check: it is the last gate before the
# in-tree producer is restored, so an in-tree producer that GitOps restored too
# early (claiming beside a live external one) is caught here, not at step 6.
gate_step3() { # for-step
	printf 'GATE before step %s: the external producer grants must be revoked\n' "$1"
	if ! grants_revoked; then
		refuse "$1" "${GRANT_NOTE}. Revoke the external producer grant (step 3) before restoring the in-tree producer."
	fi
	printf '  all %s grant(s) revoked\n' "${#grant_kinds[@]}"
	check_authority "$1"
}

gate_step4() { # for-step
	printf 'GATE before step %s: the in-tree producer must be restored\n' "$1"
	in_tree_rolled_out || refuse "$1" "deployment/${in_tree_deployment} is not rolled out; restore the in-tree producer (step 4) first."
	printf '  deployment/%s rolled out\n' "$in_tree_deployment"
}
