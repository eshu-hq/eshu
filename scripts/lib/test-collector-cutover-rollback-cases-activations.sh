#!/usr/bin/env bash
# Activations/GitOps mode cases for scripts/collector-cutover-rollback.sh
# (#9293, unblocked by the #9267 drill): collectors whose authority lives in
# GitOps values rather than a shared component registry. Pod-local
# registry.json writes on emptyDir volumes are re-seeded from the
# eshu-component-activations ConfigMap on every pod start, so `component
# disable` and `revoke-grant` cannot stop them. In this mode the GitOps removal
# commit REPLACES script steps 1 and 3 with read-only proofs; steps 2, 5, and 6
# are unchanged and step 4 restores through GitOps (git revert + sync).
# Sourced by scripts/test-collector-cutover-rollback.sh after the harness.

# The harness defines these variables and the entry script sources this file.
# shellcheck disable=SC2154,SC2034

REMOVAL_SHA="abcdef1234567890abcdef1234567890abcdef12"

# activations_args builds the argument list of a GitOps-mode run: base_args
# with the helm restore trio swapped for the mandatory GitOps restore flag and
# the removal-commit proof options.
activations_args() {
	base_args
	local -a swapped=()
	local skip=0 word
	for word in "${ARGS[@]}"; do
		if [[ "${skip}" == 1 ]]; then
			skip=0
			continue
		fi
		case "${word}" in
		--helm-release | --helm-chart | --restore-values) skip=1 ;;
		*) swapped+=("${word}") ;;
		esac
	done
	ARGS=("${swapped[@]}" --activations-mode --restore-applied-by-gitops
		--removal-commit "$REMOVAL_SHA" --gitops-repo "${FAKE_STATE}/repo")
}

# new_gitops_state is the world after the GitOps removal synced: the activation
# is gone from the registry seed and the live ConfigMap, the worker is gone,
# no claim is held, no grant row remains, and the removal diff names ext-1.
new_gitops_state() {
	new_clear_state "$1"
	: >"${FAKE_STATE}/seed_activations"
	: >"${FAKE_STATE}/configmap_entries"
	: >"${FAKE_STATE}/grants"
	touch "${FAKE_STATE}/worker_absent"
}

# --- the defect this mode fixes (RE:rollback-procedure-001) -------------------------

# The coordinator restart at the end of step 1 reinstalls the activation from
# the unchanged ConfigMap, so the post-restart confirmation fails: step 1 ends
# FAILED after scaling the live producer to zero. Running it to watch it fail
# would disrupt a live environment for a foreknown outcome.
new_state reseed-step1
touch "${FAKE_STATE}/reseed_on_restart"
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "step 1 ends FAILED after the coordinator restart re-seeds the activation" 3
expect_out "the failure is the post-restart confirmation" "FAILED[step 1]"
expect_no_call "no drain query runs after the re-seeded step 1 fails" "psql drain"
expect_no_call "no revoke runs after the re-seeded step 1 fails" "revoke-grant"

# --- option rules ---------------------------------------------------------------------

# strip_arg removes a flag (and its value when take is 1) from ARGS.
strip_arg() { # flag [take-value]
	local flag="$1" take="${2:-0}" skip=0 word
	local -a kept=()
	for word in "${ARGS[@]}"; do
		if [[ "${skip}" == 1 ]]; then
			skip=0
			continue
		fi
		if [[ "${word}" == "${flag}" ]]; then
			skip="${take}"
			continue
		fi
		kept+=("${word}")
	done
	ARGS=("${kept[@]}")
}

# activations_direct_args composes the direct producer swap with the GitOps
# mode swap: the restored producer is direct and the removal proved by GitOps.
activations_direct_args() {
	base_args
	local -a swapped=()
	local skip=0 word
	for word in "${ARGS[@]}"; do
		if [[ "${skip}" == 1 ]]; then
			skip=0
			continue
		fi
		case "${word}" in
		--in-tree-instance)
			skip=1
			swapped+=(--in-tree-direct)
			;;
		--helm-release | --helm-chart | --restore-values) skip=1 ;;
		*) swapped+=("${word}") ;;
		esac
	done
	ARGS=("${swapped[@]}" --activations-mode --restore-applied-by-gitops
		--removal-commit "$REMOVAL_SHA" --gitops-repo "${FAKE_STATE}/repo")
}

new_state activations-plan
activations_args
run_script plan "${ARGS[@]}"
expect_rc "plan in activations mode exits 0" 0

