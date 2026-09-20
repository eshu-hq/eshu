// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ownerLedgerBackfillKey and ownerLedgerBackfillMarkSQL mirror what
// go/internal/storage/postgres/graph_node_owner_backfill_store.go writes when
// eshu-api's CloudResource owner-ledger upgrade backfill completes.
const (
	ownerLedgerBackfillKey     = "cloud_resource_owner:v1"
	ownerLedgerBackfillMarkSQL = `INSERT INTO graph_node_owner_backfill_state (backfill_key, completed_at)
VALUES ($1, $2)
ON CONFLICT (backfill_key) DO NOTHING`
)

// MarkOwnerLedgerBackfillComplete records that the CloudResource owner-ledger
// upgrade backfill already ran, the state of any long-lived deployment.
//
// Without it, eshu-api started against the seeded graph runs that backfill at
// startup, and its first page (MATCH (n:CloudResource) ... ORDER BY n.uid
// LIMIT 500) exceeds the 10s graph read deadline on 150,000 nodes, which makes
// startup fatal ("backfill cloud resource owner ledger: ... graph query exceeded
// its deadline", issue #6797). The backfill is a one-time upgrade migration for
// graph rows written before the ledger existed; this gate models steady state,
// not an upgrade, so the seeded CloudResource nodes are not ledger-backed here.
func MarkOwnerLedgerBackfillComplete(ctx context.Context, pool *pgxpool.Pool, at time.Time) error {
	if _, err := pool.Exec(ctx, ownerLedgerBackfillMarkSQL, ownerLedgerBackfillKey, at.UTC()); err != nil {
		return fmt.Errorf("record owner ledger backfill marker: %w", err)
	}
	return nil
}
