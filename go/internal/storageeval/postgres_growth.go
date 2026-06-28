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

// HostedGrowthFactGrowth records fact_records growth by family.
type HostedGrowthFactGrowth struct {
	ModelVersion  string                         `json:"model_version"`
	RowsPerSecond float64                        `json:"rows_per_second"`
	Before        HostedGrowthFactTotals         `json:"before"`
	After         HostedGrowthFactTotals         `json:"after"`
	Families      []HostedGrowthFactFamilyGrowth `json:"families"`
}

// HostedGrowthFactTotals records fact_records aggregate size at one point.
type HostedGrowthFactTotals struct {
	FactRecordsRows int64     `json:"fact_records_rows"`
	IndexBytes      int64     `json:"index_bytes"`
	TotalBytes      int64     `json:"total_bytes"`
	ObservedAt      time.Time `json:"observed_at"`
	BoundedEvidence bool      `json:"bounded_evidence"`
}

// HostedGrowthFactFamily names the fact families that can drive table growth.
type HostedGrowthFactFamily string

const (
	// HostedGrowthFactFamilyCollector covers hosted and source collectors.
	HostedGrowthFactFamilyCollector HostedGrowthFactFamily = "collector"
	// HostedGrowthFactFamilyParser covers parser-emitted source facts.
	HostedGrowthFactFamilyParser HostedGrowthFactFamily = "parser"
	// HostedGrowthFactFamilySearchDocuments covers search-document facts.
	HostedGrowthFactFamilySearchDocuments HostedGrowthFactFamily = "search_documents"
	// HostedGrowthFactFamilyCorrelation covers reducer correlation facts.
	HostedGrowthFactFamilyCorrelation HostedGrowthFactFamily = "correlation"
)

// HostedGrowthFactFamilyGrowth records one family growth sample.
type HostedGrowthFactFamilyGrowth struct {
	Family                  HostedGrowthFactFamily `json:"family"`
	FactKindCount           int                    `json:"fact_kind_count"`
	BeforeRows              int64                  `json:"before_rows"`
	AfterRows               int64                  `json:"after_rows"`
	AfterIndexBytes         int64                  `json:"after_index_bytes"`
	WriteAmplificationRatio float64                `json:"write_amplification_ratio"`
	P95Insert               time.Duration          `json:"p95_insert_ns"`
	BoundedEvidence         bool                   `json:"bounded_evidence"`
}

// HostedGrowthIndexBloat records table and index bloat evidence.
type HostedGrowthIndexBloat struct {
	TableBloatRatio float64                        `json:"table_bloat_ratio"`
	DeadTupleBytes  int64                          `json:"dead_tuple_bytes"`
	Indexes         []HostedGrowthIndexBloatSample `json:"indexes"`
}

// HostedGrowthIndexClass names bounded index sample groups.
type HostedGrowthIndexClass string

const (
	// HostedGrowthIndexClassActiveGeneration covers active-generation lookup indexes.
	HostedGrowthIndexClassActiveGeneration HostedGrowthIndexClass = "active_generation"
	// HostedGrowthIndexClassCorrelationLookup covers correlation lookup indexes.
	HostedGrowthIndexClassCorrelationLookup HostedGrowthIndexClass = "correlation_lookup"
)

// HostedGrowthIndexBloatSample records one bounded index-size sample.
type HostedGrowthIndexBloatSample struct {
	IndexClass              HostedGrowthIndexClass `json:"index_class"`
	SizeBytes               int64                  `json:"size_bytes"`
	BloatRatio              float64                `json:"bloat_ratio"`
	WriteAmplificationRatio float64                `json:"write_amplification_ratio"`
	BoundedEvidence         bool                   `json:"bounded_evidence"`
}

// HostedGrowthGraphWritePressure records reducer graph-write pressure.
type HostedGrowthGraphWritePressure struct {
	WriteP95                      time.Duration `json:"write_p95_ns"`
	TimeoutRetries                int64         `json:"timeout_retries"`
	RetryingGraphWriteTimeoutRows int64         `json:"retrying_graph_write_timeout_rows"`
	DeadLetterRows                int64         `json:"dead_letter_rows"`
	P95GroupRows                  int64         `json:"p95_group_rows"`
	ObservedAt                    time.Time     `json:"observed_at"`
	BoundedEvidence               bool          `json:"bounded_evidence"`
}

