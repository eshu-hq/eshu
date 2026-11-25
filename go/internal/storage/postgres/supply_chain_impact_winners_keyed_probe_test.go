// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
)

// TestKeyedTickScansIdentifierCopiesOnce is the regression test for the
// watermark stall of 2026-11-24: every keyed tick ran the five-tuple
// lateral identifier probe twice — once to build the keyed active_facts and
// again to count the pre-filter scanned copies — doubling the probe I/O on
// the drain path while the dirty log held 50k+ rows. The scanned-copies count
// must read the narrow temp table the build persisted, so the probe runs
// once per tick and the #11308 signal reports the same number it always did.
func TestKeyedTickScansIdentifierCopiesOnce(t *testing.T) {
	t.Parallel()

	tx := &phaseFakeTx{claimed: true, consumed: 3, identifier: 3, stored: 100, scanned: 187}
	store, _ := newPhaseStore(t, tx)
	res, err := store.ResweepWinnersKeyed(context.Background())
	if err != nil {
		t.Fatalf("ResweepWinnersKeyed() error = %v, want nil", err)
	}
	probeRuns := 0
	for _, q := range tx.queries {
		if strings.Contains(q, "CROSS JOIN LATERAL") {
			probeRuns++
		}
	}
	if probeRuns != 1 {
		t.Errorf("lateral identifier probe statements per tick = %d, want 1 (build persists narrow, count reads it)", probeRuns)
	}
	if res.ChunkScannedCopies != 187 {
		t.Errorf("ChunkScannedCopies = %d, want 187 pre-filter copies the probe scanned", res.ChunkScannedCopies)
	}
	if res.ChunkKeptRows != 1 {
		t.Errorf("ChunkKeptRows = %d, want 1: counting from the narrow table must not change the kept count", res.ChunkKeptRows)
	}
}
