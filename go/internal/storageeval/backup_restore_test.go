// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package storageeval

import (
	"strings"
	"testing"
	"time"
)

func TestValidateBackupRestoreProofAcceptsCoveredCleanRestore(t *testing.T) {
	proof := validBackupRestoreProof()

	if err := ValidateBackupRestoreProof(proof); err != nil {
		t.Fatalf("ValidateBackupRestoreProof() error = %v, want nil", err)
	}
}

func TestValidateBackupRestoreProofAcceptsGraphSchemaRestore(t *testing.T) {
	proof := validBackupRestoreProof()
	proof.StateClass = DurableStateGraphSchema
	proof.Baseline.StateClass = DurableStateGraphSchema
	proof.Restored.StateClass = DurableStateGraphSchema
	proof.ConsistencyChecks = []BackupRestoreConsistencyCheck{
		{Check: BackupRestoreCheckGraph, Status: BackupRestoreProofPassed},
	}

	if err := ValidateBackupRestoreProof(proof); err != nil {
		t.Fatalf("ValidateBackupRestoreProof() error = %v, want nil", err)
	}
}

func TestValidateBackupRestoreProofRejectsInvalidEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*BackupRestoreProof)
		want   string
	}{
		{
			name: "missing state class",
			mutate: func(proof *BackupRestoreProof) {
				proof.StateClass = ""
			},
			want: "state class is required",
		},
		{
			name: "missing nornicdb image",
			mutate: func(proof *BackupRestoreProof) {
				proof.NornicDBImage = ""
			},
			want: "nornicdb image is required",
		},
		{
			name: "missing nornicdb commit",
			mutate: func(proof *BackupRestoreProof) {
				proof.NornicDBCommit = ""
			},
			want: "nornicdb commit is required",
		},
		{
			name: "missing eshu commit",
			mutate: func(proof *BackupRestoreProof) {
				proof.EshuCommit = ""
			},
			want: "eshu commit is required",
		},
		{
			name: "missing artifact id",
			mutate: func(proof *BackupRestoreProof) {
				proof.Artifact.ArtifactID = ""
			},
			want: "artifact id is required",
		},
		{
			name: "missing artifact digest",
			mutate: func(proof *BackupRestoreProof) {
				proof.Artifact.Digest = ""
			},
			want: "artifact digest is required",
		},
		{
			name: "missing artifact size",
			mutate: func(proof *BackupRestoreProof) {
				proof.Artifact.SizeBytes = 0
			},
			want: "artifact size_bytes must be positive",
		},
		{
			name: "missing artifact",
			mutate: func(proof *BackupRestoreProof) {
				proof.Artifact.Status = BackupArtifactMissing
			},
			want: "artifact status must be available",
		},
		{
			name: "corrupt artifact",
			mutate: func(proof *BackupRestoreProof) {
				proof.Artifact.Status = BackupArtifactCorrupt
			},
			want: "artifact status must be available",
		},
		{
			name: "version mismatch",
			mutate: func(proof *BackupRestoreProof) {
				proof.Restored.SchemaVersion = "2.0.0"
			},
			want: "schema versions must match",
		},
		{
			name: "partial restore",
			mutate: func(proof *BackupRestoreProof) {
				proof.Restored.Count--
			},
			want: "restored count must match baseline",
		},
		{
			name: "stale generation",
			mutate: func(proof *BackupRestoreProof) {
				proof.Restored.GenerationID = "generation-older"
			},
			want: "restored generation_id must match baseline",
		},
		{
			name: "restore target not clean",
			mutate: func(proof *BackupRestoreProof) {
				proof.Restore.CleanTarget = false
			},
			want: "restore target must be clean",
		},
		{
			name: "missing restore duration",
			mutate: func(proof *BackupRestoreProof) {
				proof.Restore.Duration = 0
			},
			want: "restore duration must be positive",
		},
		{
			name: "unbounded restore",
			mutate: func(proof *BackupRestoreProof) {
				proof.Restored.Bounded = false
			},
			want: "restored state must be bounded",
		},
		{
			name: "digest drift",
			mutate: func(proof *BackupRestoreProof) {
				proof.Restored.Digest = "sha256:drift"
			},
			want: "restored digest must match baseline",
		},
		{
			name: "graph only proof for content",
			mutate: func(proof *BackupRestoreProof) {
				proof.ConsistencyChecks = []BackupRestoreConsistencyCheck{
					{Check: BackupRestoreCheckGraph, Status: BackupRestoreProofPassed},
				}
			},
			want: "graph-only restore proof is not sufficient",
		},
		{
			name: "missing content consistency",
			mutate: func(proof *BackupRestoreProof) {
				proof.ConsistencyChecks = []BackupRestoreConsistencyCheck{
					{Check: BackupRestoreCheckGraph, Status: BackupRestoreProofPassed},
					{Check: BackupRestoreCheckReadModel, Status: BackupRestoreProofPassed},
				}
			},
			want: "state class content_read_model requires content consistency check",
		},
		{
			name: "duplicate consistency check",
			mutate: func(proof *BackupRestoreProof) {
				proof.ConsistencyChecks = append(proof.ConsistencyChecks, proof.ConsistencyChecks[1])
			},
			want: "consistency check content is duplicated",
		},
		{
			name: "missing proof scenario",
			mutate: func(proof *BackupRestoreProof) {
				proof.Proofs = proof.Proofs[1:]
			},
			want: "proof scenario missing_artifact is required",
		},
		{
			name: "duplicate proof scenario",
			mutate: func(proof *BackupRestoreProof) {
				proof.Proofs = append(proof.Proofs, proof.Proofs[0])
			},
			want: "proof scenario missing_artifact is duplicated",
		},
		{
			name: "failed proof scenario",
			mutate: func(proof *BackupRestoreProof) {
				proof.Proofs[1].Status = BackupRestoreProofFailed
			},
			want: "proof scenario corrupt_artifact must be covered",
		},
		{
			name: "missing backup age observability",
			mutate: func(proof *BackupRestoreProof) {
				proof.Observability.BackupAge = false
			},
			want: "missing backup-age observability",
		},
		{
			name: "fallback does not keep postgres",
			mutate: func(proof *BackupRestoreProof) {
				proof.FallbackBehavior = FallbackFailClosed
			},
			want: "fallback behavior must keep postgres baseline",
		},
		{
			name: "missing rollback behavior",
			mutate: func(proof *BackupRestoreProof) {
				proof.RollbackBehavior = ""
			},
			want: "rollback behavior is required",
		},
		{
			name: "non-match verdict",
			mutate: func(proof *BackupRestoreProof) {
				proof.Verdict = BackupRestoreVerdictPartialRestore
				proof.FailureClass = BackupRestoreFailurePartialRestore
			},
			want: "verdict must be match",
		},
		{
			name: "missing failure class",
			mutate: func(proof *BackupRestoreProof) {
				proof.FailureClass = ""
			},
			want: "failure class is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			proof := validBackupRestoreProof()
			test.mutate(&proof)

			err := ValidateBackupRestoreProof(proof)
			if err == nil {
				t.Fatalf("ValidateBackupRestoreProof() error = nil, want %q", test.want)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateBackupRestoreProof() error = %q, want substring %q", err.Error(), test.want)
			}
		})
	}
}

