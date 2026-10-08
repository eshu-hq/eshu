// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope/selection"
)

var freshnessSelectionNow = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

// freshnessSelectionQueryer queues the five composite-read responses of a
// fully built generation followed by the selection lookup response.
func freshnessSelectionQueryer(selectionRows fakeRows) *fakeQueryer {
	observedAt := freshnessSelectionNow.Add(-time.Hour)
	return &fakeQueryer{responses: []fakeRows{
		{rows: [][]any{{"scope-1", "gen-1"}}},
		{rows: [][]any{{"gen-1", "active", "push", false, observedAt, "abc123", observedAt, "repository", "acme/orders-api"}}},
		{rows: [][]any{}},
		{rows: [][]any{}},
		{rows: [][]any{}},
		selectionRows,
	}}
}

func freshnessSelectionStore(queryer *fakeQueryer) RepositoryFreshnessStore {
	store := NewRepositoryFreshnessStore(queryer)
	store.now = func() time.Time { return freshnessSelectionNow }
	return store
}

// selectionRow is one repositoryFreshnessSelectionQuery row: selector_id,
// state, last_listed_at, first_unlisted_at, unlisted_cycle_count,
// evaluated_at, evaluation_interval_seconds.
func selectionRow(selectorID, state string, lastListed, firstUnlisted any, cycles int64, evaluatedAt time.Time) []any {
	return []any{selectorID, state, lastListed, firstUnlisted, cycles, evaluatedAt, int64(300)}
}

func TestReadRepositoryFreshnessSelectionConfirmedNotListed(t *testing.T) {
	t.Parallel()

	firstUnlisted := freshnessSelectionNow.Add(-10 * time.Minute)
	lastListed := firstUnlisted.Add(-5 * time.Minute)
	queryer := freshnessSelectionQueryer(fakeRows{rows: [][]any{
		selectionRow("sel-boatsgroup", "not_listed", lastListed, firstUnlisted, 3, freshnessSelectionNow),
	}})

	snapshot, err := freshnessSelectionStore(queryer).ReadRepositoryFreshness(context.Background(), "repo-1")
	if err != nil {
		t.Fatalf("ReadRepositoryFreshness() error = %v, want nil", err)
	}
	if got := len(queryer.queries); got != 6 {
		t.Fatalf("queries = %d, want 6 (composite read plus selection lookup)", got)
	}
	if queryer.queries[5] != repositoryFreshnessSelectionQuery {
		t.Fatalf("selection query dispatched =\n%s\nwant repositoryFreshnessSelectionQuery", queryer.queries[5])
	}
	if args := queryer.args[5]; len(args) != 1 || args[0] != "scope-1" {
		t.Fatalf("selection query args = %#v, want [scope-1]: the lookup is keyed by the resolved scope_id", args)
	}
	sel := snapshot.Selection
	if sel == nil {
		t.Fatal("Selection = nil, want a not_selected summary")
	}
	if sel.State != selection.AggregateNotSelected || sel.Reason != selection.StateNotListed {
		t.Fatalf("Selection = {%s %s}, want {not_selected not_listed}", sel.State, sel.Reason)
	}
	if !sel.LastListedAt.Equal(lastListed) || !sel.UnlistedSince.Equal(firstUnlisted) || !sel.EvaluatedAt.Equal(freshnessSelectionNow) {
		t.Fatalf("Selection timestamps = %+v, want last_listed %v unlisted_since %v evaluated %v", sel, lastListed, firstUnlisted, freshnessSelectionNow)
	}
}

