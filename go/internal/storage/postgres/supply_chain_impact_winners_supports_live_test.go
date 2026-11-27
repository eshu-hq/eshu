// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// Live proofs of the winners maintainer over the supports table (#10880 PR-C1,
// arbiter ruling R2 and section 3): the maintainer's supports gate on a real marker,
// and the differential between the winners select over the finding facts and the same
// select over the supports table.

func winnersSupportsWatermark(t *testing.T, h *keyedHarness) (at time.Time, present bool) {
	t.Helper()
	err := h.db.QueryRowContext(h.ctx,
		`SELECT materialized_at FROM supply_chain_impact_winners_materialization WHERE singleton = 1`).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false
	}
	if err != nil {
		t.Fatalf("read winners watermark: %v", err)
	}
	return at, true
}

// winnersSupportsRun runs a maintainer built from w until keyedTicks keyed ticks have
// run (or, with keyedTicks 0, for the window), then stops it.
func winnersSupportsRun(t *testing.T, h *keyedHarness, w *countingWinners, keyedTicks int, window time.Duration) {
	t.Helper()
	leases := postgres.NewSharedIntentStore(postgres.SQLDB{DB: h.db})
	m := supplychaincore.SupplyChainImpactWinnersMaintainer{
		Rebuilder: w, KeyedRebuilder: w, Contract: w, SupportsGate: h.store,
		LeaseManager: leases, LeaseOwner: "winners-c1-gate", Interval: 20 * time.Millisecond,
	}
	_, startKeyed := w.counts()
	ctx, cancel := context.WithCancel(h.ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := m.Run(ctx); err != nil {
			t.Errorf("maintainer: %v", err)
		}
	}()
	deadline := time.After(90 * time.Second)
	if keyedTicks == 0 {
		deadline = time.After(window)
	}
	for done := false; !done; {
		select {
		case <-deadline:
			done = true
		case <-time.After(20 * time.Millisecond):
			if _, keyed := w.counts(); keyedTicks > 0 && keyed-startKeyed >= keyedTicks {
				done = true
			}
		}
	}
	cancel()
	wg.Wait()
	awaitWinnersResweepLockFree(h)
}

