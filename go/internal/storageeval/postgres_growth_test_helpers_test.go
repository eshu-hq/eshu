// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package storageeval

import "time"

func validHostedGrowthPostgresProof() HostedGrowthPostgresProof {
	observedAt := time.Date(2026, 6, 17, 3, 0, 0, 0, time.UTC)
	relations := []HostedGrowthRelationMeasurement{
		validHostedGrowthRelation(HostedGrowthRelationFactRecords, 3200000, observedAt),
		validHostedGrowthRelation(HostedGrowthRelationFactWorkItems, 180000, observedAt),
		validHostedGrowthRelation(HostedGrowthRelationSharedProjectionIntents, 45000, observedAt),
		validHostedGrowthRelation(HostedGrowthRelationSharedProjectionAcceptance, 43000, observedAt),
	}

	return HostedGrowthPostgresProof{
		ProofID:    "hosted-growth-postgres-proof-20260617T030000Z",
		EshuCommit: "0123456789abcdef0123456789abcdef01234567",
		Profile:    HostedGrowthProfileHostedGrowth,
		Relations:  relations,
		QueueDrain: HostedGrowthQueueDrainMeasurement{
			QueueSurface:    QueueSurfaceReducer,
			PendingRows:     4000,
			RetryRows:       12,
			DeadLetterRows:  2,
			StaleRows:       8,
			ClaimedRows:     16,
			CompletedRows:   3600,
			FailedRows:      3,
			OldestAge:       5 * time.Minute,
			DrainDuration:   12 * time.Minute,
			WorkerCount:     8,
			ObservedAt:      observedAt,
			BoundedEvidence: true,
		},
		FactGrowth:         validHostedGrowthFactGrowth(relations, observedAt),
		IndexBloat:         validHostedGrowthIndexBloat(),
		GraphWritePressure: validHostedGrowthGraphWritePressure(observedAt),
		QueryPlans: []HostedGrowthQueryPlan{
			validHostedGrowthQueryPlan(HostedGrowthQueryClassActiveGenerationRead),
			validHostedGrowthQueryPlan(HostedGrowthQueryClassCorrelationJoin),
			validHostedGrowthQueryPlan(HostedGrowthQueryClassRetentionChangedSince),
			validHostedGrowthQueryPlan(HostedGrowthQueryClassHotAPIRead),
		},
		Retention: validHostedGrowthRetention(),
		Migration: validHostedGrowthMigration(),
		Gate: HostedGrowthOperatorGate{
			FromProfile:              HostedGrowthProfileHostedSmall,
			ToProfile:                HostedGrowthProfileHostedGrowth,
			FactRowsThreshold:        2000000,
			QueueRowsThreshold:       100000,
			IndexBytesThreshold:      10 * 1024 * 1024 * 1024,
			OldestQueueAgeThreshold:  15 * time.Minute,
			RecommendedAction:        "run_hosted_growth_postgres_proof",
			OperatorStatusSignal:     "admin_status_relation_queue_summary",
			RequiresMigrationWindow:  true,
			RequiresRollbackArtifact: true,
		},
		Observability: validHostedGrowthObservability(),
		Decision: HostedGrowthDecision{
			Recommendation:              HostedGrowthRecommendationRetentionTune,
			SchemaChangeRequired:        false,
			RationaleClass:              "retention_lag",
			LinkedIssues:                []int{3741, 3624, 3794, 3795, 3796, 3797, 3798, 3799, 3800, 3801, 3802, 3803, 3804},
			MigrationImplications:       HostedGrowthImplicationNone,
			RollbackImplications:        HostedGrowthImplicationKeepCurrentPostgres,
			RetentionImplications:       HostedGrowthImplicationTunePolicy,
			TenantIsolationImplications: HostedGrowthImplicationUnchanged,
		},
		Verdict:      HostedGrowthVerdictPass,
		FailureClass: HostedGrowthFailureNone,
	}
}

func validHostedGrowthFactGrowth(
	relations []HostedGrowthRelationMeasurement,
	observedAt time.Time,
) HostedGrowthFactGrowth {
	return HostedGrowthFactGrowth{
		ModelVersion:  "fact_records_growth_v1",
		RowsPerSecond: 1200,
		Before: HostedGrowthFactTotals{
			FactRecordsRows: 2100000,
			IndexBytes:      268435456,
			TotalBytes:      805306368,
			ObservedAt:      observedAt.Add(-time.Hour),
			BoundedEvidence: true,
		},
		After: HostedGrowthFactTotals{
			FactRecordsRows: 3200000,
			IndexBytes:      relations[0].IndexBytes,
			TotalBytes:      relations[0].TotalBytes,
			ObservedAt:      observedAt,
			BoundedEvidence: true,
		},
		Families: []HostedGrowthFactFamilyGrowth{
			validHostedGrowthFactFamily(HostedGrowthFactFamilyCollector, 40, 900000, 1400000),
			validHostedGrowthFactFamily(HostedGrowthFactFamilyParser, 17, 650000, 950000),
			validHostedGrowthFactFamily(HostedGrowthFactFamilySearchDocuments, 3, 300000, 450000),
			validHostedGrowthFactFamily(HostedGrowthFactFamilyCorrelation, 8, 250000, 400000),
		},
	}
}

