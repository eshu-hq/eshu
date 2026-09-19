// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package wait

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type recordedCall struct {
	query string
	args  []any
}

// fakeDB records every statement and answers reads from rows.
type fakeDB struct {
	calls []recordedCall
	rows  [][]any
	err   error
}

func (f *fakeDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	f.calls = append(f.calls, recordedCall{query, args})
	if f.err != nil {
		return nil, f.err
	}
	return &fakeRows{rows: f.rows}, nil
}

func (f *fakeDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	f.calls = append(f.calls, recordedCall{query, args})
	return nil, f.err
}

type fakeRows struct {
	rows [][]any
	next int
}

func (r *fakeRows) Next() bool { r.next++; return r.next <= len(r.rows) }
func (r *fakeRows) Err() error { return nil }
func (r *fakeRows) Close() error {
	return nil
}

func (r *fakeRows) Scan(dest ...any) error {
	row := r.rows[r.next-1]
	for i, value := range row {
		switch target := dest[i].(type) {
		case *string:
			*target = value.(string)
		case *int:
			*target = value.(int)
		case *int64:
			*target = value.(int64)
		case *[]byte:
			*target = value.([]byte)
		case *sql.NullTime:
			if value == nil {
				*target = sql.NullTime{}
			} else {
				*target = sql.NullTime{Time: value.(time.Time), Valid: true}
			}
		case *time.Time:
			*target = value.(time.Time)
		default:
			return errors.New("unexpected scan target")
		}
	}
	return nil
}

var storeTestT0 = time.Date(2026, time.September, 19, 9, 0, 0, 0, time.UTC)

func TestStoreUpsertBindsRowAndAnchorEpoch(t *testing.T) {
	t.Parallel()
	fake := &fakeDB{}
	store := Store{DB: fake}
	wait := crossscope.ReadinessWait{
		ScopeID: "aws:1:us-east-1:iam", Domain: reducercontract.DomainIAMCanPerformMaterialization,
		FirstDeferredAt: storeTestT0, MissingKeys: []string{"arn:a"}, MissingCount: 1,
		MissingFingerprint: "fp", CommittedGenerationID: "gen-1", CommittedFingerprint: "fp",
		UpdatedAt: storeTestT0, AnchorEpoch: 3,
	}
	if err := store.UpsertReadinessWait(context.Background(), wait); err != nil {
		t.Fatalf("UpsertReadinessWait() error = %v", err)
	}
	if len(fake.calls) != 1 || fake.calls[0].query != upsertReadinessWaitQuery {
		t.Fatalf("calls = %+v, want one upsert", fake.calls)
	}
	args := fake.calls[0].args
	if args[0] != wait.ScopeID || args[1] != string(wait.Domain) || args[3] != `["arn:a"]` {
		t.Fatalf("bound identity/keys = %v, want scope, domain, JSON keys", args[:4])
	}
	if args[7] != nil || args[9] != nil {
		t.Fatalf("zero committed cycle / settled_at bound as %v / %v, want SQL NULL", args[7], args[9])
	}
	if args[11] != int64(3) {
		t.Fatalf("anchor epoch arg = %v, want 3", args[11])
	}
}

func TestStoreClearBindsReadEpochAndCommitMarker(t *testing.T) {
	t.Parallel()
	fake := &fakeDB{}
	store := Store{DB: fake}
	wait := crossscope.ReadinessWait{
		ScopeID: "aws:1:us-east-1:iam", Domain: reducercontract.DomainIAMCanPerformMaterialization,
		AnchorEpoch: 2, CommittedGenerationID: "gen-2", ClearedAt: storeTestT0,
	}
	if err := store.ClearReadinessWait(context.Background(), wait); err != nil {
		t.Fatalf("ClearReadinessWait() error = %v", err)
	}
	if len(fake.calls) != 1 || fake.calls[0].query != clearReadinessWaitQuery {
		t.Fatalf("calls = %+v, want one clear", fake.calls)
	}
	args := fake.calls[0].args
	if args[2] != int64(2) || args[3] != "gen-2" || args[4] != nil || args[5] != storeTestT0 {
		t.Fatalf("clear args = %v, want read epoch 2, marker gen-2, NULL cycle, cleared_at", args)
	}
	if err := store.ClearReadinessWait(context.Background(), crossscope.ReadinessWait{ScopeID: "s"}); err == nil {
		t.Fatal("ClearReadinessWait() without a time error = nil, want an error")
	}
}

func TestStoreGetDecodesRowAndMissingRow(t *testing.T) {
	t.Parallel()
	fake := &fakeDB{rows: [][]any{{
		storeTestT0, []byte(`["arn:a","arn:b"]`), 2, "fp", "gen-1", storeTestT0.Add(-time.Minute), "fp",
		nil, storeTestT0, int64(4), storeTestT0,
	}}}
	store := Store{DB: fake}
	got, found, err := store.GetReadinessWait(context.Background(), "aws:1:us-east-1:iam", reducercontract.DomainIAMCanPerformMaterialization)
	if err != nil || !found {
		t.Fatalf("GetReadinessWait() = found %v, err %v; want a row", found, err)
	}
	if len(got.MissingKeys) != 2 || got.MissingCount != 2 || got.CommittedGenerationID != "gen-1" ||
		!got.CommittedCycleStartedAt.Equal(storeTestT0.Add(-time.Minute)) || got.Settled() ||
		got.AnchorEpoch != 4 || !got.Cleared() ||
		got.ScopeID != "aws:1:us-east-1:iam" || got.Domain != reducercontract.DomainIAMCanPerformMaterialization {
		t.Fatalf("decoded row = %+v", got)
	}

	empty := Store{DB: &fakeDB{}}
	if _, found, err := empty.GetReadinessWait(context.Background(), "s", "d"); err != nil || found {
		t.Fatalf("absent row: found %v, err %v; want not found, nil error", found, err)
	}
}

func TestStoreSurfacesDatabaseErrors(t *testing.T) {
	t.Parallel()
	store := Store{DB: &fakeDB{err: errors.New("boom")}}
	if _, _, err := store.GetReadinessWait(context.Background(), "s", "d"); err == nil {
		t.Fatal("GetReadinessWait() error = nil, want the database error, never a missing row")
	}
	if err := store.ClearReadinessWait(context.Background(), crossscope.ReadinessWait{ScopeID: "s", UpdatedAt: storeTestT0}); err == nil {
		t.Fatal("ClearReadinessWait() error = nil, want the database error")
	}
	if err := (Store{}).UpsertReadinessWait(context.Background(), crossscope.ReadinessWait{}); err == nil {
		t.Fatal("UpsertReadinessWait() with nil DB error = nil, want an error")
	}
}
