// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// scriptedExecQueryer serves canned query results in order and records exec calls.
type scriptedExecQueryer struct {
	queries   []*querytestutil.ScriptedRows
	queryIdx  int
	execQuery string
	execArgs  []any
}

func (s *scriptedExecQueryer) QueryContext(_ context.Context, _ string, _ ...any) (db.Rows, error) {
	if s.queryIdx >= len(s.queries) {
		return &querytestutil.ScriptedRows{}, nil
	}
	rows := s.queries[s.queryIdx]
	s.queryIdx++
	return rows, nil
}

func (s *scriptedExecQueryer) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	s.execQuery = query
	s.execArgs = args
	return sqlResultStub{}, nil
}

type sqlResultStub struct{}

func (sqlResultStub) LastInsertId() (int64, error) { return 0, nil }
func (sqlResultStub) RowsAffected() (int64, error) { return 1, nil }

func TestClaimReplayIdempotencyWinsOnInsert(t *testing.T) {
	database := &scriptedExecQueryer{queries: []*querytestutil.ScriptedRows{
		{Data: [][]any{{"k1"}}}, // INSERT ... RETURNING returned a row → claimed
	}}
	store := &postgresStore{database: database, now: func() time.Time { return time.Unix(0, 0).UTC() }}

	claim, err := store.ClaimReplayIdempotency(context.Background(), "k1", "fp", time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatalf("ClaimReplayIdempotency() error = %v", err)
	}
	if !claim.Claimed {
		t.Fatalf("expected Claimed=true when INSERT wins, got %+v", claim)
	}
}

func TestClaimReplayIdempotencyReturnsPriorOnConflict(t *testing.T) {
	database := &scriptedExecQueryer{queries: []*querytestutil.ScriptedRows{
		{Data: nil}, // INSERT conflicted → no row
		{Data: [][]any{{"fp", admin.ReplayRequestStatusCompleted, 2, []byte(`["a","b"]`)}}}, // SELECT prior
	}}
	store := &postgresStore{database: database, now: func() time.Time { return time.Unix(0, 0).UTC() }}

	claim, err := store.ClaimReplayIdempotency(context.Background(), "k1", "fp", time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatalf("ClaimReplayIdempotency() error = %v", err)
	}
	if claim.Claimed {
		t.Fatalf("expected Claimed=false on conflict, got %+v", claim)
	}
	if claim.Status != admin.ReplayRequestStatusCompleted || claim.Fingerprint != "fp" || claim.ReplayedCount != 2 {
		t.Fatalf("prior outcome not returned: %+v", claim)
	}
	if len(claim.WorkItemIDs) != 2 || claim.WorkItemIDs[0] != "a" {
		t.Fatalf("prior work item ids not decoded: %+v", claim.WorkItemIDs)
	}
}

func TestCompleteReplayIdempotencyEncodesIDsAndGuardsInProgress(t *testing.T) {
	database := &scriptedExecQueryer{}
	store := &postgresStore{database: database, now: func() time.Time { return time.Unix(0, 0).UTC() }}

	if err := store.CompleteReplayIdempotency(context.Background(), "k1", 2, []string{"a", "b"}, time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("CompleteReplayIdempotency() error = %v", err)
	}
	if !strings.Contains(database.execQuery, "WHERE idempotency_key = $1") || !strings.Contains(database.execQuery, "AND status = $6") {
		t.Fatalf("update must guard on key and in-progress status: %s", database.execQuery)
	}
	if len(database.execArgs) != 6 {
		t.Fatalf("expected 6 args, got %d: %v", len(database.execArgs), database.execArgs)
	}
	encoded, ok := database.execArgs[3].([]byte)
	if !ok || string(encoded) != `["a","b"]` {
		t.Fatalf("work_item_ids arg = %v, want JSON [\"a\",\"b\"]", database.execArgs[3])
	}
	if database.execArgs[5] != admin.ReplayRequestStatusInProgress {
		t.Fatalf("guard arg = %v, want in_progress", database.execArgs[5])
	}
}
