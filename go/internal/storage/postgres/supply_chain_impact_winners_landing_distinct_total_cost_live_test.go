// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10901 landing replay (host-independent): distinct-key total-cost replay, staging-vs-direct with full collapse accounting and a pre-registered max regression bound
//eshu:readiness:env ESHU_POSTGRES_TEST_DSN ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DSN ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DISPOSABLE

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Landing replay for eshu-hq/eshu#10901, host-independent half: the
// DISTINCT-KEY TOTAL-COST arm (adjudication condition 5). Same host, same
// run, two cells over the same mostly-distinct wave shape:
//
//   - cell "direct": the frozen pre-direction-C capture upsert
//     (hotkeyShimDirectUpsertSQL) once per statement.
//   - cell "staging": the production capture statement
//     (captureWinnersDirtyFactsSQL) once per statement plus ONE
//     collapseWinnersStaging tick transaction (GROUP BY merge-upsert plus
//     the seq-bounded delete) over the whole wave.
//
// Total system cost is writer statement time plus the amortized collapse
// (the merge-upsert and the delete ride inside the timed collapse
// transaction, not a SELECT-only probe). The wave is mostly distinct:
// every statement marks 162 fresh keys and re-marks 18 keys from its
// writer's first statement, so the collapse combines almost nothing and
// the staging cell pays the near-ceiling price. Writers are disjoint
// across writers (no shared hot row), isolating the combining cost from
// lock-wait effects.
//
// PRE-REGISTERED MAX REGRESSION BOUND (written before measuring):
// stagingTotal/directTotal <= 2.0. Rationale: the staging cell performs
// the same logical inserts as the direct cell through a cheaper statement
// (plain INSERT, no arbiter, no version bump), plus exactly one extra full
// pass over the wave (GROUP BY, merge-upsert, delete). A ratio above 2.0
// means the collapse costs more than the entire direct write path and the
// direction-C theory loses on distinct waves: fail closed, do not ship.
const (
	landingDistinctWriters            = 2
	landingDistinctStatementsPerWrite = 25
	landingDistinctFreshPerStatement  = 162
	landingDistinctRemarkPerStatement = 18

	landingDistinctKeysPerStatement = landingDistinctFreshPerStatement + landingDistinctRemarkPerStatement

	// landingDistinctMaxStagingToDirectRatio is the pre-registered
	// maximum regression bound for this gate. See the rationale above.
	landingDistinctMaxStagingToDirectRatio = 2.0
)

// landingDistinctKeys returns writer w's statement s keys in the given
// namespace: statement 0 marks all-fresh keys, later statements mark
// mostly-fresh keys plus a fixed re-mark tail from statement 0.
func landingDistinctKeys(namespace string, w, s int) []string {
	keys := make([]string, 0, landingDistinctKeysPerStatement)
	fresh := func(i int) string {
		return fmt.Sprintf("pkg:%s:%02d:%03d:%03d", namespace, w, s, i)
	}
	if s == 0 {
		for i := 0; i < landingDistinctKeysPerStatement; i++ {
			keys = append(keys, fresh(i))
		}
		return keys
	}
	for i := 0; i < landingDistinctFreshPerStatement; i++ {
		keys = append(keys, fresh(i))
	}
	for i := 0; i < landingDistinctRemarkPerStatement; i++ {
		keys = append(keys, fmt.Sprintf("pkg:%s:%02d:%03d:%03d", namespace, w, 0, i))
	}
	return keys
}

