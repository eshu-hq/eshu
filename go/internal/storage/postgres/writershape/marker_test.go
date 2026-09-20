// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writershape

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestSchemaSQL proves the marker DDL carries the columns
// the claim predicate needs.
func TestSchemaSQL(t *testing.T) {
	t.Parallel()

	sql := SchemaSQL()
	for _, want := range []string{
		"graph_writer_shape",
		"shape_key TEXT PRIMARY KEY",
		"applied_version INTEGER NOT NULL",
		"claimed_version INTEGER",
		"claimed_at TIMESTAMPTZ",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("SchemaSQL() missing %q:\n%s", want, sql)
		}
	}
}

// scriptRows is a db.Rows double yielding one scripted row per Next call.
type scriptRows struct {
	rows     [][]any
	position int
}

func (r *scriptRows) Next() bool {
	if r.position >= len(r.rows) {
		return false
	}
	r.position++
	return true
}

func (r *scriptRows) Scan(dest ...any) error {
	row := r.rows[r.position-1]
	for i := range dest {
		if i >= len(row) {
			break
		}
		switch p := dest[i].(type) {
		case *int:
			if v, ok := row[i].(int); ok {
				*p = v
			}
		case *string:
			if v, ok := row[i].(string); ok {
				*p = v
			}
		}
	}
	return nil
}

func (r *scriptRows) Err() error   { return nil }
func (r *scriptRows) Close() error { return nil }

// recordedCall captures one Exec/Query call.
type recordedCall struct {
	query string
	args  []any
}

// okResult is a sql.Result double reporting one affected row.
type okResult struct{}

func (okResult) LastInsertId() (int64, error) { return 0, nil }
func (okResult) RowsAffected() (int64, error) { return 1, nil }

// scriptDB is a db.ExecQueryer double with scripted query result rows.
type scriptDB struct {
	queries []recordedCall
	results [][][]any
}

func (s *scriptDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	s.queries = append(s.queries, recordedCall{query: query, args: append([]any(nil), args...)})
	return okResult{}, nil
}

func (s *scriptDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	s.queries = append(s.queries, recordedCall{query: query, args: append([]any(nil), args...)})
	var rows [][]any
	if len(s.results) > 0 {
		rows = s.results[0]
		s.results = s.results[1:]
	}
	return &scriptRows{rows: rows}, nil
}

// TestStoreAppliedVersion proves the applied read selects
// the marker row by key and returns its version.
func TestStoreAppliedVersion(t *testing.T) {
	t.Parallel()

	database := &scriptDB{results: [][][]any{{{3}}}}
	store := NewStore(database)

	version, err := store.AppliedVersion(context.Background(), "graph-writer-shape")
	if err != nil {
		t.Fatalf("AppliedVersion error = %v, want nil", err)
	}
	if version != 3 {
		t.Fatalf("version = %d, want 3", version)
	}
	if len(database.queries) != 1 {
		t.Fatalf("query calls = %d, want 1", len(database.queries))
	}
	q := database.queries[0]
	if !strings.Contains(q.query, "FROM graph_writer_shape") || !strings.Contains(q.query, "shape_key = $1") {
		t.Fatalf("AppliedVersion query selects the wrong row:\n%s", q.query)
	}
	if len(q.args) != 1 || q.args[0] != "graph-writer-shape" {
		t.Fatalf("args = %+v, want [graph-writer-shape]", q.args)
	}
}

// TestStoreAppliedVersionAbsent proves a missing marker
// row reads as version 0, so a fresh deployment retires on first startup.
func TestStoreAppliedVersionAbsent(t *testing.T) {
	t.Parallel()

	database := &scriptDB{}
	store := NewStore(database)

	version, err := store.AppliedVersion(context.Background(), "graph-writer-shape")
	if err != nil {
		t.Fatalf("AppliedVersion error = %v, want nil", err)
	}
	if version != 0 {
		t.Fatalf("version = %d, want 0 for an absent marker", version)
	}
}

// TestStoreClaimVersionWon proves the claim ensures the
// marker row, then runs the atomic predicate update carrying the key, the
// version, and the lease, reporting won when the row returns.
func TestStoreClaimVersionWon(t *testing.T) {
	t.Parallel()

	database := &scriptDB{results: [][][]any{{{"graph-writer-shape"}}}}
	store := NewStore(database)

	won, err := store.ClaimVersion(context.Background(), "graph-writer-shape", 1, 5*time.Minute)
	if err != nil {
		t.Fatalf("ClaimVersion error = %v, want nil", err)
	}
	if !won {
		t.Fatalf("won = false, want true (predicate row returned)")
	}
	if len(database.queries) != 2 {
		t.Fatalf("query calls = %d, want 2 (ensure row + claim update)", len(database.queries))
	}
	ensure, claim := database.queries[0], database.queries[1]
	if !strings.Contains(ensure.query, "INSERT INTO graph_writer_shape") {
		t.Fatalf("first statement does not ensure the marker row:\n%s", ensure.query)
	}
	for _, want := range []string{"applied_version < $2", "claimed_at", "RETURNING shape_key"} {
		if !strings.Contains(claim.query, want) {
			t.Fatalf("claim update missing %q:\n%s", want, claim.query)
		}
	}
	if len(claim.args) != 3 || claim.args[0] != "graph-writer-shape" || claim.args[1] != 1 || claim.args[2] != int64(300) {
		t.Fatalf("claim args = %+v, want [graph-writer-shape 1 300]", claim.args)
	}
}

// TestStoreClaimVersionLost proves an empty predicate
// result reports lost without error.
func TestStoreClaimVersionLost(t *testing.T) {
	t.Parallel()

	database := &scriptDB{}
	store := NewStore(database)

	won, err := store.ClaimVersion(context.Background(), "graph-writer-shape", 1, 5*time.Minute)
	if err != nil {
		t.Fatalf("ClaimVersion error = %v, want nil", err)
	}
	if won {
		t.Fatalf("won = true, want false (no predicate row)")
	}
}

// TestStoreMarkAndRelease proves the applied marker
// advances only forward and the claim release clears claimed_at.
func TestStoreMarkAndRelease(t *testing.T) {
	t.Parallel()

	database := &scriptDB{}
	store := NewStore(database)

	if err := store.MarkAppliedVersion(context.Background(), "graph-writer-shape", 1); err != nil {
		t.Fatalf("MarkAppliedVersion error = %v, want nil", err)
	}
	if err := store.ReleaseClaim(context.Background(), "graph-writer-shape"); err != nil {
		t.Fatalf("ReleaseClaim error = %v, want nil", err)
	}
	if len(database.queries) != 2 {
		t.Fatalf("query calls = %d, want 2", len(database.queries))
	}
	if !strings.Contains(database.queries[0].query, "applied_version < $2") {
		t.Fatalf("mark does not guard forward motion:\n%s", database.queries[0].query)
	}
	if !strings.Contains(database.queries[1].query, "claimed_at = NULL") {
		t.Fatalf("release does not clear the claim:\n%s", database.queries[1].query)
	}
}
