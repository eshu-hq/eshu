#!/usr/bin/env bash
# Gate cases for scripts/collector-cutover-rollback.sh (#8895): each refusal and
# ordering rule is proved by breaking exactly one fact of an otherwise clear
# world and asserting the exit code and that no later step ran. Together with the
# core cases they kill every mutant listed in
# scripts/lib/test-collector-cutover-rollback-mutants.sh.
# Sourced by scripts/test-collector-cutover-rollback.sh after the harness.

# The harness defines these variables and the entry script sources this file.
# shellcheck disable=SC2154,SC2034

# --- step 1 reads the registry, not the caller's list (F1) ---------------------------

# The instance named by the operator is wrong while the real one stays enabled and
# a resume starts at step 2: the gate itself must refuse.
new_state typo-resume
printf '0\n' >"${FAKE_STATE}/worker_pods"
printf '0\n' >"${FAKE_STATE}/worker_replicas"
expect_refused "step 2 refuses while the component still has an enabled activation" 4 2 -- "psql drain"
expect_out "the gate names the registry" "enabled activation"

new_state typo-list-fails
touch "${FAKE_STATE}/fail_list"
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}" --external-instance ext-TYPO
expect_rc "an unreadable registry fails step 1 and is not read as clear" 3
expect_no_call "no worker scale when the registry cannot be read" "scale"

# The registry lists one row per installed version. Activations are summed over
# all of them, so a later zero row cannot hide an earlier enabled one, and the
# reverse order cannot hide it either (the last row must not win).
for order in zero-then-one one-then-zero; do
	new_state "registry-two-versions-${order}"
	if [[ "${order}" == "zero-then-one" ]]; then
		printf '1.2.2|activations=0\n1.2.3|activations=1\n' >"${FAKE_STATE}/list_rows"
	else
		printf '1.2.2|activations=1\n1.2.3|activations=0\n' >"${FAKE_STATE}/list_rows"
	fi
	run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
	expect_rc "an enabled activation on any installed version refuses step 1 (${order})" 4
	expect_no_call "no worker scale while a version still has an activation (${order})" "scale"
done

new_state registry-two-versions-clear
printf '1.2.2|activations=0\n1.2.3|activations=0\n' >"${FAKE_STATE}/list_rows"
run_script run --yes --through-step 1 --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "two installed versions with no activation are clear" 0

# A malformed activations column is unknown, never clear: step 1 fails (3), it does
# not refuse (4), and nothing is scaled.
for column in 'activations=x' 'activations=' 'enabled=1'; do
	new_state "registry-garbled-${column//[^a-z]/}"
	printf '1.2.3|%s\n' "${column}" >"${FAKE_STATE}/list_rows"
	run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
	expect_rc "a malformed activations column (${column}) fails step 1 as unknown" 3
	expect_out "the failure says the listing could not be parsed (${column})" "could not be parsed"
	expect_no_call "no worker scale on a malformed registry row (${column})" "scale"
done

# One good row plus one garbled row is still unknown: the good row must not vouch.
new_state registry-good-and-garbled
printf '1.2.2|activations=0\n1.2.3|activations=zz\n' >"${FAKE_STATE}/list_rows"
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "a garbled row beside a clear row is unknown, not clear" 3

# --- the worker must be provably stopped (F3, F4) -------------------------------------

# The selector matches no pod (custom labels), yet the worker Deployment still
# wants replicas: an empty pod list must not pass.
new_clear_state selector-misses
printf '1\n' >"${FAKE_STATE}/worker_replicas"
expect_refused "a selector that matches no pod does not prove the worker stopped" 4 2 -- "psql drain"
expect_out "the refusal says the Deployment still wants replicas" "still wants '1' replica"

# Only kubectl's NotFound status means the Deployment is gone: a read that fails
# with other text containing "not found" (a missing binary), or answers with
# nothing, never proves the worker stopped.
new_clear_state replicas-cmd-missing
touch "${FAKE_STATE}/kubectl_cmd_missing"
expect_refused "a failing replicas read with 'command not found' is not an absent worker" 4 2 -- "psql drain"
new_clear_state replicas-empty
: >"${FAKE_STATE}/worker_replicas"
expect_refused "an empty replicas answer does not prove the worker stopped" 4 2 -- "psql drain"