new_state act-no-gitops-flag
activations_args
strip_arg --restore-applied-by-gitops
run_script plan "${ARGS[@]}"
expect_rc "--activations-mode without --restore-applied-by-gitops is a usage error" 2
expect_out "the refusal names the restore flag" "--activations-mode needs --restore-applied-by-gitops"

new_state act-with-helm
activations_args
run_script plan "${ARGS[@]}" --helm-release eshu
expect_rc "--activations-mode with helm restore options is a usage error" 2
expect_out "the refusal names the exclusion" "excludes the helm restore options"

new_state act-no-commit
activations_args
strip_arg --removal-commit 1
run_script plan "${ARGS[@]}"
expect_rc "--activations-mode without --removal-commit is a usage error" 2
expect_out "the refusal names the commit" "--activations-mode needs --removal-commit"

new_state act-bad-commit
activations_args
run_script plan "${ARGS[@]}" --removal-commit 'zzzz-not-a-sha'
expect_rc "a non-hex --removal-commit is a usage error" 2
expect_out "the refusal says what a SHA looks like" "must be a commit SHA"

new_state act-no-repo
activations_args
strip_arg --gitops-repo 1
run_script plan "${ARGS[@]}"
expect_rc "--activations-mode without --gitops-repo is a usage error" 2
expect_out "the refusal names the repo" "--activations-mode needs --gitops-repo"

new_state act-repo-not-a-dir
activations_args
run_script plan "${ARGS[@]}" --gitops-repo "${FAKE_STATE}/no-such-dir"
expect_rc "a --gitops-repo that is not a directory is a usage error" 2
expect_out "the refusal names the directory" "is not a directory"

new_state act-commit-without-mode
base_args
run_script plan "${ARGS[@]}" --removal-commit "$REMOVAL_SHA"
expect_rc "--removal-commit without --activations-mode is a usage error" 2
expect_out "the refusal names the mode" "need --activations-mode"

new_state act-unsafe-configmap
activations_args
run_script plan "${ARGS[@]}" --activations-configmap 'maps;rm'
expect_rc "an unsafe ConfigMap name is rejected" 2

new_state act-verify-needs-proof
activations_args
strip_arg --removal-commit 1
run_script verify "${ARGS[@]}" --baseline "${FAKE_STATE}/baseline"
expect_rc "verify in activations mode without --removal-commit is a usage error" 2

new_state act-capture-needs-no-proof
run_script verify --collector-kind jira --external-instance ext-1 --in-tree-instance tree-1 \
	--activations-mode --capture "${FAKE_STATE}/captured"
expect_rc "capture in activations mode needs no GitOps proof options" 0

# --- plan -------------------------------------------------------------------------

new_state act-plan-content
activations_args
run_script plan "${ARGS[@]}"
expect_rc "the GitOps plan exits 0" 0
expect_out "the plan names the mode" "Activations/GitOps mode"
expect_out "the plan prints the removal read" "git -C ${FAKE_STATE}/repo show ${REMOVAL_SHA}"
expect_out "the plan prints the ConfigMap read" "get configmap/eshu-component-activations"
expect_out "the plan marks step 1 replaced" "STEP 1 stop new claims for the external producer (activations/GitOps mode)"
expect_out "the plan marks step 3 replaced" "STEP 3 prove the external producer grants are gone (activations/GitOps mode)"
expect_out "the plan prints the revert as an operator command" "operator\$ git -C ${FAKE_STATE}/repo revert ${REMOVAL_SHA}"
expect_no_out "the plan prints no disable" "component disable"
expect_no_out "the plan prints no revoke" "revoke-grant"
expect_no_out "the plan prints no helm restore" "helm upgrade"
if [[ ! -e "${FAKE_STATE}/calls.log" ]]; then pass "the GitOps plan invokes no tool"; else fail "the GitOps plan invoked a tool"; fi

new_state act-plan-configmap
activations_args
run_script plan "${ARGS[@]}" --activations-configmap custom-activations
expect_rc "a plan with a custom ConfigMap exits 0" 0
expect_out "the plan prints the custom ConfigMap" "get configmap/custom-activations"

new_state act-default-plan-unchanged
base_args
run_script plan "${ARGS[@]}"
expect_rc "the default plan still exits 0" 0
expect_out "the default plan still disables" "component disable"
expect_no_out "the default plan never reads the removal commit" "git -C"
expect_no_out "the default plan has no operator commands" "operator\$"

# --- full run -----------------------------------------------------------------------