// HostedGrowthQueryClass names hot fact-read query classes.
type HostedGrowthQueryClass string

const (
	// HostedGrowthQueryClassActiveGenerationRead covers active generation reads.
	HostedGrowthQueryClassActiveGenerationRead HostedGrowthQueryClass = "active_generation_read"
	// HostedGrowthQueryClassCorrelationJoin covers fact-backed correlation joins.
	HostedGrowthQueryClassCorrelationJoin HostedGrowthQueryClass = "correlation_join"
	// HostedGrowthQueryClassRetentionChangedSince covers retained-window changed-since reads.
	HostedGrowthQueryClassRetentionChangedSince HostedGrowthQueryClass = "retention_changed_since"
	// HostedGrowthQueryClassHotAPIRead covers hot API fact reads.
	HostedGrowthQueryClassHotAPIRead HostedGrowthQueryClass = "hot_api_read"
)

// HostedGrowthQueryPlanStatus names the accepted plan status.
type HostedGrowthQueryPlanStatus string

const (
	// HostedGrowthQueryPlanIndexed means the plan uses bounded indexes.
	HostedGrowthQueryPlanIndexed HostedGrowthQueryPlanStatus = "indexed"
)

// HostedGrowthQueryPlan records one bounded hot query plan.
type HostedGrowthQueryPlan struct {
	QueryClass      HostedGrowthQueryClass      `json:"query_class"`
	P95             time.Duration               `json:"p95_ns"`
	RowsExamined    int64                       `json:"rows_examined"`
	PlanStatus      HostedGrowthQueryPlanStatus `json:"plan_status"`
	SeqScan         bool                        `json:"seq_scan"`
	Spill           bool                        `json:"spill"`
	ObservedAt      time.Time                   `json:"observed_at"`
	BoundedEvidence bool                        `json:"bounded_evidence"`
}

