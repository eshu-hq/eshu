// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "strings"

// supplyChainEnvironmentEvidenceDeployEvent and supplyChainEnvironmentEvidenceDeclared
// name the two states #5425 publishes on a reducer_ci_cd_run_correlation
// payload's environment_evidence field: deploy_event means a ci.deployment_event
// observed at the run's commit supplied the environment, declared means it
// came from the CI-declared workflow job gate alone. #5426 reuses this
// vocabulary verbatim rather than inventing a parallel one: reading
// reducer_ci_cd_run_correlation is not a cross-domain join (that fact kind is
// already in the supply-chain reducer's load set, see
// supplyChainImpactFactKinds() in supply_chain_impact.go), and these two
// states are exactly the corroboration signal issue #5426 was redefined to
// consume after its original terraform-tag and reducer-side
// cloud/kubernetes-join sources proved to be dead ends (#5452/PR #5790).
const (
	supplyChainEnvironmentEvidenceDeployEvent = "deploy_event"
	supplyChainEnvironmentEvidenceDeclared    = "declared"
)

// normalizeSupplyChainEnvironmentEvidence maps a raw
// reducer_ci_cd_run_correlation environment_evidence payload value onto the
// closed two-state vocabulary above. An absent key (a deployment row written
// before #5425 landed) or any value other than the exact string
// "deploy_event" maps to "declared" -- never inventing deploy_event
// corroboration the producer did not assert.
func normalizeSupplyChainEnvironmentEvidence(raw string) string {
	if strings.TrimSpace(raw) == supplyChainEnvironmentEvidenceDeployEvent {
		return supplyChainEnvironmentEvidenceDeployEvent
	}
	return supplyChainEnvironmentEvidenceDeclared
}

// recordSupplyChainEnvironmentEvidence merges one environment's evidence
// state into a finding's per-environment evidence map. It mirrors the
// collision rule the issue specifies for Environments itself: once an
// environment has been observed with deploy_event corroboration, a later,
// weaker declared-only observation for the same environment name must never
// downgrade it back.
func recordSupplyChainEnvironmentEvidence(
	state map[string]string,
	environment string,
	evidence string,
) map[string]string {
	environment = strings.TrimSpace(environment)
	if environment == "" {
		return state
	}
	if existing, ok := state[environment]; ok && existing == supplyChainEnvironmentEvidenceDeployEvent {
		return state
	}
	if state == nil {
		state = make(map[string]string, 1)
	}
	state[environment] = evidence
	return state
}
