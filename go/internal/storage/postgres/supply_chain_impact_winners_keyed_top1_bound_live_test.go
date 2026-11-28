// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestSupplyChainImpactWinnersResweepTop1ScannedBudgetBoundsTicksLive pins
// that the redefined scanned-copies budget (ruling decision 5, #11120) still
// stops a TOP-1 tick: with more index tuples queued than the budget allows,
// the first tick must stop at the budget with the backlog still queued,
// every tick must hold at or under the budget, the drain must take at least
// two ticks, and the drained table must equal a fresh recompute. The counted
// unit is pre-filter index tuples consumed (no heap fetch per copy); the
// bound value, the metering loop, and the stop behavior are unchanged from
// the Dec 6(ii) budget, so this test seeds the same uniform fan-out the
// legacy budget test pins (240 scanned per identifier) and expects the same
// stop points — plus the TOP-1 probe variant on every tick.
func TestSupplyChainImpactWinnersResweepTop1ScannedBudgetBoundsTicksLive(t *testing.T) {
	h := newKeyedHarness(t, "#11120 TOP-1 redefined scanned-copies budget")
	const keys = 2
	const budget = int64(scannedBudgetScannedPerIdent)
	if budget != 240 {
		t.Fatalf("test budget = %d, want 240 (one identifier at %d scanned each)", budget, scannedBudgetScannedPerIdent)
	}
	wantKept, wantScanned := seedScannedBudgetBacklog(h, keys)
	completeKeyPresenceFillMarkerForTop1(t, h)

	h.store.ChunkMaxScannedCopies = int(budget)
	first := chunkedTick(h)
	if first.Path != telemetry.SupplyChainImpactWinnersPathKeyed {
		t.Fatalf("first tick ran on path %q, want %q", first.Path, telemetry.SupplyChainImpactWinnersPathKeyed)
	}
	if first.ProbeVariant != supplychaincore.WinnersProbeVariantTop1 {
		t.Fatalf("first tick probe variant = %q, want %q (marker complete)", first.ProbeVariant, supplychaincore.WinnersProbeVariantTop1)
	}
	if !first.Bounded {
		t.Errorf("first tick over %d queued identifiers = %+v, want Bounded (scanned-copies budget %d)", keys+1, first, budget)
	}
	if first.ChunkIdentifiers != 1 {
		t.Errorf("first tick probed %d identifiers, want exactly the oldest (budget %d at %d scanned each)", first.ChunkIdentifiers, budget, scannedBudgetScannedPerIdent)
	}
	if first.ChunkScannedCopies != budget {
		t.Errorf("first tick scanned %d index tuples, want exactly the %d-copy budget", first.ChunkScannedCopies, budget)
	}
	if first.ChunkTruncatedBy != supplychaincore.WinnersChunkTruncatedByScannedCopies {
		t.Errorf("first tick truncated_by = %q, want %q", first.ChunkTruncatedBy, supplychaincore.WinnersChunkTruncatedByScannedCopies)
	}

	ticks := append([]supplychaincore.SupplyChainImpactWinnersResweep{first}, chunkedDrain(h)...)
	if len(ticks) < 2 {
		t.Fatalf("drain took %d ticks, want at least 2 (budget %d over %d queued scanned copies)", len(ticks), budget, wantScanned)
	}
	var gotKept, gotScanned int64
	for i, tick := range ticks {
		if tick.Path != telemetry.SupplyChainImpactWinnersPathKeyed {
			t.Errorf("tick %d ran on path %q, want keyed", i, tick.Path)
		}
		if tick.ProbeVariant != supplychaincore.WinnersProbeVariantTop1 {
			t.Errorf("tick %d probe variant = %q, want %q", i, tick.ProbeVariant, supplychaincore.WinnersProbeVariantTop1)
		}
		if tick.ChunkScannedCopies > budget {
			t.Errorf("tick %d scanned %d index tuples, want at or under the %d-copy budget", i, tick.ChunkScannedCopies, budget)
		}
		gotKept += tick.ChunkKeptRows
		gotScanned += tick.ChunkScannedCopies
	}
	if gotKept != wantKept {
		t.Errorf("drain kept %d facts, want %d (the bound must not drop or duplicate kept facts)", gotKept, wantKept)
	}
	if gotScanned != wantScanned {
		t.Errorf("drain scanned %d copies, want %d (the budget must conserve the probe volume)", gotScanned, wantScanned)
	}
	h.assertParity("TOP-1 budget drain over uniform fan-out")
}