// TestSupplyChainImpactWinnersResweepSupportsGateLive proves the maintainer gate on a
// real backfill marker: while the supports table is not ready no full or keyed resweep
// runs and the watermark does not move; the first ready tick after a not-ready period
// runs exactly one full even though the stored contract is current; and a restarted
// maintainer (a new instance over the same database) runs no second one.
func TestSupplyChainImpactWinnersResweepSupportsGateLive(t *testing.T) {
	h := newKeyedHarness(t, "#10880 C1 winners supports gate proof")
	const scope, generation = "vuln-intel://c1-gate/s1", "vuln-intel://c1-gate/s1/g1"
	h.seedScope(scope, generation)
	h.write(scope, generation, chunkedFindings("c1gate", 3, 2, 0)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging`)
	// A known marker state: no marker row, an empty old-writer fence.
	h.exec(`DELETE FROM supply_chain_impact_finding_supports_backfill`)
	h.exec(`DELETE FROM supply_chain_impact_finding_supports_dirty`)

	gate, err := h.store.WinnersSupportsGate(h.ctx)
	if err != nil || gate.Ready {
		t.Fatalf("gate with no marker = %+v, %v, want not ready", gate, err)
	}
	// Bring the contract current under a ready table first, so a later full can only
	// come from the supports gate.
	backfill := postgres.SupplyChainImpactSupportsBackfill{DB: postgres.SQLDB{DB: h.db}}
	for step := 0; ; step++ {
		report, err := backfill.RunStep(h.ctx)
		if err != nil {
			t.Fatalf("backfill step: %v", err)
		}
		if report.Complete {
			break
		}
		if step > 200 {
			t.Fatal("the supports backfill did not complete")
		}
	}
	winners := &countingWinners{store: h.store}
	winnersSupportsRun(t, h, winners, 3, 0)
	if fulls, _ := winners.counts(); fulls != 1 {
		t.Fatalf("fulls while becoming current = %d, want the one contract full", fulls)
	}
	if gate, err := h.store.WinnersSupportsGate(h.ctx); err != nil || !gate.Ready || gate.FullNeeded {
		t.Fatalf("gate after the contract full = %+v, %v, want ready with no full needed", gate, err)
	}

	// A not-ready period: the marker is reset. Nothing runs and the watermark stays.
	h.exec(`UPDATE supply_chain_impact_finding_supports_backfill SET completed_at = NULL WHERE marker_name = $1`, postgres.SupplyChainImpactSupportsBackfillMarker)
	before, _ := winnersSupportsWatermark(t, h)
	fullsBefore, keyedBefore := winners.counts()
	winnersSupportsRun(t, h, winners, 0, 400*time.Millisecond)
	fullsAfter, keyedAfter := winners.counts()
	if fullsAfter != fullsBefore || keyedAfter != keyedBefore {
		t.Fatalf("a not-ready period ran fulls %d->%d and keyed ticks %d->%d, want nothing", fullsBefore, fullsAfter, keyedBefore, keyedAfter)
	}
	if after, _ := winnersSupportsWatermark(t, h); !after.Equal(before) {
		t.Fatalf("the watermark moved from %v to %v while the table was not ready", before, after)
	}

	// The marker completes after the last committed resweep: the first ready tick runs
	// exactly one full, then keyed ticks.
	h.exec(`UPDATE supply_chain_impact_finding_supports_backfill SET completed_at = clock_timestamp() WHERE marker_name = $1`, postgres.SupplyChainImpactSupportsBackfillMarker)
	if gate, err := h.store.WinnersSupportsGate(h.ctx); err != nil || !gate.Ready || !gate.FullNeeded {
		t.Fatalf("gate after the marker completed = %+v, %v, want ready with a full needed", gate, err)
	}
	winnersSupportsRun(t, h, winners, 3, 0)
	fullsReady, _ := winners.counts()
	if fullsReady != fullsAfter+1 {
		t.Fatalf("fulls after becoming ready = %d, want exactly one more than %d", fullsReady, fullsAfter)
	}

	// A restart: a new maintainer over the same database decides from the durable
	// state and runs no second full.
	winnersSupportsRun(t, h, winners, 3, 0)
	if fulls, _ := winners.counts(); fulls != fullsReady {
		t.Fatalf("a restarted maintainer ran a second full: %d, want %d", fulls, fullsReady)
	}
}

// TestSupplyChainImpactWinnersResweepSupportsDifferentialLive is the byte-identical
// winners differential the ruling requires before C1 opens (section 3): over a seed
// with overlapping sources, competing priorities and a retracted finding, the winners
// table a supports-driven resweep wrote equals the same select over the supports
// table (EXCEPT ALL both ways is empty over every column). The seeds run through
// the production writer, which upserts supports only and never writes finding
// fact rows, so the legacy facts select is empty here and the supports select
// is the oracle (#10880).
func TestSupplyChainImpactWinnersResweepSupportsDifferentialLive(t *testing.T) {
	h := newKeyedHarness(t, "#10880 C1 winners differential proof")
	const (
		s1, g1 = "vuln-intel://c1-diff/s1", "vuln-intel://c1-diff/s1/g1"
		s2, g2 = "vuln-intel://c1-diff/s2", "vuln-intel://c1-diff/s2/g1"
	)
	h.seedScope(s1, g1)
	h.seedScope(s2, g2)
	h.write(s1, g1, chunkedFindings("diff", 4, 3, 0)...)
	// A second source for the same keys with other priorities, plus keys only it has.
	h.write(s2, g2, append(chunkedFindings("diff", 4, 3, 5), chunkedFindings("only", 2, 2, 0)...)...)
	// A retraction: a pass of the first scope that drops one triple.
	h.write(s1, g1, chunkedFindings("diff", 3, 3, 0)...)

	_, supports := postgres.WinnersProofSelects()
	columns := "canonical_key, " + strings.Join(postgres.WinnersProofDataColumns(), ", ")
	count := func(query string) int {
		t.Helper()
		return h.count(query)
	}
	if n := count(`SELECT count(*) FROM (` + supports + `) AS q`); n == 0 {
		t.Fatal("the supports select returned no winners: the seed is empty")
	}

	h.exec(`DELETE FROM supply_chain_impact_canonical_winners`)
	if got, err := h.store.ResweepWinners(h.ctx); err != nil || got.Skipped {
		t.Fatalf("supports-driven resweep = %+v, %v", got, err)
	}
	tableOnly := `SELECT ` + columns + ` FROM supply_chain_impact_canonical_winners`
	fromSupports := `SELECT ` + columns + ` FROM (` + supports + `) AS l`
	if n := count(`SELECT count(*) FROM ((` + fromSupports + `) EXCEPT ALL (` + tableOnly + `)) AS d`); n != 0 {
		t.Errorf("supports select EXCEPT ALL the supports-driven winners table = %d rows, want 0", n)
	}
	if n := count(`SELECT count(*) FROM ((` + tableOnly + `) EXCEPT ALL (` + fromSupports + `)) AS d`); n != 0 {
		t.Errorf("supports-driven winners table EXCEPT ALL the supports select = %d rows, want 0", n)
	}
}

// winnersAssertTableEqualsSupportsSelect requires the stored winners table to equal the
// winners select over the supports table, EXCEPT ALL both ways over every data column:
// the keyed tick reads the supports table, so a test whose seeds run through the
// production writer (supports only, no finding fact rows) checks the table against
// the supports oracle (#10880; the change classes of the PR-C design ruling,
// section 5). Call it after the tick and before any full recompute, which would
// overwrite the table.
func winnersAssertTableEqualsSupportsSelect(h *keyedHarness, label string) {
	h.t.Helper()
	_, supports := postgres.WinnersProofSelects()
	columns := "canonical_key, " + strings.Join(postgres.WinnersProofDataColumns(), ", ")
	table := `SELECT ` + columns + ` FROM supply_chain_impact_canonical_winners`
	fromSupports := `SELECT ` + columns + ` FROM (` + supports + `) AS l`
	if n := h.count(`SELECT count(*) FROM ((` + fromSupports + `) EXCEPT ALL (` + table + `)) AS d`); n != 0 {
		h.t.Errorf("%s: the supports select EXCEPT ALL the keyed winners table = %d rows, want 0", label, n)
	}
	if n := h.count(`SELECT count(*) FROM ((` + table + `) EXCEPT ALL (` + fromSupports + `)) AS d`); n != 0 {
		h.t.Errorf("%s: the keyed winners table EXCEPT ALL the supports select = %d rows, want 0", label, n)
	}
}
