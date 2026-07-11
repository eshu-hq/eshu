// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

func TestEnsureContentSearchIndexesPublishesReadyAfterExactBuildAndAnalyze(t *testing.T) {
	t.Parallel()

	exec := &contentSearchIndexScriptExecutor{rowsAffected: []int64{1, 1, 1, 1, 1, 1, 1, 1}}
	if err := EnsureContentSearchIndexes(context.Background(), exec); err != nil {
		t.Fatalf("EnsureContentSearchIndexes() error = %v, want nil", err)
	}

	wantOrder := []string{
		"pg_advisory_xact_lock",
		"state = 'building'",
		"pg_advisory_xact_lock",
		"state = 'building'",
		"CREATE INDEX IF NOT EXISTS content_files_content_trgm_idx",
		"CREATE INDEX IF NOT EXISTS content_entities_source_trgm_idx",
		"ANALYZE content_files",
		"state = 'ready'",
	}
	if len(exec.statements) != len(wantOrder) {
		t.Fatalf("statement count = %d, want %d: %v", len(exec.statements), len(wantOrder), exec.statements)
	}
	for i, want := range wantOrder {
		if !strings.Contains(exec.statements[i], want) {
			t.Fatalf("statement[%d] = %q, want substring %q", i, exec.statements[i], want)
		}
	}
	if !strings.Contains(exec.statements[7], "eshu_content_substring_indexes_valid()") {
		t.Fatalf("ready publication does not verify exact indexes: %q", exec.statements[7])
	}
}

func TestEnsureContentSearchIndexesReadyStateIsNoOp(t *testing.T) {
	t.Parallel()

	exec := &contentSearchIndexScriptExecutor{rowsAffected: []int64{1, 0}}
	if err := EnsureContentSearchIndexes(context.Background(), exec); err != nil {
		t.Fatalf("EnsureContentSearchIndexes() error = %v, want nil", err)
	}
	if len(exec.statements) != 2 {
		t.Fatalf("statement count = %d, want lock + readiness claim only", len(exec.statements))
	}
}

func TestEnsureContentSearchIndexesFailurePublishesFailedForRestart(t *testing.T) {
	t.Parallel()

	buildErr := errors.New("build failed")
	exec := &contentSearchIndexScriptExecutor{
		rowsAffected: []int64{1, 1, 1, 1, 1, 1, 1},
		failAt:       4,
		failErr:      buildErr,
	}
	err := EnsureContentSearchIndexes(context.Background(), exec)
	if !errors.Is(err, buildErr) {
		t.Fatalf("EnsureContentSearchIndexes() error = %v, want wrapping %v", err, buildErr)
	}
	if len(exec.statements) != 7 {
		t.Fatalf("statement count = %d, want locked build plus locked failure publication", len(exec.statements))
	}
	if !strings.Contains(exec.statements[6], "state = 'failed'") {
		t.Fatalf("failure publication = %q, want failed lifecycle state", exec.statements[6])
	}
}

type contentSearchIndexScriptExecutor struct {
	statements   []string
	rowsAffected []int64
	failAt       int
	failErr      error
}

func (e *contentSearchIndexScriptExecutor) Begin(context.Context) (Transaction, error) {
	return e, nil
}

func (e *contentSearchIndexScriptExecutor) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, errors.New("unexpected query")
}

func (e *contentSearchIndexScriptExecutor) Commit() error { return nil }

func (e *contentSearchIndexScriptExecutor) Rollback() error { return nil }

func (e *contentSearchIndexScriptExecutor) ExecContext(_ context.Context, statement string, _ ...any) (sql.Result, error) {
	index := len(e.statements)
	e.statements = append(e.statements, statement)
	if e.failErr != nil && index == e.failAt {
		return nil, e.failErr
	}
	affected := int64(1)
	if index < len(e.rowsAffected) {
		affected = e.rowsAffected[index]
	}
	return contentSearchIndexResult(affected), nil
}

type contentSearchIndexResult int64

func (r contentSearchIndexResult) LastInsertId() (int64, error) { return 0, nil }

func (r contentSearchIndexResult) RowsAffected() (int64, error) { return int64(r), nil }
