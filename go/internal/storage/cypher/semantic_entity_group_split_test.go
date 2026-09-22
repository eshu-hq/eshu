// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/semantic"
)

// cappedGroupExecutor simulates a graph backend that rejects any grouped
// transaction holding more than maxRows parameter rows in a single request
// (NornicDB: "Txn is too big to fit into one request").
type cappedGroupExecutor struct {
	maxRows int
	groups  [][]Statement
	failOn  int
	failErr error
	calls   int
}

func (e *cappedGroupExecutor) Execute(_ context.Context, stmt Statement) error {
	e.groups = append(e.groups, []Statement{stmt})
	return nil
}

func (e *cappedGroupExecutor) ExecuteGroup(_ context.Context, stmts []Statement) error {
	e.calls++
	total := 0
	for _, stmt := range stmts {
		total += semanticStatementRowCount(stmt)
	}
	if total > e.maxRows {
		return errors.New("txn is too big to fit into one request")
	}
	if e.failOn >= 0 && e.calls > e.failOn {
		return e.failErr
	}
	e.groups = append(e.groups, stmts)
	return nil
}

func semanticFunctionRows(repo string, count int) []semantic.EntityRow {
	rows := make([]semantic.EntityRow, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, semantic.EntityRow{
			RepoID:     repo,
			EntityID:   "function-x",
			EntityType: "Function",
			EntityName: "run",
			FilePath:   "/repo/src/run.js",
			Language:   "javascript",
			StartLine:  10,
			EndLine:    12,
		})
	}
	return rows
}

// A write whose rows exceed one backend request must dispatch as sequential
// bounded groups instead of a single grouped transaction.
func TestSemanticEntityWriterSplitsOversizedWriteIntoBoundedGroups(t *testing.T) {
	t.Parallel()

	executor := &cappedGroupExecutor{maxRows: 4, failOn: -1}
	writer := NewSemanticEntityWriter(executor, 2).WithMaxGroupRows(3)

	result, err := writer.WriteSemanticEntities(context.Background(), semantic.EntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows:    semanticFunctionRows("repo-1", 8),
	})
	if err != nil {
		t.Fatalf("split write failed: %v", err)
	}
	if len(executor.groups) < 2 {
		t.Fatalf("expected multiple groups, got %d", len(executor.groups))
	}
	for i, group := range executor.groups {
		total := 0
		for _, stmt := range group {
			total += semanticStatementRowCount(stmt)
		}
		if total > 3 {
			t.Fatalf("group %d holds %d rows, over the 3-row limit", i, total)
		}
	}
	delivered := 0
	for _, group := range executor.groups {
		for _, stmt := range group {
			delivered += semanticStatementRowCount(stmt)
		}
	}
	if delivered != 8 {
		t.Fatalf("expected all 8 rows delivered across groups, got %d", delivered)
	}
	if result.Groups != len(executor.groups) {
		t.Fatalf("result reports %d groups, %d dispatched", result.Groups, len(executor.groups))
	}
	// Retract statements lead the first group so a retry replays the same
	// scoped retract-then-upsert order as the single-transaction path.
	first := executor.groups[0][0]
	if first.Operation != OperationCanonicalRetract {
		t.Fatalf("first statement is %q, expected the retract", first.Operation)
	}
}

// A write that fits one backend request keeps the single atomic group.
func TestSemanticEntityWriterKeepsSmallWriteInOneGroup(t *testing.T) {
	t.Parallel()

	executor := &cappedGroupExecutor{maxRows: 100, failOn: -1}
	writer := NewSemanticEntityWriter(executor, 2).WithMaxGroupRows(50)

	_, err := writer.WriteSemanticEntities(context.Background(), semantic.EntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows:    semanticFunctionRows("repo-1", 4),
	})
	if err != nil {
		t.Fatalf("small write failed: %v", err)
	}
	if len(executor.groups) != 1 {
		t.Fatalf("expected a single group, got %d", len(executor.groups))
	}
}

