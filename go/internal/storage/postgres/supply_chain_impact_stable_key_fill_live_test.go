// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// Live proofs of the stable_fact_key fill (#10880 PR-C1k, arbiter ruling 1.4 to 1.7):
// it fills only keyless supports from their fact twin, in chunks of at most 500 under
// the pair's conflict-domain lock, resumes from its stored cursor, and completes only
// after a clean verification. The seed is written with SQL (a fact and a keyless
// support per row) so a pair can hold more than one chunk.

// fillSeedRows inserts n finding facts into the pair (scope, generation); fillSeedSupports
// then derives their supports, with fact ids prefix+8 digits and twin keys "key:"+fact id.
func fillSeedRows(t *testing.T, ctx context.Context, db *sql.DB, scope, generation, prefix string, n int) {
	t.Helper()
	fillSeedRowsPadded(t, ctx, db, scope, generation, prefix, n, 0)
}

// fillSeedRowsPadded is fillSeedRows with the fact payload padded to about padBytes (a
// hex pattern, so it compresses like repetitive JSON, not like random bytes); the supports
// derived from the facts carry the same payload.
func fillSeedRowsPadded(t *testing.T, ctx context.Context, db *sql.DB, scope, generation, prefix string, n, padBytes int) {
	t.Helper()
	supportsExec(t, ctx, db, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  fencing_token, source_confidence, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT $3::text || lpad(i::text, 8, '0'), $1, $2, 'reducer_supply_chain_impact_finding',
       'key:' || $3::text || lpad(i::text, 8, '0'), '1.0.0', 'synthetic', 1, 'observed', 'synthetic',
       $3::text || lpad(i::text, 8, '0'), now(), now(),
       jsonb_build_object('finding_id', 'finding:' || i, 'cve_id', 'CVE-2026-' || i,
                          'package_id', 'pkg:npm/p' || (i % 50), 'repository_id', 'repository:r' || (i % 100),
                          'subject_digest', '', 'advisory_id', '',
                          'provenance', jsonb_build_object('content_digest', md5(i::text)))
       || CASE WHEN $5::int > 0
               THEN jsonb_build_object('padding', substr(repeat(md5(i::text) || md5(i::text || 'a') || md5(i::text || 'b') || md5(i::text || 'c'), ($5::int / 128) + 1), 1, $5::int))
               ELSE '{}'::jsonb END
FROM generate_series(1, $4::int) AS i`, scope, generation, prefix, n, padBytes)
}

// fillSeedSupports gives every finding fact its support the way the impact writer does, with
// the writer's own statements (ProofSyncSupportsFromFacts), then clears the key of the
// supports whose fact id starts with likePrefix: the state of a support written before
// migration 350 or never rewritten since, which is what the fill exists for. Every other
// column is the writer's derivation, not a hand-built subset.
func fillSeedSupports(t *testing.T, ctx context.Context, db *sql.DB, likePrefix string) {
	t.Helper()
	if err := postgres.ProofSyncSupportsFromFacts(ctx, postgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("derive supports from facts: %v", err)
	}
	supportsExec(t, ctx, db, `UPDATE supply_chain_impact_finding_supports SET stable_fact_key = NULL WHERE fact_id LIKE $1::text || '%'`, likePrefix)
}

func fillMarkerState(t *testing.T, ctx context.Context, db *sql.DB, name string) (present, complete bool, cursorFact string, contractVersion int) {
	t.Helper()
	err := db.QueryRowContext(ctx, `
SELECT TRUE, completed_at IS NOT NULL, cursor_fact_id, contract_version
FROM supply_chain_impact_finding_supports_backfill WHERE marker_name = $1`, name).Scan(&present, &complete, &cursorFact, &contractVersion)
	if err == sql.ErrNoRows {
		return false, false, "", 0
	}
	if err != nil {
		t.Fatalf("read marker %s: %v", name, err)
	}
	return present, complete, cursorFact, contractVersion
}

func fillCount(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// fillMismatches counts supports of the prefix whose key is not the expected one: the
// twin's key, except rows ending in 7 (a key a rewrite already filled, kept) and rows
// ending in 3 (an empty twin key, left keyless).
func fillMismatches(t *testing.T, ctx context.Context, db *sql.DB, scope, prefix string) int {
	return fillCount(t, ctx, db, `
SELECT count(*) FROM supply_chain_impact_finding_supports AS s
JOIN fact_records AS f ON f.fact_id = s.fact_id
WHERE s.scope_id = $1 AND s.fact_id LIKE $2::text || '%'
  AND s.stable_fact_key IS DISTINCT FROM CASE
        WHEN right(s.fact_id, 1) = '7' THEN 'kept-' || s.fact_id
        WHEN right(s.fact_id, 1) = '3' THEN NULL
        ELSE f.stable_fact_key END`, scope, prefix)
}

func fillShapeSeed(t *testing.T, ctx context.Context, db *sql.DB, scope, generation, prefix string, n int) (fillable int) {
	t.Helper()
	fillSeedRows(t, ctx, db, scope, generation, prefix, n)
	fillSeedSupports(t, ctx, db, prefix)
	supportsExec(t, ctx, db, `UPDATE fact_records SET stable_fact_key = '' WHERE scope_id = $1 AND fact_id LIKE $2::text || '%' AND right(fact_id, 1) = '3'`, scope, prefix)
	supportsExec(t, ctx, db, `UPDATE supply_chain_impact_finding_supports SET stable_fact_key = 'kept-' || fact_id WHERE scope_id = $1 AND fact_id LIKE $2::text || '%' AND right(fact_id, 1) = '7'`, scope, prefix)
	return fillCount(t, ctx, db, `
SELECT count(*) FROM supply_chain_impact_finding_supports
WHERE scope_id = $1 AND fact_id LIKE $2::text || '%' AND right(fact_id, 1) NOT IN ('3', '7')`, scope, prefix)
}

// TestSupplyChainImpactSupportsStableKeyFillLive fills a pair of 1,300 supports: the
// filled keys equal the twin's, a key already filled is never overwritten, a support
// whose twin has an empty key stays keyless, no step touches more than 500 rows, the
// marker is set only by the step that verified clean, and the fill never touches the
// supports readiness marker.
func TestSupplyChainImpactSupportsStableKeyFillLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	fillable := fillShapeSeed(t, ctx, db, replaceSetLiveScope, replaceSetLiveGeneration, "fa", 1300)
	otherMarkers := fillCount(t, ctx, db, `SELECT count(*) FROM supply_chain_impact_finding_supports_backfill WHERE marker_name <> $1`, postgres.SupplyChainImpactStableKeyFillMarker)

	fill := postgres.SupplyChainImpactStableKeyFill{DB: postgres.SQLDB{DB: db}, VerifySliceSize: 400}
	var filled int64
	for step := 1; ; step++ {
		if step > 100 {
			t.Fatal("the fill did not complete in 100 steps")
		}
		report, err := fill.RunStep(ctx)
		if err != nil {
			t.Fatalf("step %d: %v", step, err)
		}
		if rows := report.Filled + report.Skipped; rows > 500 {
			t.Fatalf("step %d touched %d supports, want at most 500", step, rows)
		}
		filled += report.Filled
		_, complete, _, _ := fillMarkerState(t, ctx, db, postgres.SupplyChainImpactStableKeyFillMarker)
		if complete != report.Complete {
			t.Fatalf("step %d: report.Complete = %v but the marker is complete = %v", step, report.Complete, complete)
		}
		if report.Complete {
			if !report.Verified {
				t.Fatalf("step %d completed the marker without a verification slice", step)
			}
			break
		}
	}
	if filled != int64(fillable) {
		t.Fatalf("the fill reported %d rows filled, want the %d fillable supports", filled, fillable)
	}
	if n := fillMismatches(t, ctx, db, replaceSetLiveScope, "fa"); n != 0 {
		t.Fatalf("%d supports hold a key other than the expected one (twin key; kept key untouched; empty twin keyless)", n)
	}
	present, _, _, version := fillMarkerState(t, ctx, db, postgres.SupplyChainImpactStableKeyFillMarker)
	if !present || version != postgres.SupplyChainImpactStableKeyFillContractVersion || version > postgres.SupplyChainImpactSupportsContractVersion {
		t.Fatalf("fill marker present=%v contract_version=%d, want the fill's version, at most the supports version %d", present, version, postgres.SupplyChainImpactSupportsContractVersion)
	}
	if after := fillCount(t, ctx, db, `SELECT count(*) FROM supply_chain_impact_finding_supports_backfill WHERE marker_name <> $1`, postgres.SupplyChainImpactStableKeyFillMarker); after != otherMarkers {
		t.Fatalf("the fill changed the number of other marker rows from %d to %d", otherMarkers, after)
	}
	// A complete marker makes the next step a no-op.
	if report, err := fill.RunStep(ctx); err != nil || !report.Complete || report.Filled != 0 {
		t.Fatalf("step on a complete marker = %+v, %v, want complete and no rows", report, err)
	}
}

// TestSupplyChainImpactSupportsStableKeyFillRespectsThePairLockLive holds the pair's
// conflict-domain lock, the lock the impact writer takes, and proves a fill step and a
// writer pass on the same pair both wait for it, write nothing while it is held, and
// complete afterwards with no lost update: the writer's support and every fillable
// support end up keyed.
func TestSupplyChainImpactSupportsStableKeyFillRespectsThePairLockLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	fillShapeSeed(t, ctx, db, replaceSetLiveScope, replaceSetLiveGeneration, "fb", 100)
	holder, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	released := false
	release := func() {
		if !released {
			released = true
			_ = holder.Rollback()
		}
	}
	t.Cleanup(release)
	if _, err := holder.ExecContext(ctx, supplychaincore.SupplyChainImpactConflictDomainLockQuery,
		supplychaincore.SupplyChainImpactConflictDomainKey(replaceSetLiveScope, replaceSetLiveGeneration)); err != nil {
		t.Fatalf("hold the pair lock: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := postgres.SupplyChainImpactStableKeyFill{DB: postgres.SQLDB{DB: db}}.RunStep(ctx)
		errs <- err
	}()
	go func() {
		defer wg.Done()
		write := replaceSetWrite("intent:c1k:lock-writer", replaceSetFinding("repository:r_c1k_lock"))
		write.PartialEvidence = true // keep the seeded rows: this pass only adds its own finding
		_, err := replaceSetWriter(db).WriteSupplyChainImpactFindings(ctx, write)
		errs <- err
	}()
	// Both sessions wait on the advisory lock; nothing is written while it is held.
	deadline := time.Now().Add(5 * time.Second)
	for waiting := 0; waiting < 2; {
		waiting = fillCount(t, ctx, db, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND wait_event = 'advisory'`)
		if time.Now().After(deadline) {
			release()
			wg.Wait()
			t.Fatalf("only %d sessions waited on the advisory lock, want the fill step and the writer pass", waiting)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if n := fillCount(t, ctx, db, `SELECT count(*) FROM supply_chain_impact_finding_supports WHERE scope_id = $1 AND fact_id LIKE 'fb%' AND stable_fact_key IS NOT NULL AND right(fact_id, 1) <> '7'`, replaceSetLiveScope); n != 0 {
		t.Fatalf("%d supports were keyed while the pair lock was held", n)
	}
	release()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("a waiting session failed: %v", err)
		}
	}
	fill := postgres.SupplyChainImpactStableKeyFill{DB: postgres.SQLDB{DB: db}}
	for step := 0; step < 50; step++ {
		report, err := fill.RunStep(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if report.Complete {
			break
		}
	}
	if n := fillMismatches(t, ctx, db, replaceSetLiveScope, "fb"); n != 0 {
		t.Fatalf("%d seeded supports end with a wrong key after the lock was released", n)
	}
	if n := fillCount(t, ctx, db, `SELECT count(*) FROM supply_chain_impact_finding_supports WHERE scope_id = $1 AND fact_id NOT LIKE 'fb%' AND stable_fact_key IS NULL`, replaceSetLiveScope); n != 0 {
		t.Fatalf("the writer's own support lost its key (%d keyless): a lost update", n)
	}
}

