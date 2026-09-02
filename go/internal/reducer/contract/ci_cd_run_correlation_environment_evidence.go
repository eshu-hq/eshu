// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// CICDRunCorrelationEnvironmentEvidenceDeployEvent and
// CICDRunCorrelationEnvironmentEvidenceDeclared name the two states #5425
// publishes on a reducer_ci_cd_run_correlation payload's environment_evidence
// field: deploy_event means a ci.deployment_event observed at the run's
// commit supplied the environment, declared means it came from the
// CI-declared workflow job gate alone.
//
// Exported here (rather than only inside the ci_cd_run_correlation family)
// because #5426's supply_chain_impact domain reuses this vocabulary verbatim
// to read the same payload field: reading reducer_ci_cd_run_correlation is
// not a cross-domain join (that fact kind is already in the supply-chain
// reducer's load set), and these two states are exactly the corroboration
// signal issue #5426 was redefined to consume. Homing the vocabulary here
// keeps the producer (cicdrun, issue #6061) and the consumer
// (supply_chain_impact, reducer root) from importing each other.
const (
	CICDRunCorrelationEnvironmentEvidenceDeployEvent = "deploy_event"
	CICDRunCorrelationEnvironmentEvidenceDeclared    = "declared"
)
