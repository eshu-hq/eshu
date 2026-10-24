#!/usr/bin/env bash
# Direct in-tree producer cases for scripts/collector-cutover-rollback.sh (#9027).
# A direct collector (the documentation kind) runs as its own Deployment with no
# collector instance and no workflow_work_items rows, so --in-tree-direct replaces
# --in-tree-instance, every claim of the kind is external, and step 5 waits for a
# generation observed and activated after the window instead of replayed work items.
# Sourced by scripts/test-collector-cutover-rollback.sh after the harness.

# The harness defines these variables and the entry script sources this file.
# shellcheck disable=SC2154,SC2034

# direct_args builds the argument list of a direct run: base_args with the
# in-tree instance swapped for the explicit direct mode.
direct_args() {
	base_args
	local -a swapped=()
	local skip=0 word
	for word in "${ARGS[@]}"; do
		if [[ "${skip}" == 1 ]]; then
			skip=0
			continue
		fi
		if [[ "${word}" == "--in-tree-instance" ]]; then
			skip=1
			swapped+=(--in-tree-direct)
			continue
		fi
		swapped+=("${word}")
	done
	ARGS=("${swapped[@]}")
}

# --- option rules ----------------------------------------------------------------

new_state direct-usage
base_args
no_instance=()
skip=0
for word in "${ARGS[@]}"; do
	if [[ "${skip}" == 1 ]]; then
		skip=0
		continue
	fi
	if [[ "${word}" == "--in-tree-instance" ]]; then
		skip=1
		continue
	fi
	no_instance+=("${word}")
done
run_script plan "${no_instance[@]}"
expect_rc "the default mode still refuses an empty in-tree list" 2
expect_out "the refusal says the list is required" "--in-tree-instance is required"

direct_args
run_script plan "${ARGS[@]}"
expect_rc "--in-tree-direct plans without --in-tree-instance" 0
run_script plan "${ARGS[@]}" --in-tree-instance tree-1
expect_rc "--in-tree-direct and --in-tree-instance are mutually exclusive" 2
expect_out "the exclusion names both options" "--in-tree-direct excludes --in-tree-instance"

# --- plan -------------------------------------------------------------------------

new_state direct-plan
direct_args
run_script plan "${ARGS[@]}"
expect_rc "the direct plan exits 0" 0
expect_out "the plan names the direct producer" "direct collector"
expect_out "the plan names the Deployment scale-up as the restore" "scale deployment/jira-collector --replicas=1"
expect_out "the plan reads the observed-generation signal" "collector-cutover-rollback-direct-commit.sql"
expect_no_out "the plan has no work-item replay query" "collector-cutover-rollback-replay.sql"
restarts="$(printf '%s\n' "${out}" | awk '/rollout restart deployment\/eshu-coordinator/ { n++ } END { print n + 0 }')"
if [[ "${restarts}" == 1 ]]; then pass "the direct plan restarts the coordinator once, in step 1"; else
	fail "the direct plan restarts the coordinator ${restarts} time(s), want 1"
fi
if [[ ! -e "${FAKE_STATE}/calls.log" ]]; then pass "the direct plan invokes no tool"; else fail "the direct plan invoked a tool"; fi

new_state default-plan-keeps-replay
base_args
run_script plan "${ARGS[@]}"
expect_out "the default plan still replays work items" "collector-cutover-rollback-replay.sql"
expect_no_out "the default plan does not read the direct signal" "collector-cutover-rollback-direct-commit.sql"
restarts="$(printf '%s\n' "${out}" | awk '/rollout restart deployment\/eshu-coordinator/ { n++ } END { print n + 0 }')"
if [[ "${restarts}" == 2 ]]; then pass "the default plan still restarts the coordinator twice"; else
	fail "the default plan restarts the coordinator ${restarts} time(s), want 2"
fi

# --- full run -----------------------------------------------------------------------

new_state direct-run
direct_args
printf '1|2026-10-23T12:05:00.000000Z\n' >"${FAKE_STATE}/direct_commits"
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "a direct run completes" 0
expect_order "a direct run restores by scaling, then reads the generation signal" \
	"eshu component revoke-grant" \
	"kubectl -n eso scale deployment/jira-collector --replicas=1" \
	"psql direct-commit" \
	"psql truth"
expect_no_call "a direct run never reads work-item replay" "psql replay"
expect_out "a direct run records the signal as evidence" "direct_observed_generations=1"
expect_out "a direct run names the direct collector Deployment in its header" "-> in-tree direct collector (deployment/jira-collector)"
expect_out "a direct run reports step 5 as ok" "RESULT[step 5]: ok"
restarts="$(calls | awk '/rollout restart deployment\/eshu-coordinator/ { n++ } END { print n + 0 }')"
if [[ "${restarts}" == 1 ]]; then pass "a direct run restarts the coordinator once"; else
	fail "a direct run restarted the coordinator ${restarts} time(s), want 1"
fi
expect_call "the signal is read from the replay window start" "psql direct-commit kind=jira since=2026-10-23 12:00:00+00"
expect_call "the drain query gets an empty in-tree list" "psql drain kind=jira in_tree= "
expect_call "the replay window has no clock-skew margin in direct mode" "psql now kind= skew=0"

# GitOps mode keeps working: the restore values were applied elsewhere and the
# script still scales the Deployment back and reads the signal.
new_state direct-gitops
direct_args
printf '1|2026-10-23T12:05:00.000000Z\n' >"${FAKE_STATE}/direct_commits"
gitops_args=()
skip=0
for word in "${ARGS[@]}"; do
	if [[ "${skip}" -gt 0 ]]; then
		skip=$((skip - 1))
		continue
	fi
	case "${word}" in
	--helm-release | --helm-chart | --restore-values) skip=1 ;;
	*) gitops_args+=("${word}") ;;
	esac
