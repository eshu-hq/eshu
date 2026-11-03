// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// keyedSeedSettled seeds two scopes with overlapping and separate keys and
// settles the table, so a protocol test starts from a drained dirty log.
func keyedSeedSettled(h *keyedHarness) (scopeA, scopeB string) {
	scopeA, scopeB = "vuln-intel://9805/pa", "vuln-intel://9805/pb"
	h.seedScope(scopeA, scopeA+"/g1")
	h.seedScope(scopeB, scopeB+"/g1")
	h.write(scopeA, scopeA+"/g1", keyedFinding("CVE-2026-2001", "pkg:npm/x", 5), keyedFinding("CVE-2026-2002", "pkg:npm/y", 5))
	h.write(scopeB, scopeB+"/g1", keyedFinding("CVE-2026-2001", "pkg:npm/x", 8))
	h.keyed()
	h.assertParity("seed")
	return scopeA, scopeB
}

// keyedStartBlockedTick holds a SHARE lock on the winners table from another
// connection so a keyed tick blocks after it consumed the log and built its
// temp tables, and returns once the tick is blocked.
func keyedStartBlockedTick(h *keyedHarness) (release func(), done <-chan error, cancel context.CancelFunc) {
	h.t.Helper()
	holder, err := h.db.Conn(h.ctx)
	if err != nil {
		h.t.Fatalf("holder connection: %v", err)
	}
	if _, err := holder.ExecContext(h.ctx, `BEGIN`); err != nil {
		h.t.Fatalf("holder begin: %v", err)
	}
	if _, err := holder.ExecContext(h.ctx, `LOCK TABLE supply_chain_impact_canonical_winners IN SHARE MODE`); err != nil {
		h.t.Fatalf("holder lock: %v", err)
	}
	tickCtx, cancelTick := context.WithCancel(h.ctx)
	errs := make(chan error, 1)
	go func() {
		_, err := h.store.ResweepWinnersKeyed(tickCtx, h.tick())
		errs <- err
	}()
	deadline := time.Now().Add(30 * time.Second)
	for {
		var blocked int
		if err := h.db.QueryRowContext(h.ctx, `
SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database() AND wait_event_type = 'Lock' AND query LIKE '%winners_now AS%'`).Scan(&blocked); err != nil {
			h.t.Fatalf("poll blocked tick: %v", err)
		}
		if blocked == 1 {
			break
		}
		if time.Now().After(deadline) {
			h.t.Fatal("the keyed tick never blocked on the winners table")
		}
		time.Sleep(20 * time.Millisecond)
	}
	release = func() {
		_, _ = holder.ExecContext(context.Background(), `ROLLBACK`)
		_ = holder.Close()
	}
	return release, errs, cancelTick
}

// TestSupplyChainImpactWinnersResweepKeyedConsumeKeepsLateArrivalsLive is the lost-key
// mutant of the #9805 consume protocol. A change committed after the tick
// consumed the log must stay in the log for the next tick. A consume that
// deletes everything after the recompute (the round 2 mutant) loses it.
func TestSupplyChainImpactWinnersResweepKeyedConsumeKeepsLateArrivalsLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 keyed consume protocol proof")
	scopeA, _ := keyedSeedSettled(h)

	h.write(scopeA, scopeA+"/g1", keyedFinding("CVE-2026-2001", "pkg:npm/x", 50), keyedFinding("CVE-2026-2002", "pkg:npm/y", 5))
	if depth := h.dirtyDepth(); depth == 0 {
		t.Fatal("the writer captured nothing for a changed finding")
	}
	release, done, _ := keyedStartBlockedTick(h)
	// A late change commits while the tick is blocked after its consume.
	h.write(scopeA, scopeA+"/g1", keyedFinding("CVE-2026-2001", "pkg:npm/x", 50), keyedFinding("CVE-2026-2002", "pkg:npm/y", 60))
	release()
	if err := <-done; err != nil {
		t.Fatalf("blocked keyed tick: %v", err)
	}
	if depth := h.dirtyDepth(); depth == 0 {
		t.Fatal("the late arrival was consumed with the batch that did not include it: the consume deletes more than it processed")
	}
	h.keyed()
	h.assertParity("after the late arrival")
}

