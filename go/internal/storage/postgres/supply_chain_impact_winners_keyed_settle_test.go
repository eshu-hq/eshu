// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"math"
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

// TestWinnersDirtyContentionRatiosStayStratified pins the backstop shape for
// eshu-hq/eshu#10827: the 2-writer ratio cap is tighter than the 4-writer
// cap, so a unified ratio still fails here, not in CI. The caps (3.0/3.5)
// sit beyond full serialization at 2 writers (2x) and inside it at 4 (4x):
// calibrated on three local runs (ratios ~1.5/~1.7) and the slowest CI
// runner observed (inferred ~2.1/~2.9), they deflake host oversubscription
// noise while global superlinear contention growth on the gate's disjoint
// key sets still breaks the backstop arm.
func TestWinnersDirtyContentionRatiosStayStratified(t *testing.T) {
	t.Parallel()

	if winnersDirtyTwoWriterMaxRatio != 3.0 {
		t.Errorf("2-writer max contention ratio = %v, want 3.0: the calibrated backstop, not a re-tuned cap",
			winnersDirtyTwoWriterMaxRatio)
	}
	if winnersDirtyFourWriterMaxRatio != 3.5 {
		t.Errorf("4-writer max contention ratio = %v, want 3.5: the stratum's own calibrated headroom, not the 2-writer cap",
			winnersDirtyFourWriterMaxRatio)
	}
	if winnersDirtyTwoWriterMaxRatio >= winnersDirtyFourWriterMaxRatio {
		t.Errorf("stratified ratios collapsed: 2-writer %v >= 4-writer %v: one cap for both strata masks a 2-writer regression",
			winnersDirtyTwoWriterMaxRatio, winnersDirtyFourWriterMaxRatio)
	}
}

// TestWinnersDirtyGateFailsIsConjunction pins the hybrid decision: a stratum
// fails only when the absolute reference AND the ratio backstop both break.
// Vectors: the local host passes both arms; the slowest CI runner observed
// (run 44070281709, stipulated ratios) breaks the reference but holds the
// backstop and passes; a beyond-serialization collapse breaks both and fails;
// an absolute-only excursion with a healthy ratio passes.
func TestWinnersDirtyGateFailsIsConjunction(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		p95, reference  time.Duration
		ratio, maxRatio float64
		wantFail        bool
	}{
		{"local 2-writer passes both arms", 2051 * time.Microsecond, 3410 * time.Microsecond, 1.48, 3.0, false},
		{"local 4-writer passes both arms", 2288 * time.Microsecond, 3890 * time.Microsecond, 1.64, 3.5, false},
		{"slow CI 2-writer holds backstop", 3885 * time.Microsecond, 3410 * time.Microsecond, 2.13, 3.0, false},
		{"slow CI 4-writer holds backstop", 5255 * time.Microsecond, 3890 * time.Microsecond, 2.90, 3.5, false},
		{"collapse breaks both arms", 5000 * time.Microsecond, 3410 * time.Microsecond, 5.0, 3.0, true},
		{"absolute-only excursion passes", 5000 * time.Microsecond, 3410 * time.Microsecond, 1.5, 3.0, false},
		{"ratio-only excursion passes", 2000 * time.Microsecond, 3410 * time.Microsecond, 5.0, 3.0, false},
	}
	for _, tc := range cases {
		if got := winnersDirtyGateFails(tc.p95, tc.reference, tc.ratio, tc.maxRatio); got != tc.wantFail {
			t.Errorf("%s: gate fails = %v, want %v (p95 %s vs %s, ratio %.2f vs %.1f)",
				tc.name, got, tc.wantFail, tc.p95, tc.reference, tc.ratio, tc.maxRatio)
		}
	}
}

