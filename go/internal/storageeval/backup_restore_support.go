// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package storageeval

import (
	"fmt"
	"strings"
)

func validateBackupArtifact(artifact BackupArtifact) error {
	if strings.TrimSpace(artifact.ArtifactID) == "" {
		return fmt.Errorf("artifact id is required")
	}
	if !supportedBackupArtifactKind(artifact.Kind) {
		return fmt.Errorf("artifact kind %q is unsupported", artifact.Kind)
	}
	if artifact.Status != BackupArtifactAvailable {
		return fmt.Errorf("artifact status must be available")
	}
	if strings.TrimSpace(artifact.Digest) == "" {
		return fmt.Errorf("artifact digest is required")
	}
	if strings.TrimSpace(artifact.SchemaVersion) == "" {
		return fmt.Errorf("artifact schema_version is required")
	}
	if !factSchemaVersionPattern.MatchString(artifact.SchemaVersion) {
		return fmt.Errorf("artifact schema_version must be semantic version")
	}
	if strings.TrimSpace(artifact.GenerationID) == "" {
		return fmt.Errorf("artifact generation_id is required")
	}
	if artifact.CreatedAt.IsZero() {
		return fmt.Errorf("artifact created_at is required")
	}
	if artifact.SizeBytes <= 0 {
		return fmt.Errorf("artifact size_bytes must be positive")
	}
	return nil
}

func validateRestoreAttempt(restore RestoreAttempt) error {
	if strings.TrimSpace(restore.TargetID) == "" {
		return fmt.Errorf("restore target id is required")
	}
	if !restore.CleanTarget {
		return fmt.Errorf("restore target must be clean")
	}
	if restore.StartedAt.IsZero() {
		return fmt.Errorf("restore started_at is required")
	}
	if restore.Duration <= 0 {
		return fmt.Errorf("restore duration must be positive")
	}
	if !restore.Completed {
		return fmt.Errorf("restore must be completed")
	}
	return nil
}

func validateDurableStateSnapshot(
	role string,
	snapshot DurableStateSnapshot,
	owner DurableStateOwner,
	stateClass DurableStateClass,
) error {
	if snapshot.Owner != owner {
		return fmt.Errorf("%s owner must be %q", role, owner)
	}
	if snapshot.StateClass != stateClass {
		return fmt.Errorf("%s state_class must match proof state_class", role)
	}
	if strings.TrimSpace(snapshot.GenerationID) == "" {
		return fmt.Errorf("%s generation_id is required", role)
	}
	if strings.TrimSpace(snapshot.SchemaVersion) == "" {
		return fmt.Errorf("%s schema_version is required", role)
	}
	if !factSchemaVersionPattern.MatchString(snapshot.SchemaVersion) {
		return fmt.Errorf("%s schema_version must be semantic version", role)
	}
	if snapshot.Count < 0 {
		return fmt.Errorf("%s count must not be negative", role)
	}
	if strings.TrimSpace(snapshot.Digest) == "" {
		return fmt.Errorf("%s digest is required", role)
	}
	if snapshot.ObservedAt.IsZero() {
		return fmt.Errorf("%s observed_at is required", role)
	}
	if !snapshot.Bounded {
		return fmt.Errorf("%s state must be bounded", role)
	}
	return nil
}

func validateBackupRestoreConsistency(
	stateClass DurableStateClass,
	checks []BackupRestoreConsistencyCheck,
) error {
	if len(checks) == 0 {
		return fmt.Errorf("consistency checks are required")
	}
	seen := make(map[BackupRestoreConsistencyCheckKind]BackupRestoreProofStatus, len(checks))
	for _, check := range checks {
		if !supportedBackupRestoreCheck(check.Check) {
			return fmt.Errorf("consistency check %q is unsupported", check.Check)
		}
		if !supportedBackupRestoreProofStatus(check.Status) {
			return fmt.Errorf("consistency check %s status %q is unsupported", check.Check, check.Status)
		}
		if _, ok := seen[check.Check]; ok {
			return fmt.Errorf("consistency check %s is duplicated", check.Check)
		}
		seen[check.Check] = check.Status
	}
	if len(seen) == 1 {
		if _, ok := seen[BackupRestoreCheckGraph]; ok && stateClass != DurableStateGraphSchema {
			return fmt.Errorf("graph-only restore proof is not sufficient for %s", stateClass)
		}
	}
	for _, check := range requiredBackupRestoreChecks(stateClass) {
		status, ok := seen[check]
		if !ok {
			return fmt.Errorf("state class %s requires %s consistency check", stateClass, check)
		}
		if status != BackupRestoreProofPassed && status != BackupRestoreProofPlanned {
			return fmt.Errorf("consistency check %s must be covered", check)
		}
	}
	return nil
}

func validateBackupRestoreProofCoverage(proofs []BackupRestoreScenarioProof) error {
	if len(proofs) == 0 {
		return fmt.Errorf("proof scenarios are required")
	}
	seen := make(map[BackupRestoreScenario]BackupRestoreProofStatus, len(proofs))
	for _, proof := range proofs {
		if !supportedBackupRestoreScenario(proof.Scenario) {
			return fmt.Errorf("proof scenario %q is unsupported", proof.Scenario)
		}
		if !supportedBackupRestoreProofStatus(proof.Status) {
			return fmt.Errorf("proof scenario %s status %q is unsupported", proof.Scenario, proof.Status)
		}
		if _, ok := seen[proof.Scenario]; ok {
			return fmt.Errorf("proof scenario %s is duplicated", proof.Scenario)
		}
		seen[proof.Scenario] = proof.Status
	}
	for _, scenario := range requiredBackupRestoreScenarios() {
		status, ok := seen[scenario]
		if !ok {
			return fmt.Errorf("proof scenario %s is required", scenario)
		}
		if status != BackupRestoreProofPassed && status != BackupRestoreProofPlanned {
			return fmt.Errorf("proof scenario %s must be covered", scenario)
		}
	}
	return nil
}

