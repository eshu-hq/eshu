// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package storageeval

import (
	"fmt"
	"strings"
	"time"
)

// HostedGrowthProfile names the storage envelope a proof applies to.
type HostedGrowthProfile string

const (
	// HostedGrowthProfileLocalDev covers local and developer-only storage.
	HostedGrowthProfileLocalDev HostedGrowthProfile = "local_dev"
	// HostedGrowthProfileHostedSmall covers hosted installs below the growth gate.
	HostedGrowthProfileHostedSmall HostedGrowthProfile = "hosted_small"
	// HostedGrowthProfileHostedGrowth covers hosted installs above the growth gate.
	HostedGrowthProfileHostedGrowth HostedGrowthProfile = "hosted_growth"
)

// HostedGrowthRelation names a Postgres table covered by growth proof.
type HostedGrowthRelation string

const (
	// HostedGrowthRelationFactRecords covers the durable fact ledger.
	HostedGrowthRelationFactRecords HostedGrowthRelation = "fact_records"
	// HostedGrowthRelationFactWorkItems covers reducer queue work items.
	HostedGrowthRelationFactWorkItems HostedGrowthRelation = "fact_work_items"
	// HostedGrowthRelationSharedProjectionIntents covers shared projection intent rows.
	HostedGrowthRelationSharedProjectionIntents HostedGrowthRelation = "shared_projection_intents"
	// HostedGrowthRelationSharedProjectionAcceptance covers shared projection acceptance rows.
	HostedGrowthRelationSharedProjectionAcceptance HostedGrowthRelation = "shared_projection_acceptance"
)

// HostedGrowthRelationMeasurement records bounded size and latency evidence.
type HostedGrowthRelationMeasurement struct {
	Relation        HostedGrowthRelation `json:"relation"`
	RowCount        int64                `json:"row_count"`
	IndexBytes      int64                `json:"index_bytes"`
	TotalBytes      int64                `json:"total_bytes"`
	ReadP95         time.Duration        `json:"read_p95_ns"`
	WriteP95        time.Duration        `json:"write_p95_ns"`
	ObservedAt      time.Time            `json:"observed_at"`
	BoundedEvidence bool                 `json:"bounded_evidence"`
}

// HostedGrowthQueueDrainMeasurement records reducer queue drain evidence.
type HostedGrowthQueueDrainMeasurement struct {
	QueueSurface    QueueSurface  `json:"queue_surface"`
	PendingRows     int64         `json:"pending_rows"`
	RetryRows       int64         `json:"retry_rows"`
	DeadLetterRows  int64         `json:"dead_letter_rows"`
	StaleRows       int64         `json:"stale_rows"`
	ClaimedRows     int64         `json:"claimed_rows"`
	CompletedRows   int64         `json:"completed_rows"`
	FailedRows      int64         `json:"failed_rows"`
	OldestAge       time.Duration `json:"oldest_age_ns"`
	DrainDuration   time.Duration `json:"drain_duration_ns"`
	WorkerCount     int           `json:"worker_count"`
	ObservedAt      time.Time     `json:"observed_at"`
	BoundedEvidence bool          `json:"bounded_evidence"`
}

// HostedGrowthScenario names a required migration or safety proof lane.
type HostedGrowthScenario string

const (
	// HostedGrowthScenarioEmptyTable covers bootstrap on empty hosted tables.
	HostedGrowthScenarioEmptyTable HostedGrowthScenario = "empty_table"
	// HostedGrowthScenarioLargeTable covers migration at hosted-growth row counts.
	HostedGrowthScenarioLargeTable HostedGrowthScenario = "large_table"
	// HostedGrowthScenarioOldGeneration covers superseded generation retention.
	HostedGrowthScenarioOldGeneration HostedGrowthScenario = "old_generation"
	// HostedGrowthScenarioStaleRows covers stale queue and status rows.
	HostedGrowthScenarioStaleRows HostedGrowthScenario = "stale_rows"
	// HostedGrowthScenarioActiveClaim covers claimed work during migration.
	HostedGrowthScenarioActiveClaim HostedGrowthScenario = "active_claim"
	// HostedGrowthScenarioRetryDeadLetter covers retrying and dead-letter rows.
	HostedGrowthScenarioRetryDeadLetter HostedGrowthScenario = "retry_dead_letter"
	// HostedGrowthScenarioRollback covers rollback after a failed migration.
	HostedGrowthScenarioRollback HostedGrowthScenario = "rollback"
)

