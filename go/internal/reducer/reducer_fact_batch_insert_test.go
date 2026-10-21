// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

// TestReducerBatchInsertFactsChunksByBatchSize proves the insert is bounded:
// rows are split into ceil(N/factwrite.BatchSize) statements, each carrying at
// most factwrite.BatchSize rows. This is the property that keeps a single large
// scope from emitting a single statement with an unbounded parameter count
// (Postgres caps bind parameters at 65535).
func TestReducerBatchInsertFactsChunksByBatchSize(t *testing.T) {
	t.Parallel()

	const rowCount = factwrite.BatchSize*2 + 7
	rows := make([]factwrite.Row, rowCount)
	now := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	for i := range rows {
		rows[i] = factwrite.Row{
			FactID:           fmt.Sprintf("fact-%d", i),
			ScopeID:          "scope",
			GenerationID:     "gen",
			FactKind:         "reducer_test_fact",
			StableFactKey:    fmt.Sprintf("key-%d", i),
			CollectorKind:    "test",
			SourceConfidence: "inferred",
			SourceSystem:     "test",
			SourceFactKey:    "intent",
			ObservedAt:       now,
			IngestedAt:       now,
			Payload:          "{}",
		}
	}

	db := &fakeWorkloadIdentityExecer{}
	if err := factwrite.BatchInsertFacts(context.Background(), db, rows); err != nil {
		t.Fatalf("factwrite.BatchInsertFacts() error = %v", err)
	}

	if got, want := len(db.execs), 3; got != want {
		t.Fatalf("ExecContext calls = %d, want %d (chunked by batch size)", got, want)
	}
	for i, call := range db.execs {
		factIDs, ok := call.args[0].([]string)
		if !ok {
			t.Fatalf("exec %d fact_id arg type = %T, want []string", i, call.args[0])
		}
		if len(factIDs) > factwrite.BatchSize {
			t.Fatalf("exec %d carried %d rows, want <= %d", i, len(factIDs), factwrite.BatchSize)
		}
	}

	decoded := decodeBatchedFactCalls(t, db.execs)
	if len(decoded) != rowCount {
		t.Fatalf("decoded rows = %d, want %d", len(decoded), rowCount)
	}
	// Order and identity must be preserved across chunk boundaries.
	for i, row := range decoded {
		if got, want := row.FactID, fmt.Sprintf("fact-%d", i); got != want {
			t.Fatalf("decoded row %d fact_id = %q, want %q", i, got, want)
		}
	}
}

// TestReducerBatchInsertFactsEmptyIssuesNoStatements confirms an empty scope
// issues zero round-trips rather than an empty unnest statement.
func TestReducerBatchInsertFactsEmptyIssuesNoStatements(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	if err := factwrite.BatchInsertFacts(context.Background(), db, nil); err != nil {
		t.Fatalf("factwrite.BatchInsertFacts() error = %v", err)
	}
	if got := len(db.execs); got != 0 {
		t.Fatalf("ExecContext calls = %d, want 0 for empty rows", got)
	}
}
