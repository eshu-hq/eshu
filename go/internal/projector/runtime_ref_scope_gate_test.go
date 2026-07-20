// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestRuntimeProjectRejectsRefScope proves the fail-closed projection gate:
// a KindRepositoryRef scope produces zero canonical rows, zero content records,
// and zero reducer intents — and the default-branch scope still projects normally.
// This is the safety-critical isolation from epic #5393 / enabler #5417.
func TestRuntimeProjectRejectsRefScope(t *testing.T) {
	t.Parallel()

	canonicalWriter := &recordingCanonicalWriter{}
	contentWriter := &recordingContentWriter{result: content.Result{RecordCount: 0}}
	intentWriter := &recordingIntentWriter{result: IntentResult{Count: 0}}

	runtime := Runtime{
		CanonicalWriter: canonicalWriter,
		ContentWriter:   contentWriter,
		IntentWriter:    intentWriter,
	}

	scopeValue := scope.IngestionScope{
		ScopeID:       "git-repository-scope:repo-123@feature-x",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepositoryRef,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-123",
		Metadata:      map[string]string{"repo_id": "repo-123", "ref": "feature-x"},
	}
	generationValue := scope.ScopeGeneration{
		GenerationID: "generation-456",
		ScopeID:      "git-repository-scope:repo-123@feature-x",
		ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.April, 12, 11, 35, 0, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}

	result, err := runtime.Project(context.Background(), scopeValue, generationValue, []facts.Envelope{
		{
			FactID:       "fact-0",
			ScopeID:      "git-repository-scope:repo-123@feature-x",
			GenerationID: "generation-456",
			FactKind:     "repository",
			ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
			Payload: map[string]any{
				"repo_id": "repo-123",
				"name":    "test-repo",
				"ref":     "feature-x",
			},
		},
	})
	if err != nil {
		t.Fatalf("Project() error = %v, want nil", err)
	}

	// The ref-scoped scope must produce ZERO canonical writes.
	if got, want := len(canonicalWriter.calls), 0; got != want {
		t.Fatalf("canonicalWriter calls = %d, want %d (ref scope must not write canonical graph)", got, want)
	}

	// The ref-scoped scope must produce ZERO content writes.
	if got, want := len(contentWriter.calls), 0; got != want {
		t.Fatalf("contentWriter calls = %d, want %d (ref scope must not write content)", got, want)
	}

	// The ref-scoped scope must produce ZERO reducer intents.
	if got, want := len(intentWriter.calls), 0; got != want {
		t.Fatalf("intentWriter calls = %d, want %d (ref scope must not enqueue reducer intents)", got, want)
	}

	// The result should carry the correct scope/generation IDs.
	if result.ScopeID != "git-repository-scope:repo-123@feature-x" {
		t.Fatalf("result.ScopeID = %q, want ref-scoped scope ID", result.ScopeID)
	}

	// Now prove default-branch scope still projects normally.
	defScope := scope.IngestionScope{
		ScopeID:       "git-repository-scope:repo-123",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-123",
		Metadata:      map[string]string{"repo_id": "repo-123"},
	}
	defGen := scope.ScopeGeneration{
		GenerationID: "generation-789",
		ScopeID:      "git-repository-scope:repo-123",
		ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.April, 12, 11, 35, 0, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}

	// Reset writers.
	canonicalWriter.calls = nil

	_, err = runtime.Project(context.Background(), defScope, defGen, []facts.Envelope{
		{
			FactID:       "fact-def",
			ScopeID:      "git-repository-scope:repo-123",
			GenerationID: "generation-789",
			FactKind:     "repository",
			ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
			Payload: map[string]any{
				"repo_id": "repo-123",
				"name":    "test-repo",
			},
		},
	})
	if err != nil {
		t.Fatalf("default-branch Project() error = %v, want nil", err)
	}

	// Default branch must produce canonical writes.
	if got, want := len(canonicalWriter.calls), 1; got != want {
		t.Fatalf("default-branch canonicalWriter calls = %d, want %d", got, want)
	}
}
