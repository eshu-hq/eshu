// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// scriptedRows serves one row of values to Scan.
type scriptedRows struct {
	values []any
	done   bool
}

func (r *scriptedRows) Next() bool {
	if r.done || r.values == nil {
		return false
	}
	r.done = true
	return true
}

func (r *scriptedRows) Scan(dest ...any) error {
	if len(dest) != len(r.values) {
		return fmt.Errorf("scripted scan: %d destinations for %d values", len(dest), len(r.values))
	}
	for i, d := range dest {
		switch p := d.(type) {
		case *string:
			*p = r.values[i].(string)
		case *int64:
			*p = r.values[i].(int64)
		case *int16:
			*p = r.values[i].(int16)
		case *int:
			*p = r.values[i].(int)
		case *bool:
			*p = r.values[i].(bool)
		case *sql.NullTime:
			*p = sql.NullTime{}
		default:
			return fmt.Errorf("scripted scan: unsupported destination %T", d)
		}
	}
	return nil
}

func (r *scriptedRows) Err() error   { return nil }
func (r *scriptedRows) Close() error { return nil }

type okResult struct{}

func (okResult) LastInsertId() (int64, error) { return 0, nil }
func (okResult) RowsAffected() (int64, error) { return 1, nil }

// scriptedDB answers the incremental-link path of LinkNext; linkRow is the
// row the incremental statement returns.
type scriptedDB struct {
	linkRow   []any
	committed bool
}

func (d *scriptedDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return okResult{}, nil
}

func (d *scriptedDB) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	switch query {
	case lockCursorQuery:
		return &scriptedRows{values: []any{"g0", int64(1), int16(DigestVersion), int64(0), 0, nil}}, nil
	case nextActivationQuery:
		return &scriptedRows{values: []any{int64(2), "g1", "g0", false}}, nil
	case generationExistsQuery:
		return &scriptedRows{values: []any{false}}, nil
	case lockGenerationQuery:
		return &scriptedRows{values: []any{int64(1)}}, nil
	case trySlotQuery:
		return &scriptedRows{values: []any{true}}, nil
	case IncrementalLinkSQL:
		return &scriptedRows{values: d.linkRow}, nil
	}
	return nil, fmt.Errorf("scripted db: unexpected query %.60q", query)
}

func (d *scriptedDB) Begin(context.Context) (db.Transaction, error) { return &scriptedTx{db: d}, nil }

type scriptedTx struct{ db *scriptedDB }

func (t *scriptedTx) ExecContext(ctx context.Context, q string, a ...any) (sql.Result, error) {
	return t.db.ExecContext(ctx, q, a...)
}

func (t *scriptedTx) QueryContext(ctx context.Context, q string, a ...any) (db.Rows, error) {
	return t.db.QueryContext(ctx, q, a...)
}

func (t *scriptedTx) Commit() error {
	t.db.committed = true
	return nil
}
func (t *scriptedTx) Rollback() error { return nil }

// linkRow builds the incremental statement's result row: delta rows, the
// three key counts, deleted, upserted, buckets, expected deletes, expected
// upserts, foreign deletes.
func linkRow(deleted, upserted, expectedDeletes, expectedUpserts, foreign int64) []any {
	return []any{int64(10), int64(1), int64(2), int64(3), deleted, upserted, int64(4), expectedDeletes, expectedUpserts, foreign}
}

// TestIncrementalLinkRowCountInvariant is P4 (unit) of arbiter ruling
// arb-7127-g8: the ctid delete is safe only with a row-count check. A delete
// count one short, an upsert count off, or any deleted row of another scope
// is a counting failure (class internal) and the transaction does not commit.
func TestIncrementalLinkRowCountInvariant(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  []any
		ok   bool
	}{
		{"exact", linkRow(5, 7, 5, 7, 0), true},
		{"deleted one short", linkRow(4, 7, 5, 7, 0), false},
		{"upserted off", linkRow(5, 6, 5, 7, 0), false},
		{"foreign delete", linkRow(5, 7, 5, 7, 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &scriptedDB{linkRow: tc.row}
			_, err := NewLinkWriter(d).LinkNext(context.Background(), "scope")
			if tc.ok {
				if err != nil || !d.committed {
					t.Fatalf("LinkNext = %v, committed %v; want success", err, d.committed)
				}
				return
			}
			var failure *FailureError
			if !errors.As(err, &failure) || failure.Class != FailureInternal ||
				!strings.Contains(err.Error(), "row-count invariant") {
				t.Fatalf("LinkNext = %v; want a counting internal failure naming the row-count invariant", err)
			}
			if d.committed {
				t.Fatal("the link committed despite the broken invariant")
			}
		})
	}
}
