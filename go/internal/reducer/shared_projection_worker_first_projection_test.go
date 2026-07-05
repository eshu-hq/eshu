// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeFirstProjectionLookup is a test double for FirstProjectionLookup that
// records every (scopeID, currentGenerationID) probe so tests can assert the
// memoization guarantee (#3624): a scope is probed at most once per
// planRepoWideRetractWork call regardless of how many refresh rows share it.
type fakeFirstProjectionLookup struct {
	hasPrior map[string]bool // keyed by scopeID
	err      error
	calls    []string // scopeIDs probed, in call order
}

func (f *fakeFirstProjectionLookup) ScopeHasPriorGeneration(
	_ context.Context,
	scopeID, _ string,
) (bool, error) {
	f.calls = append(f.calls, scopeID)
	if f.err != nil {
		return false, f.err
	}
	return f.hasPrior[scopeID], nil
}

func firstProjectionRefreshRow(t *testing.T, repoID, scopeID, generationID string, created time.Time) SharedProjectionIntentRow {
	t.Helper()
	return BuildSharedProjectionIntent(SharedProjectionIntentInput{
		ProjectionDomain: DomainHandlesRoute,
		PartitionKey:     repoWideRetractRefreshPartitionKey(DomainHandlesRoute, repoID),
		ScopeID:          scopeID,
		AcceptanceUnitID: repoID,
		RepositoryID:     repoID,
		SourceRunID:      "run-1",
		GenerationID:     generationID,
		Payload: map[string]any{
			"repo_id":     repoID,
			"intent_type": repoRefreshIntentType,
			"action":      repoRefreshAction,
		},
		CreatedAt: created,
	})
}

// TestPlanRepoWideRetractWorkReIngestStillRetracts is the correctness
// regression: a refresh row whose scope HAS a prior activated generation MUST
// still retract. Inverting this assertion would silently drop re-ingest
// retracts and leave stale edges in the graph.
func TestPlanRepoWideRetractWorkReIngestStillRetracts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 5, 12, 0, 0, 0, time.UTC)
	row := firstProjectionRefreshRow(t, "repo-a", "scope-a", "gen-2", now)
	lookup := &fakeFirstProjectionLookup{hasPrior: map[string]bool{"scope-a": true}}

	plan, err := planRepoWideRetractWork(context.Background(), DomainHandlesRoute, []SharedProjectionIntentRow{row}, nil, lookup, nil)
	if err != nil {
		t.Fatalf("planRepoWideRetractWork: %v", err)
	}
	if len(plan.retractRows) != 1 {
		t.Fatalf("retractRows = %d, want 1 (re-ingest must still retract)", len(plan.retractRows))
	}
	if len(plan.completedRows) != 1 {
		t.Fatalf("completedRows = %d, want 1", len(plan.completedRows))
	}
}

// TestPlanRepoWideRetractWorkSkipsFirstProjectionRetract proves the #3624 fix:
// a refresh row whose scope has NO prior activated generation is a guaranteed
// no-op retract, so it is skipped from retractRows while still completing (the
// fence still opens and per-edge writes still proceed).
func TestPlanRepoWideRetractWorkSkipsFirstProjectionRetract(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 5, 12, 0, 0, 0, time.UTC)
	row := firstProjectionRefreshRow(t, "repo-a", "scope-a", "gen-1", now)
	lookup := &fakeFirstProjectionLookup{hasPrior: map[string]bool{"scope-a": false}}

	plan, err := planRepoWideRetractWork(context.Background(), DomainHandlesRoute, []SharedProjectionIntentRow{row}, nil, lookup, nil)
	if err != nil {
		t.Fatalf("planRepoWideRetractWork: %v", err)
	}
	if len(plan.retractRows) != 0 {
		t.Fatalf("retractRows = %d, want 0 (first projection has nothing to retract)", len(plan.retractRows))
	}
	if len(plan.completedRows) != 1 {
		t.Fatalf("completedRows = %d, want 1 (fence must still open)", len(plan.completedRows))
	}
}

// TestPlanRepoWideRetractWorkNilFirstProjectionPreservesLegacyBehavior locks
// down that a nil FirstProjectionLookup leaves the retract byte-identical to
// pre-#3624 behavior: the refresh row always retracts.
func TestPlanRepoWideRetractWorkNilFirstProjectionPreservesLegacyBehavior(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 5, 12, 0, 0, 0, time.UTC)
	row := firstProjectionRefreshRow(t, "repo-a", "scope-a", "gen-1", now)

	plan, err := planRepoWideRetractWork(context.Background(), DomainHandlesRoute, []SharedProjectionIntentRow{row}, nil, nil, nil)
	if err != nil {
		t.Fatalf("planRepoWideRetractWork: %v", err)
	}
	if len(plan.retractRows) != 1 {
		t.Fatalf("retractRows = %d, want 1 (nil lookup disables the skip)", len(plan.retractRows))
	}
}

// TestPlanRepoWideRetractWorkMemoizesPerScope proves two refresh rows sharing
// one scope probe the lookup exactly once, so a batch with many refresh rows
// for the same scope does not multiply DB round-trips.
func TestPlanRepoWideRetractWorkMemoizesPerScope(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 5, 12, 0, 0, 0, time.UTC)
	rowA := firstProjectionRefreshRow(t, "repo-a", "scope-a", "gen-1", now)
	rowB := firstProjectionRefreshRow(t, "repo-b", "scope-a", "gen-1", now.Add(time.Second))
	lookup := &fakeFirstProjectionLookup{hasPrior: map[string]bool{"scope-a": false}}

	plan, err := planRepoWideRetractWork(context.Background(), DomainHandlesRoute, []SharedProjectionIntentRow{rowA, rowB}, nil, lookup, nil)
	if err != nil {
		t.Fatalf("planRepoWideRetractWork: %v", err)
	}
	if len(plan.retractRows) != 0 {
		t.Fatalf("retractRows = %d, want 0", len(plan.retractRows))
	}
	if len(plan.completedRows) != 2 {
		t.Fatalf("completedRows = %d, want 2", len(plan.completedRows))
	}
	if got := len(lookup.calls); got != 1 {
		t.Fatalf("lookup probed %d times, want 1 (memoize per scope); calls=%v", got, lookup.calls)
	}
}

// TestPlanRepoWideRetractWorkPropagatesProbeError proves a probe error is
// propagated rather than silently treated as "skip the retract" — fail-safe
// means running the retract, and a plan cannot resume mid-build on error.
func TestPlanRepoWideRetractWorkPropagatesProbeError(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 5, 12, 0, 0, 0, time.UTC)
	row := firstProjectionRefreshRow(t, "repo-a", "scope-a", "gen-1", now)
	wantErr := errors.New("db unavailable")
	lookup := &fakeFirstProjectionLookup{err: wantErr}

	_, err := planRepoWideRetractWork(context.Background(), DomainHandlesRoute, []SharedProjectionIntentRow{row}, nil, lookup, nil)
	if err == nil {
		t.Fatal("planRepoWideRetractWork error = nil, want propagated probe error")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("planRepoWideRetractWork error = %v, want wrapping %v", err, wantErr)
	}
}
