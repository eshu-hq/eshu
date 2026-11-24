// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"testing"
)

// TestSupplyChainImpactWriterRewritesEveryRowOfAnUnchangedFullRerunLive pins
// today's write amplification (#10972) so a change to it is deliberate. A
// genuine full rerun of a scope whose evidence did not change derives the same
// findings: the content digest of every row is unchanged and the winners dirty
// capture stages nothing (dirtyIdentifiersToCapture, core/winners_keyed.go:198).
// But the finding upsert (factwrite.BatchInsertVersionedQuery,
// reducer/factwrite/batch_insert_versioned.go:130-146) has no content-equality
// guard, so the rerun still rewrites every row: a new xmin per row. If a future
// change skips identical rows, this test fails on purpose; update it and the
// HOLDER_FANOUT_INFLIGHT.md note together.
func TestSupplyChainImpactWriterRewritesEveryRowOfAnUnchangedFullRerunLive(t *testing.T) {
	const scope = "scope:10972:rewrite"
	c := newConvergence(t, false)
	meter := c.instrumentPassModes()
	c.seedQuiesceUnrelatedScope(scope, []string{"CVE-2026-74001", "CVE-2026-74002", "CVE-2026-74003"})
	c.drainCounted(meter, quiesceDrainLimit)

	xminBefore := c.findingXmins([]string{scope})
	contentBefore := c.findingRows(scope)
	if len(xminBefore) != 3 {
		t.Fatalf("fixture: scope holds %d finding rows, want 3: %v", len(xminBefore), xminBefore)
	}
	watermark := c.stagingWatermark()

	c.rerun(scope)
	pass := c.handleCounted(meter, c.raceClaim(scope))
	if !quiesceModeIsFull(pass.mode) {
		t.Fatalf("rerun mode = %s, want a full pass", pass.mode)
	}

	if changed := changedKeys(contentBefore, c.findingRows(scope)); len(changed) > 0 {
		t.Errorf("an unchanged full rerun changed the content of %v: the evidence did not change", changed)
	}
	if staged := c.stagedFactMarks(watermark); staged != 0 {
		t.Errorf("an unchanged full rerun staged %d winners dirty fact marks, want 0 (the capture is digest-gated)", staged)
	}
	if rewritten := changedKeys(xminBefore, c.findingXmins([]string{scope})); len(rewritten) != len(xminBefore) {
		t.Errorf("an unchanged full rerun rewrote %d of %d rows (new xmin), want all %d: the finding upsert has no content-equality guard today; if that changed on purpose, update this pin and HOLDER_FANOUT_INFLIGHT.md", len(rewritten), len(xminBefore), len(xminBefore))
	}
}
