// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// recordingQueryer captures every issued query and returns an empty result set
// so the full status read path completes regardless of query order. It lets the
// filtered-selection tests assert which queries the store decided to run.
type recordingQueryer struct {
	queries []string
}

func (q *recordingQueryer) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	q.queries = append(q.queries, query)
	return &fakeRows{}, nil
}

func queryWasIssued(queries []string, target string) bool {
	for _, query := range queries {
		if query == target {
			return true
		}
	}
	return false
}

func TestReadStatusSnapshotFilteredSkipsHeavyFactQueries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC)
	queryer := &recordingQueryer{}
	store := NewStatusStore(queryer)

	selection := statuspkg.SnapshotSelection{
		IncludeCollectorFactEvidence: false,
		IncludeRegistryCollectors:    false,
	}
	raw, err := store.ReadStatusSnapshotFiltered(context.Background(), now, selection)
	if err != nil {
		t.Fatalf("ReadStatusSnapshotFiltered() error = %v, want nil", err)
	}

	if len(raw.CollectorFactEvidence) != 0 {
		t.Fatalf("CollectorFactEvidence = %#v, want empty", raw.CollectorFactEvidence)
	}
	if len(raw.RegistryCollectors) != 0 {
		t.Fatalf("RegistryCollectors = %#v, want empty", raw.RegistryCollectors)
	}

	if queryWasIssued(queryer.queries, collectorFactEvidenceQuery) {
		t.Fatalf("collector fact evidence query was issued despite exclusion:\n%s",
			strings.Join(queryer.queries, "\n"))
	}
	if queryWasIssued(queryer.queries, registryCollectorStatusQuery) {
		t.Fatalf("registry collector status query was issued despite exclusion:\n%s",
			strings.Join(queryer.queries, "\n"))
	}
}

func TestReadStatusSnapshotFilteredFullSelectionIssuesHeavyFactQueries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC)
	queryer := &recordingQueryer{}
	store := NewStatusStore(queryer)

	if _, err := store.ReadStatusSnapshotFiltered(
		context.Background(),
		now,
		statuspkg.FullSnapshotSelection(),
	); err != nil {
		t.Fatalf("ReadStatusSnapshotFiltered() error = %v, want nil", err)
	}

	if !queryWasIssued(queryer.queries, collectorFactEvidenceQuery) {
		t.Fatalf("collector fact evidence query was not issued under full selection:\n%s",
			strings.Join(queryer.queries, "\n"))
	}
	if !queryWasIssued(queryer.queries, registryCollectorStatusQuery) {
		t.Fatalf("registry collector status query was not issued under full selection:\n%s",
			strings.Join(queryer.queries, "\n"))
	}
}

func TestReadStatusSnapshotUsesFullSelection(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC)
	queryer := &recordingQueryer{}
	store := NewStatusStore(queryer)

	if _, err := store.ReadStatusSnapshot(context.Background(), now); err != nil {
		t.Fatalf("ReadStatusSnapshot() error = %v, want nil", err)
	}

	if !queryWasIssued(queryer.queries, collectorFactEvidenceQuery) {
		t.Fatalf("ReadStatusSnapshot dropped collector fact evidence query:\n%s",
			strings.Join(queryer.queries, "\n"))
	}
	if !queryWasIssued(queryer.queries, registryCollectorStatusQuery) {
		t.Fatalf("ReadStatusSnapshot dropped registry collector status query:\n%s",
			strings.Join(queryer.queries, "\n"))
	}
}