// semanticStatementRowCount drives the grouped-transaction row cap: UNWIND
// rows count by length, retracts (repo ID lists, not rows) count zero, and
// singleton-parameterized upserts carry one row each without a "rows" key.
func TestSemanticStatementRowCount(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		stmt Statement
		want int
	}{
		{
			name: "unwind rows",
			stmt: Statement{Operation: OperationCanonicalUpsert, Parameters: map[string]any{"rows": []map[string]any{{}, {}, {}}}},
			want: 3,
		},
		{
			name: "generic slice rows",
			stmt: Statement{Operation: OperationCanonicalUpsert, Parameters: map[string]any{"rows": []any{"a", "b"}}},
			want: 2,
		},
		{
			name: "retract counts zero",
			stmt: Statement{Operation: OperationCanonicalRetract, Parameters: map[string]any{"repo_ids": []string{"repo-1"}}},
			want: 0,
		},
		{
			name: "singleton upsert counts one",
			stmt: Statement{Operation: OperationCanonicalUpsert, Parameters: map[string]any{"entity_id": "e", "properties": map[string]any{}}},
			want: 1,
		},
		{
			name: "unexpected rows type counts zero",
			stmt: Statement{Operation: OperationCanonicalUpsert, Parameters: map[string]any{"rows": "not-a-slice"}},
			want: 0,
		},
		{
			name: "other slice types count by length",
			stmt: Statement{Operation: OperationCanonicalUpsert, Parameters: map[string]any{"rows": []string{"a", "b", "c"}}},
			want: 3,
		},
	}
	for _, tc := range cases {
		if got := semanticStatementRowCount(tc.stmt); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}

// Singleton-parameterized upserts carry no "rows" key, so without per-row
// counting an oversized write in that mode would pack into a single group.
func TestSemanticEntityWriterSplitsSingletonParameterizedWrite(t *testing.T) {
	t.Parallel()

	executor := &cappedGroupExecutor{maxRows: 4, failOn: -1}
	writer := NewSemanticEntityWriterWithParameterizedRows(executor, 2).WithMaxGroupRows(3)

	result, err := writer.WriteSemanticEntities(context.Background(), semantic.EntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows:    semanticFunctionRows("repo-1", 8),
	})
	if err != nil {
		t.Fatalf("split write failed: %v", err)
	}
	if len(executor.groups) < 2 {
		t.Fatalf("expected multiple groups, got %d", len(executor.groups))
	}
	for i, group := range executor.groups {
		total := 0
		for _, stmt := range group {
			total += semanticStatementRowCount(stmt)
		}
		if total > 3 {
			t.Fatalf("group %d holds %d rows, over the 3-row limit", i, total)
		}
	}
	delivered := 0
	for _, group := range executor.groups {
		for _, stmt := range group {
			if stmt.Operation == OperationCanonicalRetract {
				continue
			}
			delivered += semanticStatementRowCount(stmt)
		}
	}
	if delivered != 8 {
		t.Fatalf("expected all 8 rows delivered across groups, got %d", delivered)
	}
	if result.Groups != len(executor.groups) {
		t.Fatalf("result reports %d groups, %d dispatched", result.Groups, len(executor.groups))
	}
}

// A failing later group aborts the write with the backend error.
func TestSemanticEntityWriterAbortsWhenAGroupFails(t *testing.T) {
	t.Parallel()

	boom := errors.New("backend unavailable")
	executor := &cappedGroupExecutor{maxRows: 1000, failOn: 1, failErr: boom}
	writer := NewSemanticEntityWriter(executor, 2).WithMaxGroupRows(3)

	_, err := writer.WriteSemanticEntities(context.Background(), semantic.EntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows:    semanticFunctionRows("repo-1", 8),
	})
	if err == nil || !strings.Contains(err.Error(), boom.Error()) {
		t.Fatalf("expected the backend error, got %v", err)
	}
}