new_gitops_state act-run
activations_args
run_script plan "${ARGS[@]}"
expect_rc "the GitOps plan for the run state exits 0" 0
plan_gitops_out="${out}"
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "a GitOps-mode run completes" 0
expect_order "a GitOps-mode run proves the removal before restoring" \
	"git -C" \
	"eshu component list" \
	"configmap/eshu-component-activations" \
	"psql drain" \
	"eshu component grants" \
	"kubectl -n eso scale deployment/jira-collector --replicas=1" \
	"psql replay" \
	"psql truth"
expect_no_call "a GitOps-mode run never disables" "component disable"
expect_no_call "a GitOps-mode run never revokes" "revoke-grant"
expect_no_call "a GitOps-mode run never scales the worker" "scale deployment/eshu-worker"
restarts_gitops="$(calls | awk '/rollout restart deployment\/eshu-coordinator/ { n++ } END { print n + 0 }')"
if [[ "${restarts_gitops}" == 1 ]]; then pass "a GitOps-mode run restarts the coordinator once, in step 4"; else
	fail "a GitOps-mode run restarted the coordinator ${restarts_gitops} time(s), want 1 (step 4 only, never step 1G)"
fi
expect_out "a GitOps-mode run reports step 1 as ok" "RESULT[step 1]: ok"
expect_out "a GitOps-mode run records the removal SHA" "EVIDENCE[step 1]: removal=${REMOVAL_SHA}"
expect_out "a GitOps-mode run records the absent grants" "EVIDENCE[step 3]: no grants remain"
expect_out "a GitOps-mode run prints the empty grants listing as the proof" "no producer grants"
expect_out "a GitOps-mode run reports the final verdict" "ROLLBACK COMPLETE"
missing=0
while IFS= read -r line; do
	cmd="${line#  \$ }"
	[[ "${cmd}" == *psql* ]] && continue
	cmd="${cmd//\\/}"
	if [[ -z "$(first_line "${cmd}")" ]]; then
		missing=1
		printf '    planned but never run: %s\n' "${cmd}"
	fi
done < <(printf '%s\n' "${plan_gitops_out}" | awk '/^  \$ /')
if [[ "${missing}" == 0 ]]; then pass "every planned GitOps command is executed"; else fail "GitOps plan and run disagree"; fi

new_gitops_state act-step1
activations_args
run_script run --yes --through-step 1 --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "step 1G alone succeeds on the removed world" 0
expect_out "step 1G records the removal" "removal=${REMOVAL_SHA}"

new_state default-no-git
base_args
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "a default run still succeeds" 0
expect_no_call "the default mode never reads the removal commit" "git -C"
expect_no_call "the default mode never reads the activations ConfigMap" "configmap/"

# --- refusals: the removal must be real ------------------------------------------------

# No GitOps removal happened: the seed still holds the activation, so the mode
# refuses where the default mode would fail after disrupting the producer.
new_state act-no-removal
activations_args
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "the mode refuses while the activation is still enabled" 4
expect_out "the refusal names the still-enabled activation" "still has 1 enabled activation"
expect_no_call "no revoke after the refused removal proof" "revoke-grant"
expect_no_call "no restore after the refused removal proof" "scale deployment/jira-collector"
expect_no_call "no coordinator restart after the refused removal proof" "rollout restart"

# The instance ID is mistyped while the real one stays enabled.
new_state act-typo-enabled
activations_args
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}" --external-instance ext-TYPO
expect_rc "a mistyped instance is refused while the real one is enabled" 4
expect_out "the refusal names the still-enabled activation" "still has 1 enabled activation"
expect_no_call "no revoke after a mistyped instance" "revoke-grant"

# The removal commit is the wrong one: the registry is clear but the diff never
# mentions the instance, so the proof itself fails.
new_gitops_state act-wrong-commit
printf 'commit 1111111111111111111111111111111111111111\n\n    Remove something unrelated\n' >"${FAKE_STATE}/removal_diff"
activations_args
run_script run --yes --through-step 1 --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "a removal commit that never mentions the instance fails step 1G" 3
expect_out "the failure names the instance" "does not mention instance ext-1"
expect_no_call "no drain query after the wrong removal commit" "psql drain"

# The removal has not synced: the registry is clear but the live ConfigMap still
# mentions the instance.
new_gitops_state act-configmap-stale
printf 'ext-1\n' >"${FAKE_STATE}/configmap_entries"
activations_args
run_script run --yes --through-step 1 --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "a ConfigMap that still mentions the instance refuses step 1G" 4
expect_out "the refusal names the ConfigMap" "still mentions instance ext-1"
expect_no_call "no drain query while the ConfigMap still mentions the instance" "psql drain"

