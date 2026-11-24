// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10837 direction C landing proofs: shared staging collapse conservation (version/captured_at/source arithmetic, one-row control, re-mark survival, budget split with staging-union horizon), writer-vs-collapse interleave at 1/2/4 writers with zero 40P01, worst-partition collapse cost, and key-ordered EXPLAIN pins
//eshu:readiness:env ESHU_POSTGRES_TEST_DSN ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DSN ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DISPOSABLE

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Live proofs for eshu-hq/eshu#10837 direction C (hot-key coalescing): the
// shared staging queue plus the in-tick collapse. Every capture owner
// plain-INSERTs into supply_chain_impact_winners_dirty_staging; the tick
// folds each wave into the versioned dirty log with one atomic key-ordered
// GROUP BY merge-upsert per kind plus the seq-bounded delete.
//
// The exact conservation identity these tests assert is:
//
//	marks == drained version sums + residual log versions + staging residual
//
// One staged row is one mark, so the stranded remainder is counted in rows,
// not versions.

// stageFactRows stages one row per mark through the production capture
// statement and returns the staged row count.
func stageFactRows(t *testing.T, ctx context.Context, database *sql.DB, pkgs, repos, digests, c0s, advisories []string) int64 {
	t.Helper()
	result, err := database.ExecContext(ctx, captureWinnersDirtyFactsSQL, pkgs, repos, digests, c0s, advisories)
	if err != nil {
		t.Fatalf("stage fact rows: %v", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		t.Fatalf("count staged rows: %v", err)
	}
	return rows
}

// winnersStagingEarliestFactRow reads the earliest staged fact row back for
// the key: the mark the collapse must take its captured_at and source from.
func winnersStagingEarliestFactRow(t *testing.T, ctx context.Context, q *sql.DB, pkg, c0 string) (seq int64, source string, captured time.Time) {
	t.Helper()
	rows, err := q.QueryContext(ctx, `SELECT seq, source, captured_at FROM supply_chain_impact_winners_dirty_staging
WHERE kind = 'fact' AND package_id = $1 AND c0 = $2 ORDER BY seq LIMIT 1`, pkg, c0)
	if err != nil {
		t.Fatalf("read staged row: %v", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		t.Fatalf("no staged row for (%s, %s)", pkg, c0)
	}
	if err := rows.Scan(&seq, &source, &captured); err != nil {
		t.Fatalf("scan staged row: %v", err)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read staged row: %v", err)
	}
	return seq, source, captured
}

// winnersCollapsedLogRow reads one collapsed log row back for the key.
func winnersCollapsedLogRow(t *testing.T, ctx context.Context, q *sql.DB, pkg, c0 string) (version int64, captured time.Time, source string) {
	t.Helper()
	if err := q.QueryRowContext(ctx, `SELECT dirty_version, captured_at, source FROM supply_chain_impact_winners_dirty_log
WHERE kind = 'fact' AND package_id = $1 AND c0 = $2`, pkg, c0).Scan(&version, &captured, &source); err != nil {
		t.Fatalf("read collapsed log row: %v", err)
	}
	return version, captured, source
}

// winnersStagingDepthOf reads the staging queue's row count.
func winnersStagingDepthOf(t *testing.T, ctx context.Context, q *sql.DB) int64 {
	t.Helper()
	var n int64
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM supply_chain_impact_winners_dirty_staging`).Scan(&n); err != nil {
		t.Fatalf("read staging depth: %v", err)
	}
	return n
}

// winnersLogVersionsOf reads the dirty log's version sums and row depth.
// Call it on the database after the tick commits: in-tick reads go through
// the tick transaction below.
func winnersLogVersionsOf(t *testing.T, ctx context.Context, q *sql.DB) (versions, depth int64) {
	t.Helper()
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(sum(dirty_version), 0), count(*) FROM supply_chain_impact_winners_dirty_log`).Scan(&versions, &depth); err != nil {
		t.Fatalf("read log versions: %v", err)
	}
	return versions, depth
}

// requireStagingEmptyInTx fails the tick-shaped transaction when the
// collapse left a staged row behind.
func requireStagingEmptyInTx(t *testing.T, ctx context.Context, tx db.Transaction) {
	t.Helper()
	rows, err := tx.QueryContext(ctx, `SELECT count(*) FROM supply_chain_impact_winners_dirty_staging`)
	if err != nil {
		t.Fatalf("read staging depth in tick: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var n int64
	if rows.Next() {
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan staging depth in tick: %v", err)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read staging depth in tick: %v", err)
	}
	if n != 0 {
		t.Fatalf("staging holds %d rows after the collapse, want an empty queue", n)
	}
}

// TestSupplyChainImpactWinnersStagingCollapseConservationLive is the failing-first
// regression (1): marks staged through the production capture statement
// collapse to exact log versions. Two waves over the same keys — the second
// with a different source — prove the version arithmetic (new key sets
// version to the staged count; conflict adds the staged count, never +1),
// the first-mark captured_at preservation, and the earliest-seq source.
func TestSupplyChainImpactWinnersStagingCollapseConservationLive(t *testing.T) {
	ctx, database := winnersSettleGateDB(t)

	const cve = "CVE-2026-1100"
	mk := func(n int, base string) (pkgs, repos, digests, c0s, advisories []string) {
		for i := 0; i < n; i++ {
			pkgs = append(pkgs, fmt.Sprintf("pkg:collapse:%s:%03d", base, i))
			repos = append(repos, "repo:collapse")
			digests = append(digests, "digest:collapse")
			c0s = append(c0s, cve)
			advisories = append(advisories, cve)
		}
		return pkgs, repos, digests, c0s, advisories
	}

	// The bootstrap moves scope pointers, so the activation trigger owns
	// baseline log rows before the gate stages anything. Conservation is
	// exact on deltas: staged marks == log version growth + staging
	// residual.
	baseVersions, baseDepth := winnersLogVersionsOf(t, ctx, database)

	// Wave one: 5 keys x 3 marks, all fact_upsert.
	pkgs, repos, digests, c0s, advisories := mk(5, "a")
	var marks int64
	for m := 0; m < 3; m++ {
		marks += stageFactRows(t, ctx, database, pkgs, repos, digests, c0s, advisories)
	}
	if marks != 15 {
		t.Fatalf("staged marks = %d, want 15: the capture must stage one row per mark", marks)
	}
	var firstCaptured time.Time
	if _, _, firstCaptured = winnersStagingEarliestFactRow(t, ctx, database, pkgs[0], cve); firstCaptured.IsZero() {
		t.Fatal("first staged row has no captured_at")
	}

	collapse := func(budget int64) winnersStagingCollapseOutcome {
		tx, err := (SQLDB{DB: database}).Begin(ctx)
		if err != nil {
			t.Fatalf("begin collapse: %v", err)
		}
		defer func() { _ = tx.Rollback() }()
		outcome, err := collapseWinnersStaging(ctx, tx, budget)
		if err != nil {
			t.Fatalf("collapse: %v", err)
		}
		requireStagingEmptyInTx(t, ctx, tx)
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit collapse: %v", err)
		}
		return outcome
	}
	outcome := collapse(0)
	if outcome.groups != 5 || outcome.rows != 15 {
		t.Fatalf("collapse outcome = %+v, want {groups 5, rows 15}", outcome)
	}
	if outcome.split {
		t.Fatal("collapse of 15 staged rows split under the default budget")
	}
	// New keys set version to the staged count; captured_at is the staged
	// minimum and the source is the earliest staged seq's row.
	version, captured, source := winnersCollapsedLogRow(t, ctx, database, pkgs[0], cve)
	if version != 3 {
		t.Fatalf("collapsed version = %d, want 3 staged marks", version)
	}
	if !captured.Equal(firstCaptured) {
		t.Fatalf("collapsed captured_at = %v, want the staged minimum %v", captured, firstCaptured)
	}
	if source != "fact_upsert" {
		t.Fatalf("collapsed source = %q, want the earliest staged seq's fact_upsert", source)
	}

	// Wave two: 2 more marks per key, staged as fact_retract through the
	// production retract path shape (same table, different source). The
	// collapse must add the staged count, preserve the first captured_at,
	// and keep the first-marker source.
	for _, pkg := range pkgs {
		if _, err := database.ExecContext(ctx, `INSERT INTO supply_chain_impact_winners_dirty_staging
(kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
VALUES ('fact', 'fact_retract', $1, 'repo:collapse', 'digest:collapse', $2, $2)`, pkg, cve); err != nil {
			t.Fatalf("stage retract mark: %v", err)
		}
		if _, err := database.ExecContext(ctx, `INSERT INTO supply_chain_impact_winners_dirty_staging
(kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
VALUES ('fact', 'fact_retract', $1, 'repo:collapse', 'digest:collapse', $2, $2)`, pkg, cve); err != nil {
			t.Fatalf("stage retract mark: %v", err)
		}
		marks += 2
	}
	outcome = collapse(0)
	if outcome.groups != 5 || outcome.rows != 10 {
		t.Fatalf("second collapse outcome = %+v, want {groups 5, rows 10}", outcome)
	}
	version, captured, source = winnersCollapsedLogRow(t, ctx, database, pkgs[0], cve)
	if version != 5 {
		t.Fatalf("collapsed version after re-marks = %d, want 3 + 2 staged marks (never +1)", version)
	}
	if !captured.Equal(firstCaptured) {
		t.Fatalf("collapsed captured_at moved to %v, want the preserved first mark %v", captured, firstCaptured)
	}
	if source != "fact_upsert" {
		t.Fatalf("collapsed source = %q, want the preserved first-marker fact_upsert", source)
	}

	// Exact conservation: 25 staged marks, no drain ran, so the log grew
	// by every version and the queue is empty.
	versions, depth := winnersLogVersionsOf(t, ctx, database)
	if versions-baseVersions != marks {
		t.Fatalf("log version growth = %d, want %d staged marks", versions-baseVersions, marks)
	}
	if depth-baseDepth != 5 {
		t.Fatalf("log depth growth = %d, want 5 collapsed key rows", depth-baseDepth)
	}
	if got := winnersStagingDepthOf(t, ctx, database); got != 0 {
		t.Fatalf("staging depth = %d, want an empty queue", got)
	}
}

// TestSupplyChainImpactWinnersStagingCollapseDeletesExactlyOneRowLive is the
// control: one staged mark collapses to exactly one log row at version 1,
// and the queue drains to zero.
func TestSupplyChainImpactWinnersStagingCollapseDeletesExactlyOneRowLive(t *testing.T) {
	ctx, database := winnersSettleGateDB(t)
	baseVersions, baseDepth := winnersLogVersionsOf(t, ctx, database)

	if n := stageFactRows(t, ctx, database,
		[]string{"pkg:control:001"}, []string{"repo:control"}, []string{"digest:control"},
		[]string{"CVE-2026-1101"}, []string{"CVE-2026-1101"}); n != 1 {
		t.Fatalf("staged rows = %d, want the one control mark", n)
	}
	tx, err := (SQLDB{DB: database}).Begin(ctx)
	if err != nil {
		t.Fatalf("begin collapse: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	outcome, err := collapseWinnersStaging(ctx, tx, 0)
	if err != nil {
		t.Fatalf("collapse: %v", err)
	}
	if outcome.groups != 1 || outcome.rows != 1 {
		t.Fatalf("control collapse outcome = %+v, want {groups 1, rows 1}", outcome)
	}
	requireStagingEmptyInTx(t, ctx, tx)
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit collapse: %v", err)
	}
	versions, depth := winnersLogVersionsOf(t, ctx, database)
	if versions-baseVersions != 1 || depth-baseDepth != 1 {
		t.Fatalf("log holds versions=%d depth=%d, want growth of exactly one row at version 1", versions-baseVersions, depth-baseDepth)
	}
}

// TestSupplyChainImpactWinnersStagingReMarkSurvivesInflightSettleLive proves
// the re-mark survival the version recheck needs: a mark committed for an
// admitted key after admission stages, the collapse folds it into the log
// (bumping past the admitted version), and the settle's recheck leaves the
// key queued. The next tick drains it with the full version.
func TestSupplyChainImpactWinnersStagingReMarkSurvivesInflightSettleLive(t *testing.T) {
	ctx, database := winnersSettleGateDB(t)
	// Clear the bootstrap baseline: the conservation below is exact on the
	// test's own rows.
	if _, err := database.ExecContext(ctx, `DELETE FROM supply_chain_impact_winners_dirty_log`); err != nil {
		t.Fatalf("clear log baseline: %v", err)
	}
	if _, err := database.ExecContext(ctx, `DELETE FROM supply_chain_impact_winners_dirty_staging`); err != nil {
		t.Fatalf("clear staging baseline: %v", err)
	}
	begin := func() db.Transaction {
		tx, err := (SQLDB{DB: database}).Begin(ctx)
		if err != nil {
			t.Fatalf("begin tick: %v", err)
		}
		return tx
	}
	const pkg, cve = "pkg:survive:001", "CVE-2026-1102"

	// Plant one queued log row at version 1: it stands for a mark the tick
	// admitted.
	if _, err := database.ExecContext(ctx, `INSERT INTO supply_chain_impact_winners_dirty_log
(kind, source, package_id, repository_id, subject_digest, c0, advisory_id, dirty_version)
VALUES ('fact', 'fact_upsert', $1, 'repo:survive', 'digest:survive', $2, $2, 1)`, pkg, cve); err != nil {
		t.Fatalf("plant queued log row: %v", err)
	}

	// The tick admits the window (snapshotting version 1) and stays open.
	tick := begin()
	defer func() { _ = tick.Rollback() }()
	if _, err := admitWinnersDirtyWindow(ctx, tick, winnersDeadlockSettleBounds.dirtyRows); err != nil {
		t.Fatalf("admit tick window: %v", err)
	}

	// A re-mark commits for the admitted key after admission: through the
	// production capture statement, so it stages.
	if n := stageFactRows(t, ctx, database,
		[]string{pkg}, []string{"repo:survive"}, []string{"digest:survive"},
		[]string{cve}, []string{cve}); n != 1 {
		t.Fatalf("staged re-mark rows = %d, want 1", n)
	}

	// The collapse folds the staged re-mark into the log, bumping it past
	// the admitted version; the settle's version recheck must then leave
	// the key queued for the next tick.
	outcome, err := collapseWinnersStaging(ctx, tick, 0)
	if err != nil {
		t.Fatalf("collapse in tick: %v", err)
	}
	if outcome.groups != 1 || outcome.rows != 1 {
		t.Fatalf("re-mark collapse outcome = %+v, want {groups 1, rows 1}", outcome)
	}
	for _, stmt := range []string{
		buildWinnersKeyedScopeIdentsFromSupportsSQL,
		buildWinnersKeyedIdentsSQL,
		analyzeWinnersKeyedIdentsSQL,
	} {
		if _, err := tick.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("build identifiers in tick: %v", err)
		}
	}
	if _, err := buildWinnersKeyedChunk(ctx, tick, winnersDeadlockSettleBounds); err != nil {
		t.Fatalf("build chunk in tick: %v", err)
	}
	tally, remaining, _, _, err := settleWinnersKeyedDirtyLog(ctx, tick, false, nil)
	if err != nil {
		t.Fatalf("settle in tick: %v", err)
	}
	if tally.total() != 0 {
		t.Fatalf("settle drained version sums = %d, want 0: the re-marked key must stay queued", tally.total())
	}
	if remaining != 1 {
		t.Fatalf("settle remaining = %d, want the one re-marked key still queued", remaining)
	}
	if err := tick.Commit(); err != nil {
		t.Fatalf("commit tick: %v", err)
	}

	// Exact conservation: 1 planted version + 1 staged re-mark = 2 log
	// versions, residual, with an empty staging queue.
	versions, depth := winnersLogVersionsOf(t, ctx, database)
	if versions != 2 || depth != 1 {
		t.Fatalf("log holds versions=%d depth=%d, want versions=2 depth=1", versions, depth)
	}
	if got := winnersStagingDepthOf(t, ctx, database); got != 0 {
		t.Fatalf("staging depth = %d, want an empty queue", got)
	}
}

// TestSupplyChainImpactWinnersStagingCollapseSplitKeepsStagingUnionHorizonLive
// proves the per-tick collapse budget: a wave bigger than the budget splits,
// the bound collapses the oldest rows first, and the residual unions the
// staging remainder so the horizon covers it. It also proves the split is
// not a loss: a second collapse at the same budget drains the rest.
func TestSupplyChainImpactWinnersStagingCollapseSplitKeepsStagingUnionHorizonLive(t *testing.T) {
	ctx, database := winnersSettleGateDB(t)

	const pkg, cve = "pkg:split:001", "CVE-2026-1103"
	const staged = 300
	const budget = 100
	for i := 0; i < staged; i++ {
		if _, err := database.ExecContext(ctx, `INSERT INTO supply_chain_impact_winners_dirty_staging
(kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
VALUES ('fact', 'fact_upsert', $1, 'repo:split', 'digest:split', $2, $2)`, pkg, cve); err != nil {
			t.Fatalf("stage split wave row %d: %v", i, err)
		}
	}

	collapse := func(budget int64) winnersStagingCollapseOutcome {
		tx, err := (SQLDB{DB: database}).Begin(ctx)
		if err != nil {
			t.Fatalf("begin collapse: %v", err)
		}
		defer func() { _ = tx.Rollback() }()
		outcome, err := collapseWinnersStaging(ctx, tx, budget)
		if err != nil {
			t.Fatalf("collapse: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit collapse: %v", err)
		}
		return outcome
	}
	first := collapse(budget)
	if !first.split {
		t.Fatal("collapse of 300 staged rows at budget 100 did not report the wave split")
	}
	if first.groups != 1 || first.rows != budget {
		t.Fatalf("split collapse outcome = %+v, want {groups 1, rows 100}", first)
	}
	version, _, _ := winnersCollapsedLogRow(t, ctx, database, pkg, cve)
	if version != budget {
		t.Fatalf("collapsed version after the split = %d, want the oldest %d staged marks", version, budget)
	}
	if got := winnersStagingDepthOf(t, ctx, database); got != staged-budget {
		t.Fatalf("staging depth after the split = %d, want %d post-bound rows still queued", got, staged-budget)
	}

	// The residual unions the staging remainder: count adds, and the horizon
	// minimum is the staging minimum (the log is otherwise empty).
	residual, err := countWinnersStagingResidual(ctx, SQLDB{DB: database}, "read split residual")
	if err != nil {
		t.Fatalf("residual: %v", err)
	}
	if residual.count != staged-budget || !residual.oldest.Valid || residual.upperBound {
		t.Fatalf("staging residual = (%d, valid=%v, upper=%v), want (200, valid, exact)", residual.count, residual.oldest.Valid, residual.upperBound)
	}
	remaining, horizon := stagingUnionRemaining(0, sql.NullTime{}, residual)
	if remaining != staged-budget || !horizon.Valid {
		t.Fatalf("union remainder = (%d, valid=%v), want (200, valid)", remaining, horizon.Valid)
	}

	// The split is not a loss: the next budget-sized collapse drains the
	// rest and the log version reaches the full staged count.
	second := collapse(budget)
	if !second.split {
		t.Fatal("collapse of the 200-row remainder at budget 100 did not report the wave split")
	}
	third := collapse(budget)
	if third.split {
		t.Fatal("collapse of the final 100 staged rows reported a split")
	}
	if third.rows != budget {
		t.Fatalf("final collapse rows = %d, want %d", third.rows, budget)
	}
	version, _, _ = winnersCollapsedLogRow(t, ctx, database, pkg, cve)
	if version != staged {
		t.Fatalf("collapsed version after the split drain = %d, want %d staged marks", version, staged)
	}
	if got := winnersStagingDepthOf(t, ctx, database); got != 0 {
		t.Fatalf("staging depth after the split drain = %d, want an empty queue", got)
	}
}

// TestSupplyChainImpactWinnersStagingInterleaveDeadlockFreeLive is the
// direction-C rerun of the writer-vs-settle interleave gate: stratified at
// 1, 2 and 4 writers, every writer hammers the shared hot keys through the
// production staging INSERT while collapse-in-tick folds and deletes. Zero
// 40P01 may surface and no statement may retry; the test fails on the first
// deadlock instead of absorbing it. Conservation closes every stratum:
// staged marks == log version sums + staging residual.
func TestSupplyChainImpactWinnersStagingInterleaveDeadlockFreeLive(t *testing.T) {
	ctx, database := winnersSettleGateDB(t)

	isDeadlock := func(err error) bool {
		return err != nil && (strings.Contains(err.Error(), "40P01") ||
			strings.Contains(err.Error(), "deadlock detected"))
	}

	// The bootstrap owns baseline log versions (its scope-pointer moves),
	// so conservation is exact on growth: the log's version growth per
	// stratum equals that stratum's staged marks.
	baseVersions, _ := winnersLogVersionsOf(t, ctx, database)
	for _, writers := range []int{1, 2, 4} {
		var failed atomic.Int64
		record := func(err error) {
			if err == nil {
				return
			}
			failed.Add(1)
		}
		var marks atomic.Int64
		stop := make(chan struct{})
		var collapseWorker sync.WaitGroup
		var writersDone sync.WaitGroup

		// The collapse-in-tick loop: one transaction per collapse, no
		// retry on any error, so a lock cycle fails the gate loudly.
		collapseWorker.Add(1)
		go func() {
			defer collapseWorker.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				tx, err := (SQLDB{DB: database}).Begin(ctx)
				if err != nil {
					record(err)
					return
				}
				if _, err := collapseWinnersStaging(ctx, tx, 0); err != nil {
					_ = tx.Rollback()
					if isDeadlock(err) {
						t.Errorf("collapse-in-tick hit a lock-order cycle (40P01): %v", err)
					}
					record(err)
					return
				}
				if err := tx.Commit(); err != nil {
					if isDeadlock(err) {
						t.Errorf("collapse-in-tick commit hit a lock-order cycle (40P01): %v", err)
					}
					record(err)
					return
				}
			}
		}()

		// Writers hammer the same hot keys through the plain staging
		// INSERT: no arbiter, no lock order to keep, no retry.
		for w := 0; w < writers; w++ {
			writersDone.Add(1)
			go func(w int) {
				defer writersDone.Done()
				for i := 0; i < 10; i++ {
					marks.Add(stageFactRowsInGate(t, ctx, database, w, record, isDeadlock))
					if failed.Load() != 0 {
						return
					}
				}
			}(w)
		}
		writersDone.Wait()
		close(stop)
		collapseWorker.Wait()

		drain := func() {
			for i := 0; i < 20; i++ {
				tx, err := (SQLDB{DB: database}).Begin(ctx)
				if err != nil {
					t.Fatalf("writers=%d begin final drain: %v", writers, err)
				}
				outcome, err := collapseWinnersStaging(ctx, tx, 0)
				if err != nil {
					_ = tx.Rollback()
					t.Fatalf("writers=%d final drain: %v", writers, err)
				}
				if err := tx.Commit(); err != nil {
					t.Fatalf("writers=%d commit final drain: %v", writers, err)
				}
				if !outcome.split && outcome.rows == 0 {
					return
				}
			}
			t.Fatalf("writers=%d: staging did not drain after 20 collapses", writers)
		}
		drain()
		if failed.Load() != 0 {
			t.Fatalf("writers=%d: %d interleaved accessor(s) failed; the gate needs zero failures", writers, failed.Load())
		}
		versions, _ := winnersLogVersionsOf(t, ctx, database)
		if staged := winnersStagingDepthOf(t, ctx, database); staged != 0 {
			t.Fatalf("writers=%d: staging holds %d rows after the drain", writers, staged)
		}
		t.Logf("interleave writers=%d marks=%d log_versions=%d", writers, marks.Load(), versions)
		if versions-baseVersions != marks.Load() {
			t.Fatalf("writers=%d: log version growth = %d, want %d staged marks", writers, versions-baseVersions, marks.Load())
		}
		// Reset for the next stratum: the conservation above already
		// proves the drain, and the reset clears the baseline with it.
		if _, err := database.ExecContext(ctx, `DELETE FROM supply_chain_impact_winners_dirty_log`); err != nil {
			t.Fatalf("writers=%d reset log: %v", writers, err)
		}
		baseVersions = 0
	}
}

