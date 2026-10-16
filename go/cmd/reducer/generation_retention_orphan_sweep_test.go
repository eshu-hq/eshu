// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	indexretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/search/index/retention"
)

// orphanSweepFakeDB answers the orphan census with one listed orphan in the
// documents table and every sweep batch with a full page, so only the row
// cap the adapter passes ends the sweep.
type orphanSweepFakeDB struct{}

func (orphanSweepFakeDB) Begin(context.Context) (db.Transaction, error) {
	return orphanSweepFakeTx{}, nil
}

func (orphanSweepFakeDB) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return orphanSweepFakeTx{}.QueryContext(ctx, query, args...)
}

type orphanSweepFakeTx struct{}

func (orphanSweepFakeTx) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	switch {
	case strings.Contains(query, "orphan census leaves"):
		return &orphanSweepFakeRows{}, nil
	case strings.Contains(query, "FROM eshu_search_index_documents AS relation"):
		return &orphanSweepFakeRows{rows: [][]any{{int64(3), int64(1), "scope-a", "gen-a"}}}, nil
	case strings.Contains(query, "orphan census"):
		return &orphanSweepFakeRows{rows: [][]any{{int64(0), int64(0), nil, nil}}}, nil
	case strings.Contains(query, "pg_try_advisory_xact_lock"):
		return &orphanSweepFakeRows{rows: [][]any{{true}}}, nil
	case strings.Contains(query, "orphan sweep batch"):
		return &orphanSweepFakeRows{rows: [][]any{{true, int64(indexretention.BatchRows), int64(indexretention.BatchRows), "k", "d"}}}, nil
	}
	return nil, fmt.Errorf("unexpected statement: %s", query)
}

func (orphanSweepFakeTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}
func (orphanSweepFakeTx) Commit() error   { return nil }
func (orphanSweepFakeTx) Rollback() error { return nil }

type orphanSweepFakeRows struct {
	rows [][]any
	at   int
}

func (r *orphanSweepFakeRows) Next() bool { r.at++; return r.at <= len(r.rows) }
func (r *orphanSweepFakeRows) Err() error { return nil }
func (r *orphanSweepFakeRows) Close() error {
	return nil
}

func (r *orphanSweepFakeRows) Scan(dest ...any) error {
	for i, value := range r.rows[r.at-1] {
		switch target := dest[i].(type) {
		case *bool:
			*target = value.(bool)
		case *int64:
			*target = value.(int64)
		case *string:
			*target = value.(string)
		case sql.Scanner:
			if err := target.Scan(value); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported scan target %T", dest[i])
		}
	}
	return nil
}

// TestSearchIndexOrphanSweeperPassesTheRowCap pins #8281 review thread A at
// the reducer wiring: the adapter hands the runner's row cap to the sweep.
func TestSearchIndexOrphanSweeperPassesTheRowCap(t *testing.T) {
	sweeper := searchIndexOrphanSweeper{database: orphanSweepFakeDB{}}
	result, err := sweeper.SweepSearchIndexOrphans(context.Background(), 0, 2*indexretention.BatchRows)
	if err != nil {
		t.Fatalf("SweepSearchIndexOrphans() error = %v", err)
	}
	if got := result.RowsSwept[indexretention.TermsTable] + result.RowsSwept[indexretention.DocumentsTable]; got != 2*indexretention.BatchRows {
		t.Fatalf("swept %d rows, want the 2-batch cap %d", got, 2*indexretention.BatchRows)
	}
}
