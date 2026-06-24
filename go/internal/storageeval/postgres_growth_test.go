// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package storageeval

import (
	"strings"
	"testing"
	"time"
)

func TestValidateHostedGrowthPostgresProofAcceptsCoveredEvidence(t *testing.T) {
	proof := validHostedGrowthPostgresProof()

	if err := ValidateHostedGrowthPostgresProof(proof); err != nil {
		t.Fatalf("ValidateHostedGrowthPostgresProof() error = %v, want nil", err)
	}
}

func TestValidateHostedGrowthPostgresProofRejectsInvalidEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*HostedGrowthPostgresProof)
		want   string
	}{
		{
			name: "missing relation sizes",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Relations = proof.Relations[1:]
			},
			want: "relation fact_records measurement is required",
		},
		{
			name: "missing index size",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Relations[0].IndexBytes = 0
			},
			want: "relation fact_records index_bytes must be positive",
		},
		{
			name: "missing fact write latency",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Relations[0].WriteP95 = 0
			},
			want: "relation fact_records write p95 latency must be positive",
		},
		{
			name: "missing relation observation timestamp",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Relations[0].ObservedAt = time.Time{}
			},
			want: "relation fact_records observed_at is required",
		},
		{
			name: "unbounded relation evidence",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Relations[0].BoundedEvidence = false
			},
			want: "relation fact_records evidence must be bounded",
		},
		{
			name: "missing queue drain evidence",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.QueueDrain.CompletedRows = 0
			},
			want: "queue drain completed_rows must be positive",
		},
		{
			name: "missing empty table migration",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Migration.Scenarios = proof.Migration.Scenarios[1:]
			},
			want: "migration scenario empty_table is required",
		},
		{
			name: "missing large table migration",
			mutate: func(proof *HostedGrowthPostgresProof) {
				for i := range proof.Migration.Scenarios {
					if proof.Migration.Scenarios[i].Scenario == HostedGrowthScenarioLargeTable {
						proof.Migration.Scenarios[i].Status = HostedGrowthScenarioFailed
					}
				}
			},
			want: "migration scenario large_table must pass",
		},
		{
			name: "missing rollback scenario",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Migration.RollbackBehavior = ""
			},
			want: "rollback behavior is required",
		},
		{
			name: "active claim deletion",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Migration.DeletesActiveWork = true
			},
			want: "migration must not delete active work",
		},
		{
			name: "active claim retry mutation",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Migration.RetriesActiveWork = true
			},
			want: "migration must not retry active work",
		},
		{
			name: "missing retained window correctness",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Migration.ChangedSinceRetainedWindowCorrect = false
			},
			want: "changed-since retained-window correctness is required",
		},
		{
			name: "partition unique constraint unsafe",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Migration.NativePartitioning = true
				proof.Migration.UniqueConstraintsIncludePartitionKey = false
			},
			want: "native partitioning proof must include partition keys in unique constraints",
		},
		{
			name: "missing retry dead letter stale row coverage",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.QueueDrain.RetryRows = 0
				proof.QueueDrain.DeadLetterRows = 0
				proof.QueueDrain.StaleRows = 0
			},
			want: "queue drain must include retry, dead-letter, and stale rows",
		},
		{
			name: "non reducer queue surface",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.QueueDrain.QueueSurface = QueueSurfaceWorkflow
			},
			want: "queue drain surface must be reducer",
		},
		{
			name: "operator gate not hosted growth",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Gate.ToProfile = HostedGrowthProfileHostedSmall
			},
			want: "operator gate must target hosted_growth",
		},
		{
			name: "operator gate not hosted small source",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Gate.FromProfile = HostedGrowthProfileLocalDev
			},
			want: "operator gate must start from hosted_small",
		},
		{
			name: "missing observability",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Observability.QueueDepth = false
			},
			want: "missing queue-depth observability",
		},
		{
			name: "non passing verdict",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Verdict = HostedGrowthVerdictInsufficientEvidence
				proof.FailureClass = HostedGrowthFailureMissingMigrationProof
			},
			want: "verdict must be pass",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			proof := validHostedGrowthPostgresProof()
			test.mutate(&proof)

			err := ValidateHostedGrowthPostgresProof(proof)
			if err == nil {
				t.Fatalf("ValidateHostedGrowthPostgresProof() error = nil, want %q", test.want)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateHostedGrowthPostgresProof() error = %q, want substring %q", err.Error(), test.want)
			}
		})
	}
}

func validHostedGrowthPostgresProof() HostedGrowthPostgresProof {
	observedAt := time.Date(2026, 6, 17, 3, 0, 0, 0, time.UTC)
	relations := []HostedGrowthRelationMeasurement{
		validHostedGrowthRelation(HostedGrowthRelationFactRecords, 3200000, observedAt),
		validHostedGrowthRelation(HostedGrowthRelationFactWorkItems, 180000, observedAt),
		validHostedGrowthRelation(HostedGrowthRelationSharedProjectionIntents, 45000, observedAt),
		validHostedGrowthRelation(HostedGrowthRelationSharedProjectionAcceptance, 43000, observedAt),
	}

	return HostedGrowthPostgresProof{
		ProofID:    "hosted-growth-postgres-proof-2749",
		EshuCommit: "commit-2749",
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
		Migration: HostedGrowthMigrationProof{
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
		},
		Gate: HostedGrowthOperatorGate{
			FromProfile:              HostedGrowthProfileHostedSmall,
			ToProfile:                HostedGrowthProfileHostedGrowth,
			FactRowsThreshold:        2000000,
			QueueRowsThreshold:       100000,
			IndexBytesThreshold:      10 * 1024 * 1024 * 1024,
			OldestQueueAgeThreshold:  15 * time.Minute,
			RecommendedAction:        "run hosted-growth proof before enabling higher collector fanout",
			OperatorStatusSignal:     "/admin/status queue and relation-size summary",
			RequiresMigrationWindow:  true,
			RequiresRollbackArtifact: true,
		},
		Observability: HostedGrowthObservability{
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
		},
		Verdict:      HostedGrowthVerdictPass,
		FailureClass: HostedGrowthFailureNone,
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
