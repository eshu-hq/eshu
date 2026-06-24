// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package storageeval

func supportedHostedGrowthProfile(profile HostedGrowthProfile) bool {
	switch profile {
	case HostedGrowthProfileLocalDev, HostedGrowthProfileHostedSmall, HostedGrowthProfileHostedGrowth:
		return true
	default:
		return false
	}
}

func supportedHostedGrowthRelation(relation HostedGrowthRelation) bool {
	switch relation {
	case HostedGrowthRelationFactRecords, HostedGrowthRelationFactWorkItems,
		HostedGrowthRelationSharedProjectionIntents, HostedGrowthRelationSharedProjectionAcceptance:
		return true
	default:
		return false
	}
}

func requiredHostedGrowthRelations() []HostedGrowthRelation {
	return []HostedGrowthRelation{
		HostedGrowthRelationFactRecords,
		HostedGrowthRelationFactWorkItems,
		HostedGrowthRelationSharedProjectionIntents,
		HostedGrowthRelationSharedProjectionAcceptance,
	}
}

func supportedHostedGrowthScenario(scenario HostedGrowthScenario) bool {
	switch scenario {
	case HostedGrowthScenarioEmptyTable, HostedGrowthScenarioLargeTable,
		HostedGrowthScenarioOldGeneration, HostedGrowthScenarioStaleRows,
		HostedGrowthScenarioActiveClaim, HostedGrowthScenarioRetryDeadLetter,
		HostedGrowthScenarioRollback:
		return true
	default:
		return false
	}
}

func supportedHostedGrowthScenarioStatus(status HostedGrowthScenarioStatus) bool {
	switch status {
	case HostedGrowthScenarioPassed, HostedGrowthScenarioPlanned, HostedGrowthScenarioFailed:
		return true
	default:
		return false
	}
}

func supportedHostedGrowthRollback(rollback HostedGrowthRollbackBehavior) bool {
	switch rollback {
	case HostedGrowthRollbackKeepCurrentPostgres, HostedGrowthRollbackDiscardCandidate,
		HostedGrowthRollbackFailClosed:
		return true
	default:
		return false
	}
}

func requiredHostedGrowthScenarios(status HostedGrowthScenarioStatus) []HostedGrowthScenarioProof {
	scenarios := requiredHostedGrowthScenarioNames()
	proofs := make([]HostedGrowthScenarioProof, 0, len(scenarios))
	for _, scenario := range scenarios {
		proofs = append(proofs, HostedGrowthScenarioProof{Scenario: scenario, Status: status})
	}
	return proofs
}

func requiredHostedGrowthScenarioNames() []HostedGrowthScenario {
	return []HostedGrowthScenario{
		HostedGrowthScenarioEmptyTable,
		HostedGrowthScenarioLargeTable,
		HostedGrowthScenarioOldGeneration,
		HostedGrowthScenarioStaleRows,
		HostedGrowthScenarioActiveClaim,
		HostedGrowthScenarioRetryDeadLetter,
		HostedGrowthScenarioRollback,
	}
}