# The restore overlay turned the extension collector off, so the chart deleted the
# worker Deployment: NotFound is stopped, and a full rerun still completes.
new_clear_state worker-absent
touch "${FAKE_STATE}/worker_absent"
run_script run --yes --through-step 3 --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "a rerun from step 1 with the worker Deployment deleted succeeds" 0
expect_out "the missing worker Deployment is reported" "does not exist; treating the worker as already removed"
expect_out "the evidence records the absent worker" "worker=absent"
expect_call "step 1 still restarts the coordinator when the worker is absent" "rollout restart deployment/eshu-coordinator"
expect_call "step 1 still confirms the coordinator rollout when the worker is absent" "rollout status deployment/eshu-coordinator"

new_state worker-absent-rerun
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "the first run succeeds" 0
touch "${FAKE_STATE}/worker_absent"
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "a full rerun after the chart deleted the worker succeeds" 0

new_state worker-scale-forbidden
touch "${FAKE_STATE}/scale_fail_worker"
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "a scale error other than NotFound still fails step 1" 3
expect_no_call "no coordinator restart after a failed worker scale" "rollout restart"

# kubectl cannot be read: that is never read as "no pods".
new_clear_state get-pods-fails
touch "${FAKE_STATE}/kubectl_fail_get"
expect_refused "a failing pod listing does not pass the step 1 gate" 4 2 -- "psql drain"
new_clear_state get-deploy-fails
touch "${FAKE_STATE}/kubectl_fail_get_deploy"
expect_refused "a failing Deployment read does not pass the step 1 gate" 4 2 -- "psql drain"
new_clear_state worker-gone
touch "${FAKE_STATE}/worker_absent"
run_script run --yes --from-step 2 --through-step 2 --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "an absent worker Deployment passes the step 1 gate" 0

# --- a worker pod that is still alive blocks every later step ------------------------

new_clear_state worker-alive-3
printf '1\n' >"${FAKE_STATE}/worker_pods"
printf '1\n' >"${FAKE_STATE}/worker_replicas"
expect_refused "revoke refuses while a worker pod is alive" 4 3 -- "revoke-grant"
new_clear_state worker-alive-4
printf '1\n' >"${FAKE_STATE}/worker_pods"
printf '1\n' >"${FAKE_STATE}/worker_replicas"
expect_refused "restore refuses while a worker pod is alive" 4 4 -- "helm" "scale deployment/jira-collector"
new_clear_state worker-alive-5
printf '1\n' >"${FAKE_STATE}/worker_pods"
printf '1\n' >"${FAKE_STATE}/worker_replicas"
expect_refused "replay refuses while a worker pod is alive" 4 5 --since "2026-10-23 12:00:00+00" -- "psql replay"
new_clear_state worker-alive-6
printf '1\n' >"${FAKE_STATE}/worker_pods"
printf '1\n' >"${FAKE_STATE}/worker_replicas"
expect_refused "verification refuses while a worker pod is alive" 4 6 -- "psql truth"

# --- the drain counts claimed items and active claims independently --------------------

new_clear_state claimed-only
printf 'jira|ext-1|claimed|\njira|tree-1|pending|\n' >"${FAKE_STATE}/claims"
expect_refused "revoke refuses with a claimed item but no active claim row" 4 3 -- "revoke-grant"
new_clear_state active-only
printf 'jira|ext-1|pending|active\njira|tree-1|pending|\n' >"${FAKE_STATE}/claims"
expect_refused "revoke refuses with an active claim but no claimed item" 4 3 -- "revoke-grant"
new_clear_state in-tree-claims-ok
printf 'jira|tree-1|claimed|active\n' >"${FAKE_STATE}/claims"
run_script run --yes --from-step 3 --through-step 3 --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "a claim held by the in-tree instance does not block the drain" 0
new_clear_state drain-fails
touch "${FAKE_STATE}/psql_fail_drain"
expect_refused "an unreadable claim table fails the drain gate and is not read as drained" 3 3 -- "revoke-grant"
new_clear_state drain-fails-wait
touch "${FAKE_STATE}/psql_fail_drain"
expect_refused "an unreadable claim table fails the drain wait" 3 2 -- "revoke-grant"
new_clear_state drain-garbage
touch "${FAKE_STATE}/psql_garbage_drain"
expect_refused "a malformed claim table answer fails the drain gate" 3 3 -- "revoke-grant"

# --- grants: a missing or unreadable listing is not a revocation ------------------------

