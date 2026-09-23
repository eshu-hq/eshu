// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"context"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// reportingDifferentialExecutor simulates a Bolt seam: it reports summary
// counters through the context the recorder stashed a collector in, proving
// the counters path needs no interface change.
type reportingDifferentialExecutor struct {
	stubDifferentialExecutor
	reports []sourcecypher.WriteCounters
}

func (s *reportingDifferentialExecutor) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	for _, counters := range s.reports {
		sourcecypher.ReportWriteCounts(ctx, stmt.Cypher, stmt.Parameters, counters)
	}
	return s.stubDifferentialExecutor.Execute(ctx, stmt)
}

// reportingDifferentialGroupExecutor is the grouped variant: one ExecuteGroup
// call fans out to per-statement Bolt runs, each reporting its counters.
type reportingDifferentialGroupExecutor struct {
	stubDifferentialGroupExecutor
	perStatement []sourcecypher.WriteCounters
}

func (s *reportingDifferentialGroupExecutor) ExecuteGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	for i, stmt := range stmts {
		if i < len(s.perStatement) {
			sourcecypher.ReportWriteCounts(ctx, stmt.Cypher, stmt.Parameters, s.perStatement[i])
		}
	}
	return s.stubDifferentialGroupExecutor.ExecuteGroup(ctx, stmts)
}

func TestWriteCountersAttachToSingleRecord(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	recorder := NewDifferentialRecorder()
	inner := &reportingDifferentialExecutor{
		reports: []sourcecypher.WriteCounters{{NodesCreated: 2, PropertiesSet: 3}},
	}
	stmt := sourcecypher.Statement{Cypher: "MERGE (n:File {path: $path})", Parameters: map[string]any{"path": "a"}}
	if err := WrapExecutor(inner, recorder, "nornicdb").Execute(context.Background(), stmt); err != nil {
		t.Fatal(err)
	}
	records := recorder.Records()
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	if got := records[0].Counters; got.NodesCreated != 2 || got.PropertiesSet != 3 {
		t.Fatalf("record Counters = %+v, want NodesCreated 2 PropertiesSet 3", got)
	}
}

func TestWriteCountersJoinGroupByFingerprintInOrder(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	recorder := NewDifferentialRecorder()
	inner := &reportingDifferentialGroupExecutor{
		perStatement: []sourcecypher.WriteCounters{
			{NodesCreated: 1},
			{NodesDeleted: 4},
		},
	}
	stmts := []sourcecypher.Statement{
		{Cypher: "MERGE (a:A) RETURN a"},
		{Cypher: "MATCH (b:B) DETACH DELETE b"},
	}
	grouped, ok := WrapExecutor(inner, recorder, "neo4j").(sourcecypher.GroupExecutor)
	if !ok {
		t.Fatal("WrapExecutor did not preserve the group surface")
	}
	if err := grouped.ExecuteGroup(context.Background(), stmts); err != nil {
		t.Fatal(err)
	}
	records := recorder.Records()
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}
	if records[0].Counters.NodesCreated != 1 || records[1].Counters.NodesDeleted != 4 {
		t.Fatalf("records counters = %+v / %+v, want 1 created then 4 deleted",
			records[0].Counters, records[1].Counters)
	}
}

func TestWriteCountersSumRetrySurplusWithoutNewRecords(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	recorder := NewDifferentialRecorder()
	// The recorder wraps above the retry seam, so one logical execution can
	// report twice. Surplus entries sum into the statement's record; they
	// must never create orphan records (execution-count compare stability).
	inner := &reportingDifferentialExecutor{
		reports: []sourcecypher.WriteCounters{{NodesCreated: 1}, {NodesCreated: 1}},
	}
	stmt := sourcecypher.Statement{Cypher: "MERGE (n) RETURN n"}
	if err := WrapExecutor(inner, recorder, "nornicdb").Execute(context.Background(), stmt); err != nil {
		t.Fatal(err)
	}
	records := recorder.Records()
	if len(records) != 1 {
		t.Fatalf("records = %d, want exactly 1 (no orphans)", len(records))
	}
	if got := records[0].Counters.NodesCreated; got != 2 {
		t.Fatalf("record Counters.NodesCreated = %d, want summed 2", got)
	}
}

func TestWriteCountersAbsentWithoutSeamReports(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	recorder := NewDifferentialRecorder()
	stmt := sourcecypher.Statement{Cypher: "MERGE (n) RETURN n"}
	if err := WrapExecutor(&stubDifferentialExecutor{}, recorder, "nornicdb").Execute(context.Background(), stmt); err != nil {
		t.Fatal(err)
	}
	records := recorder.Records()
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1 (execution proof intact)", len(records))
	}
	if got := records[0].Counters; got != (sourcecypher.WriteCounters{}) {
		t.Fatalf("record Counters = %+v, want zero without seam reports", got)
	}
}

func TestCompareRecordingsIgnoresWriteCounters(t *testing.T) {
	t.Parallel()
	fp := DifferentialFingerprint{Statement: "MERGE (n) RETURN n", Parameters: `{}`}
	a := []DifferentialRecord{{Backend: "nornicdb", Fingerprint: fp, Counters: sourcecypher.WriteCounters{NodesCreated: 1}}}
	b := []DifferentialRecord{{Backend: "neo4j", Fingerprint: fp, Counters: sourcecypher.WriteCounters{NodesCreated: 7}}}
	if diffs := CompareRecordings(a, b); len(diffs) != 0 {
		t.Fatalf("differences = %v, want none: write counters must not move the closed slice-3 gate", diffs)
	}
}
