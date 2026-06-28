// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package storageeval

import (
	"fmt"
	"math"
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
	if queue.PendingRows < 0 || queue.FailedRows < 0 {
		return fmt.Errorf("queue drain row counts must not be negative")
	}
	if queue.ClaimedRows <= 0 {
		return fmt.Errorf("queue drain claimed_rows must be positive")
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

func validateHostedGrowthFactGrowth(
	growth HostedGrowthFactGrowth,
	relations []HostedGrowthRelationMeasurement,
) error {
	if strings.TrimSpace(growth.ModelVersion) == "" {
		return fmt.Errorf("fact growth model version is required")
	}
	if growth.ModelVersion != "fact_records_growth_v1" {
		return fmt.Errorf("fact growth model version must be fact_records_growth_v1")
	}
	if nonFinite(growth.RowsPerSecond) {
		return fmt.Errorf("fact growth rows_per_second must be finite")
	}
	if growth.RowsPerSecond <= 0 {
		return fmt.Errorf("fact growth rows_per_second must be positive")
	}
	if err := validateHostedGrowthFactTotals("before", growth.Before); err != nil {
		return err
	}
	if err := validateHostedGrowthFactTotals("after", growth.After); err != nil {
		return err
	}
	if growth.After.FactRecordsRows < growth.Before.FactRecordsRows {
		return fmt.Errorf("fact growth after rows must be at least before rows")
	}
	if growth.After.IndexBytes < growth.Before.IndexBytes {
		return fmt.Errorf("fact growth after index bytes must be at least before index bytes")
	}
	factRelation, ok := hostedGrowthRelation(relations, HostedGrowthRelationFactRecords)
	if !ok {
		return fmt.Errorf("relation fact_records measurement is required")
	}
	if growth.After.FactRecordsRows != factRelation.RowCount {
		return fmt.Errorf("fact growth after rows must match fact_records relation row count")
	}
	if growth.After.IndexBytes != factRelation.IndexBytes {
		return fmt.Errorf("fact growth after index bytes must match fact_records relation index bytes")
	}
	if growth.After.TotalBytes != factRelation.TotalBytes {
		return fmt.Errorf("fact growth after total bytes must match fact_records relation total bytes")
	}
	if err := validateHostedGrowthFactFamilies(growth.Families, growth.After.FactRecordsRows); err != nil {
		return err
	}
	return nil
}

func validateHostedGrowthFactTotals(label string, totals HostedGrowthFactTotals) error {
	if totals.FactRecordsRows <= 0 {
		return fmt.Errorf("fact growth %s rows must be positive", label)
	}
	if totals.IndexBytes <= 0 {
		return fmt.Errorf("fact growth %s index bytes must be positive", label)
	}
	if totals.TotalBytes < totals.IndexBytes {
		return fmt.Errorf("fact growth %s total bytes must be at least index bytes", label)
	}
	if totals.ObservedAt.IsZero() {
		return fmt.Errorf("fact growth %s observed_at is required", label)
	}
	if !totals.BoundedEvidence {
		return fmt.Errorf("fact growth %s evidence must be bounded", label)
	}
	return nil
}

func validateHostedGrowthFactFamilies(families []HostedGrowthFactFamilyGrowth, afterRows int64) error {
	if len(families) == 0 {
		return fmt.Errorf("fact growth families are required")
	}
	seen := make(map[HostedGrowthFactFamily]struct{}, len(families))
	var familyRows int64
	for _, family := range families {
		if !supportedHostedGrowthFactFamily(family.Family) {
			return fmt.Errorf("fact growth family %q is unsupported", family.Family)
		}
		if _, ok := seen[family.Family]; ok {
			return fmt.Errorf("fact growth family %s is duplicated", family.Family)
		}
		seen[family.Family] = struct{}{}
		if nonFinite(family.WriteAmplificationRatio) {
			return fmt.Errorf("fact growth family %s ratios must be finite", family.Family)
		}
		if family.FactKindCount <= 0 || family.BeforeRows < 0 ||
			family.AfterRows < family.BeforeRows || family.AfterIndexBytes <= 0 ||
			family.WriteAmplificationRatio <= 0 || family.P95Insert <= 0 ||
			!family.BoundedEvidence {
			return fmt.Errorf("fact growth family %s must include positive counts, write amplification, insert latency, and bounded evidence", family.Family)
		}
		familyRows += family.AfterRows
	}
	for _, family := range requiredHostedGrowthFactFamilies() {
		if _, ok := seen[family]; !ok {
			return fmt.Errorf("fact growth family %s is required", family)
		}
	}
	if familyRows != afterRows {
		return fmt.Errorf("fact growth family rows must match fact_records after rows")
	}
	return nil
}

func validateHostedGrowthIndexBloat(bloat HostedGrowthIndexBloat) error {
	if nonFinite(bloat.TableBloatRatio) {
		return fmt.Errorf("index bloat table ratio must be finite")
	}
	if bloat.TableBloatRatio < 0 {
		return fmt.Errorf("index bloat table ratio must not be negative")
	}
	if bloat.DeadTupleBytes < 0 {
		return fmt.Errorf("index bloat dead tuple bytes must not be negative")
	}
	if len(bloat.Indexes) == 0 {
		return fmt.Errorf("index bloat samples are required")
	}
	seen := make(map[HostedGrowthIndexClass]struct{}, len(bloat.Indexes))
	for _, sample := range bloat.Indexes {
		if !supportedHostedGrowthIndexClass(sample.IndexClass) {
			return fmt.Errorf("index bloat class %q is unsupported", sample.IndexClass)
		}
		if _, ok := seen[sample.IndexClass]; ok {
			return fmt.Errorf("index bloat class %s is duplicated", sample.IndexClass)
		}
		seen[sample.IndexClass] = struct{}{}
		if nonFinite(sample.BloatRatio) || nonFinite(sample.WriteAmplificationRatio) {
			return fmt.Errorf("index bloat sample %s ratios must be finite", sample.IndexClass)
		}
		if sample.SizeBytes <= 0 || sample.BloatRatio < 0 ||
			sample.WriteAmplificationRatio <= 0 || !sample.BoundedEvidence {
			return fmt.Errorf("index bloat sample %s must include size, bloat, write amplification, and bounded evidence", sample.IndexClass)
		}
	}
	for _, indexClass := range requiredHostedGrowthIndexClasses() {
		if _, ok := seen[indexClass]; !ok {
			return fmt.Errorf("index bloat class %s is required", indexClass)
		}
	}
	return nil
}

func nonFinite(value float64) bool {
	return math.IsNaN(value) || math.IsInf(value, 0)
}

func validateHostedGrowthGraphWritePressure(pressure HostedGrowthGraphWritePressure) error {
	if pressure.WriteP95 <= 0 {
		return fmt.Errorf("graph-write pressure write p95 must be positive")
	}
	if pressure.TimeoutRetries < 0 || pressure.RetryingGraphWriteTimeoutRows < 0 ||
		pressure.DeadLetterRows < 0 {
		return fmt.Errorf("graph-write pressure retry and dead-letter counts must not be negative")
	}
	if pressure.P95GroupRows <= 0 {
		return fmt.Errorf("graph-write pressure p95 group rows must be positive")
	}
	if pressure.ObservedAt.IsZero() {
		return fmt.Errorf("graph-write pressure observed_at is required")
	}
	if !pressure.BoundedEvidence {
		return fmt.Errorf("graph-write pressure evidence must be bounded")
	}
	return nil
}

func validateHostedGrowthQueryPlans(plans []HostedGrowthQueryPlan) error {
	if len(plans) == 0 {
		return fmt.Errorf("query plans are required")
	}
	seen := make(map[HostedGrowthQueryClass]struct{}, len(plans))
	for _, plan := range plans {
		if !supportedHostedGrowthQueryClass(plan.QueryClass) {
			return fmt.Errorf("query plan class %q is unsupported", plan.QueryClass)
		}
		if _, ok := seen[plan.QueryClass]; ok {
			return fmt.Errorf("query plan %s is duplicated", plan.QueryClass)
		}
		seen[plan.QueryClass] = struct{}{}
		if plan.P95 <= 0 || plan.RowsExamined <= 0 || plan.PlanStatus != HostedGrowthQueryPlanIndexed ||
			plan.SeqScan || plan.Spill || plan.ObservedAt.IsZero() || !plan.BoundedEvidence {
			return fmt.Errorf("query plan %s must be indexed without seq scan or spill", plan.QueryClass)
		}
	}
	for _, queryClass := range requiredHostedGrowthQueryClasses() {
		if _, ok := seen[queryClass]; !ok {
			return fmt.Errorf("query plan %s is required", queryClass)
		}
	}
	return nil
}

func validateHostedGrowthRetention(retention HostedGrowthRetentionProof) error {
	if retention.SupersededRows < 0 {
		return fmt.Errorf("retention superseded rows must not be negative")
	}
	if retention.OldestSupersededAge <= 0 {
		return fmt.Errorf("retention oldest superseded age must be positive")
	}
	if retention.RetentionLag < 0 {
		return fmt.Errorf("retention lag must not be negative")
	}
	if retention.PruneDuration <= 0 {
		return fmt.Errorf("retention prune duration must be positive")
	}
	if retention.PruneBatchRows <= 0 {
		return fmt.Errorf("retention prune batch rows must be positive")
	}
	if !retention.BoundedEvidence {
		return fmt.Errorf("retention evidence must be bounded")
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

func hostedGrowthRelation(
	relations []HostedGrowthRelationMeasurement,
	relation HostedGrowthRelation,
) (HostedGrowthRelationMeasurement, bool) {
	for _, candidate := range relations {
		if candidate.Relation == relation {
			return candidate, true
		}
	}
	return HostedGrowthRelationMeasurement{}, false
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
	if gate.RecommendedAction != "run_hosted_growth_postgres_proof" {
		return fmt.Errorf("operator gate recommended action must be run_hosted_growth_postgres_proof")
	}
	if gate.OperatorStatusSignal != "admin_status_relation_queue_summary" {
		return fmt.Errorf("operator gate status signal must be admin_status_relation_queue_summary")
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
