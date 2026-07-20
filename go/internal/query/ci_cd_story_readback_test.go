// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"testing"
)

func TestLoadRepositoryScopedCICDEvidenceUsesBoundedRepositoryScope(t *testing.T) {
	t.Parallel()

	rows := make([]CICDRunCorrelationRow, 0, cicdStoryRunCorrelationLimit+1)
	for i := range cicdStoryRunCorrelationLimit + 1 {
		rows = append(rows, CICDRunCorrelationRow{
			CorrelationID: fmt.Sprintf("correlation-%02d", i),
			RepositoryID:  "repo://example/api",
			Provider:      "github_actions",
			RunID:         fmt.Sprintf("run-%02d", i),
			Outcome:       "exact",
		})
	}
	store := &recordingCICDRunCorrelationStore{rows: rows}

	summary, err := loadRepositoryScopedCICDEvidence(
		context.Background(),
		fakePortContentStore{},
		store,
		"repo://example/api",
	)
	if err != nil {
		t.Fatalf("loadRepositoryScopedCICDEvidence() error = %v, want nil", err)
	}
	if got, want := store.lastFilter.RepositoryID, "repo://example/api"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.Limit, cicdStoryRunCorrelationLimit+1; got != want {
		t.Fatalf("Limit = %d, want %d", got, want)
	}
	live := mustMapField(t, summary, "live_run_correlations")
	if got, want := live["count"], cicdStoryRunCorrelationLimit; got != want {
		t.Fatalf("live_run_correlations.count = %#v, want %#v", got, want)
	}
	if got, want := live["truncated"], true; got != want {
		t.Fatalf("live_run_correlations.truncated = %#v, want %#v", got, want)
	}
}

func TestLoadRepositoryScopedCICDEvidenceResolvesByCanonicalRepositoryID(t *testing.T) {
	t.Parallel()

	canonicalRepoID := "repository:r_008329d6"
	rawRepoID := "github.com/eshu-hq/eshu"

	// Seed the store with rows carrying the canonical repository_id.
	rows := []CICDRunCorrelationRow{
		{
			CorrelationID: "corr-1",
			RepositoryID:  canonicalRepoID,
			Provider:      "github_actions",
			RunID:         "123",
			Outcome:       "exact",
		},
	}
	store := &recordingCICDRunCorrelationStore{rows: rows}

	// Positive case: querying by the canonical id returns the row.
	summary, err := loadRepositoryScopedCICDEvidence(
		context.Background(),
		fakePortContentStore{},
		store,
		canonicalRepoID,
	)
	if err != nil {
		t.Fatalf("loadRepositoryScopedCICDEvidence(canonical) error = %v, want nil", err)
	}
	live := mustMapField(t, summary, "live_run_correlations")
	if got, want := live["count"], 1; got != want {
		t.Fatalf("live_run_correlations.count = %#v, want 1 (canonical repo id must resolve)", got)
	}

	// Negative case: querying by the old raw provider namespace must NOT
	// cross-join into the canonical repo's story.
	summaryRaw, err := loadRepositoryScopedCICDEvidence(
		context.Background(),
		fakePortContentStore{},
		store,
		rawRepoID,
	)
	if err != nil {
		t.Fatalf("loadRepositoryScopedCICDEvidence(raw) error = %v, want nil", err)
	}
	liveRaw := mustMapField(t, summaryRaw, "live_run_correlations")
	if got, want := liveRaw["count"], 0; got != want {
		t.Fatalf("live_run_correlations.count = %#v, want 0 (raw provider id must not cross-join into canonical namespace)", got)
	}
}
