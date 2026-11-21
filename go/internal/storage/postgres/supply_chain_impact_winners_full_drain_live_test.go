// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Full-resweep drain proofs for #11117 B. The full resweep recomputes every
// key, so under the resweep lock and in its own transaction it reads S from
// the DB clock, deletes the dirty-log and staged rows visible to that delete,
// then runs the rebuild, and stamps GREATEST(prev, LEAST(S, residual min -
// 1us)). A mark that executes before S and commits after the rebuild snapshot
// stays queued for the next tick; a failed full rolls the deletes back.

// TestSupplyChainImpactWinnersResweepFullDrainsPreStampRowsLive proves the
// full consumes every mark captured before it: staged writer marks and a
// queued dirty-log row are gone after the full, the watermark covers them,
// and the next keyed tick advances the watermark instead of draining a
// backlog the full already recomputed.
func TestSupplyChainImpactWinnersResweepFullDrainsPreStampRowsLive(t *testing.T) {
	h := newKeyedHarness(t, "#11117 full resweep drain proof")
	h.seedScope("vuln-intel://11117/drain", "vuln-intel://11117/drain/g1")
	h.write("vuln-intel://11117/drain", "vuln-intel://11117/drain/g1", chunkedFindings("drain", 6, 2, 0)...)
	h.exec(`INSERT INTO supply_chain_impact_winners_dirty_log (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
VALUES ('fact', 'audit', 'pkg:npm/drain-00', 'repo-9805', '', 'CVE-2026-drain-00-00', '')`)
	if h.stagingDepth() == 0 || h.dirtyDepth() == 0 {
		t.Fatalf("fixture: staging %d, log %d; want both non-empty before the full", h.stagingDepth(), h.dirtyDepth())
	}
	before := horizonDBNow(h)

	full, err := h.store.ResweepWinners(h.ctx)
	if err != nil || full.Skipped {
		t.Fatalf("full resweep = %+v, %v", full, err)
	}
	if n := h.pendingDepth(); n != 0 {
		t.Fatalf("pending marks after the full = %d (log %d, staging %d), want 0: the full left rows it covered queued", n, h.dirtyDepth(), h.stagingDepth())
	}
	stamped := chunkedWatermark(h)
	if !stamped.After(before) {
		t.Fatalf("full watermark = %v, want after %v (the DB clock read before the full)", stamped, before)
	}
	h.assertParity("after the drained full")

	keyed := chunkedTick(h)
	if keyed.Bounded || keyed.Remaining != 0 {
		t.Fatalf("keyed tick after the drained full = %+v, want an idle unbounded tick", keyed)
	}
	if got := chunkedWatermark(h); !got.After(stamped) {
		t.Fatalf("keyed tick watermark = %v, want it advanced past the full's %v", got, stamped)
	}
}

