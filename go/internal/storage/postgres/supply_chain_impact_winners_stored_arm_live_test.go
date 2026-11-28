// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Live proofs for the ruled #9805 stored-arm fix (contract v4, re-sourced
// round 5 under contract v5): the full enumeration unions the live candidate
// set with the stored winners' identifiers. P3 proves the UnlessCurrent
// healing path (a stored v4 runs the full and heals, a stored v5 skips).
// P4 proves a dead-key delete composes
// with a residual mark (the delete fires, the mark is never lost, the stamp
// caps under it). P5 proves the old-vs-new enumeration plans and the
// dead-only chunk timing. P6 proves a stored-only key overlapping a
// concurrent writer revive converges. P7 (audit parity) rides every proof:
// the audit keeps the legacy select as its oracle, and with the table
// converged it reports zero repairs, converged and empty alike.
//
// Residual, telemetry and the audit are unchanged by the fix: the resweep
// still reports Path full with the same count shape, the residual still caps
// the stamp, and the audit still runs the legacy statement verbatim.

// TestSupplyChainImpactWinnersUnlessCurrentHealsStoredOnlyKeysLive proves the
// v5 healing path: a binary on contract 5 that reads a stored v4 runs the
// full, the full deletes the stored-only key, the stored contract becomes
// v5, and the next UnlessCurrent skips. The audit then reports zero repairs.
func TestSupplyChainImpactWinnersUnlessCurrentHealsStoredOnlyKeysLive(t *testing.T) {
	_ = supplyChainImpactLiveDSN(t, "#9805 v5 UnlessCurrent healing proof")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	db := openAdvisorySuppressionIsolatedDB(t)
	store := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: db})

	winnersLiveSeedScope(t, ctx, db, winnersLiveSourceScope)
	winnersLiveSeedFact(t, ctx, db, "f-heal-live", winnersLiveSourceScope, winnersLiveFinding("CVE-2026-9821", "pkg:npm/heal", 5, nil))
	winnersLiveSeedFact(t, ctx, db, "f-heal-dead", winnersLiveSourceScope, winnersLiveFinding("CVE-2026-9822", "pkg:npm/gone", 6, nil))
	if first, err := store.ResweepWinners(ctx); err != nil || first.Skipped || first.Inserted != 2 {
		t.Fatalf("seed resweep = %+v, %v; want 2 inserted", first, err)
	}
	// Roll the stored contract back to v4: the next UnlessCurrent must run.
	winnersLiveExec(t, ctx, db, `UPDATE supply_chain_impact_winners_materialization SET contract_version = 4, contract_hash = '2e8903a9f694893f3caf8bdb1ec8bce8afd705c92974dc12e567cd371a2dfc40'`)
	// The dead key: tombstoned after the last resweep, still stored.
	winnersLiveExec(t, ctx, db, `UPDATE fact_records SET is_tombstone = TRUE WHERE fact_id = 'f-heal-dead'`)

	heal, err := store.ResweepWinnersUnlessCurrent(ctx)
	if err != nil || heal.Skipped {
		t.Fatalf("UnlessCurrent over stored v4 = %+v, %v; want a healing full", heal, err)
	}
	if heal.Path != telemetry.SupplyChainImpactWinnersPathFull {
		t.Errorf("healing resweep path = %q, want full (telemetry unchanged)", heal.Path)
	}
	if heal.Deleted != 1 || heal.Unchanged != 1 {
		t.Errorf("healing resweep = %+v, want exactly the stored-only key deleted", heal)
	}
	contract, err := store.WinnersContract(ctx)
	if err != nil {
		t.Fatalf("read contract: %v", err)
	}
	if !contract.Present || contract.StoredVersion != 5 || contract.StoredHash != contract.BinaryHash {
		t.Errorf("stored contract = %+v, want version 5 with the binary hash", contract)
	}

	skip, err := store.ResweepWinnersUnlessCurrent(ctx)
	if err != nil || !skip.Skipped || !skip.ContractCurrent {
		t.Fatalf("UnlessCurrent over stored v5 = %+v, %v; want Skipped with ContractCurrent", skip, err)
	}
	t.Logf("healing-full: stored v4 ran (deleted=%d), stored v5 skips", heal.Deleted)
	fullDiffAssertParity(t, ctx, db, "healed")

	audit, err := store.AuditWinners(ctx)
	if err != nil || audit.Skipped || audit.Repairs != 0 {
		t.Fatalf("audit of the healed table = %+v, %v; want zero repairs against the oracle", audit, err)
	}
}

