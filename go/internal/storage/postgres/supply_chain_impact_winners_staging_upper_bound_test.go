// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// residualFakeRows serves one scripted staging-residual row: the walked
// count, the lowest and newest visible seq, and the exact oldest capture time.
type residualFakeRows struct {
	walked int64
	lo, hi sql.NullInt64
	oldest sql.NullTime
	served bool
}

func (r *residualFakeRows) Next() bool {
	if r.served {
		return false
	}
	r.served = true
	return true
}

func (r *residualFakeRows) Scan(dest ...any) error {
	if len(dest) != 4 {
		return fmt.Errorf("residual scan: %d destinations, want 4", len(dest))
	}
	count, ok := dest[0].(*int64)
	lo, okLo := dest[1].(*sql.NullInt64)
	hi, okHi := dest[2].(*sql.NullInt64)
	oldest, okMin := dest[3].(*sql.NullTime)
	if !ok || !okLo || !okHi || !okMin {
		return fmt.Errorf("residual scan: unexpected destination types %T %T %T %T", dest[0], dest[1], dest[2], dest[3])
	}
	*count, *lo, *hi, *oldest = r.walked, r.lo, r.hi, r.oldest
	return nil
}

func (*residualFakeRows) Err() error   { return nil }
func (*residualFakeRows) Close() error { return nil }

// residualFakeTx is the phase fake transaction with the staging residual read
// scripted by the test, so a tick can see a queue past the exact-count cap.
type residualFakeTx struct {
	*phaseFakeTx
	residual residualFakeRows
}

func (tx *residualFakeTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if query == selectWinnersStagingResidualSQL {
		tx.queries = append(tx.queries, query)
		rows := tx.residual
		return &rows, nil
	}
	return tx.phaseFakeTx.QueryContext(ctx, query, args...)
}

// residualFakeDB hands the scripted transaction to the tick.
type residualFakeDB struct{ tx *residualFakeTx }

func (residualFakeDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, fmt.Errorf("keyed tick must read through its transaction")
}

func (d residualFakeDB) Begin(context.Context) (db.Transaction, error) { return d.tx, nil }

// TestResweepWinnersKeyedReportsStagingResidualUpperBound pins the #11129
// bounded staging read through every tick path that unions the staging
// residual into its result: past the exact-count cap the depth is the visible
// seq span and RemainingUpperBound is set; at or under the cap the depth is
// the exact count and the flag stays clear. Bounded keys on the residual being
// non-empty (the re-tick signal), never on the flag, so an empty queue is
// neither bounded nor an upper bound.
func TestResweepWinnersKeyedReportsStagingResidualUpperBound(t *testing.T) {
	t.Parallel()

	const exactCap = int64(winnersStagingExactCountCap)
	valid := func(n int64) sql.NullInt64 { return sql.NullInt64{Int64: n, Valid: true} }
	oldest := sql.NullTime{Time: phaseFakeNow.Add(-time.Minute), Valid: true}
	paths := []struct {
		name string
		tx   phaseFakeTx
	}{
		{"idle tick", phaseFakeTx{claimed: true}},
		{"keyed apply", phaseFakeTx{claimed: true, consumed: 3, identifier: 3, stored: 100}},
		{"over the cap full fallback", phaseFakeTx{claimed: true, consumed: 3, identifier: defaultWinnersKeyedMaxIdentifiers + 1, stored: 1000000}},
	}
	residuals := []struct {
		name          string
		residual      residualFakeRows
		wantRemaining int64
		wantUpper     bool
		wantBounded   bool
	}{
		{
			name:          "past the cap reports the seq span as an upper bound",
			residual:      residualFakeRows{walked: exactCap + 1, lo: valid(100), hi: valid(exactCap + 9000), oldest: oldest},
			wantRemaining: exactCap + 9000 - 100 + 1,
			wantUpper:     true,
			wantBounded:   true,
		},
		{
			name:          "exactly the cap stays exact",
			residual:      residualFakeRows{walked: exactCap, lo: valid(1), hi: valid(exactCap), oldest: oldest},
			wantRemaining: exactCap,
			wantBounded:   true,
		},
		{
			name:          "small residual stays exact",
			residual:      residualFakeRows{walked: 7, lo: valid(3), hi: valid(90), oldest: oldest},
			wantRemaining: 7,
			wantBounded:   true,
		},
		{
			name:     "empty queue is neither bounded nor an upper bound",
			residual: residualFakeRows{},
		},
	}
	for _, path := range paths {
		for _, res := range residuals {
			t.Run(path.name+"/"+res.name, func(t *testing.T) {
				t.Parallel()

				tx := &residualFakeTx{phaseFakeTx: &phaseFakeTx{
					claimed: path.tx.claimed, consumed: path.tx.consumed,
					identifier: path.tx.identifier, stored: path.tx.stored,
				}, residual: res.residual}
				store := NewSupplyChainImpactWinnersStore(residualFakeDB{tx: tx})
				got, err := store.ResweepWinnersKeyed(context.Background())
				if err != nil {
					t.Fatalf("ResweepWinnersKeyed() error = %v", err)
				}
				if got.Remaining != res.wantRemaining {
					t.Errorf("Remaining = %d, want %d", got.Remaining, res.wantRemaining)
				}
				if got.RemainingUpperBound != res.wantUpper {
					t.Errorf("RemainingUpperBound = %v, want %v", got.RemainingUpperBound, res.wantUpper)
				}
				if got.Bounded != res.wantBounded {
					t.Errorf("Bounded = %v, want %v (re-tick keys on a non-empty residual, not on the upper-bound flag)", got.Bounded, res.wantBounded)
				}
			})
		}
	}
}

// TestWinnersStagingDepthReportsUpperBoundPastTheCap pins the depth rule the
// residual read and the gauge sample share: exact up to the cap; past it the
// visible seq span, marked an upper bound and never below the walked count; a
// missing seq edge cannot compute a span, so the walked count stands exact.
func TestWinnersStagingDepthReportsUpperBoundPastTheCap(t *testing.T) {
	t.Parallel()

	const exactCap = int64(winnersStagingExactCountCap)
	valid := func(n int64) sql.NullInt64 { return sql.NullInt64{Int64: n, Valid: true} }
	tests := []struct {
		name      string
		walked    int64
		lo, hi    sql.NullInt64
		wantDepth int64
		wantUpper bool
	}{
		{"empty", 0, sql.NullInt64{}, sql.NullInt64{}, 0, false},
		{"at the cap is exact", exactCap, valid(1), valid(exactCap + 500), exactCap, false},
		{"past the cap is the span", exactCap + 1, valid(10), valid(exactCap + 1010), exactCap + 1001, true},
		{"span is never below the walked count", exactCap + 1, valid(10), valid(11), exactCap + 1, true},
		{"missing seq edge keeps the walked count", exactCap + 1, sql.NullInt64{}, valid(5), exactCap + 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			depth, upper := winnersStagingDepth(tt.walked, tt.lo, tt.hi)
			if depth != tt.wantDepth || upper != tt.wantUpper {
				t.Errorf("winnersStagingDepth(%d, %v, %v) = (%d, %v), want (%d, %v)",
					tt.walked, tt.lo, tt.hi, depth, upper, tt.wantDepth, tt.wantUpper)
			}
		})
	}
}
