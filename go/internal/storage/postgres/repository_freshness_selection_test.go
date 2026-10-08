// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// selectionTestFreshnessPrefix returns the five positional responses every
// resolved freshness read consumes before the #7625 selection reads: scope
// resolve, generation, empty stage counts, empty shared pending, and an
// empty webhook lookup.
func selectionTestFreshnessPrefix() []fakeRows {
	activatedAt := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	observedAt := activatedAt.Add(-2 * time.Minute)
	return []fakeRows{
		{rows: [][]any{{"scope-1", "gen-1"}}},
		{rows: [][]any{{"gen-1", "active", "push", false, activatedAt, "abc123", observedAt, "repository", "acme/orders-api"}}},
		{rows: [][]any{}},
		{rows: [][]any{}},
		{rows: [][]any{}},
	}
}

// TestReadRepositoryFreshnessPopulatesSelectionBlock proves the freshness
// read dispatches the two #7625 selection statements and composes a
// confirmed exclusion into not_selected: one live not_listed row seen twice
// over 26 hours, with no generation observed after the exclusion began.
func TestReadRepositoryFreshnessPopulatesSelectionBlock(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	evaluatedAt := now.Add(-time.Hour).Truncate(time.Second)
	stateSince := now.Add(-26 * time.Hour).Truncate(time.Second)
	responses := append(selectionTestFreshnessPrefix(),
		fakeRows{rows: [][]any{{
			"sel_test", "not_listed", int64(12345), evaluatedAt,
			int64(48 * 3600), stateSince, int64(2), nil,
		}}},
		// MAX(observed_at) over no newer generation: the NULL row reads as
		// the zero time, so rule 4 passes.
		fakeRows{rows: [][]any{{nil}}},
	)
	queryer := &fakeQueryer{responses: responses}

	store := NewRepositoryFreshnessStore(queryer)
	snapshot, err := store.ReadRepositoryFreshness(context.Background(), "repo-1")
	if err != nil {
		t.Fatalf("ReadRepositoryFreshness() error = %v, want nil", err)
	}
	if len(queryer.queries) != 7 {
		t.Fatalf("queries dispatched = %d, want 7 (resolve, generation, stages, shared, webhook, selection rows, latest observed)", len(queryer.queries))
	}
	if queryer.queries[5] != selectionObservationsByScopeQuery {
		t.Fatalf("selection rows query dispatched = \n%s\nwant the shipped selectionObservationsByScopeQuery constant", queryer.queries[5])
	}
	if queryer.queries[6] != latestGenerationObservedAtQuery {
		t.Fatalf("latest observed query dispatched = \n%s\nwant the shipped latestGenerationObservedAtQuery constant", queryer.queries[6])
	}
	if snapshot.Selection.State != statuspkg.RepositorySelectionNotSelected {
		t.Fatalf("Selection.State = %q, want %q", snapshot.Selection.State, statuspkg.RepositorySelectionNotSelected)
	}
	if snapshot.Selection.LiveSelectorCount != 1 {
		t.Fatalf("LiveSelectorCount = %d, want 1", snapshot.Selection.LiveSelectorCount)
	}
	if verdict := statuspkg.ComputeRepositoryFreshnessVerdict(snapshot, ""); verdict != statuspkg.RepositoryFreshnessNotSelected {
		t.Fatalf("verdict = %q, want %q", verdict, statuspkg.RepositoryFreshnessNotSelected)
	}
}

// TestReadRepositoryFreshnessSelectionUnknownWithoutRows proves a scope
// with no observation rows reads selection unknown rather than a fabricated
// state.
func TestReadRepositoryFreshnessSelectionUnknownWithoutRows(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: selectionTestFreshnessPrefix()}
	store := NewRepositoryFreshnessStore(queryer)
	snapshot, err := store.ReadRepositoryFreshness(context.Background(), "repo-1")
	if err != nil {
		t.Fatalf("ReadRepositoryFreshness() error = %v, want nil", err)
	}
	if snapshot.Selection.State != statuspkg.RepositorySelectionUnknown {
		t.Fatalf("Selection.State = %q, want %q", snapshot.Selection.State, statuspkg.RepositorySelectionUnknown)
	}
	if verdict := statuspkg.ComputeRepositoryFreshnessVerdict(snapshot, ""); verdict != statuspkg.RepositoryFreshnessCurrent {
		t.Fatalf("verdict = %q, want current (unknown selection leaves the verdict alone)", verdict)
	}
}