// HostedGrowthScenarioStatus records whether a proof lane passed.
type HostedGrowthScenarioStatus string

const (
	// HostedGrowthScenarioPassed means executable evidence covered the lane.
	HostedGrowthScenarioPassed HostedGrowthScenarioStatus = "passed"
	// HostedGrowthScenarioPlanned means the lane is planned but not yet proof.
	HostedGrowthScenarioPlanned HostedGrowthScenarioStatus = "planned"
	// HostedGrowthScenarioFailed means the lane failed or is uncovered.
	HostedGrowthScenarioFailed HostedGrowthScenarioStatus = "failed"
)

// HostedGrowthScenarioProof records one required hosted-growth proof lane.
type HostedGrowthScenarioProof struct {
	Scenario HostedGrowthScenario       `json:"scenario"`
	Status   HostedGrowthScenarioStatus `json:"status"`
	Evidence string                     `json:"evidence,omitempty"`
}

// HostedGrowthRollbackBehavior records how a failed migration is unwound.
type HostedGrowthRollbackBehavior string

const (
	// HostedGrowthRollbackKeepCurrentPostgres leaves current Postgres state authoritative.
	HostedGrowthRollbackKeepCurrentPostgres HostedGrowthRollbackBehavior = "keep_current_postgres"
	// HostedGrowthRollbackDiscardCandidate drops the migrated candidate state.
	HostedGrowthRollbackDiscardCandidate HostedGrowthRollbackBehavior = "discard_candidate"
	// HostedGrowthRollbackFailClosed blocks hosted-growth promotion.
	HostedGrowthRollbackFailClosed HostedGrowthRollbackBehavior = "fail_closed"
)

// HostedGrowthMigrationProof records migration, rollback, and correctness proof.
type HostedGrowthMigrationProof struct {
	Strategy                              string                       `json:"strategy"`
	NativePartitioning                    bool                         `json:"native_partitioning"`
	PrimaryKeyIncludesPartitionKey        bool                         `json:"primary_key_includes_partition_key"`
	UniqueConstraintsIncludePartitionKey  bool                         `json:"unique_constraints_include_partition_key"`
	ActiveGenerationReadCorrect           bool                         `json:"active_generation_read_correct"`
	ChangedSinceRetainedWindowCorrect     bool                         `json:"changed_since_retained_window_correct"`
	DeletesActiveWork                     bool                         `json:"deletes_active_work"`
	RetriesActiveWork                     bool                         `json:"retries_active_work"`
	RollbackBehavior                      HostedGrowthRollbackBehavior `json:"rollback_behavior"`
	Scenarios                             []HostedGrowthScenarioProof  `json:"scenarios"`
	PostMigrationReadP95                  time.Duration                `json:"post_migration_read_p95_ns"`
	PostMigrationWriteP95                 time.Duration                `json:"post_migration_write_p95_ns"`
	PostMigrationQueueClaimP95            time.Duration                `json:"post_migration_queue_claim_p95_ns"`
	PostMigrationQueueDrainDuration       time.Duration                `json:"post_migration_queue_drain_duration_ns"`
	PostMigrationActiveGenerationRows     int64                        `json:"post_migration_active_generation_rows"`
	PostMigrationChangedSinceRetainedRows int64                        `json:"post_migration_changed_since_retained_rows"`
	PostMigrationActiveClaimRowsPreserved int64                        `json:"post_migration_active_claim_rows_preserved"`
	PostMigrationRetryRowsPreserved       int64                        `json:"post_migration_retry_rows_preserved"`
	PostMigrationDeadLetterRowsPreserved  int64                        `json:"post_migration_dead_letter_rows_preserved"`
	PostMigrationStaleRowsClassified      int64                        `json:"post_migration_stale_rows_classified"`
}

// HostedGrowthOperatorGate records when hosted-small must move to hosted-growth.
type HostedGrowthOperatorGate struct {
	FromProfile              HostedGrowthProfile `json:"from_profile"`
	ToProfile                HostedGrowthProfile `json:"to_profile"`
	FactRowsThreshold        int64               `json:"fact_rows_threshold"`
	QueueRowsThreshold       int64               `json:"queue_rows_threshold"`
	IndexBytesThreshold      int64               `json:"index_bytes_threshold"`
	OldestQueueAgeThreshold  time.Duration       `json:"oldest_queue_age_threshold_ns"`
	RecommendedAction        string              `json:"recommended_action"`
	OperatorStatusSignal     string              `json:"operator_status_signal"`
	RequiresMigrationWindow  bool                `json:"requires_migration_window"`
	RequiresRollbackArtifact bool                `json:"requires_rollback_artifact"`
}

