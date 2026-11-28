// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// Live differential for the ruled #9805 full fix, re-sourced round 5: the
// new chunked-full rebuild path (ResweepWinners, bounded keyed chunks over
// supports plus the stored identifiers) agrees with the winners selects row
// for row on the same seeded backend. On fact-synced states (clean scopes,
// dead-key classes, resweep to zero) both the shipped legacy
// supplyChainImpactWinnerSelectSQL and the supports-swapped twin agree with
// the stored table. On the dirty/orphan scope that mirrors the replica
// findings, the supports twin is the oracle: the legacy fact select cannot
// see an orphan support, a supports-payload rewrite, or a fact rewrite the
// capture missed, so the phase asserts supports parity plus the dirt's
// expected winner movement, and logs the legacy divergence instead of
// asserting on it. Every parity phase asserts byte-equal rows (EXCEPT ALL 0
// both directions) AND equal counts. It also records the chunked walk's wall
// time (every sequential chunk is bounded above by the walk total) and the
// advisory-lock single-flight.

const (
	fullDiffCleanScope = "vuln-intel://9805-full-diff/clean"
	fullDiffDirtyScope = "vuln-intel://9805-full-diff/dirty"
)

// fullDiffWinnerColumns mirrors supplyChainImpactWinnerDataColumns in
// supply_chain_impact_canonical_winners_rebuild.go (the stored content
// columns the legacy select projects, in legacy output order): the
// canonical key plus every content column, materialized_at excluded. If the
// store gains a column, the EXCEPT ALL below fails to compile against the
// stored table until this list is extended, so the differential cannot
// silently drop a column.
var fullDiffWinnerColumns = []string{
	"canonical_key",
	"winner_fact_id", "winner_scope_id", "finding_id", "priority_score",
	"source_count", "impact_status", "ecosystem", "severity_bucket", "repository_id",
	"cve_id", "advisory_id", "package_id", "subject_digest", "image_ref", "priority_bucket",
	"detection_profile", "observed_version", "match_reason", "suppression_state", "suppression_expires_at",
	"service_ids", "workload_ids", "environments", "payload", "source_confidence",
}

func fullDiffTextList(alias string) string {
	cols := make([]string, len(fullDiffWinnerColumns))
	for i, c := range fullDiffWinnerColumns {
		cols[i] = alias + `."` + c + `"::text`
	}
	return strings.Join(cols, ", ")
}

// fullDiffExceptAll materializes the legacy select once and counts EXCEPT
// ALL in both directions against the stored winners: legacy-minus-stored
// and stored-minus-legacy. Both must be zero for the paths to agree.
func fullDiffExceptAll(t *testing.T, ctx context.Context, db *sql.DB) (forward, reverse int64) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `CREATE TEMP TABLE legacy_now AS `+postgres.WinnerSelectSQLForTest); err != nil {
		t.Fatalf("materialize legacy winners: %v", err)
	}
	defer func() {
		if _, err := db.ExecContext(ctx, `DROP TABLE legacy_now`); err != nil {
			t.Fatalf("drop legacy_now: %v", err)
		}
	}()
	count := func(name, first, firstTable, second, secondTable string) int64 {
		t.Helper()
		var n int64
		q := `SELECT count(*) FROM (SELECT ` + first + ` FROM ` + firstTable +
			` EXCEPT ALL SELECT ` + second + ` FROM ` + secondTable + `) AS d`
		if err := db.QueryRowContext(ctx, q).Scan(&n); err != nil {
			t.Fatalf("differential %s: %v", name, err)
		}
		return n
	}
	forward = count("legacy-minus-stored",
		fullDiffTextList("l"), "legacy_now AS l",
		fullDiffTextList("w"), "supply_chain_impact_canonical_winners AS w")
	reverse = count("stored-minus-legacy",
		fullDiffTextList("w"), "supply_chain_impact_canonical_winners AS w",
		fullDiffTextList("l"), "legacy_now AS l")
	return forward, reverse
}