// TestSupplyChainImpactWinnersResweepKeyedRollbackReturnsTheKeysLive proves a keyed
// tick that does not commit leaves the dirty log untouched.
func TestSupplyChainImpactWinnersResweepKeyedRollbackReturnsTheKeysLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 keyed rollback proof")
	scopeA, _ := keyedSeedSettled(h)

	h.write(scopeA, scopeA+"/g1", keyedFinding("CVE-2026-2001", "pkg:npm/x", 50), keyedFinding("CVE-2026-2002", "pkg:npm/y", 5))
	before := h.dirtyDepth()
	release, done, cancel := keyedStartBlockedTick(h)
	cancel()
	if err := <-done; err == nil {
		t.Fatal("a cancelled keyed tick returned no error")
	}
	release()
	if after := h.dirtyDepth(); after != before {
		t.Fatalf("dirty log depth after a rolled-back tick = %d, want %d", after, before)
	}
	h.keyed()
	h.assertParity("after the rolled-back tick")
}

// TestSupplyChainImpactWinnersResweepKeyedCapFallsBackToFullLive proves the cap: more
// distinct identifiers than the limit runs the shipped full rebuild in the
// same transaction and still consumes the log.
func TestSupplyChainImpactWinnersResweepKeyedCapFallsBackToFullLive(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*keyedHarness)
	}{
		{"identifier cap", func(h *keyedHarness) { h.store.MaxIdentifiers = 1 }},
		{"recomputed-key fraction cap", func(h *keyedHarness) { h.store.MaxKeyFraction = 0.01 }},
		// The default fraction is one half; two identifiers against three stored
		// winners exceed it.
		{"default fraction cap", func(h *keyedHarness) { h.store.MaxKeyFraction = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newKeyedHarness(t, "#9805 keyed cap proof")
			scopeA, _ := keyedSeedSettled(h)
			tc.mutate(h)
			h.write(scopeA, scopeA+"/g1", keyedFinding("CVE-2026-2001", "pkg:npm/x", 50), keyedFinding("CVE-2026-2002", "pkg:npm/y", 7))
			got, err := h.store.ResweepWinnersKeyed(h.ctx, h.tick())
			if err != nil || got.Skipped {
				t.Fatalf("capped tick = %+v, %v", got, err)
			}
			if got.Path != telemetry.SupplyChainImpactWinnersPathFull {
				t.Fatalf("capped tick path = %q, want %q", got.Path, telemetry.SupplyChainImpactWinnersPathFull)
			}
			if depth := h.dirtyDepth(); depth != 0 {
				t.Fatalf("dirty log depth after the capped tick = %d, want 0 (the full rebuild covers what was consumed)", depth)
			}
			h.assertParity("capped tick")
		})
	}
}

// TestSupplyChainImpactWinnersResweepDirtyLogStatsLive proves the depth and age
// readings the gauges are fed from.
func TestSupplyChainImpactWinnersResweepDirtyLogStatsLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 dirty log stats proof")
	empty, err := h.store.DirtyLogStats(h.ctx)
	if err != nil || empty != (supplychaincore.SupplyChainImpactWinnersDirtyStats{}) {
		t.Fatalf("empty log stats = %+v, %v; want zeroes", empty, err)
	}
	h.exec(`
INSERT INTO supply_chain_impact_winners_dirty_log (kind, source, package_id, repository_id, subject_digest, c0, advisory_id, captured_at)
VALUES ('fact', 'fact_upsert', 'p', 'r', '', 'c', '', now() - interval '90 seconds'),
       ('fact', 'fact_upsert', 'p2', 'r', '', 'c', '', now())`)
	got, err := h.store.DirtyLogStats(h.ctx)
	if err != nil {
		t.Fatalf("DirtyLogStats() error = %v", err)
	}
	if got.Depth != 2 || got.OldestAge < 89*time.Second || got.OldestAge > 120*time.Second {
		t.Fatalf("stats = %+v, want depth 2 and an oldest age near 90s", got)
	}
}
