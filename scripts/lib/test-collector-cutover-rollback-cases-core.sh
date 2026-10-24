#!/usr/bin/env bash
# Core cases for scripts/collector-cutover-rollback.sh (#8895): plan, step order,
# run guards, the first refusal cases, timeouts, idempotence, and failing steps.
# Sourced by scripts/test-collector-cutover-rollback.sh after the harness.

# The harness defines these variables and the entry script sources this file.
# shellcheck disable=SC2154,SC2034

# --- plan -------------------------------------------------------------------

new_state plan
base_args
run_script plan "${ARGS[@]}"
expect_rc "plan exits 0" 0
for n in 1 2 3 4 5 6; do expect_out "plan prints STEP ${n}" "STEP ${n} "; done
expect_out "plan prints the disable command" "eshu component disable dev.example.jira --instance ext-1 --component-home /data/components"
expect_out "plan prints the revoke command" "eshu component revoke-grant dev.example.jira --version 1.2.3 --kind work_item.issue --scope jira --component-home /data/components"
expect_out "plan prints the helm restore" "helm upgrade eshu ./chart --namespace eso --reuse-values -f /tmp/rollback-values.yaml"
if [[ ! -e "${FAKE_STATE}/calls.log" ]]; then pass "plan invokes no tool"; else fail "plan invoked a tool: $(calls)"; fi
plan_out="${out}"

# --- run: order, evidence, and plan/run agreement -----------------------------

new_state order
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "run succeeds" 0
expect_order "run executes the six steps in order" \
	"eshu component disable" \
	"kubectl -n eso scale deployment/eshu-worker --replicas=0" \
	"kubectl -n eso rollout restart deployment/eshu-coordinator" \
	"psql drain" \
	"eshu component revoke-grant" \
	"helm upgrade" \
	"kubectl -n eso scale deployment/jira-collector --replicas=1" \
	"psql replay" \
	"psql truth"
revoke_line="$(first_line 'eshu component revoke-grant')"
helm_line="$(first_line 'helm upgrade')"
drain_between="$(calls | awk -v a="${revoke_line}" -v b="${helm_line}" 'NR > a && NR < b && index($0, "psql drain") { n++ } END { print n + 0 }')"
if [[ "${drain_between}" -ge 1 ]]; then pass "drain is re-checked between revoke and restore"; else fail "no drain re-check before restore"; fi
for n in 1 2 3 4 5 6; do expect_out "run reports STEP ${n}" "STEP ${n}/6 "; done
expect_out "run prints evidence lines" "EVIDENCE[step 2]"
expect_out "run reports the final verdict" "ROLLBACK COMPLETE"
missing=0
while IFS= read -r line; do
	cmd="${line#  \$ }"
	[[ "${cmd}" == *psql* ]] && continue
	# The plan prints shell-quoted words. Newer bash versions (the CI runner's 5.2) escape
	# braces as \{ where older ones do not, while the fake logs the plain arguments.
	cmd="${cmd//\\/}"
	if [[ -z "$(first_line "${cmd}")" ]]; then
		missing=1
		printf '    planned but never run: %s\n' "${cmd}"
	fi
done < <(printf '%s\n' "${plan_out}" | awk '/^  \$ /')
if [[ "${missing}" == 0 ]]; then pass "every planned command is executed"; else fail "plan and run disagree"; fi

# --- run guards ---------------------------------------------------------------

new_state noyes
run_script run --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "run without --yes is a usage error" 2
expect_no_call "run without --yes calls nothing" "eshu"

new_state nobaseline
run_script run --yes "${ARGS[@]}"
expect_rc "run without a baseline is refused before any change" 2
if [[ ! -e "${FAKE_STATE}/calls.log" ]]; then pass "missing baseline calls nothing"; else fail "missing baseline still called a tool"; fi

new_state unsafe
run_script plan "${ARGS[@]}" --collector-kind 'jira;drop'
expect_rc "unsafe identifier is rejected" 2

# --- refusals -------------------------------------------------------------------

# Claims still held: step 4 must refuse to restore the in-tree producer.
new_state refuse4
printf '0\n' >"${FAKE_STATE}/worker_pods"
printf '0\n' >"${FAKE_STATE}/worker_replicas"
: >"${FAKE_STATE}/activations"
printf 'dev.example.jira|1.2.3|work_item.issue|jira|true\ndev.example.jira|1.2.3|work_item.comment|jira|true\n' >"${FAKE_STATE}/grants"
rm -f "${FAKE_STATE}/drain_polls_left"
run_script run --yes --from-step 4 --restore-applied-by-gitops --baseline "${FAKE_STATE}/baseline" \
	"${ARGS[@]:0:${#ARGS[@]}-6}"
expect_rc "restore refuses while claims are held" 4
expect_out "refusal names the held claims" "REFUSED"
expect_no_call "no in-tree scale after refusal" "scale deployment/jira-collector"
expect_no_call "no helm after refusal" "helm"

# Step 1 unconfirmed (worker pod survives): step 2 must not start waiting.
new_state refuse2
touch "${FAKE_STATE}/worker_stuck"
run_script run --yes --from-step 2 --step-timeout 1 --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "drain wait refuses before step 1 is confirmed" 4
expect_no_call "no drain query before step 1 is confirmed" "psql drain"

