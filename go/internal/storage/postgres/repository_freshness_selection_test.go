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
// fully built generation, the live selection lookup response, and any later
// responses (the latest-generation read when rules (a)-(c) hold).
func freshnessSelectionQueryer(selectionRows fakeRows, later ...fakeRows) *fakeQueryer {
	observedAt := freshnessSelectionNow.Add(-time.Hour)
	return &fakeQueryer{responses: append([]fakeRows{
		{rows: [][]any{{"scope-1", "gen-1"}}},
		{rows: [][]any{{"gen-1", "active", "push", false, observedAt, "abc123", observedAt, "repository", "acme/orders-api"}}},
		{rows: [][]any{}},
		{rows: [][]any{}},
		{rows: [][]any{}},
		selectionRows,
	}, later...)}
}

func freshnessSelectionStore(queryer *fakeQueryer) RepositoryFreshnessStore {
	store := NewRepositoryFreshnessStore(queryer)
	store.now = func() time.Time { return freshnessSelectionNow }
	return store
}

// selectionRow is one live selection lookup row: selector_id, state,
// last_listed_at, state_since, state_cycle_count, evaluated_at,
// liveness_window_seconds (48h).
func selectionRow(selectorID, state string, lastListed any, stateSince time.Time, cycles int64, evaluatedAt time.Time) []any {
	return []any{selectorID, state, lastListed, stateSince, cycles, evaluatedAt, int64(172800)}
}

func latestGenerationRow(observedAt any) fakeRows {
	return fakeRows{rows: [][]any{{observedAt}}}
}

func TestReadRepositoryFreshnessSelectionConfirmedNotListed(t *testing.T) {
	t.Parallel()

	since := freshnessSelectionNow.Add(-10 * time.Minute)
	lastListed := since.Add(-5 * time.Minute)
	queryer := freshnessSelectionQueryer(fakeRows{rows: [][]any{
		selectionRow("sel-acme", "not_listed", lastListed, since, 3, freshnessSelectionNow),
	}}, latestGenerationRow(since))

	snapshot, err := freshnessSelectionStore(queryer).ReadRepositoryFreshness(context.Background(), "repo-1")
	if err != nil {
		t.Fatalf("ReadRepositoryFreshness() error = %v, want nil", err)
	}
	if got := len(queryer.queries); got != 7 {
		t.Fatalf("queries = %d, want 7 (composite read, selection lookup, latest generation)", got)
	}
	if !strings.Contains(queryer.queries[5], "FROM repository_selection_observations") {
		t.Fatalf("query 6 =\n%s\nwant the live selection lookup", queryer.queries[5])
	}
	if args := queryer.args[5]; len(args) != 2 || args[0] != "scope-1" || args[1] != freshnessSelectionNow {
		t.Fatalf("selection query args = %#v, want [scope-1 now]: keyed by the resolved scope_id and the read clock", args)
	}
	if queryer.queries[6] != repositoryFreshnessLatestGenerationQuery {
		t.Fatalf("query 7 =\n%s\nwant repositoryFreshnessLatestGenerationQuery", queryer.queries[6])
	}
	if args := queryer.args[6]; len(args) != 1 || args[0] != "scope-1" {
		t.Fatalf("latest generation args = %#v, want [scope-1]", args)
	}
	sel := snapshot.Selection
	if sel.State != selection.AggregateNotSelected || sel.Reason != selection.StateNotListed || sel.LiveSelectorCount != 1 {
		t.Fatalf("Selection = %+v, want not_selected/not_listed over 1 live selector (generation observed exactly at state_since)", sel)
	}
	if !sel.LastListedAt.Equal(lastListed) || !sel.StateSince.Equal(since) || !sel.EvaluatedAt.Equal(freshnessSelectionNow) {
		t.Fatalf("Selection timestamps = %+v, want last_listed %v state_since %v evaluated %v", sel, lastListed, since, freshnessSelectionNow)
	}
}

func TestReadRepositoryFreshnessSelectionGenerationAfterExclusion(t *testing.T) {
	t.Parallel()

	since := freshnessSelectionNow.Add(-10 * time.Minute)
	queryer := freshnessSelectionQueryer(fakeRows{rows: [][]any{
		selectionRow("sel-a", "rule_excluded", since, since, 2, freshnessSelectionNow),
	}}, latestGenerationRow(since.Add(time.Microsecond)))
	snapshot, err := freshnessSelectionStore(queryer).ReadRepositoryFreshness(context.Background(), "repo-1")
	if err != nil || snapshot.Selection.State != selection.AggregateExcludedStillIngested {
		t.Fatalf("Selection = %+v, %v; want excluded_still_ingested", snapshot.Selection, err)
	}

	none := freshnessSelectionQueryer(fakeRows{rows: [][]any{
		selectionRow("sel-a", "rule_excluded", since, since, 2, freshnessSelectionNow),
	}}, latestGenerationRow(nil))
	snapshot, err = freshnessSelectionStore(none).ReadRepositoryFreshness(context.Background(), "repo-1")
	if err != nil || snapshot.Selection.State != selection.AggregateNotSelected {
		t.Fatalf("no generation: Selection = %+v, %v; want not_selected", snapshot.Selection, err)
	}
}

