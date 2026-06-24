// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package storageeval

import (
	"fmt"
	"strings"
	"time"
)

// DurableStateClass names a durable state family covered by backup proof.
type DurableStateClass string

const (
	// DurableStateContentReadModel covers content rows and derived read models.
	DurableStateContentReadModel DurableStateClass = "content_read_model"
	// DurableStateFactFamily covers a bounded semantic fact family.
	DurableStateFactFamily DurableStateClass = "fact_family"
	// DurableStateRelationshipEvidence covers relationship provenance state.
	DurableStateRelationshipEvidence DurableStateClass = "relationship_evidence"
	// DurableStateSearchDocument covers curated search documents.
	DurableStateSearchDocument DurableStateClass = "search_document"
	// DurableStateGraphSchema covers graph schema application state.
	DurableStateGraphSchema DurableStateClass = "graph_schema_state"
)

// DurableStateOwner identifies the source of a durable state snapshot.
type DurableStateOwner string

const (
	// DurableStateOwnerPostgresBaseline is the current production baseline.
	DurableStateOwnerPostgresBaseline DurableStateOwner = "postgres_baseline"
	// DurableStateOwnerNornicDBRestored is the restored NornicDB candidate.
	DurableStateOwnerNornicDBRestored DurableStateOwner = "nornicdb_restored"
)

// BackupArtifactKind identifies the artifact shape used for restore proof.
type BackupArtifactKind string

const (
	// BackupArtifactNornicDBSnapshot is a NornicDB snapshot artifact.
	BackupArtifactNornicDBSnapshot BackupArtifactKind = "nornicdb_snapshot"
	// BackupArtifactExportBundle is an explicit export bundle.
	BackupArtifactExportBundle BackupArtifactKind = "export_bundle"
	// BackupArtifactObjectStore is an object-store artifact.
	BackupArtifactObjectStore BackupArtifactKind = "object_store_artifact"
)

// BackupArtifactStatus records whether a backup artifact is usable.
type BackupArtifactStatus string

const (
	// BackupArtifactAvailable means the artifact exists and passed integrity checks.
	BackupArtifactAvailable BackupArtifactStatus = "available"
	// BackupArtifactMissing means the artifact was absent.
	BackupArtifactMissing BackupArtifactStatus = "missing"
	// BackupArtifactCorrupt means artifact integrity checks failed.
	BackupArtifactCorrupt BackupArtifactStatus = "corrupt"
	// BackupArtifactVersionMismatch means the artifact cannot restore this schema.
	BackupArtifactVersionMismatch BackupArtifactStatus = "version_mismatch"
)

// BackupArtifact records backup identity without storing artifact payloads.
type BackupArtifact struct {
	ArtifactID    string               `json:"artifact_id"`
	Kind          BackupArtifactKind   `json:"kind"`
	Status        BackupArtifactStatus `json:"status"`
	Digest        string               `json:"digest"`
	SchemaVersion string               `json:"schema_version"`
	GenerationID  string               `json:"generation_id"`
	CreatedAt     time.Time            `json:"created_at"`
	SizeBytes     int64                `json:"size_bytes"`
}

// RestoreAttempt records the clean restore target and duration.
type RestoreAttempt struct {
	TargetID    string        `json:"target_id"`
	CleanTarget bool          `json:"clean_target"`
	StartedAt   time.Time     `json:"started_at"`
	Duration    time.Duration `json:"duration_ns"`
	Completed   bool          `json:"completed"`
}

// DurableStateSnapshot summarizes one durable-state side of restore parity.
type DurableStateSnapshot struct {
	Owner         DurableStateOwner `json:"owner"`
	StateClass    DurableStateClass `json:"state_class"`
	GenerationID  string            `json:"generation_id"`
	SchemaVersion string            `json:"schema_version"`
	Count         int               `json:"count"`
	Digest        string            `json:"digest"`
	ObservedAt    time.Time         `json:"observed_at"`
	Bounded       bool              `json:"bounded"`
}