# Step 2 not drained: step 3 must not revoke.
new_state refuse3
printf '0\n' >"${FAKE_STATE}/worker_pods"
printf '0\n' >"${FAKE_STATE}/worker_replicas"
rm -f "${FAKE_STATE}/drain_polls_left"
run_script run --yes --from-step 3 --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "revoke refuses while claims are held" 4
expect_no_call "no revoke before drain" "revoke-grant"

# Step 3 not done: step 4 must not restore.
new_state refuse4b
printf '0\n' >"${FAKE_STATE}/worker_pods"
printf '0\n' >"${FAKE_STATE}/worker_replicas"
: >"${FAKE_STATE}/claims"
run_script run --yes --from-step 4 --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "restore refuses while a grant is still live" 4
expect_no_call "no helm while a grant is live" "helm"

# --- a mistyped --external-instance must not pass any gate (F1) ----------------------

# The registry still holds the real instance (ext-1): disabling the typo reports
# "not enabled", which must not be read as "already disabled".
new_state typo-enabled
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}" --external-instance ext-TYPO
expect_rc "a mistyped external instance is refused while the real one is enabled" 4
expect_out "the refusal names the still-enabled activation" "still has"
expect_no_call "no revoke after a mistyped instance" "revoke-grant"
expect_no_call "no helm after a mistyped instance" "helm"
expect_no_call "no worker scale after a mistyped instance" "scale"

# The real instance is already disabled but still holds a claimed item: the drain
# gate counts the collector kind, not only the instance IDs the caller named.
new_state typo-claims
: >"${FAKE_STATE}/activations"
rm -f "${FAKE_STATE}/drain_polls_left"
run_script run --yes --drain-timeout 1 --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}" --external-instance ext-TYPO
expect_rc "a mistyped instance cannot hide a claim held by another instance of the kind" 5
expect_no_call "no revoke while another instance holds a claim" "revoke-grant"
expect_no_call "no helm while another instance holds a claim" "helm"

# --- timeouts ---------------------------------------------------------------------

new_state drain-timeout
rm -f "${FAKE_STATE}/drain_polls_left"
run_script run --yes --drain-timeout 1 --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "a drain that never finishes times out" 5
expect_no_call "timeout stops before revoke" "revoke-grant"
expect_no_call "timeout stops before restore" "helm"

new_state replay-timeout
printf '1|0|1|0|0\n' >"${FAKE_STATE}/replay"
run_script run --yes --replay-timeout 1 --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "a replay that never finishes times out" 5
expect_no_call "replay timeout stops before truth capture" "psql truth"

new_state replay-failed
printf '0|0|1|0|3\n' >"${FAKE_STATE}/replay"
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "terminal replay failures abort" 3
expect_no_call "failed replay stops before truth capture" "psql truth"

# --- idempotent re-run ------------------------------------------------------------

new_state idem
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "first run succeeds" 0
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "second run is idempotent" 0
expect_out "second run reports the instance as not enabled" "is not enabled"

# --- a failing step aborts the run ---------------------------------------------------

new_state fail-disable
touch "${FAKE_STATE}/fail_disable"
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "failed disable aborts" 3
expect_no_call "no scale after a failed disable" "scale"
expect_no_call "no revoke after a failed disable" "revoke-grant"

new_state fail-revoke
: >"${FAKE_STATE}/grants"
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "failed revoke aborts" 3
expect_no_call "no restore after a failed revoke" "helm"
expect_no_call "no in-tree scale after a failed revoke" "scale deployment/jira-collector"

new_state fail-helm
touch "${FAKE_STATE}/fail_helm"
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "failed helm restore aborts" 3
expect_no_call "no in-tree scale after a failed helm restore" "scale deployment/jira-collector"
expect_no_call "no replay after a failed helm restore" "psql replay"

new_state fail-rollout
touch "${FAKE_STATE}/rollout_fail"
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "failed coordinator rollout aborts step 1" 3
expect_no_call "no drain after a failed rollout" "psql drain"

# --- secrets never reach argv or output ------------------------------------------------

new_state secrets
rc=0
out="$(PATH="${bin_dir}:${PATH}" PGPASSWORD='s3cr3t-token-value' ROLLBACK_POLL_SECONDS=0.05 \
	"${script}" run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}" 2>&1)" || rc=$?
expect_rc "run with a PGPASSWORD set succeeds" 0
if [[ "${out}" != *s3cr3t-token-value* && "$(calls)" != *s3cr3t-token-value* ]]; then
	pass "the database password is never printed or passed on a command line"
else
	fail "the database password leaked"
fi

# --- injectable binaries -----------------------------------------------------------------

new_state inject
cp "${bin_dir}/eshu" "${tmp_root}/my-eshu"
rc=0
out="$(PATH="${bin_dir}:${PATH}" ROLLBACK_ESHU_BIN="${tmp_root}/my-eshu" "${script}" plan "${ARGS[@]}" 2>&1)" || rc=$?
expect_rc "plan honors ROLLBACK_ESHU_BIN" 0
expect_out "plan prints the injected binary" "${tmp_root}/my-eshu component disable"
