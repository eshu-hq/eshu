// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"sync"
	"testing"
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