func validBackupRestoreProof() BackupRestoreProof {
	observedAt := time.Date(2026, 6, 2, 14, 0, 0, 0, time.UTC)
	baseline := DurableStateSnapshot{
		Owner:         DurableStateOwnerPostgresBaseline,
		StateClass:    DurableStateContentReadModel,
		GenerationID:  "generation-content-1",
		SchemaVersion: "1.0.0",
		Count:         42,
		Digest:        "sha256:content-state-match",
		ObservedAt:    observedAt,
		Bounded:       true,
	}
	restored := baseline
	restored.Owner = DurableStateOwnerNornicDBRestored
	restored.ObservedAt = observedAt.Add(2 * time.Minute)

	return BackupRestoreProof{
		ProofID:        "backup-restore-proof-1290",
		StateClass:     DurableStateContentReadModel,
		Scope:          Scope{Kind: ScopeRepository, ID: "repo-123"},
		NornicDBImage:  "nornicdb-main-eshu:cb20824-arm64",
		NornicDBCommit: "cb20824",
		EshuCommit:     "eshu-commit-under-test",
		Artifact: BackupArtifact{
			ArtifactID:    "artifact-content-1",
			Kind:          BackupArtifactNornicDBSnapshot,
			Status:        BackupArtifactAvailable,
			Digest:        "sha256:backup-artifact",
			SchemaVersion: "1.0.0",
			GenerationID:  "generation-content-1",
			CreatedAt:     observedAt,
			SizeBytes:     1048576,
		},
		Restore: RestoreAttempt{
			TargetID:    "restore-target-clean-1",
			CleanTarget: true,
			StartedAt:   observedAt.Add(time.Minute),
			Duration:    45 * time.Second,
			Completed:   true,
		},
		Baseline: baseline,
		Restored: restored,
		ConsistencyChecks: []BackupRestoreConsistencyCheck{
			{Check: BackupRestoreCheckGraph, Status: BackupRestoreProofPassed},
			{Check: BackupRestoreCheckContent, Status: BackupRestoreProofPassed},
			{Check: BackupRestoreCheckReadModel, Status: BackupRestoreProofPassed},
		},
		Proofs:           requiredBackupRestoreProofs(BackupRestoreProofPlanned),
		FallbackBehavior: FallbackKeepPostgres,
		RollbackBehavior: BackupRestoreRollbackKeepPostgresBaseline,
		Observability: BackupRestoreObservability{
			BackupAge:           true,
			ArtifactSize:        true,
			RestoreDuration:     true,
			RestoreFailureClass: true,
			ParityDrift:         true,
		},
		Verdict:      BackupRestoreVerdictMatch,
		FailureClass: BackupRestoreFailureNone,
	}
}
