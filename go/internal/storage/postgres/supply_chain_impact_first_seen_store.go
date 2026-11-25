// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// This file implements the durable side of the #11031 finding first-seen
// stamp (supplychaincore.SupplyChainImpactFirstSeenStore) on the production
// write transaction: one row per stable fact key in
// supply_chain_impact_finding_first_seen (migration 357), claimed with
// INSERT ... ON CONFLICT DO NOTHING and resolved with a SELECT, both inside
// the write transaction after fencing admission. A rejected stale pass never
// reaches the claim; concurrent first claims of one key collapse on the
// primary key so exactly one row survives and every claimant resolves to it.
// The claim runs before the fact-row upsert in the same transaction, so the
// payload provenance the upsert persists carries the resolved stamp.
//
// The claim offers a pre-stamped payload's own instant (cassette replay)
// resolved against the write time in Go, never trusting a future offer: a
// replay keeps its original first-seen while clock skew clamps to now.

// claimSupplyChainImpactFirstSeenQuery inserts one stamp row per claimed key
// and keeps the existing row on conflict: the first writer wins, which is
// what makes concurrent first claims idempotent without serializing passes.
// The effective instant is resolved in Go (offer clamped to now), so the
// statement carries plain values and no per-row CASE.
const claimSupplyChainImpactFirstSeenQuery = `
INSERT INTO supply_chain_impact_finding_first_seen (stable_fact_key, first_seen_at, publish_source)
SELECT unnest($1::text[]), unnest($2::timestamptz[]), unnest($3::text[])
ON CONFLICT (stable_fact_key) DO NOTHING`

// resolveSupplyChainImpactFirstSeenQuery reads back the surviving stamps for
// the claimed keys: existing rows for re-claims, the just-inserted rows for
// first writes.
const resolveSupplyChainImpactFirstSeenQuery = `
SELECT stable_fact_key, first_seen_at, publish_source
FROM supply_chain_impact_finding_first_seen
WHERE stable_fact_key = ANY($1::text[])`

// ClaimSupplyChainImpactFirstSeen claims one durable stamp per stable key
// and resolves every claim to its surviving stamp. An empty claim set issues
// no statement. A claim failure fails the pass: a finding written without
// its stamp would silently drop out of the publish-to-finding cohort.
func (t supplyChainImpactTx) ClaimSupplyChainImpactFirstSeen(
	ctx context.Context,
	claims []supplychaincore.SupplyChainImpactFirstSeenClaim,
	now time.Time,
) (map[string]supplychaincore.FirstSeenStamp, error) {
	keys := make([]string, 0, len(claims))
	ats := make([]time.Time, 0, len(claims))
	sources := make([]string, 0, len(claims))
	for _, claim := range claims {
		if claim.StableKey == "" {
			continue
		}
		keys = append(keys, claim.StableKey)
		ats = append(ats, supplychaincore.ResolveFirstSeenOffer(claim.OfferedAt, now))
		sources = append(sources, claim.PublishSource)
	}
	stamps := make(map[string]supplychaincore.FirstSeenStamp, len(keys))
	if len(keys) == 0 {
		return stamps, nil
	}
	if _, err := t.ExecContext(ctx, claimSupplyChainImpactFirstSeenQuery, keys, ats, sources); err != nil {
		return nil, fmt.Errorf("insert supply chain impact first seen: %w", err)
	}
	rows, err := t.QueryContext(ctx, resolveSupplyChainImpactFirstSeenQuery, keys)
	if err != nil {
		return nil, fmt.Errorf("resolve supply chain impact first seen: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var key string
		var at time.Time
		var source string
		if err := rows.Scan(&key, &at, &source); err != nil {
			return nil, fmt.Errorf("scan supply chain impact first seen: %w", err)
		}
		stamps[key] = supplychaincore.FirstSeenStamp{FirstSeenAt: at.UTC(), PublishSource: source}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read supply chain impact first seen: %w", err)
	}
	return stamps, nil
}

// The offer clamp is supplychaincore.ResolveFirstSeenOffer, called above:
// the production claim resolves offers exactly the way the unit-proven pure
// function does, so the two can never drift.
