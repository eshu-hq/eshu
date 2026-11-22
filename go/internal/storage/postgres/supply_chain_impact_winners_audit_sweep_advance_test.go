// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// sweepRowTx is a writer transaction holding the post-UPDATE sweep row. It
// answers advanceWinnersAuditSweepSQL with the columns its RETURNING clause
// names, read from that row, so the test binds the statement text and not a
// copy of the scan.
type sweepRowTx struct {
	row       map[string]any
	committed bool
}

func (t *sweepRowTx) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	if query != advanceWinnersAuditSweepSQL {
		return nil, fmt.Errorf("unexpected query %q", query)
	}
	at := strings.LastIndex(query, "RETURNING")
	if at < 0 {
		return nil, fmt.Errorf("advance statement has no RETURNING clause")
	}
	var values []any
	for _, col := range strings.Split(query[at+len("RETURNING"):], ",") {
		v, ok := t.row[strings.TrimSpace(col)]
		if !ok {
			return nil, fmt.Errorf("RETURNING names unknown column %q", strings.TrimSpace(col))
		}
		values = append(values, v)
	}
	return &sweepRowRows{values: values}, nil
}

func (t *sweepRowTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, fmt.Errorf("no exec expected on a range without candidates")
}
func (t *sweepRowTx) Commit() error   { t.committed = true; return nil }
func (t *sweepRowTx) Rollback() error { return nil }

// sweepRowDB begins sweepRowTx.
type sweepRowDB struct{ tx *sweepRowTx }

func (d sweepRowDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, fmt.Errorf("no query outside the transaction expected")
}
func (d sweepRowDB) Begin(context.Context) (db.Transaction, error) { return d.tx, nil }

// sweepRowRows is one row; Scan fails on a column-count mismatch as pgx does.
type sweepRowRows struct {
	values []any
	done   bool
}

func (r *sweepRowRows) Next() bool   { ok := !r.done; r.done = true; return ok }
func (r *sweepRowRows) Err() error   { return nil }
func (r *sweepRowRows) Close() error { return nil }
func (r *sweepRowRows) Scan(dest ...any) error {
	if len(dest) != len(r.values) {
		return fmt.Errorf("scan %d destinations for %d columns", len(dest), len(r.values))
	}
	for i, d := range dest {
		if s, ok := d.(sql.Scanner); ok {
			if err := s.Scan(r.values[i]); err != nil {
				return fmt.Errorf("column %d: %w", i, err)
			}
			continue
		}
		reflect.ValueOf(d).Elem().Set(reflect.ValueOf(r.values[i]))
	}
	return nil
}

// TestVerifyWinnersAuditRangeKeepsTheLastSweepTotals is the #11131 review F1
// regression: a committed range that does not complete the sweep must hand
// back the persisted last-sweep totals. Before the fix the advance returned
// only four columns, so every audit_sweep_last_* gauge read 0 after each
// committed range.
func TestVerifyWinnersAuditRangeKeepsTheLastSweepTotals(t *testing.T) {
	started := time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)
	completed := time.Date(2026, 11, 20, 22, 0, 0, 0, time.UTC)
	hi := &WinnersAuditRangeIdent{"pkg-b", "repo-b", "sha256:b", "CVE-2", "GHSA-b"}
	tx := &sweepRowTx{row: map[string]any{
		"sweep_id": int64(4), "cursor_package_id": hi.PackageID, "cursor_repository_id": hi.RepositoryID,
		"cursor_subject_digest": hi.SubjectDigest, "cursor_c0": hi.C0, "cursor_advisory_id": hi.AdvisoryID,
		"ranges_done": int64(6), "sweep_started_at": started, "last_sweep_completed_at": completed,
		"last_sweep_seconds": float64(36000), "last_sweep_ranges": int64(840), "last_sweep_facts": int64(8370000),
		"last_sweep_candidates": int64(12), "last_sweep_repairs": int64(3), "last_sweep_discards": int64(9),
	}}
	store := SupplyChainImpactWinnersStore{DB: sweepRowDB{tx: tx}}

	out, err := store.VerifyWinnersAuditRange(context.Background(), WinnersAuditRangeVerify{
		SweepID: 4,
		Range:   WinnersAuditRange{Lo: &WinnersAuditRangeIdent{"pkg-a", "repo-a", "sha256:a", "CVE-1", "GHSA-a"}, Hi: hi, Facts: 10000},
	})
	if err != nil {
		t.Fatalf("verify range: %v", err)
	}
	if !out.Advanced || !tx.committed {
		t.Fatalf("outcome = %+v, committed = %v; want an advanced, committed range", out, tx.committed)
	}
	want := WinnersAuditSweep{
		SweepID: 4, Cursor: hi, RangesDone: 6, StartedAt: started, LastCompletedAt: completed,
		LastSeconds: 36000, LastRanges: 840, LastFacts: 8370000, LastCandidates: 12, LastRepairs: 3, LastDiscards: 9,
	}
	if !reflect.DeepEqual(out.Sweep, want) {
		t.Fatalf("sweep after a committed range = %+v\nwant the persisted row %+v", out.Sweep, want)
	}
}
