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

func supportedHostedGrowthFactFamily(family HostedGrowthFactFamily) bool {
	switch family {
	case HostedGrowthFactFamilyCollector, HostedGrowthFactFamilyParser,
		HostedGrowthFactFamilySearchDocuments, HostedGrowthFactFamilyCorrelation:
		return true
	default:
		return false
	}
}

func requiredHostedGrowthFactFamilies() []HostedGrowthFactFamily {
	return []HostedGrowthFactFamily{
		HostedGrowthFactFamilyCollector,
		HostedGrowthFactFamilyParser,
		HostedGrowthFactFamilySearchDocuments,
		HostedGrowthFactFamilyCorrelation,
	}
}

func supportedHostedGrowthIndexClass(indexClass HostedGrowthIndexClass) bool {
	switch indexClass {
	case HostedGrowthIndexClassActiveGeneration, HostedGrowthIndexClassCorrelationLookup:
		return true
	default:
		return false
	}
}

func requiredHostedGrowthIndexClasses() []HostedGrowthIndexClass {
	return []HostedGrowthIndexClass{
		HostedGrowthIndexClassActiveGeneration,
		HostedGrowthIndexClassCorrelationLookup,
	}
}

func supportedHostedGrowthQueryClass(queryClass HostedGrowthQueryClass) bool {
	switch queryClass {
	case HostedGrowthQueryClassActiveGenerationRead, HostedGrowthQueryClassCorrelationJoin,
		HostedGrowthQueryClassRetentionChangedSince, HostedGrowthQueryClassHotAPIRead:
		return true
	default:
		return false
	}
}

func requiredHostedGrowthQueryClasses() []HostedGrowthQueryClass {
	return []HostedGrowthQueryClass{
		HostedGrowthQueryClassActiveGenerationRead,
		HostedGrowthQueryClassCorrelationJoin,
		HostedGrowthQueryClassRetentionChangedSince,
		HostedGrowthQueryClassHotAPIRead,
	}
}

func supportedHostedGrowthRecommendation(recommendation HostedGrowthRecommendation) bool {
	switch recommendation {
	case HostedGrowthRecommendationPartition, HostedGrowthRecommendationArchive,
		HostedGrowthRecommendationSplit, HostedGrowthRecommendationRetentionTune,
		HostedGrowthRecommendationDefer:
		return true
	default:
		return false
	}
}

func supportedHostedGrowthImplication(implication HostedGrowthImplication) bool {
	switch implication {
	case HostedGrowthImplicationNone, HostedGrowthImplicationKeepCurrentPostgres,
		HostedGrowthImplicationTunePolicy, HostedGrowthImplicationUnchanged,
		HostedGrowthImplicationMigrationWindow:
		return true
	default:
		return false
	}
}

func supportedHostedGrowthRationaleClass(rationaleClass string) bool {
	switch rationaleClass {
	case "growth_threshold", "retention_lag", "family_dominance", "archive_pressure", "below_threshold":
		return true
	default:
		return false
	}
}

func requiredHostedGrowthLinkedIssues() []int {
	return []int{3741, 3624, 3794, 3795, 3796, 3797, 3798, 3799, 3800, 3801, 3802, 3803, 3804}
}

func requiredHostedGrowthLinkedIssue(issue int) bool {
	for _, required := range requiredHostedGrowthLinkedIssues() {
		if issue == required {
			return true
		}
	}
	return false
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
