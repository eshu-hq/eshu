// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// More D3 guard cases (arbiter D3 / D2 ruling): cross-scope evidence that
// arrives after the holder's last full pass, a closure over its cap, and a
// full-pass marker D2 calls due. Each holds on main and pins what any
// narrowing of the rerun must keep.

// TestScopedReachDifferentialLinkAfterTheFullPassIsNeverInertLive is case 7.
// The holder's full pass records it closed. Then a linker advisory lands in
// another scope naming an own package and the stamped one, and the stamp
// arrives. The rerun must not end inert, and a following full pass must change
// nothing: the stamp-side closure, not the stored bit, decides.
func TestScopedReachDifferentialLinkAfterTheFullPassIsNeverInertLive(t *testing.T) {
	const holder, linker, consumer = "scope:10972:d3:late-link", "scope:10972:d3:late-linker", "scope:10972:d3:late-consumer"
	c, meter := newReach(t, func(c *convergence) {
		c.seedReachScope(holder, append(c.reachAdvisory(holder, "CVE-2026-73070", "CVE-2026-73070", "d3-late-own"), c.reachConsumption(holder, "d3-late-own"))...)
		c.seedReachScope(consumer, c.reachConsumption(consumer, "d3-late-far"))
	})
	// After the holder's full pass the link lands in a new scope with no
	// pending pass, so nothing fans out to the holder (it holds no row for it).
	fanOutSeedScope(t, c.ctx, c.db, linker, "g1")
	c.upsert(c.reachAdvisory(linker, "CVE-2026-73072", "CVE-2026-73072", "d3-late-own", "d3-late-far")...)
	c.addSourceAdvisory("CVE-2026-73071", "CVE-2026-73071", "d3-late-far")
	pass := c.stampedRerun(meter, holder, "CVE-2026-73071")
	if pass.mode == "scoped_inert" {
		t.Errorf("rerun ended inert after a link landed past the holder's full pass: the closed bit alone cannot prove closure")
	}
	wantNoFullPassChange(t, c, meter, holder)
	if !c.activeFinding(holder, "CVE-2026-73071") {
		t.Fatal("fixture: a full pass of the holder derives no row for CVE-2026-73071, so the late link does not reach it and the case proves nothing")
	}
}

// TestScopedReachDifferentialClosureOverItsCapRunsFullLive is case 8. The
// stamped package is linked through a chain of advisories that never reaches
// the holder, so with room the closure would end inert. With the evidence
// budget too small to finish the closure, the rerun must run full.
func TestScopedReachDifferentialClosureOverItsCapRunsFullLive(t *testing.T) {
	const holder, chainScope = "scope:10972:d3:cap", "scope:10972:d3:cap-chain"
	c, meter := newReach(t, func(c *convergence) {
		c.seedReachScope(holder, append(c.reachAdvisory(holder, "CVE-2026-73080", "CVE-2026-73080", "d3-cap-own"), c.reachConsumption(holder, "d3-cap-own"))...)
		var links []string
		for i := 0; i < 6; i++ {
			links = append(links, fmt.Sprintf("d3-cap-link-%d", i))
		}
		chainEnvelopes := c.reachAdvisory(chainScope, "CVE-2026-73089", "CVE-2026-73089", links[0])
		for i := 1; i < len(links); i++ {
			id := fmt.Sprintf("CVE-2026-7308%d", i)
			chainEnvelopes = append(chainEnvelopes, c.reachAdvisory(chainScope, id, id, links[i-1], links[i])...)
		}
		c.seedReachScope(chainScope, chainEnvelopes...)
	})
	c.addSourceAdvisory("CVE-2026-73088", "CVE-2026-73088", "d3-cap-link-0")
	c.handler.EvidenceBudget = 2
	pass := c.stampedRerun(meter, holder, "CVE-2026-73088")
	if !quiesceModeIsFull(pass.mode) {
		t.Errorf("rerun mode = %s, want a full mode: a closure that hits its cap proves nothing and must run full", pass.mode)
	}
}

// TestScopedReachDifferentialDueMarkerRunsFullAndResetsLive is case 10. The
// holder would be inert, but its full-pass marker is due under D2 (the
// scoped-rerun count reached N). The rerun runs full and resets the marker.
func TestScopedReachDifferentialDueMarkerRunsFullAndResetsLive(t *testing.T) {
	const holder = "scope:10972:d3:due"
	c, meter := newReach(t, func(c *convergence) {
		c.seedReachScope(holder, append(c.reachAdvisory(holder, "CVE-2026-73100", "CVE-2026-73100", "d3-due-own"), c.reachConsumption(holder, "d3-due-own"))...)
	})
	id := fanOutItemID(holder, reachGen(holder))
	if _, err := c.db.ExecContext(c.ctx, `UPDATE fact_work_items
SET payload = jsonb_set(payload, ARRAY[$2::text, $3::text], to_jsonb(16)) WHERE work_item_id = $1`,
		id, supplychaincore.PayloadKeyFullPassMarker, supplychaincore.FullPassMarkerScopedSinceFullKey); err != nil {
		t.Fatal(err)
	}
	if marker, _ := fanOutStoredPayload(t, c.db, id)[supplychaincore.PayloadKeyFullPassMarker].(map[string]any); marker[supplychaincore.FullPassMarkerScopedSinceFullKey] != float64(16) {
		t.Fatalf("the holder's marker is %v, want scoped_since_full 16: the setup full pass left no D2 full-pass marker to age", marker)
	}
	c.addSourceAdvisory("CVE-2026-73001", "CVE-2026-73001", "d3-elsewhere")
	pass := c.stampedRerun(meter, holder, "CVE-2026-73001")
	if !quiesceModeIsFull(pass.mode) {
		t.Errorf("rerun mode = %s, want a full mode: a marker due under D2 forces the reap", pass.mode)
	}
	marker, _ := fanOutStoredPayload(t, c.db, id)[supplychaincore.PayloadKeyFullPassMarker].(map[string]any)
	if marker[supplychaincore.FullPassMarkerScopedSinceFullKey] != float64(0) {
		t.Errorf("marker after the due rerun = %v, want scoped_since_full 0: a full pass resets it", marker)
	}
}