// TestSupplyChainImpactWinnersResweepFullLeavesLateCommitMarkQueuedLive
// proves the drain deletes only rows visible to it: a writer that changes a
// fact and stages its mark before the full but commits after it keeps that
// mark queued, and the next keyed tick applies it.
func TestSupplyChainImpactWinnersResweepFullLeavesLateCommitMarkQueuedLive(t *testing.T) {
	h := newKeyedHarness(t, "#11117 full resweep late-commit proof")
	h.seedScope("vuln-intel://11117/late", "vuln-intel://11117/late/g1")
	h.write("vuln-intel://11117/late", "vuln-intel://11117/late/g1", chunkedFindings("late", 3, 1, 0)...)
	if _, err := h.store.ResweepWinners(h.ctx); err != nil {
		t.Fatalf("initial full resweep: %v", err)
	}
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging`)

	late, err := h.db.BeginTx(h.ctx, nil)
	if err != nil {
		t.Fatalf("begin late writer: %v", err)
	}
	defer func() { _ = late.Rollback() }()
	for _, q := range []string{
		`UPDATE fact_records SET payload = jsonb_set(payload, '{priority_score}', '97') WHERE payload->>'cve_id' = 'CVE-2026-late-01-00'`,
		`INSERT INTO supply_chain_impact_winners_dirty_staging (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
SELECT 'fact', 'fact_upsert', COALESCE(payload->>'package_id', ''), COALESCE(payload->>'repository_id', ''), COALESCE(payload->>'subject_digest', ''),
       COALESCE(NULLIF(payload->>'cve_id', ''), NULLIF(payload->>'advisory_id', ''), ''), COALESCE(payload->>'advisory_id', '')
FROM fact_records WHERE payload->>'cve_id' = 'CVE-2026-late-01-00'`,
	} {
		if _, err := late.ExecContext(h.ctx, q); err != nil {
			t.Fatalf("late writer statement: %v", err)
		}
	}

	if full, err := h.store.ResweepWinners(h.ctx); err != nil || full.Skipped {
		t.Fatalf("full resweep beside the open writer = %+v, %v", full, err)
	}
	if err := late.Commit(); err != nil {
		t.Fatalf("commit late writer: %v", err)
	}
	if n := h.stagingDepth(); n != 1 {
		t.Fatalf("staged marks after the late commit = %d, want 1: the full deleted a mark it never saw", n)
	}
	chunkedDrain(h)
	if n := h.pendingDepth(); n != 0 {
		t.Fatalf("pending marks after the keyed drain = %d, want 0", n)
	}
	h.assertParity("after the late-commit mark applied")
}

// TestSupplyChainImpactWinnersResweepFullRollbackRestoresDrainedRowsLive
// proves the drain and the rebuild commit together: a rebuild that fails
// after the drain leaves every queued mark in place.
func TestSupplyChainImpactWinnersResweepFullRollbackRestoresDrainedRowsLive(t *testing.T) {
	h := newKeyedHarness(t, "#11117 full resweep rollback proof")
	h.seedScope("vuln-intel://11117/rollback", "vuln-intel://11117/rollback/g1")
	h.write("vuln-intel://11117/rollback", "vuln-intel://11117/rollback/g1", chunkedFindings("rollback", 4, 1, 0)...)
	h.exec(`INSERT INTO supply_chain_impact_winners_dirty_log (kind, source, scope_id, generation_id)
VALUES ('scope', 'activation', 'vuln-intel://11117/rollback', 'vuln-intel://11117/rollback/g1')`)
	logBefore, stagingBefore := h.dirtyDepth(), h.stagingDepth()
	if logBefore == 0 || stagingBefore == 0 {
		t.Fatalf("fixture: log %d, staging %d; want both non-empty", logBefore, stagingBefore)
	}
	h.exec(`CREATE FUNCTION winners_11117_refuse_stamp() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'winners 11117 refuse stamp'; END $$`)
	h.exec(`CREATE TRIGGER winners_11117_refuse_stamp BEFORE INSERT OR UPDATE ON supply_chain_impact_winners_materialization
FOR EACH ROW EXECUTE FUNCTION winners_11117_refuse_stamp()`)

	if got, err := h.store.ResweepWinners(h.ctx); err == nil {
		t.Fatalf("full resweep with a refused stamp = %+v, want an error", got)
	}
	if l, s := h.dirtyDepth(), h.stagingDepth(); l != logBefore || s != stagingBefore {
		t.Fatalf("after the failed full: log %d staging %d, want %d and %d restored", l, s, logBefore, stagingBefore)
	}

	h.exec(`DROP TRIGGER winners_11117_refuse_stamp ON supply_chain_impact_winners_materialization`)
	if full, err := h.store.ResweepWinners(h.ctx); err != nil || full.Skipped {
		t.Fatalf("full resweep after the trigger dropped = %+v, %v", full, err)
	}
	if n := h.pendingDepth(); n != 0 {
		t.Fatalf("pending marks after the retried full = %d, want 0", n)
	}
}

// TestSupplyChainImpactWinnersResweepFullStampsUnderResidualLive proves the
// stamp is capped by the residual set the rebuild's snapshot sees: a mark
// captured before S that commits after the drain but before the rebuild
// stays queued, and the watermark is its captured_at minus 1us, not S.
func TestSupplyChainImpactWinnersResweepFullStampsUnderResidualLive(t *testing.T) {
	h := newKeyedHarness(t, "#11117 full resweep residual stamp proof")
	h.seedScope("vuln-intel://11117/residual", "vuln-intel://11117/residual/g1")
	h.write("vuln-intel://11117/residual", "vuln-intel://11117/residual/g1", chunkedFindings("residual", 2, 1, 0)...)

	var captured time.Time
	injected := false
	hooked := beforeRebuildDB{inner: postgres.SQLDB{DB: h.db}, hook: func() {
		if injected {
			return
		}
		injected = true
		if err := h.db.QueryRowContext(h.ctx, `INSERT INTO supply_chain_impact_winners_dirty_staging (kind, source, package_id, repository_id, subject_digest, c0, advisory_id, captured_at)
VALUES ('fact', 'fact_upsert', 'pkg:npm/residual-00', 'repo-9805', '', 'CVE-2026-residual-00-00', '', clock_timestamp() - interval '1 second')
RETURNING captured_at`).Scan(&captured); err != nil {
			t.Fatalf("inject residual mark: %v", err)
		}
	}}
	store := h.store
	store.DB = hooked
	if full, err := store.ResweepWinners(h.ctx); err != nil || full.Skipped {
		t.Fatalf("full resweep = %+v, %v", full, err)
	}
	if !injected {
		t.Fatal("the rebuild statement never ran through the hook")
	}
	if n := h.stagingDepth(); n != 1 {
		t.Fatalf("staged marks after the full = %d, want only the residual mark", n)
	}
	if got, want := chunkedWatermark(h), captured.UTC().Add(-time.Microsecond); !got.Equal(want) {
		t.Fatalf("full watermark = %v, want the residual captured_at minus 1us %v", got, want)
	}
}

// beforeRebuildDB runs hook, on its own connection, right before the first
// statement of the transaction that carries the winners rebuild.
type beforeRebuildDB struct {
	inner postgres.SQLDB
	hook  func()
}

func (d beforeRebuildDB) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return d.inner.QueryContext(ctx, query, args...)
}

func (d beforeRebuildDB) Begin(ctx context.Context) (db.Transaction, error) {
	tx, err := d.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return beforeRebuildTx{Transaction: tx, hook: d.hook}, nil
}

type beforeRebuildTx struct {
	db.Transaction
	hook func()
}

func (tx beforeRebuildTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if strings.Contains(query, "WITH winners_now AS") {
		tx.hook()
	}
	return tx.Transaction.QueryContext(ctx, query, args...)
}
