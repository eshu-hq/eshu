// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/node"
)

func TestReportWriteCountsRoundTripThroughContext(t *testing.T) {
	collector := NewWriteCountsCollector()
	ctx := WithWriteCountsCollector(context.Background(), collector)
	params := map[string]any{"repo_id": "r1"}
	ReportWriteCounts(ctx, "MERGE (r:Repository {id: $repo_id})", params, WriteCounters{NodesCreated: 1})
	entries := collector.Entries()
	if len(entries) != 1 {
		t.Fatalf("Entries() count = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Cypher != "MERGE (r:Repository {id: $repo_id})" {
		t.Fatalf("entry Cypher = %q", entry.Cypher)
	}
	if entry.Parameters["repo_id"] != "r1" {
		t.Fatalf("entry Parameters = %v", entry.Parameters)
	}
	if entry.Counters.NodesCreated != 1 {
		t.Fatalf("entry Counters = %+v, want NodesCreated 1", entry.Counters)
	}
}

func TestReportWriteCountsWithoutCollectorIsNoOp(t *testing.T) {
	// No collector in ctx: Bolt seams call this on every statement, so it
	// must never panic or implicitly allocate.
	ReportWriteCounts(context.Background(), "MATCH (n) RETURN n", nil, WriteCounters{})
	if got := WriteCountsCollectorFromContext(context.Background()); got != nil {
		t.Fatalf("WriteCountsCollectorFromContext() = %v, want nil", got)
	}
}

func TestWriteCountsCollectorIsSafeForConcurrentUse(t *testing.T) {
	collector := NewWriteCountsCollector()
	ctx := WithWriteCountsCollector(context.Background(), collector)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ReportWriteCounts(ctx, "MERGE (n) RETURN n", nil, WriteCounters{NodesCreated: 1})
		}()
	}
	wg.Wait()
	if got := len(collector.Entries()); got != 8 {
		t.Fatalf("Entries() count = %d, want 8", got)
	}
}

// stubWriteSummaryCounters is a fake Bolt summary-counter surface with a
// distinct value per field, so the mapping test catches swaps.
type stubWriteSummaryCounters struct{}

func (stubWriteSummaryCounters) NodesCreated() int         { return 1 }
func (stubWriteSummaryCounters) NodesDeleted() int         { return 2 }
func (stubWriteSummaryCounters) RelationshipsCreated() int { return 3 }
func (stubWriteSummaryCounters) RelationshipsDeleted() int { return 4 }
func (stubWriteSummaryCounters) PropertiesSet() int        { return 5 }
func (stubWriteSummaryCounters) LabelsAdded() int          { return 6 }
func (stubWriteSummaryCounters) LabelsRemoved() int        { return 7 }

func TestWriteCountersFromSummaryMapsAllFields(t *testing.T) {
	got := WriteCountersFromSummary(stubWriteSummaryCounters{})
	want := WriteCounters{
		NodesCreated: 1, NodesDeleted: 2,
		RelationshipsCreated: 3, RelationshipsDeleted: 4,
		PropertiesSet: 5, LabelsAdded: 6, LabelsRemoved: 7,
	}
	if got != want {
		t.Fatalf("WriteCountersFromSummary() = %+v, want %+v", got, want)
	}
}

func TestParseDroppedEdgesConvertsDriverRows(t *testing.T) {
	got := ParseDroppedEdges([]any{
		map[string]any{"t": "DEPENDS_ON", "dir": "in", "src": "test/7324"},
		map[string]any{"t": "REPO_CONTAINS", "dir": "out", "src": "projector/canonical"},
		// A missing evidence_source becomes "": the writer classes it other.
		map[string]any{"t": "BUILT_FROM", "dir": "in", "src": nil},
		// Malformed rows are driver-shape drift, not dropped edges.
		"not-a-map",
		map[string]any{"t": "CORRELATES_DEPLOYABLE_UNIT", "src": "test/7324"},
		map[string]any{"dir": "in", "src": "test/7324"},
	})
	want := []DroppedEdge{
		{Direction: "in", Type: "DEPENDS_ON", EvidenceSource: "test/7324"},
		{Direction: "out", Type: "REPO_CONTAINS", EvidenceSource: "projector/canonical"},
		{Direction: "in", Type: "BUILT_FROM", EvidenceSource: ""},
	}
	if len(got) != len(want) {
		t.Fatalf("ParseDroppedEdges() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ParseDroppedEdges()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseDroppedEdgesKeepsEmptyListAttributed(t *testing.T) {
	got := ParseDroppedEdges([]any{})
	if got == nil || len(got) != 0 {
		t.Fatalf("ParseDroppedEdges([]) = %#v, want a non-nil empty slice (an attributed edgeless retirement)", got)
	}
	if got := ParseDroppedEdges(nil); got != nil {
		t.Fatalf("ParseDroppedEdges(nil) = %#v, want nil", got)
	}
	if got := ParseDroppedEdges("dropped"); got != nil {
		t.Fatalf("ParseDroppedEdges(string) = %#v, want nil", got)
	}
}

func TestIsRepositoryPathCleanupCypherMatchesTheConstantOnly(t *testing.T) {
	if !IsRepositoryPathCleanupCypher(node.RepositoryPathCleanupCypher) {
		t.Fatal("IsRepositoryPathCleanupCypher(constant) = false, want true")
	}
	if IsRepositoryPathCleanupCypher("MATCH (r:Repository {path: $path})\nWHERE r.id <> $repo_id\nDETACH DELETE r") {
		t.Fatal("IsRepositoryPathCleanupCypher(old text) = true, want false after the RETURN dropped rewrite")
	}
	if IsRepositoryPathCleanupCypher(node.RepositoryUpsertCypher) {
		t.Fatal("IsRepositoryPathCleanupCypher(upsert) = true, want false")
	}
}

func TestWriteCountersAddSums(t *testing.T) {
	got := WriteCounters{NodesCreated: 1, LabelsRemoved: 2}.Add(WriteCounters{NodesCreated: 3, PropertiesSet: 4})
	want := WriteCounters{NodesCreated: 4, LabelsRemoved: 2, PropertiesSet: 4}
	if got != want {
		t.Fatalf("Add() = %+v, want %+v", got, want)
	}
}

func TestWriteCountsCollectorEntriesSnapshotIsCopy(t *testing.T) {
	collector := NewWriteCountsCollector()
	ctx := WithWriteCountsCollector(context.Background(), collector)
	ReportWriteCounts(ctx, "MATCH (n) RETURN n", nil, WriteCounters{})
	snapshot := collector.Entries()
	snapshot[0].Cypher = "MUTATED"
	if got := collector.Entries()[0].Cypher; got != "MATCH (n) RETURN n" {
		t.Fatalf("collector entry mutated through snapshot: %q", got)
	}
}