func TestReadRepositoryFreshnessSelectionNoLiveRowsIsUnknown(t *testing.T) {
	t.Parallel()

	queryer := freshnessSelectionQueryer(fakeRows{rows: [][]any{}})
	snapshot, err := freshnessSelectionStore(queryer).ReadRepositoryFreshness(context.Background(), "repo-1")
	if err != nil {
		t.Fatalf("ReadRepositoryFreshness() error = %v, want nil", err)
	}
	if snapshot.Selection != (selection.Summary{State: selection.AggregateUnknown}) {
		t.Fatalf("Selection = %+v, want unknown with no other fields", snapshot.Selection)
	}
	if len(queryer.queries) != 6 {
		t.Fatalf("queries = %d, want 6: no latest-generation read without live rows", len(queryer.queries))
	}
}

func TestReadRepositoryFreshnessSelectionPendingAndSelectedSkipTheGenerationRead(t *testing.T) {
	t.Parallel()

	pending := freshnessSelectionQueryer(fakeRows{rows: [][]any{
		selectionRow("sel-a", "not_listed", nil, freshnessSelectionNow, 1, freshnessSelectionNow),
	}})
	snapshot, err := freshnessSelectionStore(pending).ReadRepositoryFreshness(context.Background(), "repo-1")
	if err != nil || snapshot.Selection.State != selection.AggregatePendingConfirmation {
		t.Fatalf("pending read = %+v, %v; want pending_confirmation", snapshot.Selection, err)
	}
	if !snapshot.Selection.LastListedAt.IsZero() || len(pending.queries) != 6 {
		t.Fatalf("pending read = %+v after %d queries, want a zero LastListedAt for NULL and no generation read", snapshot.Selection, len(pending.queries))
	}

	since := freshnessSelectionNow.Add(-time.Hour)
	mixed := freshnessSelectionQueryer(fakeRows{rows: [][]any{
		selectionRow("sel-a", "selected", freshnessSelectionNow, since, 4, freshnessSelectionNow),
		selectionRow("sel-b", "rule_excluded", freshnessSelectionNow, since, 4, freshnessSelectionNow),
	}})
	snapshot, err = freshnessSelectionStore(mixed).ReadRepositoryFreshness(context.Background(), "repo-1")
	if err != nil || snapshot.Selection.State != selection.AggregateSelected || snapshot.Selection.LiveSelectorCount != 2 || len(mixed.queries) != 6 {
		t.Fatalf("mixed read = %+v, %v after %d queries; want selected over 2 live selectors and no generation read", snapshot.Selection, err, len(mixed.queries))
	}
}

// TestReadRepositoryFreshnessSelectionErrorFailsTheRead pins the reader's
// sub-query error contract for both selection reads: an error fails the whole
// read, like every other freshness sub-query, so the handler answers 500 and
// the error counter increments. It must never return a snapshot that would
// render a fresh-looking verdict with the selection evidence missing.
func TestReadRepositoryFreshnessSelectionErrorFailsTheRead(t *testing.T) {
	t.Parallel()

	since := freshnessSelectionNow.Add(-10 * time.Minute)
	confirmed := fakeRows{rows: [][]any{selectionRow("sel-a", "not_listed", nil, since, 2, freshnessSelectionNow)}}
	for name, queryer := range map[string]*fakeQueryer{
		"selection lookup":         freshnessSelectionQueryer(fakeRows{err: errors.New("relation does not exist")}),
		"selection scan":           freshnessSelectionQueryer(fakeRows{rows: [][]any{{"sel-a", "selected"}}}),
		"latest generation lookup": freshnessSelectionQueryer(confirmed, fakeRows{err: errors.New("conn reset")}),
		"latest generation scan":   freshnessSelectionQueryer(confirmed, fakeRows{rows: [][]any{{"not-a-time"}}}),
	} {
		_, err := freshnessSelectionStore(queryer).ReadRepositoryFreshness(context.Background(), "repo-1")
		if err == nil {
			t.Fatalf("%s: ReadRepositoryFreshness() error = nil, want an error", name)
		}
		if !strings.Contains(err.Error(), "read selection") {
			t.Fatalf("%s: error = %v, want it to name the selection read", name, err)
		}
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
	if snapshot.Selection != (selection.Summary{}) || len(queryer.queries) != 2 {
		t.Fatalf("Selection = %+v after %d queries, want zero after 2: an ungenerated scope is already unknown", snapshot.Selection, len(queryer.queries))
	}
}