// stageFactRowsInGate stages one hot-key wave, recording (never retrying)
// the first failure. It returns the staged mark count, or 0 after a failure.
func stageFactRowsInGate(t *testing.T, ctx context.Context, database *sql.DB, w int, record func(error), isDeadlock func(error) bool) int64 {
	t.Helper()
	const hot, tail = 162, 18
	pkgs := make([]string, 0, hot+tail)
	for g := 0; g < hot; g++ {
		pkgs = append(pkgs, fmt.Sprintf("pkg:hotkey:%03d", g))
	}
	for g := 0; g < tail; g++ {
		pkgs = append(pkgs, fmt.Sprintf("pkg:hotkey-tail:%02d:%03d", w, g))
	}
	const repos, digest, cve = "repo:hotkey", "digest:hotkey", "CVE-2026-1104"
	reposArr := make([]string, len(pkgs))
	digestArr := make([]string, len(pkgs))
	cveArr := make([]string, len(pkgs))
	for i := range pkgs {
		reposArr[i], digestArr[i], cveArr[i] = repos, digest, cve
	}
	result, err := database.ExecContext(ctx, captureWinnersDirtyFactsSQL, pkgs, reposArr, digestArr, cveArr, cveArr)
	if err != nil {
		if isDeadlock(err) {
			t.Errorf("staging writer hit a lock-order cycle (40P01): %v", err)
		}
		record(err)
		return 0
	}
	rows, err := result.RowsAffected()
	if err != nil {
		record(err)
		return 0
	}
	return rows
}