done
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${gitops_args[@]}" --restore-applied-by-gitops
expect_rc "a direct run with GitOps restore completes" 0
expect_no_call "GitOps restore runs no helm" "helm upgrade"
expect_call "GitOps restore still scales the Deployment" "kubectl -n eso scale deployment/jira-collector --replicas=1"

# --- drain and authority: every claim of the kind is external ----------------------------

new_clear_state direct-drain
direct_args
printf 'jira|ext-9|claimed|active\n' >"${FAKE_STATE}/claims"
expect_refused "a direct run refuses to revoke while any claim of the kind is held" 4 3 -- "revoke-grant"
expect_out "the refusal reports the held claim" "still hold 1 claimed work item(s)"

new_clear_state direct-drain-tree-named
direct_args
printf 'jira|tree-1|claimed|active\n' >"${FAKE_STATE}/claims"
expect_refused "an instance named like the default in-tree one is still external in direct mode" 4 3 -- "revoke-grant"

# The restored direct collector writes no claim of its own, so a claim that exists
# after the restore belongs to the external producer running beside it: dual
# authority. Before the restore the same claim is only a refusal.
new_clear_state direct-dual-verify
direct_args
verify_direct=(--collector-kind jira --external-instance ext-1 --in-tree-direct)
printf 'jira|ext-1|claimed|active\n' >"${FAKE_STATE}/claims"
run_script verify "${verify_direct[@]}" --baseline "${FAKE_STATE}/baseline"
expect_rc "verify in direct mode fails with exit 7 when an external claim exists beside the restored producer" 7
expect_out "the failure says dual authority" "DUAL AUTHORITY"
: >"${FAKE_STATE}/claims"
run_script verify "${verify_direct[@]}" --baseline "${FAKE_STATE}/baseline"
expect_rc "verify in direct mode passes with no claim of the kind" 0

new_clear_state direct-dual-resume
direct_args
printf 'jira|ext-1|claimed|\n' >"${FAKE_STATE}/claims"
printf '0\n' >"${FAKE_STATE}/intree_replicas"
expect_refused "step 4 in direct mode refuses (not dual authority) while an external claim is held" 4 4 -- "scale deployment/jira-collector"

new_clear_state direct-dual-step5
direct_args
printf '1|2026-10-23T12:05:00.000000Z\n' >"${FAKE_STATE}/direct_commits"
: >"${FAKE_STATE}/claims"
printf '3\n' >"${FAKE_STATE}/overlaps"
run_script run --yes --from-step 5 --since '2026-10-23 12:00:00+00' --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "an overlap count still fails a direct run with exit 7" 7
rm -f "${FAKE_STATE}/overlaps"

# --- step 5: the observed-generation signal -----------------------------------------------

new_clear_state direct-step5-none
direct_args
printf '0|\n' >"${FAKE_STATE}/direct_commits"
run_script run --yes --from-step 5 --through-step 5 --since '2026-10-23 12:00:00+00' --replay-timeout 0 "${ARGS[@]}"
expect_rc "step 5 times out with the hard-stop code when no generation was observed and activated after --since" 5
expect_out "the timeout names the missing generation" "no generation was observed and activated"

new_clear_state direct-step5-advances
direct_args
printf '0|\n' >"${FAKE_STATE}/direct_commits"
(sleep 0.3 && printf '2|2026-10-23T12:07:00.000000Z\n' >"${FAKE_STATE}/direct_commits") &
bg_pid=$!
run_script run --yes --from-step 5 --through-step 5 --since '2026-10-23 12:00:00+00' --replay-timeout 20 "${ARGS[@]}"
wait "${bg_pid}"
expect_rc "step 5 passes once the signal advances after --since" 0
expect_out "step 5 records the latest commit time" "latest_activated_at=2026-10-23T12:07:00.000000Z"

new_clear_state direct-step5-garbage
direct_args
touch "${FAKE_STATE}/psql_garbage_direct-commit"
run_script run --yes --from-step 5 --through-step 5 --since '2026-10-23 12:00:00+00' --replay-timeout 0 "${ARGS[@]}"
expect_rc "an unreadable signal fails step 5 and never reads as progress" 3

new_clear_state direct-step5-gate
direct_args
printf '1|2026-10-23T12:05:00.000000Z\n' >"${FAKE_STATE}/direct_commits"
touch "${FAKE_STATE}/intree_rollout_fail"
expect_refused "step 5 in direct mode still refuses before the Deployment is rolled out" 4 5 \
	--since '2026-10-23 12:00:00+00' -- "psql direct-commit"

# --- input rules ----------------------------------------------------------------------------

new_state direct-deployment-required
direct_args
without_deployment=()
skip=0
for word in "${ARGS[@]}"; do
	if [[ "${skip}" == 1 ]]; then
		skip=0
		continue
	fi
	if [[ "${word}" == "--in-tree-deployment" ]]; then
		skip=1
		continue
	fi
	without_deployment+=("${word}")
done
run_script plan "${without_deployment[@]}"
expect_rc "direct mode requires the in-tree Deployment name" 2
run_script plan "${ARGS[@]}" --in-tree-deployment 'jira;rm'
expect_rc "an unsafe Deployment name is rejected in direct mode" 2

# --- help text ---------------------------------------------------------------------------------

run_script --help
expect_out "help documents --in-tree-direct" "--in-tree-direct"
