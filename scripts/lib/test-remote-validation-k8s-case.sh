#!/usr/bin/env bash

# gate_commands_are_mirrored reports whether each command is present in both
# the local gate registry and the CI workflow gate.
gate_commands_are_mirrored() {
	local registry_gate="$1" workflow_gate="$2"
	shift 2
	local command

	for command in "$@"; do
		printf '%s\n' "${registry_gate}" | rg --fixed-strings "${command}" >/dev/null || return 1
		printf '%s\n' "${workflow_gate}" | rg --fixed-strings "${command}" >/dev/null || return 1
	done
}

# run_remote_validation_k8s_case proves that Kubernetes proof scripts and
# fixtures select the remote-validation gate identically in the registry and
# workflow, and that the gate runs both provenance verifier self-tests.
run_remote_validation_k8s_case() {
	local repo_root="$1" registry="$2" registry_gate="$3"
	local workflow_filter="$4" workflow_gate="$5"
	local script_trigger='scripts/**/*k8s*.sh'
	local fixture_trigger='tests/fixtures/governance_k8s_two_team_proof/**'
	local verifier_test='bash scripts/test-verify-k8s-two-team-governance-proof.sh'
	local provenance_test='bash scripts/test-k8s-two-team-governance-provenance.sh'
	local selection trigger representative fixture

	if gate_commands_are_mirrored "${registry_gate}" "${workflow_gate}" \
		"${verifier_test}" "${provenance_test}" &&
		! gate_commands_are_mirrored "${provenance_test}" \
			"${verifier_test} ${provenance_test}" "${verifier_test}" "${provenance_test}" &&
		! gate_commands_are_mirrored "${verifier_test}" \
			"${verifier_test} ${provenance_test}" "${verifier_test}" "${provenance_test}" &&
		! gate_commands_are_mirrored "${verifier_test} ${provenance_test}" \
			"${provenance_test}" "${verifier_test}" "${provenance_test}" &&
		! gate_commands_are_mirrored "${verifier_test} ${provenance_test}" \
			"${verifier_test}" "${verifier_test}" "${provenance_test}"; then
		record_pass "Kubernetes governance verifier and provenance capture tests run locally and in CI"
	else
		record_fail "Kubernetes governance verifier and provenance capture tests run locally and in CI"
	fi
	if printf '%s\n' "${registry_gate}" |
		rg --fixed-strings "      - \"${fixture_trigger}\"" >/dev/null &&
		printf '%s\n' "${workflow_filter}" |
			rg --fixed-strings "              - '${fixture_trigger}'" >/dev/null; then
		record_pass "Kubernetes governance proof fixtures trigger the static contract gate"
	else
		record_fail "Kubernetes governance proof fixtures trigger the static contract gate"
	fi
	if [[ "$(printf '%s\n' "${registry_gate}" | rg -c --fixed-strings "      - \"${script_trigger}\"")" == 1 ]] &&
		[[ "$(printf '%s\n' "${workflow_filter}" | rg -c --fixed-strings "              - '${script_trigger}'")" == 1 ]] &&
		! printf '%s\n%s\n' "${registry_gate}" "${workflow_filter}" |
			rg 'scripts/\*\*/(run-k8s-|verify-k8s-|test-verify-k8s-)|scripts/lib/k8s-two-team-governance-provenance\.sh' >/dev/null; then
		record_pass "Kubernetes proof scripts use one mirrored recursive trigger"
	else
		record_fail "Kubernetes proof scripts use one mirrored recursive trigger"
	fi

	while IFS='|' read -r trigger representative; do
		selection="$(
			printf '%s\n' "${representative}" |
				(cd "${repo_root}/go" && go run ./cmd/ci-gates select \
					--registry "${registry}" --tier pre-pr --paths-from - --explain)
		)"
		if printf '%s\n' "${selection}" |
			rg '^SELECTED[[:space:]]+remote-validation-artifacts[[:space:]]' >/dev/null &&
			printf '%s\n' "${selection}" |
				rg --fixed-strings "matched trigger \"${trigger}\" on path \"${representative}\"" >/dev/null; then
			record_pass "Kubernetes proof path selects remote-validation-artifacts (${representative})"
		else
			record_fail "Kubernetes proof path selects remote-validation-artifacts (${representative})"
		fi
	done <<'K8S_PROOF_PATHS'
scripts/**/*k8s*.sh|scripts/run-k8s-two-team-governance-proof.sh
scripts/**/*k8s*.sh|scripts/verify-k8s-two-team-governance-proof.sh
scripts/**/*k8s*.sh|scripts/test-verify-k8s-two-team-governance-proof.sh
scripts/**/*k8s*.sh|scripts/test-k8s-two-team-governance-provenance.sh
scripts/**/*k8s*.sh|scripts/k8s-two-team-governance-manifests.sh
scripts/**/*k8s*.sh|scripts/lib/k8s-two-team-governance-provenance.sh
scripts/**/*k8s*.sh|scripts/lib/test-remote-validation-k8s-case.sh
K8S_PROOF_PATHS

	fixture='tests/fixtures/governance_k8s_two_team_proof/good/provenance.json'
	selection="$(
		printf '%s\n' "${fixture}" |
			(cd "${repo_root}/go" && go run ./cmd/ci-gates select \
				--registry "${registry}" --tier pre-pr --paths-from - --explain)
	)"
	if printf '%s\n' "${selection}" |
		rg '^SELECTED[[:space:]]+remote-validation-artifacts[[:space:]]' >/dev/null &&
		printf '%s\n' "${selection}" |
			rg --fixed-strings "matched trigger \"${fixture_trigger}\" on path \"${fixture}\"" >/dev/null; then
		record_pass "Kubernetes governance proof fixture selects remote-validation-artifacts"
	else
		record_fail "Kubernetes governance proof fixture selects remote-validation-artifacts"
	fi
}