func TestReadRepositoryFreshnessSelectionNoLiveRows(t *testing.T) {
	t.Parallel()

	stale := freshnessSelectionNow.Add(-16 * time.Minute)
	for name, rows := range map[string]fakeRows{
		"no rows":         {rows: [][]any{}},
		"only stale rows": {rows: [][]any{selectionRow("sel-old", "archived_excluded", stale, nil, 0, stale)}},
	} {
		snapshot, err := freshnessSelectionStore(freshnessSelectionQueryer(rows)).ReadRepositoryFreshness(context.Background(), "repo-1")
		if err != nil {
			t.Fatalf("%s: ReadRepositoryFreshness() error = %v, want nil", name, err)
		}
		if snapshot.Selection != nil {
			t.Fatalf("%s: Selection = %+v, want nil so the verdict is unchanged", name, snapshot.Selection)
		}
	}
}

func TestReadRepositoryFreshnessSelectionPendingAndSelected(t *testing.T) {
	t.Parallel()

	pending := freshnessSelectionQueryer(fakeRows{rows: [][]any{
		selectionRow("sel-a", "not_listed", nil, freshnessSelectionNow, 1, freshnessSelectionNow),
	}})
	snapshot, err := freshnessSelectionStore(pending).ReadRepositoryFreshness(context.Background(), "repo-1")
	if err != nil || snapshot.Selection == nil || snapshot.Selection.State != selection.AggregatePending {
		t.Fatalf("pending read = %+v, %v; want a pending summary", snapshot.Selection, err)
	}
	if !snapshot.Selection.LastListedAt.IsZero() {
		t.Fatalf("LastListedAt = %v, want zero for a NULL last_listed_at", snapshot.Selection.LastListedAt)
	}

	mixed := freshnessSelectionQueryer(fakeRows{rows: [][]any{
		selectionRow("sel-a", "selected", freshnessSelectionNow, nil, 0, freshnessSelectionNow),
		selectionRow("sel-b", "rule_excluded", freshnessSelectionNow, nil, 0, freshnessSelectionNow),
	}})
	snapshot, err = freshnessSelectionStore(mixed).ReadRepositoryFreshness(context.Background(), "repo-1")
	if err != nil || snapshot.Selection == nil || snapshot.Selection.State != selection.AggregateSelected {
		t.Fatalf("mixed read = %+v, %v; want a selected summary", snapshot.Selection, err)
	}
}

// TestReadRepositoryFreshnessSelectionErrorFailsTheRead pins the reader's
// sub-query error contract for the selection lookup: an error fails the
// whole read, like every other freshness sub-query, so the handler answers
// 500 and the error counter increments. It must never return a snapshot that
// would render a fresh-looking verdict with the selection evidence missing.
func TestReadRepositoryFreshnessSelectionErrorFailsTheRead(t *testing.T) {
	t.Parallel()

	queryer := freshnessSelectionQueryer(fakeRows{err: errors.New("relation does not exist")})
	_, err := freshnessSelectionStore(queryer).ReadRepositoryFreshness(context.Background(), "repo-1")
	if err == nil {
		t.Fatal("ReadRepositoryFreshness() error = nil, want the selection lookup error")
	}
	if !strings.Contains(err.Error(), "read selection") {
		t.Fatalf("error = %v, want it to name the selection lookup", err)
	}

	scanQueryer := freshnessSelectionQueryer(fakeRows{rows: [][]any{{"sel-a", "selected"}}})
	if _, err := freshnessSelectionStore(scanQueryer).ReadRepositoryFreshness(context.Background(), "repo-1"); err == nil {
		t.Fatal("ReadRepositoryFreshness() error = nil on a selection scan failure, want an error")
	}
}

func TestReadRepositoryFreshnessSelectionSkippedWithoutGeneration(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: [][]any{{"scope-1", "gen-1"}}},
		{rows: [][]any{}},
	}}
	snapshot, err := freshnessSelectionStore(queryer).ReadRepositoryFreshness(context.Background(), "repo-1")
	if err != nil {
		t.Fatalf("ReadRepositoryFreshness() error = %v, want nil", err)
	}
	if snapshot.Selection != nil || len(queryer.queries) != 2 {
		t.Fatalf("Selection = %+v after %d queries, want nil after 2: an ungenerated scope is already unknown", snapshot.Selection, len(queryer.queries))
	}
}