// BackupRestoreConsistencyCheckKind names a required restore consistency lane.
type BackupRestoreConsistencyCheckKind string

const (
	// BackupRestoreCheckGraph verifies restored graph/schema consistency.
	BackupRestoreCheckGraph BackupRestoreConsistencyCheckKind = "graph"
	// BackupRestoreCheckContent verifies restored content parity.
	BackupRestoreCheckContent BackupRestoreConsistencyCheckKind = "content"
	// BackupRestoreCheckFact verifies restored fact-family parity.
	BackupRestoreCheckFact BackupRestoreConsistencyCheckKind = "fact"
	// BackupRestoreCheckReadModel verifies restored read-model parity.
	BackupRestoreCheckReadModel BackupRestoreConsistencyCheckKind = "read_model"
	// BackupRestoreCheckRelationshipEvidence verifies relationship evidence.
	BackupRestoreCheckRelationshipEvidence BackupRestoreConsistencyCheckKind = "relationship_evidence"
)

// BackupRestoreProofStatus records proof coverage for a scenario or check.
type BackupRestoreProofStatus string

const (
	// BackupRestoreProofPassed means executable evidence passed.
	BackupRestoreProofPassed BackupRestoreProofStatus = "passed"
	// BackupRestoreProofPlanned means an accepted proof plan covers the case.
	BackupRestoreProofPlanned BackupRestoreProofStatus = "planned"
	// BackupRestoreProofFailed means proof failed or is explicitly uncovered.
	BackupRestoreProofFailed BackupRestoreProofStatus = "failed"
)

// BackupRestoreConsistencyCheck records one restore consistency check.
type BackupRestoreConsistencyCheck struct {
	Check  BackupRestoreConsistencyCheckKind `json:"check"`
	Status BackupRestoreProofStatus          `json:"status"`
}

// BackupRestoreScenario names a required backup/restore failure scenario.
type BackupRestoreScenario string

const (
	// BackupRestoreScenarioMissingArtifact covers absent backup artifacts.
	BackupRestoreScenarioMissingArtifact BackupRestoreScenario = "missing_artifact"
	// BackupRestoreScenarioCorruptArtifact covers failed integrity checks.
	BackupRestoreScenarioCorruptArtifact BackupRestoreScenario = "corrupt_artifact"
	// BackupRestoreScenarioVersionMismatch covers incompatible schema versions.
	BackupRestoreScenarioVersionMismatch BackupRestoreScenario = "version_mismatch"
	// BackupRestoreScenarioPartialRestore covers incomplete restored state.
	BackupRestoreScenarioPartialRestore BackupRestoreScenario = "partial_restore"
	// BackupRestoreScenarioStaleGeneration covers stale restored generations.
	BackupRestoreScenarioStaleGeneration BackupRestoreScenario = "stale_generation"
	// BackupRestoreScenarioFallbackToPostgres covers baseline fallback behavior.
	BackupRestoreScenarioFallbackToPostgres BackupRestoreScenario = "fallback_to_postgres_baseline"
)

// BackupRestoreScenarioProof records coverage for one restore scenario.
type BackupRestoreScenarioProof struct {
	Scenario BackupRestoreScenario    `json:"scenario"`
	Status   BackupRestoreProofStatus `json:"status"`
	Evidence string                   `json:"evidence,omitempty"`
}

// BackupRestoreRollbackBehavior records how a failed restore is unwound.
type BackupRestoreRollbackBehavior string

const (
	// BackupRestoreRollbackDiscardRestoredCandidate discards restored candidate state.
	BackupRestoreRollbackDiscardRestoredCandidate BackupRestoreRollbackBehavior = "discard_restored_candidate"
	// BackupRestoreRollbackKeepPostgresBaseline keeps Postgres as durable truth.
	BackupRestoreRollbackKeepPostgresBaseline BackupRestoreRollbackBehavior = "keep_postgres_baseline"
	// BackupRestoreRollbackFailClosed blocks promotion when restore proof fails.
	BackupRestoreRollbackFailClosed BackupRestoreRollbackBehavior = "fail_closed"
)