// TestWinnersDirtyTailStallNoiseRescuesMedianHealthyP95Spike pins the #11060
// hardening: a stratum that trips both gate arms is still a pass when the
// median statement cost is healthy and only a few stalled statements inflate
// the nearest-rank p95 (runner oversubscription: 4-writer p50 1.04ms vs p95
// 6.6ms over n=60). A uniform regression (low dispersion) or a median shift
// (p50 past the reference) stays a failure, and degenerate inputs never
// read as noise (fail-closed: the gate failure stands).
func TestWinnersDirtyTailStallNoiseRescuesMedianHealthyP95Spike(t *testing.T) {
	t.Parallel()

	if winnersDirtyTailStallDispersion != 3.0 {
		t.Errorf("tail-stall dispersion = %v, want 3.0: a uniform 2x cost drift must not read as noise",
			winnersDirtyTailStallDispersion)
	}
	cases := []struct {
		name                string
		p50, p95, reference time.Duration
		wantNoise           bool
	}{
		{"11060 flake is noise", 1040 * time.Microsecond, 6600 * time.Microsecond, 3890 * time.Microsecond, true},
		{"uniform drift is not noise", 2500 * time.Microsecond, 5000 * time.Microsecond, 3410 * time.Microsecond, false},
		{"median shift is not noise", 4500 * time.Microsecond, 7000 * time.Microsecond, 3890 * time.Microsecond, false},
		{"healthy p95 is not noise", 1000 * time.Microsecond, 2000 * time.Microsecond, 3890 * time.Microsecond, false},
		{"boundary dispersion is noise", 1000 * time.Microsecond, 3000 * time.Microsecond, 2500 * time.Microsecond, true},
		{"zero p50 never reads as noise", 0, 6600 * time.Microsecond, 3890 * time.Microsecond, false},
		{"zero reference never reads as noise", 1040 * time.Microsecond, 6600 * time.Microsecond, 0, false},
	}
	for _, tc := range cases {
		if got := winnersDirtyIsTailStallNoise(tc.p50, tc.p95, tc.reference); got != tc.wantNoise {
			t.Errorf("%s: tail-stall noise = %v, want %v (p50 %s, p95 %s, reference %s)",
				tc.name, got, tc.wantNoise, tc.p50, tc.p95, tc.reference)
		}
	}
}

// TestWinnersDirtyStratumFailsComposesGateAndNoise pins the decision the live
// gate enforces: the hybrid conjunction fails a stratum, and the tail-stall
// signature rescues only a median-healthy p95 spike. The #11060 flake
// (both arms tripped, median healthy) passes; a beyond-serialization
// collapse with a shifted median still fails.
func TestWinnersDirtyStratumFailsComposesGateAndNoise(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                string
		p50, p95, reference time.Duration
		ratio, maxRatio     float64
		wantFail            bool
	}{
		{"11060 flake rescued", 1040 * time.Microsecond, 6600 * time.Microsecond, 3890 * time.Microsecond, 4.7, 3.5, false},
		{"collapse with median shift fails", 4500 * time.Microsecond, 7000 * time.Microsecond, 3890 * time.Microsecond, 5.0, 3.5, true},
		{"uniform drift fails", 2500 * time.Microsecond, 5000 * time.Microsecond, 3410 * time.Microsecond, 3.6, 3.0, true},
		{"clean pass stays pass", 1000 * time.Microsecond, 2000 * time.Microsecond, 3890 * time.Microsecond, 1.4, 3.5, false},
		{"absolute-only excursion stays pass", 1000 * time.Microsecond, 5000 * time.Microsecond, 3410 * time.Microsecond, 1.5, 3.0, false},
	}
	for _, tc := range cases {
		if got := winnersDirtyStratumFails(tc.p50, tc.p95, tc.reference, tc.ratio, tc.maxRatio); got != tc.wantFail {
			t.Errorf("%s: stratum fails = %v, want %v (p50 %s, p95 %s vs %s, ratio %.2f vs %.1f)",
				tc.name, got, tc.wantFail, tc.p50, tc.p95, tc.reference, tc.ratio, tc.maxRatio)
		}
	}
}

