// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// countsQueryer serves one canned row set for the selection counts query
// and empty rows for every other status statement.
type countsQueryer struct {
	queries []string
	rows    [][]any
}

func (q *countsQueryer) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	q.queries = append(q.queries, query)
	if query == repositorySelectionCountsQuery {
		return &fakeRows{rows: q.rows}, nil
	}
	return &fakeRows{}, nil
}

func TestReadRepositorySelectionCountsMapsStatesAndVerdicts(t *testing.T) {
	t.Parallel()

	queryer := &countsQueryer{rows: [][]any{
		{"archived_excluded", "unclassified", int64(3)},
		{"not_listed", "not_found_or_inaccessible", int64(1)},
		{"not_listed", "renamed", int64(1)},
		{"not_listed", "transferred", int64(22)},
		{"not_listed", "unclassified", int64(1)},
		{"selected", "unclassified", int64(777)},
	}}
	got, err := readRepositorySelectionCounts(context.Background(), queryer, time.Now())
	if err != nil {
		t.Fatalf("readRepositorySelectionCounts() error = %v", err)
	}
	if got.States["selected"] != 777 || got.States["not_listed"] != 25 || got.States["archived_excluded"] != 3 {
		t.Fatalf("States = %+v, want selected 777, not_listed 25, archived_excluded 3", got.States)
	}
	if got.Classifications["transferred"] != 22 || got.Classifications["renamed"] != 1 ||
		got.Classifications["not_found_or_inaccessible"] != 1 || got.Classifications["unclassified"] != 1 {
		t.Fatalf("Classifications = %+v, want the not_listed verdict shares", got.Classifications)
	}
	if len(got.Classifications) != 4 {
		t.Fatalf("Classifications = %+v, want only not_listed verdicts: selected rows never dilute a share", got.Classifications)
	}
}

func TestReadRepositorySelectionCountsEmptyTableReadsAbsent(t *testing.T) {
	t.Parallel()

	got, err := readRepositorySelectionCounts(context.Background(), &countsQueryer{}, time.Now())
	if err != nil {
		t.Fatalf("readRepositorySelectionCounts() error = %v", err)
	}
	if !got.Empty() {
		t.Fatalf("counts = %+v, want the zero section", got)
	}
}

func TestReadStatusSnapshotFilteredSurfacesRepositorySelection(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	queryer := &countsQueryer{rows: [][]any{
		{"not_listed", "transferred", int64(22)},
		{"selected", "unclassified", int64(777)},
	}}
	raw, err := NewStatusStore(queryer).ReadStatusSnapshotFiltered(
		context.Background(), now, statuspkg.SnapshotSelection{},
	)
	if err != nil {
		t.Fatalf("ReadStatusSnapshotFiltered() error = %v", err)
	}
	if raw.RepositorySelection.States["not_listed"] != 22 || raw.RepositorySelection.Classifications["transferred"] != 22 {
		t.Fatalf("RepositorySelection = %+v, want the counts read surfaced", raw.RepositorySelection)
	}
	if !queryWasIssued(queryer.queries, repositorySelectionCountsQuery) {
		t.Fatal("selection counts query was not issued on the standard read")
	}
}

func TestReadStatusSnapshotFilteredSemanticOnlySkipsRepositorySelection(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	queryer := &countsQueryer{}
	raw, err := NewStatusStore(queryer).ReadStatusSnapshotFiltered(
		context.Background(), now, statuspkg.SemanticOnlySnapshotSelection(),
	)
	if err != nil {
		t.Fatalf("ReadStatusSnapshotFiltered() error = %v", err)
	}
	if queryWasIssued(queryer.queries, repositorySelectionCountsQuery) {
		t.Fatal("selection counts query was issued on the semantic-only read")
	}
	if !raw.RepositorySelection.Empty() {
		t.Fatalf("RepositorySelection = %+v, want absent on the semantic-only read", raw.RepositorySelection)
	}
	for _, issued := range queryer.queries {
		if strings.Contains(issued, "repository_selection_observations") {
			t.Fatalf("observations read on semantic-only path: %q", issued)
		}
	}
}