func validHostedGrowthIndexBloat() HostedGrowthIndexBloat {
	return HostedGrowthIndexBloat{
		TableBloatRatio: 0.18,
		DeadTupleBytes:  73400320,
		Indexes: []HostedGrowthIndexBloatSample{
			{IndexClass: HostedGrowthIndexClassActiveGeneration, SizeBytes: 134217728, BloatRatio: 0.16, WriteAmplificationRatio: 1.31, BoundedEvidence: true},
			{IndexClass: HostedGrowthIndexClassCorrelationLookup, SizeBytes: 94371840, BloatRatio: 0.19, WriteAmplificationRatio: 1.44, BoundedEvidence: true},
		},
	}
}

func validHostedGrowthGraphWritePressure(observedAt time.Time) HostedGrowthGraphWritePressure {
	return HostedGrowthGraphWritePressure{
		WriteP95:                      125 * time.Millisecond,
		TimeoutRetries:                3,
		RetryingGraphWriteTimeoutRows: 6,
		DeadLetterRows:                0,
		P95GroupRows:                  180,
		ObservedAt:                    observedAt,
		BoundedEvidence:               true,
	}
}

func validHostedGrowthRetention() HostedGrowthRetentionProof {
	return HostedGrowthRetentionProof{
		SupersededRows:      420000,
		OldestSupersededAge: 48 * time.Hour,
		RetentionLag:        time.Hour,
		PruneDuration:       48 * time.Second,
		PruneBatchRows:      5000,
		ArchiveRequired:     false,
		BoundedEvidence:     true,
	}
}

func validHostedGrowthMigration() HostedGrowthMigrationProof {
	return HostedGrowthMigrationProof{
		Strategy:                              "partition_by_scope_generation_and_queue_domain_after_catalog_proof",
		NativePartitioning:                    false,
		PrimaryKeyIncludesPartitionKey:        true,
		UniqueConstraintsIncludePartitionKey:  true,
		ActiveGenerationReadCorrect:           true,
		ChangedSinceRetainedWindowCorrect:     true,
		DeletesActiveWork:                     false,
		RetriesActiveWork:                     false,
		RollbackBehavior:                      HostedGrowthRollbackKeepCurrentPostgres,
		Scenarios:                             requiredHostedGrowthScenarios(HostedGrowthScenarioPassed),
		PostMigrationReadP95:                  120 * time.Millisecond,
		PostMigrationWriteP95:                 150 * time.Millisecond,
		PostMigrationQueueClaimP95:            80 * time.Millisecond,
		PostMigrationQueueDrainDuration:       10 * time.Minute,
		PostMigrationActiveGenerationRows:     3200000,
		PostMigrationChangedSinceRetainedRows: 55000,
		PostMigrationActiveClaimRowsPreserved: 16,
		PostMigrationRetryRowsPreserved:       12,
		PostMigrationDeadLetterRowsPreserved:  2,
		PostMigrationStaleRowsClassified:      8,
	}
}

func validHostedGrowthObservability() HostedGrowthObservability {
	return HostedGrowthObservability{
		RelationSize:      true,
		IndexSize:         true,
		ReadLatency:       true,
		WriteLatency:      true,
		QueueDepth:        true,
		OldestAge:         true,
		RetryCount:        true,
		DeadLetters:       true,
		StaleRows:         true,
		ActiveClaims:      true,
		MigrationDuration: true,
		RollbackStatus:    true,
	}
}

func validHostedGrowthFactFamily(
	family HostedGrowthFactFamily,
	factKindCount int,
	beforeRows int64,
	afterRows int64,
) HostedGrowthFactFamilyGrowth {
	return HostedGrowthFactFamilyGrowth{
		Family:                  family,
		FactKindCount:           factKindCount,
		BeforeRows:              beforeRows,
		AfterRows:               afterRows,
		AfterIndexBytes:         afterRows * 128,
		WriteAmplificationRatio: 1.25,
		P95Insert:               85 * time.Millisecond,
		BoundedEvidence:         true,
	}
}

func validHostedGrowthQueryPlan(queryClass HostedGrowthQueryClass) HostedGrowthQueryPlan {
	return HostedGrowthQueryPlan{
		QueryClass:      queryClass,
		P95:             75 * time.Millisecond,
		RowsExamined:    18000,
		PlanStatus:      HostedGrowthQueryPlanIndexed,
		SeqScan:         false,
		Spill:           false,
		ObservedAt:      time.Date(2026, 6, 17, 3, 0, 0, 0, time.UTC),
		BoundedEvidence: true,
	}
}

func validHostedGrowthRelation(
	relation HostedGrowthRelation,
	rowCount int64,
	observedAt time.Time,
) HostedGrowthRelationMeasurement {
	return HostedGrowthRelationMeasurement{
		Relation:        relation,
		RowCount:        rowCount,
		IndexBytes:      rowCount * 128,
		TotalBytes:      rowCount * 384,
		ReadP95:         75 * time.Millisecond,
		WriteP95:        95 * time.Millisecond,
		ObservedAt:      observedAt,
		BoundedEvidence: true,
	}
}
