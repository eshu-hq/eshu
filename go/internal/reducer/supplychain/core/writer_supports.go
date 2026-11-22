// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import "context"

// SupplyChainImpactSupportsWrite counts what one supports upsert did: rows
// inserted, rows updated because a derived column changed, and rows left alone
// because the fact row already matched (the write-on-change guard).
type SupplyChainImpactSupportsWrite struct {
	Inserted  int64
	Updated   int64
	Unchanged int64
}

// SupplyChainImpactSupportsMaintainer is the optional surface a write
// transaction offers for keeping the per-support table current (#10880 PR-B1,
// migration 336). postgres.SupplyChainImpactBeginner's transaction implements
// it. A transaction without it maintains nothing, which is only valid in a test:
// the marker-gated backfill and the drift audit repair a missed write in
// production.
//
// Both calls run in the transaction that writes the finding facts, under the
// conflict-domain lock the writer already holds, so a support row is never
// visible without its fact and never outlives its retraction. Each derives its
// rows from fact_records, never from the in-memory findings, so the fact payload
// stays the one source of truth for the canonical key and the filter columns.
type SupplyChainImpactSupportsMaintainer interface {
	// UpsertImpactSupports writes one support row per live (non-tombstoned) fact
	// id, in fact id order, rewriting a row only when a derived column changed.
	UpsertImpactSupports(ctx context.Context, factIDs []string) (SupplyChainImpactSupportsWrite, error)
	// DeleteRetractedImpactSupports removes the supports of the (scope,
	// generation) whose fact the retraction tombstoned, and returns the rows
	// removed.
	DeleteRetractedImpactSupports(ctx context.Context, scopeID, generationID string) (int64, error)
}