# The commit cannot be read: an unreadable proof is unknown, never clear.
new_gitops_state act-git-unreadable
touch "${FAKE_STATE}/git_fail"
activations_args
run_script run --yes --through-step 1 --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "an unreadable removal commit fails step 1G" 3
expect_out "the failure names the removal commit" "reading the removal commit"
expect_no_call "no drain query when the removal commit cannot be read" "psql drain"

# Grant rows remain: the removal did not delete the grant source.
new_gitops_state act-rows-remain
printf 'dev.example.jira|1.2.3|work_item.issue|jira|false\n' >"${FAKE_STATE}/grants"
activations_args
run_script run --yes --from-step 3 --through-step 3 --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "step 3G fails while grant rows remain" 3
expect_out "the failure names the remaining rows" "grant rows still exist"
expect_no_call "no restore while grant rows remain" "scale deployment/jira-collector"

# A mistyped producer ID reads zero rows while the activation stays enabled:
# the registry guard refuses instead of reading it as revoked.
new_state act-typo-producer
: >"${FAKE_STATE}/grants"
activations_args
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}" --producer-id dev.example.typo
expect_rc "a mistyped producer ID is refused while the activation is enabled" 4
expect_no_call "no restore after a mistyped producer ID" "scale deployment/jira-collector"

# Step 2 still gates on the confirmed removal: a live worker blocks the drain.
new_gitops_state act-worker-alive-2
printf '1\n' >"${FAKE_STATE}/worker_pods"
printf '1\n' >"${FAKE_STATE}/worker_replicas"
rm -f "${FAKE_STATE}/worker_absent"
activations_args
run_script run --yes --from-step 2 --step-timeout 0 --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "step 2 in GitOps mode refuses while a worker pod is alive" 4
expect_no_call "no drain query while a worker pod is alive" "psql drain"

# A resume at step 2 re-proves the removal, including the ConfigMap read: the
# step 1G confirmation is not a memory of the earlier run.
new_gitops_state act-configmap-gate-2
printf 'ext-1\n' >"${FAKE_STATE}/configmap_entries"
activations_args
run_script run --yes --from-step 2 --through-step 2 --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "step 2 in GitOps mode refuses while the ConfigMap still mentions the instance" 4
expect_no_call "no drain query while the resumed ConfigMap check fails" "psql drain"

# --- verify ----------------------------------------------------------------------------

new_gitops_state act-verify
activations_args
run_script verify "${ARGS[@]}" --baseline "${FAKE_STATE}/baseline"
expect_rc "verify in activations mode passes on the removed world" 0
expect_out "verify records the absent grants" "no external producer grants remain"

new_gitops_state act-verify-rows
printf 'dev.example.jira|1.2.3|work_item.issue|jira|false\n' >"${FAKE_STATE}/grants"
activations_args
run_script verify "${ARGS[@]}" --baseline "${FAKE_STATE}/baseline"
expect_rc "verify in activations mode refuses while grant rows remain" 4

# --- direct producer composes with the mode ------------------------------------------------

new_gitops_state act-direct-plan
activations_direct_args
run_script plan "${ARGS[@]}"
expect_rc "the direct GitOps plan exits 0" 0
expect_out "the direct GitOps plan names the mode" "Activations/GitOps mode"
expect_out "the direct GitOps plan names the direct producer" "direct collector"
expect_no_out "the direct GitOps plan prints no disable" "component disable"
if [[ ! -e "${FAKE_STATE}/calls.log" ]]; then pass "the direct GitOps plan invokes no tool"; else fail "the direct GitOps plan invoked a tool"; fi

new_gitops_state act-direct-run
activations_direct_args
printf '1|2026-10-23T12:05:00.000000Z\n' >"${FAKE_STATE}/direct_commits"
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "a direct run in activations mode completes" 0
expect_order "a direct GitOps run proves the removal, then reads the generation signal" \
	"git -C" \
	"eshu component grants" \
	"kubectl -n eso scale deployment/jira-collector --replicas=1" \
	"psql direct-commit" \
	"psql truth"
expect_no_call "a direct GitOps run never restarts the coordinator" "rollout restart"
expect_no_call "a direct GitOps run never reads work-item replay" "psql replay"

# --- help text ---------------------------------------------------------------------------------

run_script --help
expect_out "help documents --activations-mode" "--activations-mode"
expect_out "help documents --removal-commit" "--removal-commit"
expect_out "help documents --gitops-repo" "--gitops-repo"