// fullDiffAssertParity asserts the byte-equality half of the differential on
// a fact-synced state: the stored winners match the legacy select AND the
// supports-swapped twin EXCEPT ALL 0 in both directions over every content
// column, AND the row counts agree. A delete the resweep missed fails the
// counts even when the row sets happen to align.
func fullDiffAssertParity(t *testing.T, ctx context.Context, db *sql.DB, phase string) {
	t.Helper()
	fwd, rev := fullDiffExceptAll(t, ctx, db)
	sfwd, srev, stored, oracle := fullSupportsExceptAll(t, ctx, db, phase)
	var legacy int64
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM (`+postgres.WinnerSelectSQLForTest+`) AS legacy`).Scan(&legacy); err != nil {
		t.Fatalf("%s legacy count: %v", phase, err)
	}
	t.Logf("%s differential: legacy-minus-stored=%d stored-minus-legacy=%d stored=%d legacy=%d",
		phase, fwd, rev, stored, legacy)
	if fwd != 0 || rev != 0 {
		t.Fatalf("%s chunked-full disagrees with legacy: forward=%d reverse=%d", phase, fwd, rev)
	}
	if sfwd != 0 || srev != 0 {
		t.Fatalf("%s chunked-full disagrees with the supports oracle: forward=%d reverse=%d", phase, sfwd, srev)
	}
	if stored != legacy || stored != oracle {
		t.Fatalf("%s row counts disagree: stored=%d legacy=%d supports=%d", phase, stored, legacy, oracle)
	}
}

func fullDiffSeed(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	winnersLiveSeedScope(t, ctx, db, fullDiffCleanScope)
	winnersLiveSeedScope(t, ctx, db, fullDiffDirtyScope)
	winnersLiveSeedScope(t, ctx, db, winnersLiveOperatorScope)
	seed := func(id, scope, digest string, payload map[string]any) {
		t.Helper()
		// The production writer always emits subject_digest (possibly
		// empty; see writer_identity.go), so every seeded payload carries
		// the key: the shipped identifier probe equates the payload field
		// to the identifier component, and a missing key reads NULL, which
		// never equals the '' identifier a missing key enumerates to. A
		// key-absent raw payload would diverge from legacy under the
		// closed-gate probe on every path that runs it (keyed tick, audit,
		// chunked full alike); that pre-existing shipped semantic is out of
		// scope here, so the seed honors the writer contract instead.
		if payload == nil {
			payload = map[string]any{}
		}
		payload["subject_digest"] = digest
		winnersLiveSeedFact(t, ctx, db, id, scope, payload)
	}
	find := func(cve, pkg string, priority int) map[string]any {
		return winnersLiveFinding(cve, pkg, priority, nil)
	}
	// Clean scope: six findings over five canonical keys; f-dupe-lo and
	// f-dupe-hi share a key so the winner choice (higher priority) is
	// exercised, and f-expiry carries an operator overlay.
	seed("f-a1", fullDiffCleanScope, "sha256:aaa", find("CVE-2026-9801", "pkg:npm/alpha", 10))
	seed("f-a2", fullDiffCleanScope, "", find("CVE-2026-9802", "pkg:npm/beta", 20))
	seed("f-dupe-lo", fullDiffCleanScope, "", find("CVE-2026-9803", "pkg:npm/dupe", 5))
	seed("f-dupe-hi", fullDiffCleanScope, "", find("CVE-2026-9803", "pkg:npm/dupe", 42))
	seed("f-expiry", fullDiffCleanScope, "", find("CVE-2026-9804", "pkg:npm/expiry", 6))
	seed("f-quiet", fullDiffCleanScope, "", find("CVE-2026-9805", "pkg:npm/quiet", 2))
	winnersLiveSeedFact(t, ctx, db, "op-9805", winnersLiveOperatorScope, winnersLiveFinding(
		"CVE-2026-9804", "pkg:npm/expiry", 0, map[string]any{"suppression_state": "ignored", "subject_digest": ""}))
	// Dirty scope: four findings; the staleness is planted after the
	// backfill completes (see fullDiffPlantDirt), so this seed is current.
	seed("f-b1", fullDiffDirtyScope, "", find("CVE-2026-9811", "pkg:npm/dirty-one", 10))
	seed("f-b2", fullDiffDirtyScope, "", find("CVE-2026-9812", "pkg:npm/dirty-two", 11))
	seed("f-b3", fullDiffDirtyScope, "sha256:bbb", find("CVE-2026-9813", "pkg:npm/dirty-three", 12))
	seed("f-b4", fullDiffDirtyScope, "", find("CVE-2026-9814", "pkg:npm/dirty-four", 13))
}

// fullDiffCompleteBackfill drives the real supports backfill sweep to its
// readiness marker, so the later resweep probes supports for candidate
// fact_id presence (gate open) instead of falling back to the fact_records
// payload-expression probe.
func fullDiffCompleteBackfill(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	bf := postgres.SupplyChainImpactSupportsBackfill{DB: postgres.SQLDB{DB: db}}
	for i := 0; i < 100; i++ {
		report, err := bf.RunStep(ctx)
		if err != nil {
			t.Fatalf("supports backfill step %d: %v", i, err)
		}
		if report.Complete {
			return
		}
	}
	t.Fatal("supports backfill did not complete in 100 steps")
}

// fullDiffPlantDirt mirrors the replica findings on the dirty scope after
// the backfill marker is complete: (a) a stale stored priority_score
// column, (b) a supports payload rewrite to a new priority while the frozen
// fact payload keeps the seeded value, (c) an orphan supports row on the
// active generation whose key columns match a live identifier but has no
// fact_record, (d) a fact payload rewrite the capture misses while the
// supports payload keeps the seeded value. The supports payload is the
// authority post-stop, so (b) must move the winner to the supports value,
// (c) must win its key exactly as the keyed tick ranks it, and (d) must be
// ignored: a resweep that followed the fact payload would regress to the
// fact_records row source. (a) must not move anything: ranking never reads
// stored supports columns. The differential against the supports oracle
// stays 0/0 through all four; the legacy fact oracle diverges, which the
// phase logs instead of asserting on.
func fullDiffPlantDirt(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	plant := func(query string, args ...any) {
		t.Helper()
		postgresproof.SeedFindingFacts(t, db, func() {
			if _, err := db.ExecContext(ctx, query, args...); err != nil {
				t.Fatalf("plant dirt %q: %v", query, err)
			}
		})
	}
	// (d) plants the ignored fact rewrite: the fact payload moves to 999
	// while the supports payload keeps the seeded priority 13. A chunk
	// that read fact_records would move the winner to 999.
	plant(`UPDATE fact_records SET payload = jsonb_set(payload, '{priority_score}', '999')
WHERE fact_id = 'f-b4' AND scope_id = $1`, fullDiffDirtyScope)
	// (a) stale stored priority_score column (never read by the probe or the rank).
	if _, err := db.ExecContext(ctx, `UPDATE supply_chain_impact_finding_supports SET priority_score = 1 WHERE fact_id = 'f-b1'`); err != nil {
		t.Fatalf("plant stale supports priority: %v", err)
	}
	// (b) supports payload rewrite to 7 (authoritative: the winner must
	// follow the supports payload, proving payload-derived ranking).
	if _, err := db.ExecContext(ctx, `UPDATE supply_chain_impact_finding_supports
SET payload = jsonb_set(payload, '{priority_score}', '7') WHERE fact_id = 'f-b2'`); err != nil {
		t.Fatalf("plant supports payload rewrite: %v", err)
	}
	// (c) orphan supports on the active generation: key columns copied from
	// f-b3's live row, a fact_id with no fact_record, a winning priority in
	// both the stored column and the payload (ranking reads the payload, so
	// the payload must carry the win; the column alone would not move it).
	if _, err := db.ExecContext(ctx, `
INSERT INTO supply_chain_impact_finding_supports
    (fact_id, canonical_key, scope_id, generation_id, finding_id, has_payload_finding_id,
     priority_score, source_confidence, suppression_state, impact_status, ecosystem,
     severity_bucket, repository_id, cve_id, advisory_id, package_id, subject_digest,
     image_ref, priority_bucket, detection_profile, observed_version, match_reason, payload)
SELECT 'fact:orphan:9805', canonical_key, scope_id, generation_id, 'finding:orphan',
     1, 9999, source_confidence, suppression_state, impact_status, ecosystem,
     severity_bucket, repository_id, cve_id, advisory_id, package_id, subject_digest,
     image_ref, priority_bucket, detection_profile, observed_version, match_reason,
     jsonb_set(payload, '{priority_score}', '9999')
FROM supply_chain_impact_finding_supports WHERE fact_id = 'f-b3'`); err != nil {
		t.Fatalf("plant orphan supports: %v", err)
	}
	// The (d) fact rewrite tripped the old-writer fence, which would close
	// the supports gate and dodge the presence probe this phase must run.
	// Clearing it is the test setup, not a heal: every presence-relevant
	// supports column (the key columns and fact_id presence) is untouched
	// by construction above, and the orphan (a supports-only insert) fires
	// no fact trigger. A regression — reading stored ranking columns, not
	// following the supports payload, dropping the orphan, or following the
	// fact rewrite — still fails the supports EXCEPT ALL below.
	if _, err := db.ExecContext(ctx, `DELETE FROM supply_chain_impact_finding_supports_dirty`); err != nil {
		t.Fatalf("clear supports fence after planting: %v", err)
	}
	ready, err := postgres.SupplyChainImpactSupportsReady(ctx, postgres.SQLDB{DB: db})
	if err != nil || !ready {
		t.Fatalf("supports gate ready = %v, %v: the dirt phase must run the presence probe", ready, err)
	}
}

func TestSupplyChainImpactWinnersFullChunkedMatchesLegacyLive(t *testing.T) {
	_ = supplyChainImpactLiveDSN(t, "#9805 chunked-full vs legacy differential proof")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	db := openAdvisorySuppressionIsolatedDB(t)
	store := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: db})
	store.MaxKeyFraction = 1000
	// Two identifiers per chunk: the ~10 seeded identifiers walk as five or
	// more bounded chunks, so the timing below covers a multi-chunk walk.
	store.ChunkMaxIdentifiers = 2

	fullDiffSeed(t, ctx, db)

	// Phase 1: closed gate (no backfill marker on a fresh bootstrap), so the
	// chunks run the supports payload-expression probe. Facts and supports
	// are in sync, so both oracles agree.
	firstStart := time.Now()
	first, err := store.ResweepWinners(ctx)
	if err != nil || first.Skipped {
		t.Fatalf("phase-1 full resweep = %+v, %v", first, err)
	}
	firstElapsed := time.Since(firstStart)
	t.Logf("phase-1 closed-gate full resweep: inserted=%d updated=%d deleted=%d unchanged=%d in %v",
		first.Inserted, first.Updated, first.Deleted, first.Unchanged, firstElapsed)
	fwd, rev := fullDiffExceptAll(t, ctx, db)
	t.Logf("phase-1 differential: legacy-minus-stored=%d stored-minus-legacy=%d", fwd, rev)
	if fwd != 0 || rev != 0 {
		t.Fatalf("phase-1 chunked-full disagrees with legacy: forward=%d reverse=%d", fwd, rev)
	}

	// Phase 2: open gate with planted replica dirt; the chunks run the
	// triple-guarded supports presence probe over the dirtied supports.
	// The supports twin is the oracle here: (b) and (c) move winners by
	// design and (d) is invisible to a supports reader, so the legacy fact
	// oracle diverges and is logged, not asserted on.
	fullDiffCompleteBackfill(t, ctx, db)
	fullDiffPlantDirt(t, ctx, db)
	secondStart := time.Now()
	second, err := store.ResweepWinners(ctx)
	if err != nil || second.Skipped {
		t.Fatalf("phase-2 full resweep = %+v, %v", second, err)
	}
	secondElapsed := time.Since(secondStart)
	t.Logf("phase-2 open-gate full resweep over replica dirt: inserted=%d updated=%d deleted=%d unchanged=%d in %v",
		second.Inserted, second.Updated, second.Deleted, second.Unchanged, secondElapsed)
	// (a) the stale stored priority_score column must not move f-b1: the
	// winner keeps the seeded payload priority 10, not the stored 1.
	var b1 int
	if err := db.QueryRowContext(ctx, `SELECT priority_score FROM supply_chain_impact_canonical_winners
WHERE winner_fact_id = 'f-b1'`).Scan(&b1); err != nil {
		t.Fatalf("read b1 winner: %v", err)
	}
	if b1 != 10 {
		t.Fatalf("b1 stored priority = %d, want 10 (ranking never reads the stored column)", b1)
	}
	// (b) the supports payload rewrite is authoritative: f-b2's winner
	// follows it to 7, not the frozen fact value.
	var b2 int
	if err := db.QueryRowContext(ctx, `SELECT priority_score FROM supply_chain_impact_canonical_winners
WHERE winner_fact_id = 'f-b2'`).Scan(&b2); err != nil {
		t.Fatalf("read b2 winner: %v", err)
	}
	if b2 != 7 {
		t.Fatalf("b2 stored priority = %d, want 7 (the resweep must follow the supports payload)", b2)
	}
	// (c) the orphan wins its key: a supports-authored row with no fact
	// twin ranks exactly as the keyed tick ranks it.
	var orphan int
	if err := db.QueryRowContext(ctx, `SELECT priority_score FROM supply_chain_impact_canonical_winners
WHERE winner_fact_id = 'fact:orphan:9805'`).Scan(&orphan); err != nil {
		t.Fatalf("read orphan winner: %v", err)
	}
	if orphan != 9999 {
		t.Fatalf("orphan stored priority = %d, want 9999 (supports-authored rows rank)", orphan)
	}
	// (d) the fact rewrite the capture missed must be ignored: f-b4's
	// winner keeps the supports priority 13, not the fact payload's 999.
	// A chunk that read fact_records would move it to 999.
	var b4 int
	if err := db.QueryRowContext(ctx, `SELECT priority_score FROM supply_chain_impact_canonical_winners
WHERE winner_fact_id = 'f-b4'`).Scan(&b4); err != nil {
		t.Fatalf("read b4 winner: %v", err)
	}
	if b4 != 13 {
		t.Fatalf("b4 stored priority = %d, want 13 (the resweep must ignore the fact payload)", b4)
	}
	sfwd, srev, _, _ := fullSupportsExceptAll(t, ctx, db, "phase-2")
	if sfwd != 0 || srev != 0 {
		t.Fatalf("phase-2 chunked-full disagrees with the supports oracle: forward=%d reverse=%d", sfwd, srev)
	}
	lfwd, lrev := fullDiffExceptAll(t, ctx, db)
	t.Logf("phase-2 legacy divergence (informational: the fact oracle cannot see supports dirt): legacy-minus-stored=%d stored-minus-legacy=%d",
		lfwd, lrev)

	// Heal the phase-2 dirt so the dead-key phases start fact-synced like
	// every other phase boundary: each dirt's proof already landed above,
	// and the dead-key deletes below must be judged on synced oracles.
	// The fact heal plants frozen history (no supports sync follows); the
	// supports heals restore the writer-derived values the dirt overwrote.
	winnersLivePlantFact(t, ctx, db, `UPDATE fact_records SET payload = jsonb_set(payload, '{priority_score}', '13')
WHERE fact_id = 'f-b4' AND scope_id = $1`, fullDiffDirtyScope)
	if _, err := db.ExecContext(ctx, `UPDATE supply_chain_impact_finding_supports
SET payload = jsonb_set(payload, '{priority_score}', '11') WHERE fact_id = 'f-b2'`); err != nil {
		t.Fatalf("heal b2 supports payload: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE supply_chain_impact_finding_supports
SET priority_score = 10 WHERE fact_id = 'f-b1'`); err != nil {
		t.Fatalf("heal b1 stored priority: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM supply_chain_impact_finding_supports WHERE fact_id = 'fact:orphan:9805'`); err != nil {
		t.Fatalf("remove orphan supports: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM supply_chain_impact_finding_supports_dirty`); err != nil {
		t.Fatalf("clear supports fence after healing: %v", err)
	}

	// Phase 3: dead-key classes the live-only enumeration dropped. Tombstone
	// one clean fact (its key goes stored-only) and rotate one dirty key to a
	// new package_id (its old key goes stored-only, its new key is inserted).
	// The stored arm must carry both dead keys into a chunk so the deletes fire.
	winnersLiveExec(t, ctx, db, `UPDATE fact_records SET is_tombstone = TRUE WHERE fact_id = 'f-a2'`)
	winnersLiveExec(t, ctx, db, `UPDATE fact_records SET payload = jsonb_set(payload, '{package_id}', '"pkg:npm/rotated"') WHERE fact_id = 'f-b1'`)
	thirdStart := time.Now()
	third, err := store.ResweepWinners(ctx)
	if err != nil || third.Skipped {
		t.Fatalf("phase-3 full resweep = %+v, %v", third, err)
	}
	thirdElapsed := time.Since(thirdStart)
	t.Logf("phase-3 dead-key resweep: inserted=%d updated=%d deleted=%d unchanged=%d in %v",
		third.Inserted, third.Updated, third.Deleted, third.Unchanged, thirdElapsed)
	if third.Deleted != 2 || third.Inserted != 1 {
		t.Fatalf("phase-3 resweep = %+v, want exactly 2 deleted (tombstoned + rotation-dropped) and 1 inserted", third)
	}
	fullDiffAssertParity(t, ctx, db, "phase-3")

	// Phase 4: resweep to zero. Every fact is tombstoned, so the live arm is
	// empty and only the stored arm feeds the chunks; every stored row must go.
	winnersLiveExec(t, ctx, db, `UPDATE fact_records SET is_tombstone = TRUE`)
	fourthStart := time.Now()
	fourth, err := store.ResweepWinners(ctx)
	if err != nil || fourth.Skipped {
		t.Fatalf("phase-4 full resweep = %+v, %v", fourth, err)
	}
	fourthElapsed := time.Since(fourthStart)
	t.Logf("phase-4 reswept-to-zero: inserted=%d updated=%d deleted=%d unchanged=%d in %v",
		fourth.Inserted, fourth.Updated, fourth.Deleted, fourth.Unchanged, fourthElapsed)
	if fourth.Deleted != 8 || fourth.Inserted != 0 || fourth.Updated != 0 || fourth.Unchanged != 0 {
		t.Fatalf("phase-4 resweep = %+v, want all 8 remaining winners deleted", fourth)
	}
	fullDiffAssertParity(t, ctx, db, "phase-4")

	// Every chunk ran sequentially inside its resweep transaction, so each
	// chunk took at most the walk total, far under the 60 s bound.
	for name, elapsed := range map[string]time.Duration{"phase-1": firstElapsed, "phase-2": secondElapsed, "phase-3": thirdElapsed, "phase-4": fourthElapsed} {
		if elapsed > 60*time.Second {
			t.Errorf("%s walk took %v: a chunk cannot be shown bounded under 60 s", name, elapsed)
		}
	}

	// Single-flight: a second resweep cannot enter while the resweep lock is
	// held; it reports Skipped and writes nothing. The session lock below
	// shares the resweep key namespace with the transaction lock the claim
	// takes (see tryClaimSupplyChainImpactWinnersResweepSQL), so holding it
	// is the same contention as a concurrent resweep.
	holder, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("hold lock conn: %v", err)
	}
	defer func() { _ = holder.Close() }()
	if _, err := holder.ExecContext(ctx, `SELECT pg_advisory_lock(hashtext('supply_chain_impact_winners'), hashtext('resweep'))`); err != nil {
		t.Fatalf("hold resweep lock: %v", err)
	}
	blocked, err := store.ResweepWinners(ctx)
	if err != nil {
		t.Fatalf("blocked resweep: %v", err)
	}
	if !blocked.Skipped {
		t.Fatalf("resweep under a held resweep lock = %+v, want Skipped", blocked)
	}
	if _, err := holder.ExecContext(ctx, `SELECT pg_advisory_unlock(hashtext('supply_chain_impact_winners'), hashtext('resweep'))`); err != nil {
		t.Fatalf("release resweep lock: %v", err)
	}
	after, err := store.ResweepWinners(ctx)
	if err != nil || after.Skipped {
		t.Fatalf("uncontended resweep after release = %+v, %v: want a commit", after, err)
	}
	t.Logf("single-flight: contended resweep Skipped=true, released resweep committed (unchanged=%d)", after.Unchanged)
}
