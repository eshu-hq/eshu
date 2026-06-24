// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package storageeval

import (
	"fmt"
	"strings"
)

func validateHostedGrowthRelations(relations []HostedGrowthRelationMeasurement) error {
	seen := make(map[HostedGrowthRelation]struct{}, len(relations))
	for _, relation := range relations {
		if !supportedHostedGrowthRelation(relation.Relation) {
			return fmt.Errorf("unsupported relation %q", relation.Relation)
		}
		if _, ok := seen[relation.Relation]; ok {
			return fmt.Errorf("relation %s measurement is duplicated", relation.Relation)
		}
		seen[relation.Relation] = struct{}{}
		if err := validateHostedGrowthRelation(relation); err != nil {
			return err
		}
	}
	for _, relation := range requiredHostedGrowthRelations() {
		if _, ok := seen[relation]; !ok {
			return fmt.Errorf("relation %s measurement is required", relation)
		}
	}
	return nil
}

func validateHostedGrowthRelation(relation HostedGrowthRelationMeasurement) error {
	if relation.RowCount <= 0 {
		return fmt.Errorf("relation %s row_count must be positive", relation.Relation)
	}
	if relation.IndexBytes <= 0 {
		return fmt.Errorf("relation %s index_bytes must be positive", relation.Relation)
	}
	if relation.TotalBytes < relation.IndexBytes {
		return fmt.Errorf("relation %s total_bytes must be at least index_bytes", relation.Relation)
	}
	if relation.ReadP95 <= 0 {
		return fmt.Errorf("relation %s read p95 latency must be positive", relation.Relation)
	}
	if relation.WriteP95 <= 0 {
		return fmt.Errorf("relation %s write p95 latency must be positive", relation.Relation)
	}
	if relation.ObservedAt.IsZero() {
		return fmt.Errorf("relation %s observed_at is required", relation.Relation)
	}
	if !relation.BoundedEvidence {
		return fmt.Errorf("relation %s evidence must be bounded", relation.Relation)
	}
	return nil
}

func validateHostedGrowthQueueDrain(queue HostedGrowthQueueDrainMeasurement) error {
	if !supportedQueueSurface(queue.QueueSurface) {
		return fmt.Errorf("unsupported queue surface %q", queue.QueueSurface)
	}
	if queue.QueueSurface != QueueSurfaceReducer {
		return fmt.Errorf("queue drain surface must be reducer to prove fact_work_items drain, got %q", queue.QueueSurface)
	}
	if queue.PendingRows < 0 || queue.FailedRows < 0 || queue.ClaimedRows < 0 {
		return fmt.Errorf("queue drain row counts must not be negative")
	}
	if queue.RetryRows <= 0 || queue.DeadLetterRows <= 0 || queue.StaleRows <= 0 {
		return fmt.Errorf("queue drain must include retry, dead-letter, and stale rows")
	}
	if queue.CompletedRows <= 0 {
		return fmt.Errorf("queue drain completed_rows must be positive")
	}
	if queue.OldestAge <= 0 {
		return fmt.Errorf("queue drain oldest_age must be positive")
	}
	if queue.DrainDuration <= 0 {
		return fmt.Errorf("queue drain duration must be positive")
	}
	if queue.WorkerCount <= 0 {
		return fmt.Errorf("queue drain worker_count must be positive")
	}
	if queue.ObservedAt.IsZero() {
		return fmt.Errorf("queue drain observed_at is required")
	}
	if !queue.BoundedEvidence {
		return fmt.Errorf("queue drain evidence must be bounded")
	}
	return nil
}

