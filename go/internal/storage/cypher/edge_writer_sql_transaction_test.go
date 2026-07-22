// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

type recordingGroupExecutor struct {
	executeCalls []Statement
	groupCalls   [][]Statement
}

func (r *recordingGroupExecutor) Execute(_ context.Context, stmt Statement) error {
	r.executeCalls = append(r.executeCalls, stmt)
	return nil
}

func (r *recordingGroupExecutor) ExecuteGroup(_ context.Context, stmts []Statement) error {
	cloned := make([]Statement, len(stmts))
	copy(cloned, stmts)
	r.groupCalls = append(r.groupCalls, cloned)
	return nil
}

func TestEdgeWriterSQLRelationshipSequentialWritesBypassManagedTransactions(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewEdgeWriter(executor, 0)
	writer.SQLRelationshipSequentialWrites = true
	rows := []reducer.SharedProjectionIntentRow{{
		IntentID:     "sql-edge",
		RepositoryID: "repo-a",
		Payload: map[string]any{
			"source_entity_id":   "sql:function",
			"source_entity_type": "SqlFunction",
			"target_entity_id":   "sql:table",
			"target_entity_type": "SqlTable",
			"relationship_type":  "WRITES_TO",
		},
	}}

	if err := writer.WriteEdges(context.Background(), reducer.DomainSQLRelationships, rows, "test"); err != nil {
		t.Fatalf("WriteEdges: %v", err)
	}
	if len(executor.groupCalls) != 0 {
		t.Fatalf("ExecuteGroup calls = %d, want 0", len(executor.groupCalls))
	}
	if len(executor.executeCalls) != 1 {
		t.Fatalf("Execute calls = %d, want 1", len(executor.executeCalls))
	}
}

func TestEdgeWriterSQLRelationshipSequentialWritesPreserveWorkerConcurrency(t *testing.T) {
	t.Parallel()

	const writers = 2
	probe := &concurrencyProbeExecutor{release: make(chan struct{})}
	writer := NewEdgeWriter(probe, 0)
	writer.SQLRelationshipSequentialWrites = true
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		go func(i int) {
			row := reducer.SharedProjectionIntentRow{
				IntentID:     "sql-edge",
				RepositoryID: "repo-a",
				Payload: map[string]any{
					"source_entity_id":   "sql:function:" + string(rune('a'+i)),
					"source_entity_type": "SqlFunction",
					"target_entity_id":   "sql:table",
					"target_entity_type": "SqlTable",
					"relationship_type":  "WRITES_TO",
				},
			}
			errs <- writer.WriteEdges(context.Background(), reducer.DomainSQLRelationships, []reducer.SharedProjectionIntentRow{row}, "test")
		}(i)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && probe.currentConcurrency() < writers {
		time.Sleep(time.Millisecond)
	}
	if got := probe.currentConcurrency(); got != writers {
		close(probe.release)
		t.Fatalf("concurrent auto-commit writes = %d, want %d", got, writers)
	}
	close(probe.release)
	for range writers {
		if err := <-errs; err != nil {
			t.Fatalf("WriteEdges: %v", err)
		}
	}
	if got := probe.peakConcurrency(); got != writers {
		t.Fatalf("peak concurrent auto-commit writes = %d, want %d", got, writers)
	}
}

func TestEdgeWriterSQLRelationshipDefaultUsesManagedTransactions(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewEdgeWriter(executor, 0)
	rows := []reducer.SharedProjectionIntentRow{{
		IntentID:     "sql-edge",
		RepositoryID: "repo-a",
		Payload: map[string]any{
			"source_entity_id":   "sql:function",
			"source_entity_type": "SqlFunction",
			"target_entity_id":   "sql:table",
			"target_entity_type": "SqlTable",
			"relationship_type":  "WRITES_TO",
		},
	}}

	if err := writer.WriteEdges(context.Background(), reducer.DomainSQLRelationships, rows, "test"); err != nil {
		t.Fatalf("WriteEdges: %v", err)
	}
	if len(executor.executeCalls) != 0 {
		t.Fatalf("Execute calls = %d, want 0", len(executor.executeCalls))
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("ExecuteGroup calls = %d, want 1", len(executor.groupCalls))
	}
}
