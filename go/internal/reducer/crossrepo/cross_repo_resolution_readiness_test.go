// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossrepo

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func TestCrossRepoResolutionGatesUntilBackwardEvidenceCommitted(t *testing.T) {
	t.Parallel()

	evidenceLoader := &fakeEvidenceFactLoader{
		facts: []relationships.EvidenceFact{
			{
				EvidenceKind:     relationships.EvidenceKindTerraformAppRepo,
				RelationshipType: relationships.RelProvisionsDependencyFor,
				SourceRepoID:     "infra-repo",
				TargetRepoID:     "app-repo",
				Confidence:       0.99,
			},
		},
	}
	intentWriter := &recordingRepoDependencyIntentWriter{}
	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: evidenceLoader,
		IntentWriter:   intentWriter,
		ReadinessLookup: func(key gpphase.PhaseKey, phase gpphase.Phase) (bool, bool) {
			if got, want := key.ScopeID, "scope-1"; got != want {
				t.Fatalf("ScopeID = %q, want %q", got, want)
			}
			if got, want := key.AcceptanceUnitID, "scope-1"; got != want {
				t.Fatalf("AcceptanceUnitID = %q, want %q", got, want)
			}
			if got, want := key.SourceRunID, "gen-1"; got != want {
				t.Fatalf("SourceRunID = %q, want %q", got, want)
			}
			if got, want := key.GenerationID, "gen-1"; got != want {
				t.Fatalf("GenerationID = %q, want %q", got, want)
			}
			if got, want := key.Keyspace, gpphase.KeyspaceCrossRepoEvidence; got != want {
				t.Fatalf("Keyspace = %q, want %q", got, want)
			}
			if got, want := phase, gpphase.PhaseBackwardEvidenceCommitted; got != want {
				t.Fatalf("phase = %q, want %q", got, want)
			}
			return false, false
		},
	}

	count, err := handler.Resolve(context.Background(), "scope-1", "gen-1")
	if err == nil {
		t.Fatal("Resolve() error = nil, want backward-evidence deferral")
	}
	var deferral BackwardEvidenceNotReadyError
	if !errors.As(err, &deferral) {
		t.Fatalf("Resolve() error type = %T, want BackwardEvidenceNotReadyError", err)
	}
	if !deferral.Retryable() {
		t.Fatal("deferral Retryable() = false, want true so the queue re-runs the scope")
	}
	if got, want := deferral.FailureClass(), CrossRepoBackwardEvidenceNotReadyFailureClass; got != want {
		t.Fatalf("deferral FailureClass() = %q, want %q", got, want)
	}
	if count != 0 {
		t.Fatalf("Resolve() = %d, want 0 when readiness is missing", count)
	}
	if evidenceLoader.calls != 0 {
		t.Fatalf("evidence loader calls = %d, want 0 when gated", evidenceLoader.calls)
	}
	if len(intentWriter.rows) != 0 {
		t.Fatalf("intent writes = %d, want 0 when gated", len(intentWriter.rows))
	}
}