new_clear_state grants-missing
: >"${FAKE_STATE}/grants"
expect_refused "restore refuses when no grant row exists" 4 4 -- "helm" "scale deployment/jira-collector"
new_clear_state grants-live
printf 'dev.example.jira|1.2.3|work_item.issue|jira|true\ndev.example.jira|1.2.3|work_item.comment|jira|false\n' >"${FAKE_STATE}/grants"
expect_refused "restore refuses while one grant is still live" 4 4 -- "helm"
new_clear_state grants-list-fails
touch "${FAKE_STATE}/fail_grants"
expect_refused "restore refuses when the grant listing fails" 4 4 -- "helm"

# --- step 4: the dual-authority check runs before the restore (F11) ---------------------

new_clear_state early-restore
printf '2\n' >"${FAKE_STATE}/overlaps"
expect_refused "an overlap found before the restore stops the rollback" 7 4 -- "helm" "scale deployment/jira-collector"
expect_out "the overlap is reported at the step 3 gate" "DUAL AUTHORITY[step 4]"
new_clear_state dual-query-fails
touch "${FAKE_STATE}/psql_fail_dual"
expect_refused "an unreadable authority check stops the restore" 3 4 -- "helm"

# --- step 5 and 6 gates ---------------------------------------------------------------------

since_args=(--since "2026-10-23 12:00:00+00")
new_clear_state claims-held-5
printf 'jira|ext-1|claimed|active\n' >"${FAKE_STATE}/claims"
expect_refused "replay refuses while the external instance holds a claim" 4 5 "${since_args[@]}" -- "psql replay"
new_clear_state active-claim-5
printf 'jira|ext-1|pending|active\n' >"${FAKE_STATE}/claims"
expect_refused "replay refuses while the external instance holds an active claim row" 4 5 "${since_args[@]}" -- "psql replay"
new_clear_state grants-live-5
printf 'dev.example.jira|1.2.3|work_item.issue|jira|false\ndev.example.jira|1.2.3|work_item.comment|jira|true\n' >"${FAKE_STATE}/grants"
expect_refused "replay refuses while a grant is live" 4 5 "${since_args[@]}" -- "psql replay"
new_clear_state in-tree-not-rolled-5
touch "${FAKE_STATE}/intree_rollout_fail"
expect_refused "replay refuses when the in-tree Deployment is not rolled out" 4 5 "${since_args[@]}" -- "psql replay"
expect_out "the refusal names the in-tree Deployment" "deployment/jira-collector is not rolled out"
new_clear_state in-tree-not-rolled-6
touch "${FAKE_STATE}/intree_rollout_fail"
expect_refused "verification refuses when the in-tree Deployment is not rolled out" 4 6 -- "psql truth"
new_clear_state claims-held-6
printf 'jira|ext-1|claimed|active\n' >"${FAKE_STATE}/claims"
expect_refused "verification refuses while the external instance holds a claim" 4 6 -- "psql truth"
new_clear_state active-claim-6
printf 'jira|ext-1|pending|active\n' >"${FAKE_STATE}/claims"
expect_refused "verification refuses while the external instance holds an active claim row" 4 6 -- "psql truth"
new_clear_state grants-live-6
printf 'dev.example.jira|1.2.3|work_item.issue|jira|false\ndev.example.jira|1.2.3|work_item.comment|jira|false\n' >"${FAKE_STATE}/grants"
expect_refused "verification refuses while grants are live" 4 6 -- "psql truth"
new_clear_state step-6-clear
run_script run --yes --from-step 6 --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "step 6 alone runs when every earlier gate holds and needs no --since" 0

# --- the replay gate needs real progress, not just a quiet queue ------------------------

for replay_case in "0|0|0|0|0|nothing completed" "0|1|2|0|0|an item still claimed" "0|0|2|1|0|a retryable item remains" "3|0|2|0|0|an item still pending"; do
	counts="${replay_case%|*}"
	reason="${replay_case##*|}"
	new_clear_state "replay-${reason// /-}"
	printf '%s\n' "${counts}" >"${FAKE_STATE}/replay"
	expect_refused "replay does not finish while ${reason}" 5 5 "${since_args[@]}" --replay-timeout 0 -- "psql truth"
done

# --- step 6 compares truth, and a difference stops the run (exit 6) ----------------------

new_state run-truth-mismatch
printf 'work_item.comment|10|keysA|payloadA\nwork_item.issue|4|keysB|payloadCHANGED\n' >"${FAKE_STATE}/truth"
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "a run whose replayed truth differs from the baseline exits 6" 6
expect_no_out "a mismatched run never reports completion" "ROLLBACK COMPLETE"