// HostedGrowthRetentionProof records retention lag and prune cost.
type HostedGrowthRetentionProof struct {
	SupersededRows      int64         `json:"superseded_rows"`
	OldestSupersededAge time.Duration `json:"oldest_superseded_age_ns"`
	RetentionLag        time.Duration `json:"retention_lag_ns"`
	PruneDuration       time.Duration `json:"prune_duration_ns"`
	PruneBatchRows      int64         `json:"prune_batch_rows"`
	ArchiveRequired     bool          `json:"archive_required"`
	BoundedEvidence     bool          `json:"bounded_evidence"`
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

// HostedGrowthRecommendation records the measured storage decision.
type HostedGrowthRecommendation string

const (
	// HostedGrowthRecommendationPartition chooses native partitioning.
	HostedGrowthRecommendationPartition HostedGrowthRecommendation = "partition"
	// HostedGrowthRecommendationArchive chooses archive tables.
	HostedGrowthRecommendationArchive HostedGrowthRecommendation = "archive"
	// HostedGrowthRecommendationSplit chooses fact-family splits.
	HostedGrowthRecommendationSplit HostedGrowthRecommendation = "split"
	// HostedGrowthRecommendationRetentionTune chooses retention-policy tuning.
	HostedGrowthRecommendationRetentionTune HostedGrowthRecommendation = "retention_tune"
	// HostedGrowthRecommendationDefer records no physical schema change yet.
	HostedGrowthRecommendationDefer HostedGrowthRecommendation = "defer"
)

// HostedGrowthImplication records low-cardinality decision implications.
type HostedGrowthImplication string

const (
	// HostedGrowthImplicationNone means no change to that axis.
	HostedGrowthImplicationNone HostedGrowthImplication = "none"
	// HostedGrowthImplicationKeepCurrentPostgres keeps current Postgres state.
	HostedGrowthImplicationKeepCurrentPostgres HostedGrowthImplication = "keep_current_postgres"
	// HostedGrowthImplicationTunePolicy changes retention policy only.
	HostedGrowthImplicationTunePolicy HostedGrowthImplication = "tune_policy"
	// HostedGrowthImplicationUnchanged means the implication is unchanged.
	HostedGrowthImplicationUnchanged HostedGrowthImplication = "unchanged"
	// HostedGrowthImplicationMigrationWindow requires a migration window.
	HostedGrowthImplicationMigrationWindow HostedGrowthImplication = "migration_window_required"
)

// HostedGrowthDecision records the measured storage decision.
type HostedGrowthDecision struct {
	Recommendation              HostedGrowthRecommendation `json:"recommendation"`
	SchemaChangeRequired        bool                       `json:"schema_change_required"`
	RationaleClass              string                     `json:"rationale_class"`
	LinkedIssues                []int                      `json:"linked_issues"`
	MigrationImplications       HostedGrowthImplication    `json:"migration_implications"`
	RollbackImplications        HostedGrowthImplication    `json:"rollback_implications"`
	RetentionImplications       HostedGrowthImplication    `json:"retention_implications"`
	TenantIsolationImplications HostedGrowthImplication    `json:"tenant_isolation_implications"`
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
	ProofID            string                            `json:"proof_id"`
	EshuCommit         string                            `json:"eshu_commit"`
	Profile            HostedGrowthProfile               `json:"profile"`
	Relations          []HostedGrowthRelationMeasurement `json:"relations"`
	QueueDrain         HostedGrowthQueueDrainMeasurement `json:"queue_drain"`
	FactGrowth         HostedGrowthFactGrowth            `json:"fact_growth"`
	IndexBloat         HostedGrowthIndexBloat            `json:"index_bloat"`
	GraphWritePressure HostedGrowthGraphWritePressure    `json:"graph_write_pressure"`
	QueryPlans         []HostedGrowthQueryPlan           `json:"query_plans"`
	Retention          HostedGrowthRetentionProof        `json:"retention"`
	Migration          HostedGrowthMigrationProof        `json:"migration"`
	Gate               HostedGrowthOperatorGate          `json:"gate"`
	Observability      HostedGrowthObservability         `json:"observability"`
	Decision           HostedGrowthDecision              `json:"decision"`
	Verdict            HostedGrowthVerdict               `json:"verdict"`
	FailureClass       HostedGrowthFailureClass          `json:"failure_class"`
}

// ValidateHostedGrowthPostgresProof verifies one passing hosted-growth proof.
func ValidateHostedGrowthPostgresProof(proof HostedGrowthPostgresProof) error {
	if !validHostedGrowthProofID(proof.ProofID) {
		return fmt.Errorf("proof id must be a public hosted-growth proof token")
	}
	if !validHostedGrowthCommit(proof.EshuCommit) {
		return fmt.Errorf("eshu commit must be a git SHA")
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
	if err := validateHostedGrowthFactGrowth(proof.FactGrowth, proof.Relations); err != nil {
		return err
	}
	if err := validateHostedGrowthIndexBloat(proof.IndexBloat); err != nil {
		return err
	}
	if err := validateHostedGrowthGraphWritePressure(proof.GraphWritePressure); err != nil {
		return err
	}
	if err := validateHostedGrowthQueryPlans(proof.QueryPlans); err != nil {
		return err
	}
	if err := validateHostedGrowthRetention(proof.Retention); err != nil {
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
	if err := validateHostedGrowthDecision(proof); err != nil {
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

func validHostedGrowthProofID(proofID string) bool {
	if proofID == "hosted-growth-postgres-proof-test" {
		return true
	}
	const prefix = "hosted-growth-postgres-proof-"
	if !strings.HasPrefix(proofID, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(proofID, prefix)
	if len(suffix) != len("20260617T030000Z") || suffix[8] != 'T' || suffix[15] != 'Z' {
		return false
	}
	for i, r := range suffix {
		if i == 8 || i == 15 {
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func validHostedGrowthCommit(commit string) bool {
	if len(commit) < 7 || len(commit) > 40 {
		return false
	}
	for _, r := range commit {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
