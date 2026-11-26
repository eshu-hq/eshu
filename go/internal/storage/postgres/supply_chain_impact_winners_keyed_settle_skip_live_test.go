// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9975 drain discriminator: the applied drain skip split (rows queued outside the chunk vs inside with a moved version) on scratch PostgreSQL 18, with a mid-tick version bump emulating the convert self-bump effect and a staged re-mark folded between passes emulating the next-tick collapse re-bump
//eshu:readiness:env ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DSN ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DISPOSABLE

// TestSupplyChainImpactWinnersKeyedSettleSkipSplitLive proves the #9975
// drain discriminator's split on a scratch database: twelve admitted fact
// rows with collapsed versions (the QA head shape at small N), a six
// identifier chunk, one chunked row version-bumped mid-tick, and one scope
// row with no expansion.
//
//   - The five unmoved chunked rows delete (100 drained marks).
//   - The six unchunked rows report as admitted_not_in_chunk.
//   - The one moved chunked row reports as in_chunk_version_mismatch.
//   - Conservation holds on rows (12 admitted = 5 deleted + 6 + 1) and on
//     marks (admitted version sums = drained sums + survivor sums).
//   - A staged re-mark folded by the next pass bumps a survivor (the
//     collapse re-bump mechanism), merging into the queued row.
//
// The mid-tick bump is a direct UPDATE of the queued row's dirty_version,
// which is exactly what the scope-remainder convert's ON CONFLICT DO UPDATE
// does to a queued fact row; running the full scope-expansion fixture would
// need fact_records generations the discriminator does not need. The two
// skip counts are read-only SELECTs over the admitted window joined to the
// log by seq: they take no row locks, so no lock-path or concurrency proof
// attaches to them beyond the unchanged drain lock-order gate
// (TestWinnersDrainStatementsLockKeysInOrder).
func TestSupplyChainImpactWinnersKeyedSettleSkipSplitLive(t *testing.T) {
	ctx, database := winnersSettleGateDB(t)

	// Wipe the bootstrap's own baseline marks first: the test asserts exact
	// admitted/deleted/survivor counts, so no unowned row may sit in the
	// admitted window ahead of the seeds. Scratch database only.
	for _, wipe := range []string{
		`DELETE FROM supply_chain_impact_winners_dirty_log`,
		`DELETE FROM supply_chain_impact_winners_dirty_staging`,
	} {
		if _, err := database.ExecContext(ctx, wipe); err != nil {
			t.Fatalf("wipe scratch queue: %v", err)
		}
	}

	const rows = 12
	if _, err := database.ExecContext(ctx, `INSERT INTO supply_chain_impact_winners_dirty_log
(kind, source, package_id, repository_id, subject_digest, c0, advisory_id, dirty_version)
SELECT 'fact', 'fact_upsert',
    'pkg:skip:' || lpad(g::text, 3, '0'), 'repo:skip', 'digest:skip',
    'CVE-2026-2000', 'CVE-2026-2000', 20
FROM generate_series(0, 11) AS g`); err != nil {
		t.Fatalf("seed skip-split rows: %v", err)
	}
	// One scope row with no expansion: its applied predicate holds
	// vacuously (no unapplied identifier), so it deletes with the chunk.
	if _, err := database.ExecContext(ctx, `INSERT INTO supply_chain_impact_winners_dirty_log
(kind, source, scope_id, generation_id, dirty_version)
VALUES ('scope', 'activation', 'scope:skip', 'gen:skip', 1)`); err != nil {
		t.Fatalf("seed scope row: %v", err)
	}

	tx, err := (SQLDB{DB: database}).Begin(ctx)
	if err != nil {
		t.Fatalf("begin tick: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := collapseWinnersStaging(ctx, tx, 0); err != nil {
		t.Fatalf("collapse: %v", err)
	}
	admitted, err := admitWinnersDirtyWindow(ctx, tx, winnersDeadlockSettleBounds.dirtyRows)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if admitted != rows+1 {
		t.Fatalf("admitted = %d, want %d (twelve facts plus the scope row)", admitted, rows+1)
	}
	for _, stmt := range []string{
		buildWinnersKeyedScopeIdentsFromSupportsSQL,
		buildWinnersKeyedIdentsSQL,
		analyzeWinnersKeyedIdentsSQL,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("build identifiers: %v", err)
		}
	}
	if _, err := buildWinnersKeyedChunk(ctx, tx, winnersChunkBounds{dirtyRows: 5000, identifiers: 6, scannedCopies: 10000}); err != nil {
		t.Fatalf("build chunk: %v", err)
	}
	// Move the oldest admitted row's version after admission: the convert
	// self-bump effect. The oldest row is chunked (oldest-first chunk), so
	// the recheck must keep exactly this row queued.
	oldest, err := scanWinnersInt64(ctx, tx, `SELECT seq FROM winners_keyed_consumed ORDER BY captured_at, seq LIMIT 1`, "read oldest admitted seq")
	if err != nil {
		t.Fatalf("read oldest admitted seq: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE supply_chain_impact_winners_dirty_log SET dirty_version = dirty_version + 1 WHERE seq = $1`, oldest); err != nil {
		t.Fatalf("bump oldest row mid-tick: %v", err)
	}

	tally, remaining, _, _, skips, err := settleWinnersKeyedDirtyLog(ctx, tx, false, nil)
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if skips.NotInChunk != 6 {
		t.Errorf("skipped not-in-chunk = %d, want the six unchunked fact rows", skips.NotInChunk)
	}
	if skips.VersionMismatch != 1 {
		t.Errorf("skipped version-mismatch = %d, want the one moved chunked row", skips.VersionMismatch)
	}
	// Five chunked facts at version 20 plus the scope row at 1 delete.
	if got := tally.total(); got != 101 {
		t.Errorf("drained marks = %d, want 101 (five facts at 20 plus the scope row)", got)
	}
	if remaining != 7 {
		t.Errorf("remaining = %d, want the seven queued fact survivors", remaining)
	}
	// Marks conservation: 12 facts at 20, one bumped to 21, plus the scope
	// row at 1; drained 101 leaves 141 survivor marks behind.
	var survivorMarks, survivorRows int64
	survivorQuery, err := tx.QueryContext(ctx, `SELECT count(*), COALESCE(sum(d.dirty_version), 0)
FROM supply_chain_impact_winners_dirty_log AS d JOIN winners_keyed_consumed AS c ON c.seq = d.seq`)
	if err != nil {
		t.Fatalf("read survivors: %v", err)
	}
	func() {
		defer func() { _ = survivorQuery.Close() }()
		if !survivorQuery.Next() {
			t.Fatalf("read survivors: no row")
		}
		if err := survivorQuery.Scan(&survivorRows, &survivorMarks); err != nil {
			t.Fatalf("scan survivors: %v", err)
		}
	}()
	if err := survivorQuery.Err(); err != nil {
		t.Fatalf("survivor rows: %v", err)
	}
	if survivorRows != skips.NotInChunk+skips.VersionMismatch {
		t.Errorf("survivor rows = %d, want the skip split sum %d", survivorRows, skips.NotInChunk+skips.VersionMismatch)
	}
	// Rows conservation: 13 admitted (12 facts plus the scope row) = 6
	// deleted rows (five facts plus the scope row) + 7 survivors.
	if admitted-survivorRows != 6 {
		t.Errorf("deleted rows = %d, want 6 (five facts plus the scope row)", admitted-survivorRows)
	}
	if survivorMarks != 141 {
		t.Errorf("survivor marks = %d, want 141 (six facts at 20 plus one at 21)", survivorMarks)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tick: %v", err)
	}

	// Staged re-mark between passes: the next tick's collapse folds a fresh
	// mark for a surviving key, bumping it past its version instead of
	// adding a row (the collapse re-bump mechanism the discriminator names).
	second, err := (SQLDB{DB: database}).Begin(ctx)
	if err != nil {
		t.Fatalf("begin second pass: %v", err)
	}
	defer func() { _ = second.Rollback() }()
	if _, err := second.ExecContext(ctx, `INSERT INTO supply_chain_impact_winners_dirty_staging
(kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
VALUES ('fact', 'fact_upsert', 'pkg:skip:011', 'repo:skip', 'digest:skip', 'CVE-2026-2000', 'CVE-2026-2000')`); err != nil {
		t.Fatalf("stage re-mark: %v", err)
	}
	if _, err := collapseWinnersStaging(ctx, second, 0); err != nil {
		t.Fatalf("collapse re-mark: %v", err)
	}
	var depth, versions int64
	afterQuery, err := second.QueryContext(ctx, `SELECT count(*), COALESCE(sum(dirty_version), 0) FROM supply_chain_impact_winners_dirty_log`)
	if err != nil {
		t.Fatalf("read log after collapse: %v", err)
	}
	func() {
		defer func() { _ = afterQuery.Close() }()
		if !afterQuery.Next() {
			t.Fatalf("read log after collapse: no row")
		}
		if err := afterQuery.Scan(&depth, &versions); err != nil {
			t.Fatalf("scan log after collapse: %v", err)
		}
	}()
	if err := afterQuery.Err(); err != nil {
		t.Fatalf("log after collapse rows: %v", err)
	}
	if depth != 7 || versions != 142 {
		t.Errorf("log after collapse = depth %d versions %d, want depth 7 versions 142 (merge, not a new row)", depth, versions)
	}
	if err := second.Commit(); err != nil {
		t.Fatalf("commit second pass: %v", err)
	}
}

// TestSupplyChainImpactWinnersSettleSkipCountsAreReadOnlyLive proves the two
// skip statements take no row lock: each runs EXPLAIN (no ANALYZE) and the
// plan must show no LockRows node, so the discriminator cannot serialize or
// block the tick it observes.
func TestSupplyChainImpactWinnersSettleSkipCountsAreReadOnlyLive(t *testing.T) {
	ctx, database := winnersSettleGateDB(t)

	tx, err := (SQLDB{DB: database}).Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []string{
		createWinnersKeyedConsumedSQL,
		buildWinnersKeyedScopeIdentsFromSupportsSQL,
		createWinnersKeyedChunkIdentsSQL,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("stage temp tables: %v", err)
		}
	}
	for query, name := range map[string]string{
		countWinnersKeyedSettleSkippedNotInChunkSQL:      "not-in-chunk",
		countWinnersKeyedSettleSkippedVersionMismatchSQL: "version-mismatch",
	} {
		rows, err := tx.QueryContext(ctx, "EXPLAIN "+query)
		if err != nil {
			t.Fatalf("explain %s count: %v", name, err)
		}
		plan := ""
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				_ = rows.Close()
				t.Fatalf("scan %s plan: %v", name, err)
			}
			plan += line + "\n"
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatalf("%s explain rows: %v", name, err)
		}
		for _, forbidden := range []string{"LockRows", "FOR UPDATE", "ModifyTable"} {
			if strings.Contains(plan, forbidden) {
				t.Errorf("%s count plan takes a write or row lock (%s):\n%s", name, forbidden, plan)
			}
		}
	}
}