func TestSupplyChainImpactWinnersLandingDistinctTotalCostLive(t *testing.T) {
	ctx, database := winnersSettleGateDB(t)

	// Bootstrap seeds can stage residue through the capture triggers; the
	// production tick drains continuously, so absorb that residue in one
	// collapse before the baselines. Every wave below then measures exactly
	// its own marks.
	tx0, err := (SQLDB{DB: database}).Begin(ctx)
	if err != nil {
		t.Fatalf("begin bootstrap-residue drain: %v", err)
	}
	if _, err := collapseWinnersStaging(ctx, tx0, 0); err != nil {
		_ = tx0.Rollback()
		t.Fatalf("drain bootstrap residue: %v", err)
	}
	if err := tx0.Commit(); err != nil {
		t.Fatalf("commit bootstrap-residue drain: %v", err)
	}

	const repos, digest, cve = "repo:landing-distinct", "digest:landing-distinct", "CVE-2026-1107"
	sized := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = repos
		}
		return out
	}
	digests := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = digest
		}
		return out
	}
	cves := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = cve
		}
		return out
	}

	var failed atomic.Int64
	record := func(err error) {
		if err == nil {
			return
		}
		failed.Add(1)
		t.Errorf("landing distinct-cost accessor failed: %v", err)
	}
	isDeadlock := func(err error) bool {
		return err != nil && (strings.Contains(err.Error(), "40P01") ||
			strings.Contains(err.Error(), "deadlock detected"))
	}

	// runWave hammers stmt (direct upsert or staging capture) with disjoint
	// writers over the mostly-distinct layout and returns the per-statement
	// durations plus the total staged/marked row count.
	runWave := func(cell, namespace, stmt string) ([]time.Duration, int64) {
		var mu sync.Mutex
		var durs []time.Duration
		var marks atomic.Int64
		start := make(chan struct{})
		var workers sync.WaitGroup
		for w := 0; w < landingDistinctWriters; w++ {
			workers.Add(1)
			go func(w int) {
				defer workers.Done()
				<-start
				local := make([]time.Duration, 0, landingDistinctStatementsPerWrite)
				for s := 0; s < landingDistinctStatementsPerWrite; s++ {
					pkgs := landingDistinctKeys(namespace, w, s)
					begin := time.Now()
					res, err := database.ExecContext(ctx, stmt,
						pkgs, sized(len(pkgs)), digests(len(pkgs)), cves(len(pkgs)), cves(len(pkgs)))
					elapsed := time.Since(begin)
					if err != nil {
						if isDeadlock(err) {
							t.Errorf("landing distinct %s cell hit a lock-order cycle (40P01): %v", cell, err)
						}
						record(err)
						return
					}
					n, err := res.RowsAffected()
					if err != nil {
						record(err)
						return
					}
					marks.Add(n)
					local = append(local, elapsed)
				}
				mu.Lock()
				durs = append(durs, local...)
				mu.Unlock()
			}(w)
		}
		close(start)
		workers.Wait()
		return durs, marks.Load()
	}

	sumDurations := func(durs []time.Duration) time.Duration {
		var total time.Duration
		for _, d := range durs {
			total += d
		}
		return total
	}

	resetLog := func(cell string) {
		if _, err := database.ExecContext(ctx, `DELETE FROM supply_chain_impact_winners_dirty_log`); err != nil {
			t.Fatalf("reset dirty log after %s cell: %v", cell, err)
		}
		if got := winnersStagingDepthOf(t, ctx, database); got != 0 {
			t.Fatalf("staging holds %d rows after %s cell, want an empty queue", got, cell)
		}
	}

	// Cell 1: production staging capture over the distinct layout, then one
	// tick-shaped collapse transaction. The collapse time is the full
	// merge-upsert plus the seq-bounded delete, not a SELECT probe.
	stagingBase, _ := winnersLogVersionsOf(t, ctx, database)
	stagingDurs, stagingMarks := runWave("staging", "landing-staging", captureWinnersDirtyFactsSQL)
	if failed.Load() != 0 {
		t.Fatalf("staging wave: %d accessor(s) failed", failed.Load())
	}
	stagingWriterSum := sumDurations(stagingDurs)
	collapseBegin := time.Now()
	tx, err := (SQLDB{DB: database}).Begin(ctx)
	if err != nil {
		t.Fatalf("begin distinct-wave collapse: %v", err)
	}
	outcome, err := collapseWinnersStaging(ctx, tx, 0)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("distinct-wave collapse: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit distinct-wave collapse: %v", err)
	}
	collapseElapsed := time.Since(collapseBegin)
	stagingVersions, _ := winnersLogVersionsOf(t, ctx, database)
	if stagingVersions-stagingBase != stagingMarks {
		t.Fatalf("staging conservation broken: log version growth = %d, want %d staged marks", stagingVersions-stagingBase, stagingMarks)
	}
	const wantDistinctPerWriter = landingDistinctKeysPerStatement + (landingDistinctStatementsPerWrite-1)*landingDistinctFreshPerStatement
	if outcome.rows != stagingMarks || outcome.groups != int64(landingDistinctWriters*wantDistinctPerWriter) {
		t.Fatalf("staging collapse outcome = %+v, want {groups %d, rows %d}",
			outcome, landingDistinctWriters*wantDistinctPerWriter, stagingMarks)
	}
	stagingTotal := stagingWriterSum + collapseElapsed
	t.Logf("landing_distinct cell=staging writers=%d statements=%d marks=%d groups=%d writer_sum=%s (%.6fs) collapse=%s (%.6fs) total=%s (%.6fs)",
		landingDistinctWriters, len(stagingDurs), stagingMarks, outcome.groups,
		stagingWriterSum, stagingWriterSum.Seconds(), collapseElapsed, collapseElapsed.Seconds(),
		stagingTotal, stagingTotal.Seconds())
	resetLog("staging")

	// Cell 2: frozen direct upsert over the same distinct layout.
	directBase, _ := winnersLogVersionsOf(t, ctx, database)
	directDurs, directMarks := runWave("direct", "landing-direct", hotkeyShimDirectUpsertSQL)
	if failed.Load() != 0 {
		t.Fatalf("direct wave: %d accessor(s) failed", failed.Load())
	}
	directVersions, _ := winnersLogVersionsOf(t, ctx, database)
	if directVersions-directBase != directMarks {
		t.Fatalf("direct conservation broken: log version growth = %d, want %d upserted marks", directVersions-directBase, directMarks)
	}
	directTotal := sumDurations(directDurs)
	t.Logf("landing_distinct cell=direct writers=%d statements=%d marks=%d writer_sum=%s (%.6fs)",
		landingDistinctWriters, len(directDurs), directMarks, directTotal, directTotal.Seconds())
	resetLog("direct")

	if stagingMarks != directMarks {
		t.Fatalf("wave mismatch: staging marks = %d, direct marks = %d; the cells did different logical work", stagingMarks, directMarks)
	}
	ratio := float64(stagingTotal) / float64(directTotal)
	t.Logf("landing_distinct verdict staging_total=%s (%.6fs) direct_total=%s (%.6fs) ratio=%.3f bound=%.2f marks=%d",
		stagingTotal, stagingTotal.Seconds(), directTotal, directTotal.Seconds(), ratio,
		landingDistinctMaxStagingToDirectRatio, stagingMarks)
	if ratio > landingDistinctMaxStagingToDirectRatio {
		t.Errorf("landing_distinct KILL: staging total %s exceeds %.2fx the direct total %s (ratio %.3f): direction C loses on distinct waves beyond the pre-registered bound",
			stagingTotal, landingDistinctMaxStagingToDirectRatio, directTotal, ratio)
	}
}
