// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	semanticstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/semantic"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// recordingQueryer captures every issued query and returns an empty result set
// so the full status read path completes regardless of query order. It lets the
// filtered-selection tests assert which queries the store decided to run.
type recordingQueryer struct {
	queries []string
}

func (q *recordingQueryer) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
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

func TestReadStatusSnapshotFilteredSemanticOnlyReadsOneStatement(t *testing.T) {
	t.Parallel()
	asOf := time.Date(2026, 10, 1, 10, 0, 0, 0, time.FixedZone("EDT", -4*60*60))
	queryer := &recordingQueryer{}
	raw, err := NewStatusStore(queryer).ReadStatusSnapshotFiltered(
		context.Background(), asOf, statuspkg.SemanticOnlySnapshotSelection(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(queryer.queries) != 1 || queryer.queries[0] != semanticstore.SemanticExtractionObservabilityQuery {
		t.Fatalf("queries = %d; want semantic observability statement only", len(queryer.queries))
	}
	if !raw.AsOf.Equal(asOf.UTC()) || raw.SemanticExtraction.State != statuspkg.SemanticExtractionUnavailable {
		t.Fatalf("raw = %+v; want semantic unavailable and UTC asOf", raw)
	}
	if len(raw.ScopeCounts) != 0 || len(raw.GenerationCounts) != 0 || raw.Queue != (statuspkg.QueueSnapshot{}) {
		t.Fatalf("semantic-only read populated unrelated status: %+v", raw)
	}
}

func TestReadStatusSnapshotFilteredRejectsInvalidModesBeforeSQL(t *testing.T) {
	t.Parallel()
	for _, selection := range []statuspkg.SnapshotSelection{
		{Mode: "unknown"},
		{Mode: statuspkg.SnapshotModeSemanticOnly, IncludeRegistryCollectors: true},
		{Mode: statuspkg.SnapshotModeSemanticOnly, IncludeCollectorFactEvidence: true},
	} {
		queryer := &recordingQueryer{}
		_, err := NewStatusStore(queryer).ReadStatusSnapshotFiltered(context.Background(), time.Now(), selection)
		if err == nil || len(queryer.queries) != 0 {
			t.Fatalf("selection %+v: err=%v queries=%d; want fail closed before SQL", selection, err, len(queryer.queries))
		}
	}
}
