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

# in_tree_read_pattern appears in the logged kubectl call that reads the direct
# Deployment replica counts and in no other call, whatever Deployment name it
# carries. A default-mode run that logs it read the Deployment.
in_tree_read_pattern='jsonpath={.spec.replicas}{"|"}{.status.replicas}'
# in_tree_read_call is the whole logged read: the namespace first, the request
# timeout last.
in_tree_read_call() { # request-timeout
	printf 'kubectl -n eso get deployment/jira-collector -o %s --request-timeout=%s' "${in_tree_read_pattern}" "$1"
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
# Steps 4, 5, and 6 run the authority check, so the plan lists its Deployment read in each.
plan_step_reads() { # <step number> <plan text> -> count of Deployment reads under that STEP block
	printf '%s\n' "$2" | awk -v step="$1" '
		/^STEP [0-9]/ { cur = $2 }
		cur == step && index($0, "kubectl -n eso get deployment/jira-collector -o jsonpath=") { n++ }
		END { print n + 0 }'
}
for step in 4 5 6; do
	plan_reads="$(plan_step_reads "${step}" "${out}")"
	if [[ "${plan_reads}" == 1 ]]; then pass "a direct plan lists the Deployment read once under step ${step}"; else
		fail "a direct plan lists the Deployment read ${plan_reads} time(s) under step ${step}, want 1"
	fi
done
for step in 1 2 3; do
	plan_reads="$(plan_step_reads "${step}" "${out}")"
	if [[ "${plan_reads}" == 0 ]]; then pass "a direct plan lists no Deployment read under step ${step}"; else
		fail "a direct plan lists the Deployment read ${plan_reads} time(s) under step ${step}, want 0"
	fi
done
if [[ "${plan_out}" != *"get deployment/jira-collector"* ]]; then pass "the default plan lists no read of the in-tree Deployment"; else
	fail "the default plan lists a read of the in-tree Deployment"
fi
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

# The restored direct collector writes no claim of its own, so the Deployment's
# replica counts stand in for it. A claim beside a running Deployment is dual
# authority; the same claim with the Deployment at zero is the external producer
# alone, which is only a refusal. verify reads the Deployment, so it needs its name.
new_clear_state direct-dual-verify
direct_args
verify_direct=(--collector-kind jira --external-instance ext-1 --in-tree-direct
	--namespace eso --in-tree-deployment jira-collector)
printf 'jira|ext-1|claimed|active\n' >"${FAKE_STATE}/claims"
run_script verify "${verify_direct[@]}" --baseline "${FAKE_STATE}/baseline"
expect_rc "verify in direct mode fails with exit 7 when an external claim exists beside the running producer" 7
expect_out "the failure says dual authority" "DUAL AUTHORITY"
expect_out "the failure names both live producers" "external claims=1; direct Deployment running=yes"
expect_call "verify reads the Deployment replica counts in the namespace with the request timeout" "$(in_tree_read_call 30s)"
: >"${FAKE_STATE}/claims"
run_script verify "${verify_direct[@]}" --baseline "${FAKE_STATE}/baseline"
expect_rc "verify in direct mode passes with no claim of the kind" 0

# The reported bug: before the restore the Deployment is at zero and only the
# external producer exists, so a claim is a refusal, never dual authority.
new_clear_state direct-verify-prerestore
printf '0\n' >"${FAKE_STATE}/intree_replicas"
printf 'jira|ext-1|claimed|active\n' >"${FAKE_STATE}/claims"
run_script verify "${verify_direct[@]}" --baseline "${FAKE_STATE}/baseline"
expect_rc "verify before the restore refuses an external claim and is not dual authority" 4
expect_no_out "the pre-restore claim is not labelled dual authority" "DUAL AUTHORITY"
expect_out "the pre-restore read reports the Deployment as not running" "in_tree_direct_running=no"
expect_out "the refusal names the external claim" "an external instance still holds 1 claimed work item(s)"
: >"${FAKE_STATE}/claims"
run_script verify "${verify_direct[@]}" --baseline "${FAKE_STATE}/baseline"
expect_rc "verify before the restore with no claim passes the authority check" 0
expect_out "the pre-restore pass reports the Deployment as not running" "in_tree_direct_running=no"

# A pod that is still terminating after a scale to zero can still produce.
new_clear_state direct-verify-terminating
printf '0\n' >"${FAKE_STATE}/intree_replicas"
printf 'jira|ext-1|claimed|active\n' >"${FAKE_STATE}/claims"
printf '1\n' >"${FAKE_STATE}/intree_status_pods"
run_script verify "${verify_direct[@]}" --baseline "${FAKE_STATE}/baseline"
expect_rc "a Deployment at zero desired replicas with a live pod still counts as running" 7

# A Deployment just scaled up has desired replicas before it has any pod.
new_clear_state direct-verify-scaling-up
printf '1\n' >"${FAKE_STATE}/intree_replicas"
printf '0\n' >"${FAKE_STATE}/intree_status_pods"
printf 'jira|ext-1|claimed|active\n' >"${FAKE_STATE}/claims"
run_script verify "${verify_direct[@]}" --baseline "${FAKE_STATE}/baseline"
expect_rc "a Deployment with desired replicas and no pod yet counts as running" 7

# An unreadable rollout fails closed: it counts as running and says so.
for unreadable in kubectl_fail_get_deploy intree_get_missing intree_get_garbage kubectl_cmd_missing; do
	new_clear_state "direct-verify-${unreadable}"
	printf '0\n' >"${FAKE_STATE}/intree_replicas"
	touch "${FAKE_STATE}/${unreadable}"
	printf 'jira|ext-1|claimed|active\n' >"${FAKE_STATE}/claims"
	run_script verify "${verify_direct[@]}" --baseline "${FAKE_STATE}/baseline"
	expect_rc "an unreadable Deployment (${unreadable}) beside an external claim fails closed with exit 7" 7
	expect_out "the failure says the read failed (${unreadable})" "could not be read"
	expect_out "the failure says the Deployment is treated as running (${unreadable})" "it is treated as running"
	: >"${FAKE_STATE}/claims"
	run_script verify "${verify_direct[@]}" --baseline "${FAKE_STATE}/baseline"
	expect_out "an unreadable Deployment with no claim still says so (${unreadable})" "in_tree_direct_running=unknown"
	expect_out "the passing evidence line names the read error and its exit code (${unreadable})" \
		"overlapping_claim_pairs=0 The direct Deployment deployment/jira-collector could not be read (exit "
done

# The read carries the request timeout of the environment, not a fixed one.
new_clear_state direct-verify-timeout
: >"${FAKE_STATE}/claims"
ROLLBACK_KUBECTL_REQUEST_TIMEOUT=7s run_script verify "${verify_direct[@]}" --baseline "${FAKE_STATE}/baseline"
expect_call "the Deployment read uses ROLLBACK_KUBECTL_REQUEST_TIMEOUT" "$(in_tree_read_call 7s)"

# The complete table of the Dual authority section on the direct runbook page:
# every combination of external claims and Deployment state. The page check
# needs the docs tree, which the mutant runner's scratch copy lacks.
direct_doc="${repo_root}/docs/public/operate/collector-cutover-rollback-direct.md"
doc_result() { # claims-cell deployment-cell -> the result cell of that row
	awk -F'|' -v claims="$1" -v deployment="$2" '
		function trim(v) { gsub(/^ +| +$/, "", v); return v }
		trim($2) == claims && trim($3) == deployment { print trim($4); exit }' "${direct_doc}"
}
matrix_case() { # claims(1|0) deployment(running|stopped|unreadable) want-exit page-prefix
	local claims="$1" state="$2" want="$3" prefix="$4" claims_cell deployment_cell cell
	new_clear_state "direct-matrix-${claims}-${state}"
	case "${state}" in
	running)
		printf '1\n' >"${FAKE_STATE}/intree_replicas"
		deployment_cell="Running"
		;;
	stopped)
		printf '0\n' >"${FAKE_STATE}/intree_replicas"
		deployment_cell="Not running"
		;;
	unreadable)
		printf '0\n' >"${FAKE_STATE}/intree_replicas"
		touch "${FAKE_STATE}/intree_get_missing"
		deployment_cell="Unreadable or missing"
		;;
	esac
	claims_cell="None"
	: >"${FAKE_STATE}/claims"
	if [[ "${claims}" == 1 ]]; then
		claims_cell="1 or more"
		printf 'jira|ext-1|claimed|active\n' >"${FAKE_STATE}/claims"
	fi
	run_script verify "${verify_direct[@]}" --baseline "${FAKE_STATE}/baseline"
	expect_rc "authority matrix: claims=${claims}, Deployment ${state}" "${want}"
	if [[ ! -d "${repo_root}/docs" ]]; then return 0; fi
	cell="$(doc_result "${claims_cell}" "${deployment_cell}")"
	if [[ "${cell}" == "${prefix}"* ]]; then pass "the direct page row (${claims_cell} / ${deployment_cell}) starts with: ${prefix}"; else
		fail "the direct page row (${claims_cell} / ${deployment_cell}) has result '${cell}', want it to start with: ${prefix}"
	fi
}
matrix_case 1 running 7 "Exit 7"
matrix_case 1 stopped 4 "Exit 4"
matrix_case 1 unreadable 7 "Exit 7"
matrix_case 0 running 0 "Passes"
matrix_case 0 stopped 0 "Passes"
matrix_case 0 unreadable 0 "Passes"

