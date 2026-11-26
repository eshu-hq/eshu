// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// prevFakeRows is a one-row scripted cursor supporting the destinations the
// watermark stamp sites scan: *time.Time (selectWinnersWatermarkSQL via
// scanWinnersTime), *sql.NullTime (RETURNING previous columns), and *int64
// (count columns).
type prevFakeRows struct {
	values []any
	served bool
}

func (r *prevFakeRows) Next() bool {
	if r.served || r.values == nil {
		return false
	}
	r.served = true
	return true
}

func (r *prevFakeRows) Scan(dest ...any) error {
	if len(dest) != len(r.values) {
		return fmt.Errorf("scan: %d destinations for %d values", len(dest), len(r.values))
	}
	for i, d := range dest {
		switch p := d.(type) {
		case *time.Time:
			switch v := r.values[i].(type) {
			case time.Time:
				*p = v
			case sql.NullTime:
				if v.Valid {
					*p = v.Time
				}
			default:
				return fmt.Errorf("scan: bad time value %T", r.values[i])
			}
		case *sql.NullTime:
			switch v := r.values[i].(type) {
			case sql.NullTime:
				*p = v
			case time.Time:
				*p = sql.NullTime{Time: v, Valid: true}
			default:
				return fmt.Errorf("scan: bad nulltime value %T", r.values[i])
			}
		case *int64:
			*p = r.values[i].(int64)
		default:
			return fmt.Errorf("scan: unsupported destination %T", d)
		}
	}
	return nil
}

func (r *prevFakeRows) Err() error   { return nil }
func (r *prevFakeRows) Close() error { return nil }

// prevFakeTx scripts the watermark stamp sites: the first
// selectWinnersWatermarkSQL read reports pre (the pre-stamp R), later ones
// report post (the committed C). The stamp statements' RETURNING columns
// report post, the way GREATEST makes the RETURNING subquery read the
// post-upsert value.
type prevFakeTx struct {
	pre      time.Time
	post     time.Time
	preEmpty bool
	reads    int
	queries  []string
}

func (tx *prevFakeTx) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	tx.queries = append(tx.queries, query)
	switch query {
	case selectWinnersWatermarkSQL:
		tx.reads++
		if tx.reads == 1 && !tx.preEmpty {
			return &prevFakeRows{values: []any{tx.pre}}, nil
		}
		if tx.reads == 1 && tx.preEmpty {
			return &prevFakeRows{}, nil
		}
		return &prevFakeRows{values: []any{tx.post}}, nil
	case stampWinnersWatermarkSQL:
		return &prevFakeRows{values: []any{sql.NullTime{Time: tx.post, Valid: true}}}, nil
	case applyWinnersKeyedSQL:
		return &prevFakeRows{values: []any{
			int64(1), int64(0), int64(0), int64(0), int64(1),
			sql.NullTime{Time: tx.post, Valid: true},
		}}, nil
	case rebuildSupplyChainImpactWinnersSQL:
		return &prevFakeRows{values: []any{
			int64(1), int64(0), int64(0), int64(0),
			sql.NullTime{Time: tx.post, Valid: true},
		}}, nil
	}
	return nil, fmt.Errorf("unscripted query %q", query)
}

type prevFakeResult struct{}

func (prevFakeResult) LastInsertId() (int64, error) { return 0, nil }
func (prevFakeResult) RowsAffected() (int64, error) { return 0, nil }

func (tx *prevFakeTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return prevFakeResult{}, nil
}

func (tx *prevFakeTx) Commit() error   { return nil }
func (tx *prevFakeTx) Rollback() error { return nil }

// TestStampWinnersWatermarkReportsReplacedValue pins the PreviousWatermark
// contract (winners_maintainer.go: the watermark the resweep replaced): the
// idle stamp reports the pre-stamp R, not the RETURNING post-upsert value.
// A clamped tick (candidate under R, GREATEST keeps R) then reports age 0.
func TestStampWinnersWatermarkReportsReplacedValue(t *testing.T) {
	t.Parallel()

	pre := time.Date(2026, 11, 20, 6, 0, 0, 0, time.UTC)
	tx := &prevFakeTx{pre: pre, post: pre}
	got, err := stampWinnersWatermark(context.Background(), tx, pre)
	if err != nil {
		t.Fatalf("stampWinnersWatermark() error = %v", err)
	}
	if !got.Equal(pre) {
		t.Fatalf("stampWinnersWatermark() previous = %v, want the pre-stamp %v", got, pre)
	}
	if tx.reads != 1 {
		t.Fatalf("pre-stamp reads = %d, want exactly one before the stamp statement", tx.reads)
	}
}

// TestStampSitesReportPreStampOnAdvance pins the advancing side: when the
// stamp moves the watermark from R to C, PreviousWatermark is R (the value
// replaced), so the maintainer age is C-R, not 0.
func TestStampSitesReportPreStampOnAdvance(t *testing.T) {
	t.Parallel()

	pre := time.Date(2026, 11, 20, 6, 0, 0, 0, time.UTC)
	post := pre.Add(45 * time.Second)

	t.Run("apply", func(t *testing.T) {
		t.Parallel()
		tx := &prevFakeTx{pre: pre, post: post}
		var got supplychaincore.SupplyChainImpactWinnersResweep
		if err := applyWinnersKeyed(context.Background(), tx, post, &got); err != nil {
			t.Fatalf("applyWinnersKeyed() error = %v", err)
		}
		if !got.PreviousWatermark.Equal(pre) {
			t.Fatalf("apply PreviousWatermark = %v, want the pre-stamp %v", got.PreviousWatermark, pre)
		}
	})

	t.Run("rebuild", func(t *testing.T) {
		t.Parallel()
		tx := &prevFakeTx{pre: pre, post: post}
		got, err := runSupplyChainImpactWinnersRebuild(context.Background(), tx, post)
		if err != nil {
			t.Fatalf("runSupplyChainImpactWinnersRebuild() error = %v", err)
		}
		if !got.PreviousWatermark.Equal(pre) {
			t.Fatalf("rebuild PreviousWatermark = %v, want the pre-stamp %v", got.PreviousWatermark, pre)
		}
	})
}

// TestStampWinnersWatermarkFirstSweepIsZero pins the empty-table edge: with
// no watermark row before the stamp, the previous watermark is zero.
func TestStampWinnersWatermarkFirstSweepIsZero(t *testing.T) {
	t.Parallel()

	post := time.Date(2026, 11, 20, 6, 0, 0, 0, time.UTC)
	tx := &prevFakeTx{preEmpty: true, post: post}
	got, err := stampWinnersWatermark(context.Background(), tx, post)
	if err != nil {
		t.Fatalf("stampWinnersWatermark() error = %v", err)
	}
	if !got.IsZero() {
		t.Fatalf("stampWinnersWatermark() previous = %v, want zero on the first sweep", got)
	}
}
