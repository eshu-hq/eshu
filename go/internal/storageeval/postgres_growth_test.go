// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package storageeval

import (
	"math"
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
			name: "private proof id",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.ProofID = "private_prod_cluster_alpha"
			},
			want: "proof id must be a public hosted-growth proof token",
		},
		{
			name: "private commit label",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.EshuCommit = "private_prod_cluster_alpha"
			},
			want: "eshu commit must be a git SHA",
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
			name: "missing active claim evidence",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.QueueDrain.ClaimedRows = 0
			},
			want: "queue drain claimed_rows must be positive",
		},
		{
			name: "missing fact growth evidence",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.FactGrowth = HostedGrowthFactGrowth{}
			},
			want: "fact growth model version is required",
		},
		{
			name: "contradictory fact growth rows",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.FactGrowth.After.FactRecordsRows = proof.Relations[0].RowCount - 1
			},
			want: "fact growth after rows must match fact_records relation row count",
		},
		{
			name: "missing fact family",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.FactGrowth.Families = proof.FactGrowth.Families[1:]
			},
			want: "fact growth family collector is required",
		},
		{
			name: "unreconciled fact family rows",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.FactGrowth.Families[0].AfterRows--
			},
			want: "fact growth family rows must match fact_records after rows",
		},
		{
			name: "missing rows per second",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.FactGrowth.RowsPerSecond = 0
			},
			want: "fact growth rows_per_second must be positive",
		},
		{
			name: "nan rows per second",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.FactGrowth.RowsPerSecond = math.NaN()
			},
			want: "fact growth rows_per_second must be finite",
		},
		{
			name: "infinite rows per second",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.FactGrowth.RowsPerSecond = math.Inf(1)
			},
			want: "fact growth rows_per_second must be finite",
		},
		{
			name: "nan family write amplification",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.FactGrowth.Families[0].WriteAmplificationRatio = math.NaN()
			},
			want: "fact growth family collector ratios must be finite",
		},
		{
			name: "missing index bloat evidence",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.IndexBloat.Indexes = nil
			},
			want: "index bloat samples are required",
		},
		{
			name: "infinite table bloat ratio",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.IndexBloat.TableBloatRatio = math.Inf(1)
			},
			want: "index bloat table ratio must be finite",
		},
		{
			name: "nan sample bloat ratio",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.IndexBloat.Indexes[0].BloatRatio = math.NaN()
			},
			want: "index bloat sample active_generation ratios must be finite",
		},
		{
			name: "infinite sample write amplification",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.IndexBloat.Indexes[0].WriteAmplificationRatio = math.Inf(1)
			},
			want: "index bloat sample active_generation ratios must be finite",
		},
		{
			name: "missing required index bloat class",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.IndexBloat.Indexes = proof.IndexBloat.Indexes[:1]
			},
			want: "index bloat class correlation_lookup is required",
		},
		{
			name: "duplicate index bloat class",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.IndexBloat.Indexes[1].IndexClass = HostedGrowthIndexClassActiveGeneration
			},
			want: "index bloat class active_generation is duplicated",
		},
		{
			name: "missing graph write pressure",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.GraphWritePressure.WriteP95 = 0
			},
			want: "graph-write pressure write p95 must be positive",
		},
		{
			name: "missing query plan class",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.QueryPlans = proof.QueryPlans[1:]
			},
			want: "query plan active_generation_read is required",
		},
		{
			name: "broad query plan",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.QueryPlans[0].SeqScan = true
			},
			want: "query plan active_generation_read must be indexed without seq scan or spill",
		},
		{
			name: "missing retention proof",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Retention.PruneBatchRows = 0
			},
			want: "retention prune batch rows must be positive",
		},
		{
			name: "missing decision proof",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Decision.Recommendation = ""
			},
			want: "decision recommendation is required",
		},
		{
			name: "unsupported defer decision",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Decision.Recommendation = HostedGrowthRecommendationDefer
			},
			want: "defer decision requires fact rows, index bytes, queue rows, queue age, and retention lag below gate thresholds",
		},
		{
			name: "retention tune with schema change",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Decision.SchemaChangeRequired = true
			},
			want: "retention_tune decision requires retention lag without schema or archive requirements",
		},
		{
			name: "retention tune with archive requirement",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Retention.ArchiveRequired = true
			},
			want: "retention_tune decision requires retention lag without schema or archive requirements",
		},
		{
			name: "partition below growth thresholds",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Decision.Recommendation = HostedGrowthRecommendationPartition
				proof.Decision.SchemaChangeRequired = true
				proof.Migration.NativePartitioning = true
				proof.Gate.FactRowsThreshold = proof.FactGrowth.After.FactRecordsRows
				proof.Gate.IndexBytesThreshold = proof.FactGrowth.After.IndexBytes
			},
			want: "partition decision requires native partitioning and row or index growth over threshold",
		},
		{
			name: "archive without retention lag",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Decision.Recommendation = HostedGrowthRecommendationArchive
				proof.Decision.SchemaChangeRequired = true
				proof.Retention.ArchiveRequired = true
				proof.Retention.RetentionLag = 0
			},
			want: "archive decision requires archive posture and measured retention lag",
		},
		{
			name: "split without dominant family",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Decision.Recommendation = HostedGrowthRecommendationSplit
				proof.Decision.SchemaChangeRequired = true
			},
			want: "split decision requires a dominant fact family",
		},
		{
			name: "extra linked issue",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Decision.LinkedIssues = append(proof.Decision.LinkedIssues, 9999)
			},
			want: "decision linked issues must match the required issue set",
		},
		{
			name: "duplicate linked issue",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Decision.LinkedIssues[1] = proof.Decision.LinkedIssues[0]
			},
			want: "decision linked issue 3741 is duplicated",
		},
		{
			name: "unsupported linked issue",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Decision.LinkedIssues[0] = 9999
			},
			want: "decision linked issue 3741 is required",
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
			name: "operator gate private action label",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Gate.RecommendedAction = "private_prod_cluster_alpha"
			},
			want: "operator gate recommended action must be run_hosted_growth_postgres_proof",
		},
		{
			name: "private rationale label",
			mutate: func(proof *HostedGrowthPostgresProof) {
				proof.Decision.RationaleClass = "private_prod_cluster_alpha"
			},
			want: "decision rationale class is unsupported",
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