func validateBackupRestoreRollback(rollback BackupRestoreRollbackBehavior) error {
	switch rollback {
	case "":
		return fmt.Errorf("rollback behavior is required")
	case BackupRestoreRollbackDiscardRestoredCandidate, BackupRestoreRollbackKeepPostgresBaseline,
		BackupRestoreRollbackFailClosed:
		return nil
	default:
		return fmt.Errorf("rollback behavior %q is unsupported", rollback)
	}
}

func requireBackupRestoreObservability(observability BackupRestoreObservability) error {
	checks := []struct {
		label string
		ok    bool
	}{
		{"backup-age", observability.BackupAge},
		{"artifact-size", observability.ArtifactSize},
		{"restore-duration", observability.RestoreDuration},
		{"restore-failure-class", observability.RestoreFailureClass},
		{"parity-drift", observability.ParityDrift},
	}
	for _, check := range checks {
		if !check.ok {
			return fmt.Errorf("missing %s observability", check.label)
		}
	}
	return nil
}

func validateBackupRestoreScope(scope Scope) error {
	if strings.TrimSpace(string(scope.Kind)) == "" {
		return fmt.Errorf("scope kind is required")
	}
	if !supportedScopeKind(scope.Kind) {
		return fmt.Errorf("unsupported scope kind %q", scope.Kind)
	}
	if strings.TrimSpace(scope.ID) == "" {
		return fmt.Errorf("scope id is required")
	}
	return nil
}

func supportedDurableStateClass(stateClass DurableStateClass) bool {
	switch stateClass {
	case DurableStateContentReadModel, DurableStateFactFamily, DurableStateRelationshipEvidence,
		DurableStateSearchDocument, DurableStateGraphSchema:
		return true
	default:
		return false
	}
}

func supportedBackupArtifactKind(kind BackupArtifactKind) bool {
	switch kind {
	case BackupArtifactNornicDBSnapshot, BackupArtifactExportBundle, BackupArtifactObjectStore:
		return true
	default:
		return false
	}
}

func supportedBackupRestoreCheck(check BackupRestoreConsistencyCheckKind) bool {
	switch check {
	case BackupRestoreCheckGraph, BackupRestoreCheckContent, BackupRestoreCheckFact,
		BackupRestoreCheckReadModel, BackupRestoreCheckRelationshipEvidence:
		return true
	default:
		return false
	}
}

func supportedBackupRestoreProofStatus(status BackupRestoreProofStatus) bool {
	switch status {
	case BackupRestoreProofPassed, BackupRestoreProofPlanned, BackupRestoreProofFailed:
		return true
	default:
		return false
	}
}

func supportedBackupRestoreScenario(scenario BackupRestoreScenario) bool {
	switch scenario {
	case BackupRestoreScenarioMissingArtifact, BackupRestoreScenarioCorruptArtifact,
		BackupRestoreScenarioVersionMismatch, BackupRestoreScenarioPartialRestore,
		BackupRestoreScenarioStaleGeneration, BackupRestoreScenarioFallbackToPostgres:
		return true
	default:
		return false
	}
}

func requiredBackupRestoreChecks(stateClass DurableStateClass) []BackupRestoreConsistencyCheckKind {
	switch stateClass {
	case DurableStateContentReadModel:
		return []BackupRestoreConsistencyCheckKind{BackupRestoreCheckContent, BackupRestoreCheckReadModel}
	case DurableStateFactFamily:
		return []BackupRestoreConsistencyCheckKind{BackupRestoreCheckFact}
	case DurableStateRelationshipEvidence:
		return []BackupRestoreConsistencyCheckKind{
			BackupRestoreCheckGraph,
			BackupRestoreCheckFact,
			BackupRestoreCheckRelationshipEvidence,
		}
	case DurableStateSearchDocument:
		return []BackupRestoreConsistencyCheckKind{BackupRestoreCheckContent, BackupRestoreCheckReadModel}
	case DurableStateGraphSchema:
		return []BackupRestoreConsistencyCheckKind{BackupRestoreCheckGraph}
	default:
		return nil
	}
}

func requiredBackupRestoreProofs(status BackupRestoreProofStatus) []BackupRestoreScenarioProof {
	scenarios := requiredBackupRestoreScenarios()
	proofs := make([]BackupRestoreScenarioProof, 0, len(scenarios))
	for _, scenario := range scenarios {
		proofs = append(proofs, BackupRestoreScenarioProof{Scenario: scenario, Status: status})
	}
	return proofs
}

func requiredBackupRestoreScenarios() []BackupRestoreScenario {
	return []BackupRestoreScenario{
		BackupRestoreScenarioMissingArtifact,
		BackupRestoreScenarioCorruptArtifact,
		BackupRestoreScenarioVersionMismatch,
		BackupRestoreScenarioPartialRestore,
		BackupRestoreScenarioStaleGeneration,
		BackupRestoreScenarioFallbackToPostgres,
	}
}
