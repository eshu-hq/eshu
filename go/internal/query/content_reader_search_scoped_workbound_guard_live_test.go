// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"testing"
)

// TestScopedSearchWorkBoundGuardLive is the #7930 work-bound guard, kept as a
// RED-by-design kill record: the ten shipped cells (S1-S7 single-scope via
// searchFileContentPage, M1-M3 multi-scope via searchFileContentInRepos) in
// both plan regimes (custom default PREPARE/EXECUTE and force_generic_plan),
// each under warm-cache EXPLAIN (ANALYZE, BUFFERS) caps of 400 shared-hit
// buffers with zero reads, 100 exact heap blocks, and removed rows within
// max(200, 10x(limit+offset)). Only nodes with Actual Loops > 0 count;
// never-executed branches are reported, not failed. There is no wall-time cap
// at this rung. Shipped cells over the caps fail here: S3 (447 removed), S5
// (3606 buffers, 15300 removed), M2 (900 removed) in both regimes. That RED
// is the entry condition for the probes, so this file is ledger-classified
// scheduled (local-only kill record), never a blocking proof.
func TestScopedSearchWorkBoundGuardLive(t *testing.T) {
	ctx, db := openScopedWorkboundFixture(t)
	reader := NewContentReader(db)

	for _, cell := range scopedWorkboundCells() {
		// Warm the cache through the serving path before measuring it, so
		// the reads=0 bound tests steady state rather than first touch.
		warmScopedCell(t, ctx, reader, cell)
		query, args := shippedScopedQuery(cell)
		for _, regime := range []struct {
			name    string
			generic bool
		}{
			{name: "custom", generic: false},
			{name: "generic", generic: true},
		} {
			stats := explainScopedWork(t, ctx, db, query, args, regime.generic)
			checkScopedWorkBounds(t, cell.name+"/"+regime.name, regime.name, stats, cell.limit, cell.offset)
		}
	}
}

// warmScopedCell runs the cell once through the serving reader. It fails the
// test on error so a broken serving path cannot masquerade as a work-bound
// pass with zero rows.
func warmScopedCell(t *testing.T, ctx context.Context, reader *ContentReader, cell scopedCell) {
	t.Helper()
	var err error
	if cell.multi {
		_, err = reader.searchFileContentInRepos(ctx, cell.repoIDs, cell.pattern, cell.limit, cell.offset)
	} else {
		_, err = reader.searchFileContentPage(ctx, cell.repoIDs[0], cell.pattern, cell.limit, cell.offset)
	}
	if err != nil {
		t.Fatalf("warm cell %q: %v", cell.name, err)
	}
}