// TestWinnersDirtyContentionRatioIsQuotient pins the estimator the relative
// gate depends on: stratum p95 over the same-run control p95. A non-positive
// control is +Inf (fail-closed: a missing control never passes the gate).
func TestWinnersDirtyContentionRatioIsQuotient(t *testing.T) {
	t.Parallel()

	if got := winnersDirtyContentionRatio(4000*time.Microsecond, 2000*time.Microsecond); got != 2.0 {
		t.Errorf("contention ratio of 4000us over 2000us = %v, want 2.0", got)
	}
	if got := winnersDirtyContentionRatio(2*time.Millisecond, 2*time.Millisecond); got != 1.0 {
		t.Errorf("contention ratio of equal latencies = %v, want 1.0", got)
	}
	if got := winnersDirtyContentionRatio(time.Millisecond, 0); !math.IsInf(got, 1) {
		t.Errorf("contention ratio over a zero control = %v, want +Inf (fail-closed)", got)
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

// TestWinnersDirtyKeyOrderClauseIsExact pins the rendered lock order byte
// for byte: permuting or dropping one sort column must fail here, not pass
// a substring census.
func TestWinnersDirtyKeyOrderClauseIsExact(t *testing.T) {
	t.Parallel()

	want := "c.kind, c.package_id, c.repository_id, c.subject_digest, c.c0, " +
		"c.advisory_id, c.scope_id, c.generation_id, c.seq"
	if got := winnersDirtyKeyOrderClause("c"); got != want {
		t.Errorf("winnersDirtyKeyOrderClause(c) = %q, want %q", got, want)
	}
}

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
		"ORDER BY si.package_id, si.repository_id, si.subject_digest, si.c0, si.advisory_id",
	} {
		if !strings.Contains(convertWinnersKeyedScopeRemainderSQL, want) {
			t.Errorf("convert scope remainder statement does not contain %q", want)
		}
	}
	// One statement may propose each conflict key only once: the SELECT arm
	// must collapse one identifier arriving under two scope-row sources by
	// grouping on the key, never DISTINCT over (source, key).
	if !strings.Contains(convertWinnersKeyedScopeRemainderSelectSQL, "MIN(c.source)") {
		t.Error("convert SELECT arm does not pick the converted source deterministically via MIN(c.source)")
	}
	if !strings.Contains(convertWinnersKeyedScopeRemainderSelectSQL, "GROUP BY si.package_id") {
		t.Error("convert SELECT arm does not group by the conflict key; a mixed-source remainder would propose one key twice")
	}
	if strings.Contains(convertWinnersKeyedScopeRemainderSelectSQL, "DISTINCT") {
		t.Error("convert SELECT arm still carries DISTINCT; grouping by the conflict key owns dedup")
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
	case selectWinnersStagingResidualSQL:
		// The residual read scans four columns (count, lo, hi, min); the
		// four-destination arm of Scan reads them as an empty queue.
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
	if len(dest) == 4 {
		// The staging residual read (#11129): an empty queue.
		r.rows = r.rows[1:]
		count, ok := dest[0].(*int64)
		lo, okLo := dest[1].(*sql.NullInt64)
		hi, okHi := dest[2].(*sql.NullInt64)
		oldest, okMin := dest[3].(*sql.NullTime)
		if !ok || !okLo || !okHi || !okMin {
			return errSettleScanType
		}
		*count, *lo, *hi, *oldest = 0, sql.NullInt64{}, sql.NullInt64{}, sql.NullTime{}
		return nil
	}
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
	if _, _, _, _, err := settleWinnersKeyedDirtyLog(context.Background(), applied, false, nil); err != nil {
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
	if _, _, _, _, err := settleWinnersKeyedDirtyLog(context.Background(), overCap, true, nil); err != nil {
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
