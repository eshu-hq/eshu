// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Scanned-copies chunk budget (arbiter ruling Dec 6(ii)): one keyed tick must
// stop admitting identifiers once the scanned-copies budget is spent, so a
// scope-expanded backlog drains as short bounded ticks instead of one
// unbounded probe. Identifier atomicity holds throughout: no identifier's
// copies split across ticks, and the first identifier always completes.
//
// The backlog mirrors the Dec 6(i) RED vehicle at representative fan-out: 120
// kept copies per identifier with one retired copy per live copy (240 scanned
// per identifier). Copies fan out across scopes (one same-key finding per
// scope and generation: same-key findings in one generation collapse to one
// fact row), and the test budget is exactly one identifier's scan volume, so
// the bound is exact: a tick that admits past the budget fails the per-tick
// assertion below with no overshoot ambiguity.

// scannedBudgetKeptPerIdent is the representative fan-out: 120 live copies
// per identifier, one retired copy per live copy, i.e. 240 scanned.
const scannedBudgetKeptPerIdent = 120

// scannedBudgetScannedPerIdent is the uniform per-identifier scan volume the
// seeding below produces: one retired copy per live copy.
const scannedBudgetScannedPerIdent = 2 * scannedBudgetKeptPerIdent

// seedScannedBudgetBacklog writes keys identifiers of uniform fan-out: each
// identifier holds one same-key finding in 120 scopes, each scope with one
// retired generation behind its live one. It seeds the operator arm last so
// its zero-scan NULL-tuple identifier sorts newest. It returns the totals
// the drain must conserve.
func seedScannedBudgetBacklog(h *keyedHarness, keys int) (kept, scanned int64) {
	h.t.Helper()
	for j := 0; j < keys; j++ {
		cve := fmt.Sprintf("CVE-2026-BUDGET-%03d", j)
		pkg := fmt.Sprintf("pkg:npm/budget-%03d", j)
		for i := 0; i < scannedBudgetKeptPerIdent; i++ {
			scope := fmt.Sprintf("vuln-intel://budget/%03d/%03d", j, i)
			gen0, gen1 := scope+"/g0", scope+"/g1"
			h.seedScope(scope, gen0)
			h.write(scope, gen0, keyedFinding(cve, pkg, i))
			h.addGeneration(scope, gen1, "pending")
			h.activate(scope, gen0, gen1)
			h.write(scope, gen1, keyedFinding(cve, pkg, i))
		}
	}
	// The operator arm rides the differential: its payload carries no
	// package keys, so its dirty row expands to the NULL-tuple identifier
	// whose probe matches zero rows.
	h.seedOperator("scanned-budget-op-g0", "", map[string]map[string]any{
		"scanned-budget-op-1": {"action": "suppress", "cve_id": "CVE-2026-BUDGET-000"},
	})
	perKey := int64(scannedBudgetKeptPerIdent)
	return perKey * int64(keys), 2 * perKey * int64(keys)
}