// TestSupplyChainImpactSupportsStableKeyFillResumesAfterARestartLive runs one step with
// one fill value, abandons it (a restart), and finishes with a new value over the same
// database: the cursor in the marker row carries the sweep, no row is filled twice and
// none is missed.
func TestSupplyChainImpactSupportsStableKeyFillResumesAfterARestartLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	fillable := fillShapeSeed(t, ctx, db, replaceSetLiveScope, replaceSetLiveGeneration, "fc", 1300)

	first, err := postgres.SupplyChainImpactStableKeyFill{DB: postgres.SQLDB{DB: db}}.RunStep(ctx)
	if err != nil || first.Filled == 0 || first.Complete {
		t.Fatalf("first step = %+v, %v, want some rows filled and the sweep unfinished", first, err)
	}
	_, complete, cursor, _ := fillMarkerState(t, ctx, db, postgres.SupplyChainImpactStableKeyFillMarker)
	if complete || cursor == "" {
		t.Fatalf("after one step: complete=%v cursor=%q, want a stored cursor and no completion", complete, cursor)
	}
	remaining := fillCount(t, ctx, db, `SELECT count(*) FROM supply_chain_impact_finding_supports WHERE scope_id = $1 AND fact_id LIKE 'fc%' AND stable_fact_key IS NULL AND right(fact_id, 1) <> '3'`, replaceSetLiveScope)
	if remaining == 0 {
		t.Fatal("the first step filled every row: the seed is too small to prove a resume")
	}

	restarted := postgres.SupplyChainImpactStableKeyFill{DB: postgres.SQLDB{DB: db}, VerifySliceSize: 400}
	total := first.Filled
	for step := 0; ; step++ {
		if step > 100 {
			t.Fatal("the restarted fill did not complete")
		}
		report, err := restarted.RunStep(ctx)
		if err != nil {
			t.Fatal(err)
		}
		total += report.Filled
		if report.Complete {
			break
		}
	}
	if total != int64(fillable) {
		t.Fatalf("filled %d rows across the restart, want exactly the %d fillable supports", total, fillable)
	}
	if n := fillMismatches(t, ctx, db, replaceSetLiveScope, "fc"); n != 0 {
		t.Fatalf("%d supports hold a wrong key after the restart", n)
	}
}