// TestSupplyChainImpactWinnersDeadKeyDeleteWithResidualMarkLive proves the
// dead-key delete composes with a residual mark. The resweep blocks on a row
// lock over the dead winner (so its snapshot and drain are done), a dirty
// mark commits mid-walk, and the walk then reports the delete AND the
// residual, with the watermark capped under the mark instead of advancing to
// the stamp ceiling. A follow-up resweep drains the mark and converges.
func TestSupplyChainImpactWinnersDeadKeyDeleteWithResidualMarkLive(t *testing.T) {
	_ = supplyChainImpactLiveDSN(t, "#9805 dead-key by residual-mark proof")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	db := openAdvisorySuppressionIsolatedDB(t)
	store := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: db})

	winnersLiveSeedScope(t, ctx, db, winnersLiveSourceScope)
	winnersLiveSeedFact(t, ctx, db, "f-res-dead", winnersLiveSourceScope, winnersLiveFinding("CVE-2026-9831", "pkg:npm/resdead", 5, nil))
	winnersLiveSeedFact(t, ctx, db, "f-res-live", winnersLiveSourceScope, winnersLiveFinding("CVE-2026-9832", "pkg:npm/reslive", 6, nil))
	if first, err := store.ResweepWinners(ctx); err != nil || first.Skipped || first.Inserted != 2 {
		t.Fatalf("seed resweep = %+v, %v; want 2 inserted", first, err)
	}
	previous := winnersLiveWatermark(t, ctx, db)
	winnersLiveExec(t, ctx, db, `UPDATE fact_records SET is_tombstone = TRUE WHERE fact_id = 'f-res-dead'`)

	// Pin the walk after its snapshot and drain: the dead row's lock blocks
	// the chunk delete, exactly as the killed-holder proof pins it.
	holder, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	var one int
	if err := holder.QueryRowContext(ctx, `SELECT 1 FROM supply_chain_impact_canonical_winners WHERE cve_id = 'CVE-2026-9831' FOR UPDATE`).Scan(&one); err != nil {
		_ = holder.Rollback()
		t.Fatalf("lock dead winner: %v", err)
	}
	a := winnersLiveStart(ctx, store)
	winnersLiveAwaitBlocked(t, ctx, db, 1, nil)

	// The residual mark commits while the walk is blocked: after the drain,
	// so the residual count must see it. Backdated an hour so the stamp cap
	// bites: LEAST(S, mark - 1us) sits under the previous watermark and the
	// stamp must hold the watermark instead of advancing it.
	var pkg, repo, digest, c0, advisory string
	if err := db.QueryRowContext(ctx, `SELECT package_id, repository_id, subject_digest,
COALESCE(NULLIF(cve_id, ''), NULLIF(advisory_id, ''), ''), COALESCE(advisory_id, '')
FROM supply_chain_impact_canonical_winners WHERE cve_id = 'CVE-2026-9832'`).Scan(&pkg, &repo, &digest, &c0, &advisory); err != nil {
		_ = holder.Rollback()
		t.Fatalf("read live key: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO supply_chain_impact_winners_dirty_log
(kind, source, package_id, repository_id, subject_digest, c0, advisory_id, captured_at)
VALUES ('fact', 'fact_upsert', $1, $2, $3, $4, $5, clock_timestamp() - interval '1 hour')`,
		pkg, repo, digest, c0, advisory); err != nil {
		_ = holder.Rollback()
		t.Fatalf("plant residual mark: %v", err)
	}
	if err := holder.Commit(); err != nil {
		t.Fatalf("release dead winner: %v", err)
	}
	outcome := <-a
	if outcome.err != nil || outcome.result.Skipped {
		t.Fatalf("blocked resweep = %+v, %v; want a commit", outcome.result, outcome.err)
	}
	r := outcome.result
	t.Logf("dead-key by residual: inserted=%d updated=%d deleted=%d unchanged=%d remaining=%d watermark=%v previous=%v",
		r.Inserted, r.Updated, r.Deleted, r.Unchanged, r.Remaining, r.Watermark, r.PreviousWatermark)
	if r.Deleted != 1 {
		t.Errorf("resweep deleted = %d, want the dead key", r.Deleted)
	}
	if r.Remaining != 1 {
		t.Errorf("resweep remaining = %d, want the mid-walk mark", r.Remaining)
	}
	if !r.Watermark.Equal(previous) || !r.PreviousWatermark.Equal(previous) {
		t.Errorf("watermark = %v previous = %v, want both held at %v under the backdated mark", r.Watermark, r.PreviousWatermark, previous)
	}

	// The follow-up resweep drains the mark and converges to the legacy select.
	second, err := store.ResweepWinners(ctx)
	if err != nil || second.Skipped {
		t.Fatalf("follow-up resweep = %+v, %v; want a commit", second, err)
	}
	if second.Remaining != 0 {
		t.Errorf("follow-up remaining = %d, want the mark drained", second.Remaining)
	}
	fullDiffAssertParity(t, ctx, db, "dead-key-residual")
}

// TestSupplyChainImpactWinnersFullEnumerationExplainOldVsNewLive proves the
// plan cost of the stored arm: EXPLAIN (ANALYZE, BUFFERS) over the shipped
// enumeration and over the pre-fix live-only text derived from it by cutting
// the stored arm at its stable UNION boundaries. The new set is the old set
// plus exactly the stored-only identifiers.
func TestSupplyChainImpactWinnersFullEnumerationExplainOldVsNewLive(t *testing.T) {
	_ = supplyChainImpactLiveDSN(t, "#9805 enumeration old-vs-new EXPLAIN proof")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	db := openAdvisorySuppressionIsolatedDB(t)
	store := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: db})

	winnersLiveSeedScope(t, ctx, db, winnersLiveSourceScope)
	for i := range 10 {
		winnersLiveSeedFact(t, ctx, db, fmt.Sprintf("f-ex-%02d", i),
			winnersLiveSourceScope, winnersLiveFinding(fmt.Sprintf("CVE-2026-984%02d", i), "pkg:npm/explain", 1+i, nil))
	}
	if _, err := store.ResweepWinners(ctx); err != nil {
		t.Fatalf("seed resweep: %v", err)
	}
	// Two stored-only keys: tombstoned after the resweep, still stored.
	winnersLiveExec(t, ctx, db, `UPDATE fact_records SET is_tombstone = TRUE WHERE fact_id IN ('f-ex-00', 'f-ex-01')`)

	shipped := postgres.WinnersFullIdentsSQLForTest
	head, arm, ok := strings.Cut(shipped, "\n    UNION\n")
	if !ok {
		t.Fatal("shipped enumeration has no live-plus-stored UNION boundary to cut")
	}
	if _, _, ok := strings.Cut(arm, "\n) AS idents"); !ok {
		t.Fatal("shipped stored arm has no closing boundary to cut")
	}
	if !strings.Contains(arm, "supply_chain_impact_canonical_winners") {
		t.Fatal("cut arm is not the stored arm: the boundaries are stale")
	}
	oldCreate := strings.Replace(head+"\n) AS idents", "winners_full_idents", "explain_old_idents", 1)
	newCreate := strings.Replace(shipped, "winners_full_idents", "explain_new_idents", 1)
	// Both arms read supports since the round-5 re-source: the live arm
	// pages the supports candidate set, so a live arm over fact_records
	// would enumerate nothing on production-shaped data.
	for name, sql := range map[string]string{"old": oldCreate, "new": newCreate} {
		if !strings.Contains(sql, "supply_chain_impact_finding_supports") {
			t.Fatalf("%s enumeration does not read the supports table", name)
		}
		if strings.Contains(sql, "fact_records") {
			t.Fatalf("%s enumeration reads fact_records: the live arm pages supports", name)
		}
	}
	if strings.Contains(oldCreate, "UNION") || strings.Contains(oldCreate, "supply_chain_impact_canonical_winners") {
		t.Fatal("derived old enumeration still carries the stored arm: the cut is stale")
	}
	// EXPLAIN never executes DDL, so the plans run over the bare SELECT the
	// CREATE wraps; the set comparisons below wrap the same SELECTs.
	selectOf := func(name, create string) string {
		t.Helper()
		idx := strings.Index(create, "ON COMMIT DROP AS\n")
		if idx < 0 {
			t.Fatalf("%s enumeration has no wrapped SELECT to explain: the CREATE shape is stale", name)
		}
		sel := strings.TrimSpace(create[idx+len("ON COMMIT DROP AS\n"):])
		if !strings.HasPrefix(sel, "SELECT package_id") {
			t.Fatalf("%s enumeration SELECT is stale: %.60q", name, sel)
		}
		return sel
	}
	oldSelect, newSelect := selectOf("old", oldCreate), selectOf("new", newCreate)

	explain := func(name, sql string) {
		t.Helper()
		rows, err := db.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+sql)
		if err != nil {
			t.Fatalf("explain %s: %v", name, err)
		}
		defer func() { _ = rows.Close() }()
		var lines []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatalf("scan %s plan: %v", name, err)
			}
			lines = append(lines, line)
		}
		if err := rows.Close(); err != nil {
			t.Fatalf("close %s plan: %v", name, err)
		}
		t.Logf("%s enumeration plan:\n%s", name, strings.Join(lines, "\n"))
	}
	explain("old-live-only", oldSelect)
	explain("new-live-plus-stored", newSelect)

	sets := func(query string) int64 {
		t.Helper()
		var n int64
		if err := db.QueryRowContext(ctx, query).Scan(&n); err != nil {
			t.Fatalf("count %q: %v", query, err)
		}
		return n
	}
	cols := "package_id, repository_id, subject_digest, c0, advisory_id"
	idents := func(sel string) string {
		return `SELECT ` + cols + ` FROM (` + sel + `) AS idents`
	}
	oldCount := sets(`SELECT count(*) FROM (` + oldSelect + `) AS old_idents`)
	newCount := sets(`SELECT count(*) FROM (` + newSelect + `) AS new_idents`)
	liveOnlyInNew := sets(`SELECT count(*) FROM ((` + idents(newSelect) + `) EXCEPT (` + idents(oldSelect) + `)) AS d`)
	newOnlyInLive := sets(`SELECT count(*) FROM ((` + idents(oldSelect) + `) EXCEPT (` + idents(newSelect) + `)) AS d`)
	storedOnly := sets(`SELECT count(*) FROM ((SELECT package_id, repository_id, subject_digest,
COALESCE(NULLIF(cve_id, ''), NULLIF(advisory_id, ''), ''), COALESCE(advisory_id, '') FROM supply_chain_impact_canonical_winners)
EXCEPT (` + idents(oldSelect) + `)) AS d`)
	t.Logf("enumeration sets: old=%d new=%d new-minus-old=%d old-minus-new=%d stored-only=%d",
		oldCount, newCount, liveOnlyInNew, newOnlyInLive, storedOnly)
	if newOnlyInLive != 0 {
		t.Errorf("old enumeration holds %d identifiers the new one drops: the live arm regressed", newOnlyInLive)
	}
	if liveOnlyInNew != storedOnly || liveOnlyInNew != 2 {
		t.Errorf("new-minus-old = %d, stored-only = %d; want exactly the 2 tombstoned keys", liveOnlyInNew, storedOnly)
	}
}

// TestSupplyChainImpactWinnersDeadOnlyChunkTimingLive times the degenerate
// walk: every fact tombstoned, so the live arm is empty and only the stored
// arm feeds the chunks. Every stored row must go, and the audit then reports
// zero repairs on the empty table.
func TestSupplyChainImpactWinnersDeadOnlyChunkTimingLive(t *testing.T) {
	_ = supplyChainImpactLiveDSN(t, "#9805 dead-only chunk timing proof")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	db := openAdvisorySuppressionIsolatedDB(t)
	store := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: db})

	winnersLiveSeedScope(t, ctx, db, winnersLiveSourceScope)
	for i := range 6 {
		winnersLiveSeedFact(t, ctx, db, fmt.Sprintf("f-dead-%02d", i),
			winnersLiveSourceScope, winnersLiveFinding(fmt.Sprintf("CVE-2026-985%02d", i), "pkg:npm/deadonly", 1+i, nil))
	}
	if first, err := store.ResweepWinners(ctx); err != nil || first.Skipped {
		t.Fatalf("seed resweep = %+v, %v", first, err)
	}
	winnersLiveExec(t, ctx, db, `UPDATE fact_records SET is_tombstone = TRUE`)

	start := time.Now()
	r, err := store.ResweepWinners(ctx)
	elapsed := time.Since(start)
	if err != nil || r.Skipped {
		t.Fatalf("dead-only resweep = %+v, %v; want a commit", r, err)
	}
	t.Logf("dead-only walk: inserted=%d updated=%d deleted=%d unchanged=%d in %v",
		r.Inserted, r.Updated, r.Deleted, r.Unchanged, elapsed)
	if r.Deleted != 6 || r.Inserted != 0 || r.Updated != 0 || r.Unchanged != 0 {
		t.Errorf("dead-only resweep = %+v, want all 6 stored rows deleted", r)
	}
	if elapsed > 60*time.Second {
		t.Errorf("dead-only walk took %v: the stored-arm walk is not chunk-bounded", elapsed)
	}
	fullDiffAssertParity(t, ctx, db, "dead-only")

	audit, err := store.AuditWinners(ctx)
	if err != nil || audit.Skipped || audit.Repairs != 0 {
		t.Fatalf("audit of the reswept-to-zero table = %+v, %v; want zero repairs", audit, err)
	}
}

// TestSupplyChainImpactWinnersStoredOnlyOverlapConvergesLive proves the
// stored-only overlap converges: a writer revive of a tombstoned key races
// the full resweep that deletes it. Whichever commits first, the next
// resweep lands the live winner and the table equals the legacy select.
func TestSupplyChainImpactWinnersStoredOnlyOverlapConvergesLive(t *testing.T) {
	_ = supplyChainImpactLiveDSN(t, "#9805 stored-only overlap proof")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	db := openAdvisorySuppressionIsolatedDB(t)
	store := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: db})
	store.ChunkMaxIdentifiers = 2

	winnersLiveSeedScope(t, ctx, db, winnersLiveSourceScope)
	for i := range 40 {
		winnersLiveSeedFact(t, ctx, db, fmt.Sprintf("f-quiet-%02d", i),
			winnersLiveSourceScope, winnersLiveFinding(fmt.Sprintf("CVE-2026-98%02d", i), "pkg:npm/quiet", 1, nil))
	}
	winnersLiveSeedFact(t, ctx, db, "f-overlap-old", winnersLiveSourceScope, winnersLiveFinding("CVE-2026-9861", "pkg:npm/overlap", 5, nil))
	if first, err := store.ResweepWinners(ctx); err != nil || first.Skipped {
		t.Fatalf("seed resweep = %+v, %v", first, err)
	}
	// The key goes stored-only. The revive commits while the full is pinned
	// on the dead row's lock, so the full's snapshot cannot see it: the walk
	// must delete the stored-only key first, and the next resweep must land
	// the revive. Either commit order converges; this pins the harder one.
	winnersLiveExec(t, ctx, db, `UPDATE fact_records SET is_tombstone = TRUE WHERE fact_id = 'f-overlap-old'`)
	holder, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	var one int
	if err := holder.QueryRowContext(ctx, `SELECT 1 FROM supply_chain_impact_canonical_winners WHERE cve_id = 'CVE-2026-9861' FOR UPDATE`).Scan(&one); err != nil {
		_ = holder.Rollback()
		t.Fatalf("lock overlap winner: %v", err)
	}
	a := winnersLiveStart(ctx, store)
	winnersLiveAwaitBlocked(t, ctx, db, 1, nil)
	winnersLiveSeedFact(t, ctx, db, "f-overlap-new", winnersLiveSourceScope, winnersLiveFinding("CVE-2026-9861", "pkg:npm/overlap", 9, nil))
	if err := holder.Commit(); err != nil {
		t.Fatalf("release overlap winner: %v", err)
	}
	r1 := <-a
	if r1.err != nil || r1.result.Skipped {
		t.Fatalf("racing resweep = %+v, %v; want a commit", r1.result, r1.err)
	}
	t.Logf("overlap race: inserted=%d updated=%d deleted=%d unchanged=%d remaining=%d",
		r1.result.Inserted, r1.result.Updated, r1.result.Deleted, r1.result.Unchanged, r1.result.Remaining)
	if r1.result.Deleted != 1 {
		t.Errorf("racing resweep deleted = %d, want the stored-only key the revive could not yet cover", r1.result.Deleted)
	}

	r2, err := store.ResweepWinners(ctx)
	if err != nil || r2.Skipped {
		t.Fatalf("converging resweep = %+v, %v; want a commit", r2, err)
	}
	if r2.Inserted != 1 {
		t.Errorf("converging resweep inserted = %d, want the revive", r2.Inserted)
	}
	var winner string
	var priority int
	if err := db.QueryRowContext(ctx, `SELECT winner_fact_id, priority_score FROM supply_chain_impact_canonical_winners WHERE cve_id = 'CVE-2026-9861'`).Scan(&winner, &priority); err != nil {
		t.Fatalf("read overlap winner: %v", err)
	}
	if winner != "f-overlap-new" || priority != 9 {
		t.Errorf("overlap winner = %s priority %d, want f-overlap-new at 9: the revive was lost", winner, priority)
	}
	fullDiffAssertParity(t, ctx, db, "overlap")
}
