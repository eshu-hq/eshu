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

func TestRuntimeProjectMaterializesRepositoryRefs(t *testing.T) {
	t.Parallel()

	contentWriter := &recordingContentWriter{result: content.Result{RepositoryRefCount: 2}}
	runtime := Runtime{CanonicalWriter: &recordingCanonicalWriter{}, ContentWriter: contentWriter}
	observedAt := time.Date(2026, time.June, 1, 9, 0, 0, 0, time.UTC)
	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-123",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-123",
		Metadata: map[string]string{
			"repo_id": "repository:r_12345678",
		},
	}
	generationValue := scope.ScopeGeneration{
		GenerationID: "generation-456",
		ScopeID:      "scope-123",
		ObservedAt:   observedAt,
		IngestedAt:   observedAt.Add(5 * time.Minute),
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}

	_, err := runtime.Project(context.Background(), scopeValue, generationValue, []facts.Envelope{{
		FactID:       "fact-repository",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		FactKind:     "repository",
		ObservedAt:   observedAt,
		Payload: map[string]any{
			"repo_id":        "repository:r_12345678",
			"default_branch": "main",
			"git_refs": []any{
				map[string]any{"name": "main", "kind": "branch", "head_sha": "abc123", "is_default": true},
				map[string]any{"name": "release", "kind": "branch", "head_sha": "def456"},
			},
		},
	}})
	if err != nil {
		t.Fatalf("Project() error = %v, want nil", err)
	}
	if got, want := len(contentWriter.calls), 1; got != want {
		t.Fatalf("content writer call count = %d, want %d", got, want)
	}
	refs := contentWriter.calls[0].RepositoryRefs
	if got, want := len(refs), 2; got != want {
		t.Fatalf("repository ref count = %d, want %d", got, want)
	}
	if got, want := refs[0].Name, "main"; got != want {
		t.Fatalf("refs[0].Name = %q, want %q", got, want)
	}
	if !refs[0].Default {
		t.Fatal("refs[0].Default = false, want true")
	}
	if got, want := refs[1].HeadSHA, "def456"; got != want {
		t.Fatalf("refs[1].HeadSHA = %q, want %q", got, want)
	}
	if !refs[0].ObservedAt.Equal(observedAt) {
		t.Fatalf("refs[0].ObservedAt = %s, want %s", refs[0].ObservedAt, observedAt)
	}
}

func TestBuildRepositoryRefsSkipsMissingRepositoryID(t *testing.T) {
	t.Parallel()

	refs := buildRepositoryRefs(facts.Envelope{
		FactID:   "fact-repository",
		FactKind: "repository",
		Payload: map[string]any{
			"default_branch": "main",
			"git_refs": []any{
				map[string]any{"name": "main", "kind": "branch", "head_sha": "abc123", "is_default": true},
			},
		},
	})
	if len(refs) != 0 {
		t.Fatalf("len(refs) = %d, want 0; repository refs must not materialize from a repository fact missing repo_id", len(refs))
	}
}

func TestCanonicalGraphPhaseSkipsMissingRepositoryID(t *testing.T) {
	t.Parallel()

	rows := canonicalGraphPhaseStates("generation-456", []facts.Envelope{
		{
			FactID:       "fact-repository",
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
			FactKind:     "repository",
			Payload: map[string]any{
				"graph_id":      "repo-123",
				"source_run_id": "run-123",
			},
		},
	})
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0; repository phase rows must not publish from a repository fact missing repo_id", len(rows))
	}
}
