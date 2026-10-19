// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// retirementFakeDB is a canned ExecQueryer: ExecContext records its query
// and reports one inserted row; QueryContext records its query and returns
// the canned rows in order.
type retirementFakeDB struct {
	execQueries  []string
	queryQueries []string
	queryRows    [][]any
	queryErr     error
}

func (f *retirementFakeDB) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	f.execQueries = append(f.execQueries, query)
	return retirementFakeResult{}, nil
}

func (f *retirementFakeDB) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	f.queryQueries = append(f.queryQueries, query)
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return &retirementFakeRows{rows: f.queryRows}, nil
}

type retirementFakeResult struct{}

func (retirementFakeResult) LastInsertId() (int64, error) { return 0, nil }
func (retirementFakeResult) RowsAffected() (int64, error) { return 1, nil }

type retirementFakeRows struct {
	rows [][]any
	pos  int
}

func (r *retirementFakeRows) Next() bool {
	if r.pos >= len(r.rows) {
		return false
	}
	r.pos++
	return true
}

func (r *retirementFakeRows) Scan(dest ...any) error {
	row := r.rows[r.pos-1]
	if len(dest) != len(row) {
		return sql.ErrNoRows
	}
	for i, v := range row {
		switch d := dest[i].(type) {
		case *string:
			*d = v.(string)
		case *int64:
			*d = v.(int64)
		case *int:
			*d = v.(int)
		case **string:
			if v == nil {
				*d = nil
			} else {
				s := v.(string)
				*d = &s
			}
		case *time.Time:
			*d = v.(time.Time)
		case **time.Time:
			if v == nil {
				*d = nil
			} else {
				ts := v.(time.Time)
				*d = &ts
			}
		}
	}
	return nil
}

func (r *retirementFakeRows) Err() error   { return nil }
func (r *retirementFakeRows) Close() error { return nil }

func retirementTestRequest() RetirementRequest {
	return RetirementRequest{
		RetirementID:       "rr_test01",
		RepoID:             "git-repo-scope:test",
		ScopeID:            "git-repo-scope:test@main",
		ReasonCode:         RetirementReasonOperatorRetired,
		ReasonHash:         "reason-hash",
		ActorClass:         "operator",
		IdempotencyKeyHash: "key-hash",
	}
}

// TestInsertRetirementPendingWritesTombstone is the #7766 contract: the first
// mark opens a pending/fenced tombstone row idempotently on the operator's
// idempotency key.
func TestInsertRetirementPendingWritesTombstone(t *testing.T) {
	t.Parallel()

	fake := &retirementFakeDB{
		queryRows: [][]any{{
			"rr_test01", "git-repo-scope:test", "git-repo-scope:test@main",
			"pending", "fenced",
		}},
	}
	store := NewRetirementStore(fake)
	rec, err := store.InsertPending(context.Background(), retirementTestRequest())
	if err != nil {
		t.Fatalf("InsertPending: %v", err)
	}
	if rec.State != RetirementStatePending || rec.Phase != RetirementPhaseFenced {
		t.Fatalf("record = %s/%s, want pending/fenced", rec.State, rec.Phase)
	}
	if len(fake.queryQueries) != 1 {
		t.Fatalf("queries = %d, want exactly the single insert", len(fake.queryQueries))
	}
	insert := fake.queryQueries[0]
	for _, want := range []string{
		"INSERT INTO repository_retirements",
		"ON CONFLICT (repo_id, idempotency_key_hash)",
		"WHERE readmitted_at IS NULL",
	} {
		if !strings.Contains(insert, want) {
			t.Fatalf("insert missing %q:\n%s", want, insert)
		}
	}
}

// TestInsertRetirementPendingRejectsEmptyInputs proves the store fails closed
// before touching the database: no tombstone may open without its identity.
func TestInsertRetirementPendingRejectsEmptyInputs(t *testing.T) {
	t.Parallel()

	for _, mutate := range []func(*RetirementRequest){
		func(r *RetirementRequest) { r.RepoID = "" },
		func(r *RetirementRequest) { r.ScopeID = "" },
		func(r *RetirementRequest) { r.IdempotencyKeyHash = "" },
		func(r *RetirementRequest) { r.ReasonCode = "other" },
	} {
		req := retirementTestRequest()
		mutate(&req)
		fake := &retirementFakeDB{}
		if _, err := NewRetirementStore(fake).InsertPending(context.Background(), req); err == nil {
			t.Fatalf("InsertPending(%+v) = nil error, want refusal", req)
		}
		if len(fake.queryQueries) != 0 {
			t.Fatalf("refused request issued %d queries, want none", len(fake.queryQueries))
		}
	}
}