# verify cannot read a Deployment it was not told about.
new_clear_state direct-verify-needs-deployment
run_script verify --collector-kind jira --external-instance ext-1 --in-tree-direct --namespace eso \
	--baseline "${FAKE_STATE}/baseline"
expect_rc "verify in direct mode without --in-tree-deployment is a usage error" 2
expect_out "the usage error says why" "verify --in-tree-direct needs --in-tree-deployment"
expect_no_call "the usage error runs no tool" "kubectl"
run_script verify --collector-kind jira --external-instance ext-1 --in-tree-direct --in-tree-deployment jira-collector \
	--baseline "${FAKE_STATE}/baseline"
expect_rc "verify in direct mode without --namespace is a usage error" 2
expect_out "the usage error names the namespace" "verify --in-tree-direct needs --namespace"
run_script verify --collector-kind jira --external-instance ext-1 --in-tree-direct --namespace eso \
	--in-tree-deployment 'jira;rm' --baseline "${FAKE_STATE}/baseline"
expect_rc "verify rejects an unsafe Deployment name in direct mode" 2
run_script verify --collector-kind jira --external-instance ext-1 --in-tree-direct --capture "${FAKE_STATE}/captured"
expect_rc "capture in direct mode still needs no Deployment" 0

# The default mode never reads the in-tree Deployment and never needs its name.
new_clear_state default-verify-no-deployment
printf 'jira|ext-1|claimed|active\n' >"${FAKE_STATE}/claims"
run_script verify --collector-kind jira --external-instance ext-1 --in-tree-instance tree-1 --baseline "${FAKE_STATE}/baseline"
expect_rc "the default mode still refuses an external claim with exit 4" 4
expect_no_call "the default mode does not read the in-tree Deployment" "${in_tree_read_pattern}"
expect_out "the default mode keeps its authority line" "in_tree_claimed="
# The same, with the Deployment named: naming it must not make the default mode read it.
: >"${FAKE_STATE}/claims"
run_script verify --collector-kind jira --external-instance ext-1 --in-tree-instance tree-1 \
	--namespace eso --in-tree-deployment jira-collector --baseline "${FAKE_STATE}/baseline"
expect_rc "the default mode passes when the Deployment is named and no claim is held" 0
expect_no_call "the default mode ignores a named in-tree Deployment" "${in_tree_read_pattern}"

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
