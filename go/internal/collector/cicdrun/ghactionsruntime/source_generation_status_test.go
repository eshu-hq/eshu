// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestClaimedGenerationIsPendingBeforeProjection is the regression test for
// eshu-hq/eshu#9239: collected generations must be non-terminal so the commit
// guard (backfill.ValidateGenerationInput, "must not be terminal before
// projection") accepts them. A Completed status here burns every claim to
// attempt_budget_exhausted with zero facts landing.
func TestClaimedGenerationIsPendingBeforeProjection(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 7, 15, 0, 0, 0, time.UTC)
	client := fakeClient{page: RunPage{Snapshots: []RunSnapshot{{
		Workflow: map[string]any{"id": 42, "name": "Publish", "path": ".github/workflows/publish.yml", "state": "active"},
		Run: map[string]any{
			"id": 1001, "run_attempt": 1, "run_number": 88, "name": "Publish",
			"event": "push", "status": "completed", "conclusion": "success",
			"head_branch": "main", "head_sha": "0123456789abcdef0123456789abcdef01234567",
			"run_started_at": "2026-06-07T14:59:00Z", "updated_at": "2026-06-07T15:00:00Z",
			"html_url":   "https://github.com/example/repo/actions/runs/1001",
			"repository": map[string]any{"full_name": "example/repo", "html_url": "https://github.com/example/repo"},
			"actor":      map[string]any{"login": "builder"},
		},
	}}}}

	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              client,
		Now:                 func() time.Time { return observedAt },
		Targets: []TargetConfig{{
			ScopeID:             "ci-cd:github-actions:example/repo",
			Repository:          "example/repo",
			Token:               "token",
			AllowedRepositories: []string{"example/repo"},
			SourceURI:           "https://github.com/example/repo",
			MaxRuns:             1,
			MaxJobs:             10,
			MaxArtifacts:        10,
		}},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		CollectorKind:       scope.CollectorCICDRun,
		CollectorInstanceID: "ci-cd-primary",
		ScopeID:             "ci-cd:github-actions:example/repo",
		GenerationID:        "generation-1",
		CurrentFencingToken: 7,
	})
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if got := collected.Generation.Status; got.IsTerminal() {
		t.Fatalf("Generation.Status = %q (terminal), want non-terminal so the commit guard accepts it", got)
	}
	if got, want := collected.Generation.Status, scope.GenerationStatusPending; got != want {
		t.Fatalf("Generation.Status = %q, want %q", got, want)
	}
	drainFacts(t, collected.Facts)
}