// BackupRestoreObservability records required operator-facing restore signals.
type BackupRestoreObservability struct {
	BackupAge           bool `json:"backup_age"`
	ArtifactSize        bool `json:"artifact_size"`
	RestoreDuration     bool `json:"restore_duration"`
	RestoreFailureClass bool `json:"restore_failure_class"`
	ParityDrift         bool `json:"parity_drift"`
}

// BackupRestoreVerdict is the backup/restore proof outcome.
type BackupRestoreVerdict string

const (
	// BackupRestoreVerdictMatch means restored state matched the baseline.
	BackupRestoreVerdictMatch BackupRestoreVerdict = "match"
	// BackupRestoreVerdictMissingArtifact means the backup artifact was absent.
	BackupRestoreVerdictMissingArtifact BackupRestoreVerdict = "missing_artifact"
	// BackupRestoreVerdictCorruptArtifact means artifact integrity failed.
	BackupRestoreVerdictCorruptArtifact BackupRestoreVerdict = "corrupt_artifact"
	// BackupRestoreVerdictVersionMismatch means schema versions diverged.
	BackupRestoreVerdictVersionMismatch BackupRestoreVerdict = "version_mismatch"
	// BackupRestoreVerdictPartialRestore means restored state was incomplete.
	BackupRestoreVerdictPartialRestore BackupRestoreVerdict = "partial_restore"
	// BackupRestoreVerdictStaleGeneration means restored generation was stale.
	BackupRestoreVerdictStaleGeneration BackupRestoreVerdict = "stale_generation"
	// BackupRestoreVerdictParityDrift means counts or digests diverged.
	BackupRestoreVerdictParityDrift BackupRestoreVerdict = "parity_drift"
)

// BackupRestoreFailureClass identifies a failed backup/restore proof.
type BackupRestoreFailureClass string

const (
	// BackupRestoreFailureNone means no failure was observed.
	BackupRestoreFailureNone BackupRestoreFailureClass = "none"
	// BackupRestoreFailureMissingArtifact records absent artifacts.
	BackupRestoreFailureMissingArtifact BackupRestoreFailureClass = "missing_artifact"
	// BackupRestoreFailureCorruptArtifact records failed integrity checks.
	BackupRestoreFailureCorruptArtifact BackupRestoreFailureClass = "corrupt_artifact"
	// BackupRestoreFailureVersionMismatch records schema-version mismatch.
	BackupRestoreFailureVersionMismatch BackupRestoreFailureClass = "version_mismatch"
	// BackupRestoreFailurePartialRestore records incomplete restore output.
	BackupRestoreFailurePartialRestore BackupRestoreFailureClass = "partial_restore"
	// BackupRestoreFailureStaleGeneration records stale restored generation.
	BackupRestoreFailureStaleGeneration BackupRestoreFailureClass = "stale_generation"
	// BackupRestoreFailureParityDrift records count or digest parity drift.
	BackupRestoreFailureParityDrift BackupRestoreFailureClass = "parity_drift"
	// BackupRestoreFailureGraphOnlyProof records graph-only proof misuse.
	BackupRestoreFailureGraphOnlyProof BackupRestoreFailureClass = "graph_only_proof"
)

// BackupRestoreProof records one #1290 durable-state backup/restore proof gate.
type BackupRestoreProof struct {
	ProofID           string                          `json:"proof_id"`
	StateClass        DurableStateClass               `json:"state_class"`
	Scope             Scope                           `json:"scope"`
	NornicDBImage     string                          `json:"nornicdb_image"`
	NornicDBCommit    string                          `json:"nornicdb_commit"`
	EshuCommit        string                          `json:"eshu_commit"`
	Artifact          BackupArtifact                  `json:"artifact"`
	Restore           RestoreAttempt                  `json:"restore"`
	Baseline          DurableStateSnapshot            `json:"baseline"`
	Restored          DurableStateSnapshot            `json:"restored"`
	ConsistencyChecks []BackupRestoreConsistencyCheck `json:"consistency_checks"`
	Proofs            []BackupRestoreScenarioProof    `json:"proofs"`
	FallbackBehavior  FallbackBehavior                `json:"fallback_behavior"`
	RollbackBehavior  BackupRestoreRollbackBehavior   `json:"rollback_behavior"`
	Observability     BackupRestoreObservability      `json:"observability"`
	Verdict           BackupRestoreVerdict            `json:"verdict"`
	FailureClass      BackupRestoreFailureClass       `json:"failure_class"`
}