// TestSupplyChainImpactWinnersKeyedScannedBudgetBoundsTicksLive is the Dec
// 6(ii) RED: with more scanned copies queued than the budget allows, the
// first tick must stop at the budget with the backlog still queued, every
// tick must hold at or under the budget, the drain must take at least two
// ticks, and the drained table must equal a fresh recompute (operator arm
// included). Pre-fix the first tick eats the whole backlog in one unbounded
// probe: not Bounded, scanned past the budget, one tick total.
func TestSupplyChainImpactWinnersKeyedScannedBudgetBoundsTicksLive(t *testing.T) {
	h := newKeyedHarness(t, "Dec 6(ii) scanned-copies chunk budget")
	const keys = 2
	const budget = int64(scannedBudgetScannedPerIdent)
	wantKept, wantScanned := seedScannedBudgetBacklog(h, keys)
	if budget != 240 {
		t.Fatalf("test budget = %d, want 240 (one identifier at %d scanned each)", budget, scannedBudgetScannedPerIdent)
	}

	h.store.ChunkMaxScannedCopies = int(budget)
	first := chunkedTick(h)
	if first.Path != telemetry.SupplyChainImpactWinnersPathKeyed {
		t.Fatalf("first tick ran on path %q, want %q", first.Path, telemetry.SupplyChainImpactWinnersPathKeyed)
	}
	if !first.Bounded {
		t.Errorf("first tick over %d queued identifiers = %+v, want Bounded (scanned-copies budget %d)", keys+1, first, budget)
	}
	if first.ChunkIdentifiers != 1 {
		t.Errorf("first tick probed %d identifiers, want exactly the oldest (budget %d at %d scanned each)", first.ChunkIdentifiers, budget, scannedBudgetScannedPerIdent)
	}
	if first.ChunkScannedCopies != budget {
		t.Errorf("first tick scanned %d copies, want exactly the %d-copy budget", first.ChunkScannedCopies, budget)
	}
	if first.ChunkTruncatedBy != supplychaincore.WinnersChunkTruncatedByScannedCopies {
		t.Errorf("first tick truncated_by = %q, want %q", first.ChunkTruncatedBy, supplychaincore.WinnersChunkTruncatedByScannedCopies)
	}

	ticks := append([]supplychaincore.SupplyChainImpactWinnersResweep{first}, chunkedDrain(h)...)
	if len(ticks) < 2 {
		t.Fatalf("drain took %d ticks, want at least 2 (budget %d over %d queued scanned copies)", len(ticks), budget, wantScanned)
	}
	var gotIdents, gotKept, gotScanned int64
	for i, tick := range ticks {
		if tick.Path != telemetry.SupplyChainImpactWinnersPathKeyed {
			t.Errorf("tick %d ran on path %q, want keyed", i, tick.Path)
		}
		if tick.ChunkScannedCopies > budget {
			t.Errorf("tick %d scanned %d copies, want at or under the %d-copy budget", i, tick.ChunkScannedCopies, budget)
		}
		gotIdents += tick.ChunkIdentifiers
		gotKept += tick.ChunkKeptRows
		gotScanned += tick.ChunkScannedCopies
	}
	// The operator NULL-tuple identifier probes zero copies but still counts.
	if want := int64(keys) + 1; gotIdents != want {
		t.Errorf("drain probed %d identifiers, want %d (every identifier applied exactly once: none split, none skipped)", gotIdents, want)
	}
	if gotKept != wantKept {
		t.Errorf("drain kept %d facts, want %d", gotKept, wantKept)
	}
	if gotScanned != wantScanned {
		t.Errorf("drain scanned %d copies, want %d", gotScanned, wantScanned)
	}
	if last := ticks[len(ticks)-1]; last.Bounded {
		t.Errorf("the tick that emptied the log still reports Bounded: %+v", last)
	}
	h.assertParity("Dec 6(ii) scanned-copies budget")
}

// TestSupplyChainImpactWinnersKeyedScannedBudgetFirstIdentifierCompletesLive
// pins the hot-identifier rule under the budget: a single identifier over the
// budget still completes in one tick (never split, never stalled), and the
// tick reports Bounded while identifiers stay queued.
func TestSupplyChainImpactWinnersKeyedScannedBudgetFirstIdentifierCompletesLive(t *testing.T) {
	h := newKeyedHarness(t, "Dec 6(ii) first-identifier-completes under the scanned budget")
	const keys = 1
	wantKept, wantScanned := seedScannedBudgetBacklog(h, keys)

	// One identifier alone scans past the budget: it must still complete.
	h.store.ChunkMaxScannedCopies = scannedBudgetScannedPerIdent - 1
	first := chunkedTick(h)
	if first.Path != telemetry.SupplyChainImpactWinnersPathKeyed {
		t.Fatalf("first tick ran on path %q, want %q", first.Path, telemetry.SupplyChainImpactWinnersPathKeyed)
	}
	if first.ChunkIdentifiers != 1 {
		t.Fatalf("first tick probed %d identifiers, want exactly the first (it always completes)", first.ChunkIdentifiers)
	}
	if first.ChunkScannedCopies != scannedBudgetScannedPerIdent {
		t.Errorf("first tick scanned %d copies, want the whole first identifier (%d: never split)", first.ChunkScannedCopies, scannedBudgetScannedPerIdent)
	}
	if !first.Bounded {
		t.Errorf("first tick over a still-queued backlog = %+v, want Bounded", first)
	}

	rest := chunkedDrain(h)
	ticks := len(rest) + 1
	if ticks < 2 {
		t.Fatalf("drain took %d tick, want at least 2", ticks)
	}
	if last := rest[len(rest)-1]; last.Bounded {
		t.Errorf("the tick that emptied the log still reports Bounded: %+v", last)
	}
	var gotKept, gotScanned int64
	for _, tick := range rest {
		gotKept += tick.ChunkKeptRows
		gotScanned += tick.ChunkScannedCopies
	}
	if got := first.ChunkKeptRows + gotKept; got != wantKept {
		t.Errorf("drain kept %d facts, want %d", got, wantKept)
	}
	if got := first.ChunkScannedCopies + gotScanned; got != wantScanned {
		t.Errorf("drain scanned %d copies, want %d", got, wantScanned)
	}
	h.assertParity("Dec 6(ii) first identifier completes over budget")
}