// TestSupplyChainImpactWinnersStagingCollapseWorstPartitionCostLive measures
// the no-combining ceiling: 24,300 distinct staged keys collapse in one wave
// at the default budget, and the test asserts the conservation (24,300
// groups, version sums 24,300) while logging the collapse cost and the
// heap/dead-tuple note the shared-table validity proof needs.
func TestSupplyChainImpactWinnersStagingCollapseWorstPartitionCostLive(t *testing.T) {
	ctx, database := winnersSettleGateDB(t)

	const distinct = 24300
	const batch = 180
	baseVersions, baseDepth := winnersLogVersionsOf(t, ctx, database)
	for base := 0; base < distinct; base += batch {
		end := base + batch
		if end > distinct {
			end = distinct
		}
		pkgs := make([]string, 0, end-base)
		for g := base; g < end; g++ {
			pkgs = append(pkgs, fmt.Sprintf("pkg:worst:%05d", g))
		}
		const repos, digest, cve = "repo:worst", "digest:worst", "CVE-2026-1105"
		reposArr := make([]string, len(pkgs))
		digestArr := make([]string, len(pkgs))
		cveArr := make([]string, len(pkgs))
		for i := range pkgs {
			reposArr[i], digestArr[i], cveArr[i] = repos, digest, cve
		}
		stageFactRows(t, ctx, database, pkgs, reposArr, digestArr, cveArr, cveArr)
	}
	if got := winnersStagingDepthOf(t, ctx, database); got != distinct {
		t.Fatalf("staged worst-partition rows = %d, want %d", got, distinct)
	}
	var sizeBefore, deadBefore int64
	if err := database.QueryRowContext(ctx, `SELECT pg_total_relation_size('supply_chain_impact_winners_dirty_staging')`).Scan(&sizeBefore); err != nil {
		t.Fatalf("staging size before collapse: %v", err)
	}
	if err := database.QueryRowContext(ctx, `SELECT n_dead_tup FROM pg_stat_user_tables WHERE relname = 'supply_chain_impact_winners_dirty_staging'`).Scan(&deadBefore); err != nil {
		t.Fatalf("staging dead tuples before collapse: %v", err)
	}

	begin := time.Now()
	tx, err := (SQLDB{DB: database}).Begin(ctx)
	if err != nil {
		t.Fatalf("begin worst-partition collapse: %v", err)
	}
	outcome, err := collapseWinnersStaging(ctx, tx, 0)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("worst-partition collapse: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit worst-partition collapse: %v", err)
	}
	elapsed := time.Since(begin)
	if outcome.groups != distinct || outcome.rows != distinct {
		t.Fatalf("worst-partition collapse outcome = %+v, want {groups %d, rows %d}", outcome, distinct, distinct)
	}
	versions, depth := winnersLogVersionsOf(t, ctx, database)
	if versions-baseVersions != distinct || depth-baseDepth != distinct {
		t.Fatalf("log holds versions=%d depth=%d, want growth of %d distinct keys at version 1", versions-baseVersions, depth-baseDepth, distinct)
	}
	if got := winnersStagingDepthOf(t, ctx, database); got != 0 {
		t.Fatalf("staging depth after worst-partition collapse = %d, want an empty queue", got)
	}
	var sizeAfter, deadAfter int64
	if err := database.QueryRowContext(ctx, `SELECT pg_total_relation_size('supply_chain_impact_winners_dirty_staging')`).Scan(&sizeAfter); err != nil {
		t.Fatalf("staging size after collapse: %v", err)
	}
	if err := database.QueryRowContext(ctx, `SELECT n_dead_tup FROM pg_stat_user_tables WHERE relname = 'supply_chain_impact_winners_dirty_staging'`).Scan(&deadAfter); err != nil {
		t.Fatalf("staging dead tuples after collapse: %v", err)
	}
	t.Logf("worst_partition groups=%d rows=%d collapse=%s (%.6fs) heap_before=%d heap_after=%d dead_before=%d dead_after=%d",
		outcome.groups, outcome.rows, elapsed, elapsed.Seconds(), sizeBefore, sizeAfter, deadBefore, deadAfter)
}