func validateHostedGrowthMigration(migration HostedGrowthMigrationProof) error {
	if strings.TrimSpace(migration.Strategy) == "" {
		return fmt.Errorf("migration strategy is required")
	}
	if migration.NativePartitioning && !migration.PrimaryKeyIncludesPartitionKey {
		return fmt.Errorf("native partitioning proof must include partition key in primary key")
	}
	if migration.NativePartitioning && !migration.UniqueConstraintsIncludePartitionKey {
		return fmt.Errorf("native partitioning proof must include partition keys in unique constraints")
	}
	if !migration.ActiveGenerationReadCorrect {
		return fmt.Errorf("active-generation read correctness is required")
	}
	if !migration.ChangedSinceRetainedWindowCorrect {
		return fmt.Errorf("changed-since retained-window correctness is required")
	}
	if migration.DeletesActiveWork {
		return fmt.Errorf("migration must not delete active work")
	}
	if migration.RetriesActiveWork {
		return fmt.Errorf("migration must not retry active work")
	}
	if !supportedHostedGrowthRollback(migration.RollbackBehavior) {
		return fmt.Errorf("rollback behavior is required")
	}
	if err := validateHostedGrowthScenarioCoverage(migration.Scenarios); err != nil {
		return err
	}
	if migration.PostMigrationReadP95 <= 0 || migration.PostMigrationWriteP95 <= 0 ||
		migration.PostMigrationQueueClaimP95 <= 0 || migration.PostMigrationQueueDrainDuration <= 0 {
		return fmt.Errorf("post-migration latency and drain measurements must be positive")
	}
	if migration.PostMigrationActiveGenerationRows <= 0 ||
		migration.PostMigrationChangedSinceRetainedRows <= 0 {
		return fmt.Errorf("post-migration active and retained-window rows must be positive")
	}
	if migration.PostMigrationActiveClaimRowsPreserved <= 0 ||
		migration.PostMigrationRetryRowsPreserved <= 0 ||
		migration.PostMigrationDeadLetterRowsPreserved <= 0 ||
		migration.PostMigrationStaleRowsClassified <= 0 {
		return fmt.Errorf("post-migration active, retry, dead-letter, and stale rows must be preserved")
	}
	return nil
}

func validateHostedGrowthScenarioCoverage(proofs []HostedGrowthScenarioProof) error {
	if len(proofs) == 0 {
		return fmt.Errorf("migration scenarios are required")
	}
	seen := make(map[HostedGrowthScenario]HostedGrowthScenarioStatus, len(proofs))
	for _, proof := range proofs {
		if !supportedHostedGrowthScenario(proof.Scenario) {
			return fmt.Errorf("migration scenario %q is unsupported", proof.Scenario)
		}
		if !supportedHostedGrowthScenarioStatus(proof.Status) {
			return fmt.Errorf("migration scenario %s status %q is unsupported", proof.Scenario, proof.Status)
		}
		if _, ok := seen[proof.Scenario]; ok {
			return fmt.Errorf("migration scenario %s is duplicated", proof.Scenario)
		}
		seen[proof.Scenario] = proof.Status
	}
	for _, scenario := range requiredHostedGrowthScenarioNames() {
		status, ok := seen[scenario]
		if !ok {
			return fmt.Errorf("migration scenario %s is required", scenario)
		}
		if status != HostedGrowthScenarioPassed {
			return fmt.Errorf("migration scenario %s must pass", scenario)
		}
	}
	return nil
}

func validateHostedGrowthOperatorGate(gate HostedGrowthOperatorGate) error {
	if !supportedHostedGrowthProfile(gate.FromProfile) {
		return fmt.Errorf("operator gate from_profile is unsupported")
	}
	if gate.FromProfile != HostedGrowthProfileHostedSmall {
		return fmt.Errorf("operator gate must start from hosted_small")
	}
	if gate.ToProfile != HostedGrowthProfileHostedGrowth {
		return fmt.Errorf("operator gate must target hosted_growth")
	}
	if gate.FactRowsThreshold <= 0 || gate.QueueRowsThreshold <= 0 ||
		gate.IndexBytesThreshold <= 0 || gate.OldestQueueAgeThreshold <= 0 {
		return fmt.Errorf("operator gate thresholds must be positive")
	}
	if strings.TrimSpace(gate.RecommendedAction) == "" {
		return fmt.Errorf("operator gate recommended action is required")
	}
	if strings.TrimSpace(gate.OperatorStatusSignal) == "" {
		return fmt.Errorf("operator gate status signal is required")
	}
	if !gate.RequiresMigrationWindow {
		return fmt.Errorf("operator gate must require a migration window")
	}
	if !gate.RequiresRollbackArtifact {
		return fmt.Errorf("operator gate must require a rollback artifact")
	}
	return nil
}

func requireHostedGrowthObservability(observability HostedGrowthObservability) error {
	checks := []struct {
		label string
		ok    bool
	}{
		{"relation-size", observability.RelationSize},
		{"index-size", observability.IndexSize},
		{"read-latency", observability.ReadLatency},
		{"write-latency", observability.WriteLatency},
		{"queue-depth", observability.QueueDepth},
		{"oldest-age", observability.OldestAge},
		{"retry-count", observability.RetryCount},
		{"dead-letters", observability.DeadLetters},
		{"stale-rows", observability.StaleRows},
		{"active-claims", observability.ActiveClaims},
		{"migration-duration", observability.MigrationDuration},
		{"rollback-status", observability.RollbackStatus},
	}
	for _, check := range checks {
		if !check.ok {
			return fmt.Errorf("missing %s observability", check.label)
		}
	}
	return nil
}