// HostedGrowthObservability records operator-facing growth and migration signals.
type HostedGrowthObservability struct {
	RelationSize      bool `json:"relation_size"`
	IndexSize         bool `json:"index_size"`
	ReadLatency       bool `json:"read_latency"`
	WriteLatency      bool `json:"write_latency"`
	QueueDepth        bool `json:"queue_depth"`
	OldestAge         bool `json:"oldest_age"`
	RetryCount        bool `json:"retry_count"`
	DeadLetters       bool `json:"dead_letters"`
	StaleRows         bool `json:"stale_rows"`
	ActiveClaims      bool `json:"active_claims"`
	MigrationDuration bool `json:"migration_duration"`
	RollbackStatus    bool `json:"rollback_status"`
}

// HostedGrowthVerdict records the final hosted-growth proof result.
type HostedGrowthVerdict string

const (
	// HostedGrowthVerdictPass means the proof can gate hosted-growth promotion.
	HostedGrowthVerdictPass HostedGrowthVerdict = "pass"
	// HostedGrowthVerdictInsufficientEvidence means required proof is missing.
	HostedGrowthVerdictInsufficientEvidence HostedGrowthVerdict = "insufficient_evidence"
)

// HostedGrowthFailureClass identifies why a hosted-growth proof failed.
type HostedGrowthFailureClass string

const (
	// HostedGrowthFailureNone means no failure was observed.
	HostedGrowthFailureNone HostedGrowthFailureClass = "none"
	// HostedGrowthFailureMissingMeasurement records missing row or latency evidence.
	HostedGrowthFailureMissingMeasurement HostedGrowthFailureClass = "missing_measurement"
	// HostedGrowthFailureMissingMigrationProof records missing migration proof.
	HostedGrowthFailureMissingMigrationProof HostedGrowthFailureClass = "missing_migration_proof"
	// HostedGrowthFailureUnsafeMigration records active work or partition safety risk.
	HostedGrowthFailureUnsafeMigration HostedGrowthFailureClass = "unsafe_migration"
)

// HostedGrowthPostgresProof records the #2749 hosted-growth Postgres gate.
type HostedGrowthPostgresProof struct {
	ProofID       string                            `json:"proof_id"`
	EshuCommit    string                            `json:"eshu_commit"`
	Profile       HostedGrowthProfile               `json:"profile"`
	Relations     []HostedGrowthRelationMeasurement `json:"relations"`
	QueueDrain    HostedGrowthQueueDrainMeasurement `json:"queue_drain"`
	Migration     HostedGrowthMigrationProof        `json:"migration"`
	Gate          HostedGrowthOperatorGate          `json:"gate"`
	Observability HostedGrowthObservability         `json:"observability"`
	Verdict       HostedGrowthVerdict               `json:"verdict"`
	FailureClass  HostedGrowthFailureClass          `json:"failure_class"`
}

// ValidateHostedGrowthPostgresProof verifies one passing hosted-growth proof.
func ValidateHostedGrowthPostgresProof(proof HostedGrowthPostgresProof) error {
	if strings.TrimSpace(proof.ProofID) == "" {
		return fmt.Errorf("proof id is required")
	}
	if strings.TrimSpace(proof.EshuCommit) == "" {
		return fmt.Errorf("eshu commit is required")
	}
	if proof.Profile != HostedGrowthProfileHostedGrowth {
		return fmt.Errorf("profile must be hosted_growth")
	}
	if err := validateHostedGrowthRelations(proof.Relations); err != nil {
		return err
	}
	if err := validateHostedGrowthQueueDrain(proof.QueueDrain); err != nil {
		return err
	}
	if err := validateHostedGrowthMigration(proof.Migration); err != nil {
		return err
	}
	if err := validateHostedGrowthOperatorGate(proof.Gate); err != nil {
		return err
	}
	if err := requireHostedGrowthObservability(proof.Observability); err != nil {
		return err
	}
	if proof.Verdict == "" {
		return fmt.Errorf("verdict is required")
	}
	if proof.Verdict != HostedGrowthVerdictPass {
		return fmt.Errorf("verdict must be pass")
	}
	if proof.FailureClass == "" {
		return fmt.Errorf("failure class is required")
	}
	if proof.FailureClass != HostedGrowthFailureNone {
		return fmt.Errorf("failure class must be none for pass verdict")
	}
	return nil
}
