// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

//eshu:readiness:env ESHU_POSTGRES_TEST_DSN ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DSN ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DISPOSABLE

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// Live differential for the ruled #9805 v3-full fix: the new chunked-full
// rebuild path (ResweepWinners, bounded keyed chunks over fact_records)
// agrees with the shipped legacy supplyChainImpactWinnerSelectSQL row for
// row on the same seeded backend, on a clean scope set and on a
// dirty/orphan scope that mirrors the replica findings (orphan supports on
// an active generation, stale stored priority_score vs the fact payload).
// It also records the chunked walk's wall time (every sequential chunk is
// bounded above by the walk total) and the advisory-lock single-flight.

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
// column, (b) a stale stored payload priority, (c) an orphan supports row
// on the active generation whose key columns match a live identifier but
// has no fact_record, (d) a fact payload rewrite the capture misses so the
// stored supports priority is stale against the fact payload. None of them
// may move the differential: ranking never reads stored supports columns,
// and the orphan drops in the active-facts fact_records join.
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
	// (d) comes first so the later supports syncs do not heal it: the fact
	// payload moves to 999 while supports keep the seeded value.
	plant(`UPDATE fact_records SET payload = jsonb_set(payload, '{priority_score}', '999')
WHERE fact_id = 'f-b4' AND scope_id = $1`, fullDiffDirtyScope)
	// (a) stale stored priority_score column (never read by the probe or the rank).
	if _, err := db.ExecContext(ctx, `UPDATE supply_chain_impact_finding_supports SET priority_score = 1 WHERE fact_id = 'f-b1'`); err != nil {
		t.Fatalf("plant stale supports priority: %v", err)
	}
	// (b) stale stored payload priority (never read: payloads arrive by fact_id point lookup).
	if _, err := db.ExecContext(ctx, `UPDATE supply_chain_impact_finding_supports
SET payload = jsonb_set(payload, '{priority_score}', '7') WHERE fact_id = 'f-b2'`); err != nil {
		t.Fatalf("plant stale supports payload: %v", err)
	}
	// (c) orphan supports on the active generation: key columns copied from
	// f-b3's live row, a fact_id with no fact_record, a winning priority.
	if _, err := db.ExecContext(ctx, `
INSERT INTO supply_chain_impact_finding_supports
    (fact_id, canonical_key, scope_id, generation_id, finding_id, has_payload_finding_id,
     priority_score, source_confidence, suppression_state, impact_status, ecosystem,
     severity_bucket, repository_id, cve_id, advisory_id, package_id, subject_digest,
     image_ref, priority_bucket, detection_profile, observed_version, match_reason, payload)
SELECT 'fact:orphan:9805', canonical_key, scope_id, generation_id, 'finding:orphan',
     1, 9999, source_confidence, suppression_state, impact_status, ecosystem,
     severity_bucket, repository_id, cve_id, advisory_id, package_id, subject_digest,
     image_ref, priority_bucket, detection_profile, observed_version, match_reason, payload
FROM supply_chain_impact_finding_supports WHERE fact_id = 'f-b3'`); err != nil {
		t.Fatalf("plant orphan supports: %v", err)
	}
	// The (d) fact rewrite tripped the old-writer fence, which would close
	// the supports gate and dodge the presence probe this phase must run.
	// Clearing it is the test setup, not a heal: every presence-relevant
	// supports column (the key columns and fact_id presence) is untouched
	// by construction above, and the orphan (a supports-only insert) fires
	// no fact trigger. A regression to reading stored ranking columns or to
	// emitting the orphan still fails the EXCEPT ALL below.
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
	// chunks run the fact_records payload-expression probe.
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
	// triple-guarded supports presence probe over stale/orphan supports.
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
	// The (d) rewrite must have healed: the stored winner follows the fact
	// payload to 999, not the stale supports value.
	var b4 int
	if err := db.QueryRowContext(ctx, `SELECT priority_score FROM supply_chain_impact_canonical_winners
WHERE winner_fact_id = 'f-b4'`).Scan(&b4); err != nil {
		t.Fatalf("read healed b4 winner: %v", err)
	}
	if b4 != 999 {
		t.Fatalf("b4 stored priority = %d, want 999 (the resweep must follow the fact payload)", b4)
	}
	fwd, rev = fullDiffExceptAll(t, ctx, db)
	t.Logf("phase-2 differential: legacy-minus-stored=%d stored-minus-legacy=%d", fwd, rev)
	if fwd != 0 || rev != 0 {
		t.Fatalf("phase-2 chunked-full disagrees with legacy: forward=%d reverse=%d", fwd, rev)
	}

	// Every chunk ran sequentially inside its resweep transaction, so each
	// chunk took at most the walk total, far under the 60 s bound.
	for name, elapsed := range map[string]time.Duration{"phase-1": firstElapsed, "phase-2": secondElapsed} {
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