// TestSupplyChainImpactWinnersStagingCollapseExplainPinsKeyOrderLive pins the
// collapse's key order on the plans the tick gets: the fact SELECT arm sorts
// by the fact conflict key and the scope arm by the scope pair, so the merge
// into the versioned log runs in conflict-key order.
func TestSupplyChainImpactWinnersStagingCollapseExplainPinsKeyOrderLive(t *testing.T) {
	ctx, database := winnersSettleGateDB(t)

	tx, err := (SQLDB{DB: database}).Begin(ctx)
	if err != nil {
		t.Fatalf("begin explain transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	explain := func(stmt string) string {
		rows, err := tx.QueryContext(ctx, "EXPLAIN "+stmt, 0, 0)
		if err != nil {
			t.Fatalf("explain collapse arm: %v", err)
		}
		defer func() { _ = rows.Close() }()
		var plan strings.Builder
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatalf("scan explain row: %v", err)
			}
			plan.WriteString(line + "\n")
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("read explain rows: %v", err)
		}
		return plan.String()
	}
	pinSortKey := func(plan string, want []string, arm string) {
		t.Helper()
		// Prefix match: the array_agg(source ORDER BY seq) the earliest-seq
		// source needs appends seq after the group keys, so the sort carries
		// the conflict key first and the group order is still the key order.
		matched := false
		for _, line := range strings.Split(plan, "\n") {
			if !strings.Contains(line, "Sort Key:") {
				continue
			}
			var gotOrder []string
			for _, part := range strings.Split(line[strings.Index(line, "Sort Key:")+len("Sort Key:"):], ",") {
				col := strings.TrimSpace(part)
				if dot := strings.LastIndex(col, "."); dot >= 0 {
					col = col[dot+1:]
				}
				gotOrder = append(gotOrder, col)
			}
			if len(gotOrder) >= len(want) && strings.Join(gotOrder[:len(want)], ",") == strings.Join(want, ",") {
				matched = true
			}
		}
		if !matched {
			t.Fatalf("collapse %s arm has no Sort Key line starting with %v:\n%s", arm, want, plan)
		}
	}
	pinSortKey(explain(collapseWinnersStagingFactSelectSQL),
		[]string{"package_id", "repository_id", "subject_digest", "c0", "advisory_id"}, "fact")
	pinSortKey(explain(collapseWinnersStagingScopeSelectSQL),
		[]string{"scope_id", "generation_id"}, "scope")
}
