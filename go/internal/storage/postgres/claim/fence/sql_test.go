// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fence

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// scriptedRows serves a fixed result set.
type scriptedRows struct {
	values [][]any
	index  int
}

func (r *scriptedRows) Next() bool { r.index++; return r.index <= len(r.values) }
func (r *scriptedRows) Err() error { return nil }
func (r *scriptedRows) Close() error {
	return nil
}

func (r *scriptedRows) Scan(dest ...any) error {
	row := r.values[r.index-1]
	for i := range dest {
		switch target := dest[i].(type) {
		case *int64:
			*target = row[i].(int64)
		case *string:
			*target = row[i].(string)
		default:
			return errors.New("unsupported scan target")
		}
	}
	return nil
}

// scriptedDB answers the fence upsert and the classification read.
type scriptedDB struct {
	upsertRows [][]any
	readRows   [][]any
	queries    []string
	args       [][]any
}

func (d *scriptedDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	d.queries = append(d.queries, query)
	d.args = append(d.args, args)
	if query == UpsertQuery {
		return &scriptedRows{values: d.upsertRows}, nil
	}
	return &scriptedRows{values: d.readRows}, nil
}

func (d *scriptedDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("fence never execs")
}

func testRow() Row {
	return Row{
		GenerationID: "gen-1", WorkItemID: "item-1", ScopeID: "scope-1",
		ClaimID: "claim-1", FencingToken: 4, CommitID: "commit-claim-1-4",
		CommittedAt: time.Unix(10, 0).UTC(),
	}
}

// TestUpsertQueryIsTheRuledFence pins the D4 statement shape: the PK
// arbiter, strict-greater-or-same-claim-equal predicate, and RETURNING.
func TestUpsertQueryIsTheRuledFence(t *testing.T) {
	for _, fragment := range []string{
		"INSERT INTO generation_commit_fences",
		"ON CONFLICT (generation_id, work_item_id) DO UPDATE SET",
		"WHERE generation_commit_fences.fencing_token < EXCLUDED.fencing_token",
		"OR (generation_commit_fences.fencing_token = EXCLUDED.fencing_token",
		"AND generation_commit_fences.claim_id = EXCLUDED.claim_id)",
		"RETURNING fencing_token",
	} {
		if !strings.Contains(UpsertQuery, fragment) {
			t.Fatalf("UpsertQuery missing %q:\n%s", fragment, UpsertQuery)
		}
	}
}

func TestWriteAcceptsWhenTheUpsertReturnsARow(t *testing.T) {
	database := &scriptedDB{upsertRows: [][]any{{int64(4)}}}
	if err := Write(context.Background(), database, testRow()); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if len(database.queries) != 1 {
		t.Fatalf("queries = %d, want 1 (no classification read on accept)", len(database.queries))
	}
	args := database.args[0]
	if args[0] != "gen-1" || args[1] != "item-1" || args[4] != int64(4) || args[5] != "commit-claim-1-4" {
		t.Fatalf("upsert args = %v", args)
	}
}

func TestWriteClassifiesRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stored []any
		reason Reason
	}{
		{"higher stored token", []any{"claim-9", int64(7)}, ReasonFenceToken},
		{"equal token other claim", []any{"claim-other", int64(4)}, ReasonFenceClaimMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := &scriptedDB{readRows: [][]any{tc.stored}}
			err := Write(context.Background(), database, testRow())
			var rejection *Rejection
			if !errors.As(err, &rejection) || rejection.Reason != tc.reason || !rejection.TerminalFailure() {
				t.Fatalf("Write() error = %v, want terminal %s", err, tc.reason)
			}
			if rejection.RequestedToken != 4 || rejection.StoredToken != tc.stored[1].(int64) {
				t.Fatalf("tokens = (%d, %d)", rejection.RequestedToken, rejection.StoredToken)
			}
		})
	}
}

func TestWriteRefusalWithoutAConflictingRowIsAnIntegrityError(t *testing.T) {
	database := &scriptedDB{}
	err := Write(context.Background(), database, testRow())
	var rejection *Rejection
	if err == nil || errors.As(err, &rejection) {
		t.Fatalf("Write() error = %v, want a plain integrity error", err)
	}
}
