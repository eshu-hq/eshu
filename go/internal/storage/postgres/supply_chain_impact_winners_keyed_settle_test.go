// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestWinnersDirtyStratifiedEnvelopesStayStratified pins the arbiter's R2
// ruling shape for eshu-hq/eshu#10827: the 2-writer p95 envelope and the
// 4-writer baseline are two distinct numbers with the tighter envelope on the
// quieter stratum. A single raised cap for both strata would mask a 2-writer
// regression under 4-writer headroom, so unifying the constants (or swapping
// their order) fails here, not in CI.
func TestWinnersDirtyStratifiedEnvelopesStayStratified(t *testing.T) {
	t.Parallel()

	if winnersDirtyTwoWriterP95Envelope != 3410*time.Microsecond {
		t.Errorf("2-writer p95 envelope = %s, want 3410us (3.41ms): the arbiter's ruled envelope, not a re-tuned cap",
			winnersDirtyTwoWriterP95Envelope)
	}
	if winnersDirtyFourWriterP95Baseline != 3890*time.Microsecond {
		t.Errorf("4-writer p95 baseline = %s, want 3890us (3.89ms): the stratum's own measured baseline, not the 2-writer envelope",
			winnersDirtyFourWriterP95Baseline)
	}
	if winnersDirtyTwoWriterP95Envelope >= winnersDirtyFourWriterP95Baseline {
		t.Errorf("stratified envelope collapsed: 2-writer %s >= 4-writer %s: one cap for both strata masks a 2-writer regression",
			winnersDirtyTwoWriterP95Envelope, winnersDirtyFourWriterP95Baseline)
	}
}

// TestWinnersDirtyLatencyP95IsNearestRank pins the estimator the stratified
// gate depends on: rank ceil(0.95*n) over the sorted sample, so the bound
// holds per stratum without assuming a latency distribution.
func TestWinnersDirtyLatencyP95IsNearestRank(t *testing.T) {
	t.Parallel()

	mk := func(n int) []time.Duration {
		out := make([]time.Duration, n)
		for i := range out {
			out[i] = time.Duration(i+1) * time.Microsecond
		}
		return out
	}
	// n=20: rank ceil(19) = 19th of 20, i.e. 19us; n=21: ceil(19.95) = 20th,
	// i.e. 20us. The input order must not matter.
	if got := winnersDirtyLatencyPercentile(mk(20), 0.95); got != 19*time.Microsecond {
		t.Errorf("p95 of 1..20us = %s, want 19us (nearest-rank)", got)
	}
	if got := winnersDirtyLatencyPercentile(mk(21), 0.95); got != 20*time.Microsecond {
		t.Errorf("p95 of 1..21us = %s, want 20us (nearest-rank)", got)
	}
	shuffled := []time.Duration{5 * time.Microsecond, time.Microsecond, 3 * time.Microsecond, 2 * time.Microsecond, 4 * time.Microsecond}
	if got := winnersDirtyLatencyPercentile(shuffled, 0.95); got != 5*time.Microsecond {
		t.Errorf("p95 of shuffled 1..5us = %s, want 5us (rank ceil(4.75) = 5th)", got)
	}
	if got := winnersDirtyLatencyPercentile(mk(20), 0.5); got != 10*time.Microsecond {
		t.Errorf("p50 of 1..20us = %s, want 10us (nearest-rank)", got)
	}
}

var (
	errSettleUnscriptedQuery = errors.New("unscripted settle query")
	errSettleScanArity       = errors.New("settle scan arity mismatch")
	errSettleScanType        = errors.New("settle scan unsupported destination")
)

// TestWinnersDrainStatementsLockKeysInOrder pins arbiter verdict R2 for
// eshu-hq/eshu#10827: every multi-row dirty-log accessor takes locks in one
// total order over the conflict key. Both drain statements lock the admitted
// rows ordered by the full conflict key (kind, fact five-tuple, scope pair,
// seq tiebreak) with FOR UPDATE, recheck the admitted dirty_version after
// locking (a concurrent re-mark stays queued), and tally version sums, not
// row counts.
func TestWinnersDrainStatementsLockKeysInOrder(t *testing.T) {
	t.Parallel()

	for name, statement := range map[string]string{
		"applied": deleteWinnersKeyedAppliedDirtySQL,
		"all":     deleteWinnersKeyedAllDirtySQL,
	} {
		for _, want := range []string{
			"FOR UPDATE",
			"ORDER BY",
			"dirty_version",
			"sum(dirty_version)",
			"kind",
			"package_id",
			"repository_id",
			"subject_digest",
			"c0",
			"advisory_id",
			"scope_id",
			"generation_id",
		} {
			if !strings.Contains(statement, want) {
				t.Errorf("%s drain statement does not contain %q", name, want)
			}
		}
	}
}

// TestWinnersConvertRemainderIsKeyOrderedUpsert pins the R2 convert path: a
// partially applied scope row's remainder becomes fact rows through a
// K-ordered upsert (a concurrent writer may have marked the same key while
// the tick ran) before the lock/delete, so conversion never violates the
// versioned unique indexes.
func TestWinnersConvertRemainderIsKeyOrderedUpsert(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"ON CONFLICT",
		"DO UPDATE",
		"dirty_version",
		"ORDER BY",
	} {
		if !strings.Contains(convertWinnersKeyedScopeRemainderSQL, want) {
			t.Errorf("convert scope remainder statement does not contain %q", want)
		}
	}
}

