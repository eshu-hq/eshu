// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11129 ranged-collapse late-commit proofs: a mark that commits below the tick's lo after the bound read, one that commits a lower in-range seq after the fold's snapshot, and one that lands mid-tick all survive; the stamp stays under every unapplied mark's captured_at; the conservation identity holds
//eshu:readiness:env ESHU_POSTGRES_TEST_DSN ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DSN ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DISPOSABLE

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// TestSupplyChainImpactWinnersStagingLateSeqSurvivesRangedCollapseLive drives
// one tick's staging steps in production order (bound read, ranged fold,
// residual read, stamp) and commits writers between them:
//
//   - below: staged before every committed row (lowest seq) but committed
//     after the bound read, so it sits below the tick's lo;
//   - inside: staged between committed rows (a seq inside [lo, bound]) but
//     committed after the fold ran, so the arms' snapshot missed it;
//   - mid: staged and committed after the fold (a seq above the bound).
//
// None may be folded or deleted unmerged: all three stay staged, the residual
// read counts them, the stamp stays under each one's captured_at, and the
// next collapse folds them, so marks == log versions + staging residual.
func TestSupplyChainImpactWinnersStagingLateSeqSurvivesRangedCollapseLive(t *testing.T) {
	ctx, database := winnersStagingPlanDB(t)
	baseVersions, _ := winnersLogVersionsOf(t, ctx, database)
	baseStaged := winnersStagingDepthOf(t, ctx, database)
	stageOne := func(tx *sql.Tx, name string) {
		t.Helper()
		if _, err := tx.ExecContext(ctx, captureWinnersDirtyFactsSQL,
			[]string{"pkg:npm/late-" + name}, []string{"repo-late"}, []string{""}, []string{"CVE-2026-late-" + name}, []string{""}); err != nil {
			t.Fatalf("stage %s: %v", name, err)
		}
	}
	begin := func() *sql.Tx {
		t.Helper()
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin writer: %v", err)
		}
		t.Cleanup(func() { _ = tx.Rollback() })
		return tx
	}

	below := begin()
	stageOne(below, "below")
	committed := begin()
	stageOne(committed, "c1")
	inside := begin()
	stageOne(inside, "inside")
	stageOne(committed, "c2")
	stageOne(committed, "c3")
	if err := committed.Commit(); err != nil {
		t.Fatalf("commit visible rows: %v", err)
	}
	const marks = 6 // below, c1, inside, c2, c3, mid

	tick, err := (SQLDB{DB: database}).Begin(ctx)
	if err != nil {
		t.Fatalf("begin tick: %v", err)
	}
	defer func() { _ = tick.Rollback() }()
	staged, lo, bound, err := readWinnersStagingCollapseBound(ctx, tick, defaultWinnersStagingCollapseMaxRows)
	if err != nil {
		t.Fatalf("bound read: %v", err)
	}
	if staged != 3 {
		t.Fatalf("bound read saw %d rows, want the 3 committed ones", staged)
	}
	var belowSeq int64
	if err := below.QueryRowContext(ctx, `SELECT seq FROM supply_chain_impact_winners_dirty_staging WHERE package_id = 'pkg:npm/late-below'`).Scan(&belowSeq); err != nil {
		t.Fatalf("read below seq in its writer: %v", err)
	}
	if seq := belowSeq; seq >= lo {
		t.Fatalf("setup: below seq %d is not under lo %d", seq, lo)
	}
	if err := below.Commit(); err != nil {
		t.Fatalf("commit below: %v", err)
	}
	outcome, err := foldWinnersStagingRange(ctx, tick, lo, bound)
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	if outcome.rows != 3 || outcome.groups != 3 {
		t.Fatalf("fold = %+v, want the 3 committed rows only (the below row sits under lo)", outcome)
	}
	workMem, err := scanWinnersStagingText(ctx, tick, readWinnersStagingWorkMemSQL, "read work_mem")
	if err != nil {
		t.Fatalf("read work_mem: %v", err)
	}
	if workMem == winnersStagingCollapseWorkMem {
		t.Fatalf("work_mem after the fold = %s, want the session value restored", workMem)
	}
	if err := inside.Commit(); err != nil {
		t.Fatalf("commit inside: %v", err)
	}
	if seq := winnersLateSeqOf(t, ctx, database, "inside"); seq < lo || seq > bound {
		t.Fatalf("setup: inside seq %d is not in [%d, %d]", seq, lo, bound)
	}
	mid := begin()
	stageOne(mid, "mid")
	if err := mid.Commit(); err != nil {
		t.Fatalf("commit mid: %v", err)
	}

	residual, err := countWinnersStagingResidual(ctx, tick, "late-seq residual")
	if err != nil {
		t.Fatalf("residual: %v", err)
	}
	if residual.count != 3 || residual.upperBound || !residual.oldest.Valid {
		t.Fatalf("residual = %+v, want 3 exact rows (below, inside, mid) with a minimum", residual)
	}
	candidate, err := horizonCandidate(residual.oldest)
	if err != nil {
		t.Fatalf("horizon: %v", err)
	}
	if _, err := stampWinnersWatermark(ctx, tick, candidate); err != nil {
		t.Fatalf("stamp: %v", err)
	}
	stamped, err := scanWinnersTime(ctx, tick, selectWinnersWatermarkSQL, "read stamped watermark")
	if err != nil {
		t.Fatalf("read stamped watermark: %v", err)
	}
	if err := tick.Commit(); err != nil {
		t.Fatalf("commit tick: %v", err)
	}
	for _, name := range []string{"below", "inside", "mid"} {
		captured := winnersLateCapturedAt(t, ctx, database, name)
		if !stamped.Before(captured) {
			t.Errorf("watermark %v is not under unapplied %s mark captured at %v", stamped, name, captured)
		}
	}
	if depth := winnersStagingDepthOf(t, ctx, database); depth != 3 {
		t.Fatalf("staging depth after the tick = %d, want the 3 late rows still queued", depth)
	}

	next, err := (SQLDB{DB: database}).Begin(ctx)
	if err != nil {
		t.Fatalf("begin next tick: %v", err)
	}
	defer func() { _ = next.Rollback() }()
	folded, err := collapseWinnersStaging(ctx, next, 0)
	if err != nil {
		t.Fatalf("next collapse: %v", err)
	}
	if err := next.Commit(); err != nil {
		t.Fatalf("commit next tick: %v", err)
	}
	if folded.rows != 3 || folded.split {
		t.Fatalf("next collapse = %+v, want the 3 late rows folded", folded)
	}
	versions, _ := winnersLogVersionsOf(t, ctx, database)
	if left := winnersStagingDepthOf(t, ctx, database); versions-baseVersions+left-baseStaged != marks {
		t.Fatalf("conservation: log versions %d (base %d) + staging residual %d (base %d) != %d marks", versions, baseVersions, left, baseStaged, marks)
	}
}

// winnersLateSeqOf reads the staged seq of one late-test mark.
func winnersLateSeqOf(t *testing.T, ctx context.Context, q *sql.DB, name string) int64 {
	t.Helper()
	var seq int64
	if err := q.QueryRowContext(ctx, `SELECT seq FROM supply_chain_impact_winners_dirty_staging WHERE package_id = $1`,
		"pkg:npm/late-"+name).Scan(&seq); err != nil {
		t.Fatalf("read %s seq: %v", name, err)
	}
	return seq
}

// winnersLateCapturedAt reads the staged capture time of one late-test mark.
func winnersLateCapturedAt(t *testing.T, ctx context.Context, q *sql.DB, name string) time.Time {
	t.Helper()
	var captured time.Time
	if err := q.QueryRowContext(ctx, `SELECT captured_at FROM supply_chain_impact_winners_dirty_staging WHERE package_id = $1`,
		"pkg:npm/late-"+name).Scan(&captured); err != nil {
		t.Fatalf("read %s captured_at: %v", name, err)
	}
	return captured
}