// ValidateBackupRestoreProof verifies one passing backup/restore proof record.
func ValidateBackupRestoreProof(proof BackupRestoreProof) error {
	if strings.TrimSpace(proof.ProofID) == "" {
		return fmt.Errorf("proof id is required")
	}
	if strings.TrimSpace(string(proof.StateClass)) == "" {
		return fmt.Errorf("state class is required")
	}
	if !supportedDurableStateClass(proof.StateClass) {
		return fmt.Errorf("unsupported state class %q", proof.StateClass)
	}
	if err := validateBackupRestoreScope(proof.Scope); err != nil {
		return err
	}
	if strings.TrimSpace(proof.NornicDBImage) == "" {
		return fmt.Errorf("nornicdb image is required")
	}
	if strings.TrimSpace(proof.NornicDBCommit) == "" {
		return fmt.Errorf("nornicdb commit is required")
	}
	if strings.TrimSpace(proof.EshuCommit) == "" {
		return fmt.Errorf("eshu commit is required")
	}
	if err := validateBackupArtifact(proof.Artifact); err != nil {
		return err
	}
	if err := validateRestoreAttempt(proof.Restore); err != nil {
		return err
	}
	if err := validateDurableStateSnapshot("baseline", proof.Baseline, DurableStateOwnerPostgresBaseline, proof.StateClass); err != nil {
		return err
	}
	if err := validateDurableStateSnapshot("restored", proof.Restored, DurableStateOwnerNornicDBRestored, proof.StateClass); err != nil {
		return err
	}
	if proof.Artifact.SchemaVersion != proof.Baseline.SchemaVersion {
		return fmt.Errorf("artifact schema_version must match baseline")
	}
	if proof.Artifact.GenerationID != proof.Baseline.GenerationID {
		return fmt.Errorf("artifact generation_id must match baseline")
	}
	if proof.Baseline.SchemaVersion != proof.Restored.SchemaVersion {
		return fmt.Errorf("schema versions must match")
	}
	if proof.Baseline.GenerationID != proof.Restored.GenerationID {
		return fmt.Errorf("restored generation_id must match baseline")
	}
	if proof.Baseline.Count != proof.Restored.Count {
		return fmt.Errorf("restored count must match baseline")
	}
	if proof.Baseline.Digest != proof.Restored.Digest {
		return fmt.Errorf("restored digest must match baseline")
	}
	if err := validateBackupRestoreConsistency(proof.StateClass, proof.ConsistencyChecks); err != nil {
		return err
	}
	if err := validateBackupRestoreProofCoverage(proof.Proofs); err != nil {
		return err
	}
	if err := validateFallbackBehavior(proof.FallbackBehavior); err != nil {
		return err
	}
	if proof.FallbackBehavior != FallbackKeepPostgres {
		return fmt.Errorf("fallback behavior must keep postgres baseline")
	}
	if err := validateBackupRestoreRollback(proof.RollbackBehavior); err != nil {
		return err
	}
	if err := requireBackupRestoreObservability(proof.Observability); err != nil {
		return err
	}
	if proof.Verdict == "" {
		return fmt.Errorf("verdict is required")
	}
	if proof.Verdict != BackupRestoreVerdictMatch {
		return fmt.Errorf("verdict must be match")
	}
	if proof.FailureClass == "" {
		return fmt.Errorf("failure class is required")
	}
	if proof.FailureClass != BackupRestoreFailureNone {
		return fmt.Errorf("failure class must be none for match verdict")
	}
	return nil
}