// TestWinnersConsumedTempCarriesDirtyVersion pins that the admitted window
// carries each row's dirty_version: the settle recheck compares the locked
// log row against the admitted version, so the temp table must hold it.
func TestWinnersConsumedTempCarriesDirtyVersion(t *testing.T) {
	t.Parallel()

	if !strings.Contains(createWinnersKeyedConsumedSQL, "dirty_version") {
		t.Errorf("consumed temp DDL does not carry dirty_version:\n%s", createWinnersKeyedConsumedSQL)
	}
	if !strings.Contains(admitWinnersKeyedWindowSQL, "dirty_version") {
		t.Errorf("admit window statement does not carry dirty_version:\n%s", admitWinnersKeyedWindowSQL)
	}
}

// settleFakeTx scripts settleWinnersKeyedDirtyLog's statements and records
// them, so the test pins that the scope-remainder convert runs before the
// drain delete on the applied path and never runs on the over-cap path.
type settleFakeTx struct {
	queries []string
	drained [][2]any
}

func (tx *settleFakeTx) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	tx.queries = append(tx.queries, query)
	return settleFakeResult{}, nil
}

func (tx *settleFakeTx) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	tx.queries = append(tx.queries, query)
	switch query {
	case deleteWinnersKeyedAppliedDirtySQL, deleteWinnersKeyedAllDirtySQL:
		return &settleFakeRows{rows: tx.drained}, nil
	case countWinnersKeyedRemainingSQL:
		return &settleFakeRows{rows: [][2]any{{int64(0), nil}}}, nil
	case selectWinnersKeyedRemainingMinSQL:
		return &settleFakeRows{rows: [][2]any{{nil, nil}}}, nil
	}
	return nil, errSettleUnscriptedQuery
}

func (tx *settleFakeTx) Commit() error   { return nil }
func (tx *settleFakeTx) Rollback() error { return nil }

type settleFakeResult struct{}

func (settleFakeResult) LastInsertId() (int64, error) { return 0, nil }
func (settleFakeResult) RowsAffected() (int64, error) { return 0, nil }

type settleFakeRows struct {
	rows [][2]any
}

func (r *settleFakeRows) Next() bool { return len(r.rows) > 0 }

func (r *settleFakeRows) Scan(dest ...any) error {
	if len(dest) < 1 || len(dest) > 2 {
		return errSettleScanArity
	}
	row := r.rows[0]
	r.rows = r.rows[1:]
	switch d := dest[0].(type) {
	case *string:
		s, _ := row[0].(string)
		*d = s
	case *sql.NullTime:
		*d = sql.NullTime{}
		return nil
	case *int64:
		// The remaining-depth probe scans one column; the drain tally
		// scans (source, total).
		if len(dest) == 1 {
			n, _ := row[0].(int64)
			*d = n
			return nil
		}
		n, _ := row[1].(int64)
		*d = n
	default:
		return errSettleScanType
	}
	switch d := dest[1].(type) {
	case *int64:
		n, _ := row[1].(int64)
		*d = n
	case *sql.NullTime:
		*d = sql.NullTime{}
	default:
		return errSettleScanType
	}
	return nil
}

func (r *settleFakeRows) Err() error   { return nil }
func (r *settleFakeRows) Close() error { return nil }

// TestSettleWinnersKeyedDirtyLogConvertsBeforeItDeletes pins the settle
// order (#10706 named constraint): the scope-remainder convert commits
// atomically with the applied rows' deletion, so it must run first; the
// over-cap fallback deletes everything the tick admitted and converts
// nothing.
func TestSettleWinnersKeyedDirtyLogConvertsBeforeItDeletes(t *testing.T) {
	t.Parallel()

	applied := &settleFakeTx{drained: [][2]any{{"fact_upsert", int64(3)}}}
	if _, _, _, err := settleWinnersKeyedDirtyLog(context.Background(), applied, false); err != nil {
		t.Fatalf("settle applied: %v", err)
	}
	convertIdx, drainIdx := -1, -1
	for i, q := range applied.queries {
		switch q {
		case convertWinnersKeyedScopeRemainderSQL:
			convertIdx = i
		case deleteWinnersKeyedAppliedDirtySQL:
			drainIdx = i
		}
	}
	if convertIdx < 0 || drainIdx < 0 || convertIdx > drainIdx {
		t.Errorf("applied settle order: convert index %d, drain index %d, want convert before drain", convertIdx, drainIdx)
	}

	overCap := &settleFakeTx{drained: [][2]any{{"fact_upsert", int64(3)}}}
	if _, _, _, err := settleWinnersKeyedDirtyLog(context.Background(), overCap, true); err != nil {
		t.Fatalf("settle over-cap: %v", err)
	}
	for _, q := range overCap.queries {
		if q == convertWinnersKeyedScopeRemainderSQL {
			t.Errorf("over-cap settle runs the scope-remainder convert, want the all-rows drain only")
		}
		if q == deleteWinnersKeyedAppliedDirtySQL {
			t.Errorf("over-cap settle runs the applied drain, want the all-rows drain")
		}
	}
	found := false
	for _, q := range overCap.queries {
		if q == deleteWinnersKeyedAllDirtySQL {
			found = true
		}
	}
	if !found {
		t.Errorf("over-cap settle never ran the all-rows drain")
	}
}