// TestGetOpenRetirementByRepo proves the gate lookup reads exactly the open
// row the partial index serves.
func TestGetOpenRetirementByRepo(t *testing.T) {
	t.Parallel()

	fake := &retirementFakeDB{
		queryRows: [][]any{{
			"rr_test01", "git-repo-scope:test", "git-repo-scope:test@main",
			"blocked", "barrier",
		}},
	}
	rec, err := NewRetirementStore(fake).GetOpen(context.Background(), "git-repo-scope:test")
	if err != nil {
		t.Fatalf("GetOpen: %v", err)
	}
	if rec == nil || rec.State != RetirementStateBlocked {
		t.Fatalf("record = %+v, want blocked tombstone", rec)
	}
	if len(fake.queryQueries) != 1 {
		t.Fatalf("queries = %d, want exactly the lookup", len(fake.queryQueries))
	}
	lookup := fake.queryQueries[0]
	for _, want := range []string{"FROM repository_retirements", "readmitted_at IS NULL", "repo_id = $1"} {
		if !strings.Contains(lookup, want) {
			t.Fatalf("lookup missing %q:\n%s", want, lookup)
		}
	}
}

// TestPreviewRetirementCounts proves the dry-run counts every purge input the
// tombstone's WARN log and rows_deleted ledger must later account for.
func TestPreviewRetirementCounts(t *testing.T) {
	t.Parallel()

	fake := &retirementFakeDB{
		queryRows: [][]any{{int64(3), int64(41), int64(17), int64(9)}},
	}
	preview, err := NewRetirementStore(fake).Preview(
		context.Background(), "git-repo-scope:test", []string{"git-repo-scope:test@main"},
	)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if preview.Generations != 3 || preview.FactWorkItems != 41 ||
		preview.ContentFiles != 17 || preview.SharedIntents != 9 {
		t.Fatalf("preview = %+v, want 3/41/17/9", preview)
	}
	lookup := fake.queryQueries[0]
	for _, want := range []string{
		"scope_generations", "fact_work_items", "content_files", "shared_projection_intents",
	} {
		if !strings.Contains(lookup, want) {
			t.Fatalf("preview missing %q:\n%s", want, lookup)
		}
	}
}

// TestRetirementTombstoneRoundTripLive proves the tombstone against real
// Postgres: insert opens pending/fenced, a repeated key returns the same row
// without a second row, and a second key for the same open repo is refused.
func TestRetirementTombstoneRoundTripLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_tombstone",
		"set ESHU_POSTGRES_DSN to run the retirement tombstone Postgres proof")
	store := NewRetirementStore(SQLDB{DB: database})

	req := retirementTestRequest()
	first, err := store.InsertPending(ctx, req)
	if err != nil {
		t.Fatalf("InsertPending: %v", err)
	}
	if first.State != RetirementStatePending || first.Phase != RetirementPhaseFenced {
		t.Fatalf("record = %s/%s, want pending/fenced", first.State, first.Phase)
	}

	open, err := store.GetOpen(ctx, req.RepoID)
	if err != nil {
		t.Fatalf("GetOpen: %v", err)
	}
	if open == nil || open.RetirementID != first.RetirementID {
		t.Fatalf("open = %+v, want retirement %s", open, first.RetirementID)
	}

	repeat, err := store.InsertPending(ctx, req)
	if err != nil {
		t.Fatalf("idempotent repeat InsertPending: %v", err)
	}
	if repeat.RetirementID != first.RetirementID {
		t.Fatalf("repeat id = %s, want %s (same row, no duplicate)", repeat.RetirementID, first.RetirementID)
	}

	second := req
	second.IdempotencyKeyHash = "other-key"
	second.RetirementID = "rr_test02"
	if _, err := store.InsertPending(ctx, second); err == nil {
		t.Fatalf("second open retirement for one repo succeeded, want refusal")
	}

	preview, err := store.Preview(ctx, req.RepoID, []string{req.ScopeID})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if preview.Generations != 0 || preview.FactWorkItems != 0 ||
		preview.ContentFiles != 0 || preview.SharedIntents != 0 {
		t.Fatalf("preview on empty scope = %+v, want zeros", preview)
	}
}
