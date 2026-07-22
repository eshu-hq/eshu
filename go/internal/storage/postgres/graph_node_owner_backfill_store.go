// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	cloudResourceOwnerBackfillKey   = "cloud_resource_owner:v1"
	graphNodeOwnerBackfillBatchSize = 500
	// GraphNodeOwnerBackfillMinimumOrderKeyPrefix is lower than every valid
	// reducer source-order key. Upgrade backfills use it so an existing graph
	// row can seed an empty ledger but can never displace a real contributor.
	GraphNodeOwnerBackfillMinimumOrderKeyPrefix = "0001-01-01T00:00:00.000000000Z|"
)

const isGraphNodeOwnerBackfillCompleteSQL = `
SELECT EXISTS(
    SELECT 1
    FROM graph_node_owner_backfill_state
    WHERE backfill_key = $1
)`

const markGraphNodeOwnerBackfillCompleteSQL = `
INSERT INTO graph_node_owner_backfill_state (backfill_key, completed_at)
VALUES ($1, $2)
ON CONFLICT (backfill_key) DO NOTHING`

// GraphNodeOwnerBackfillDB combines the query and transaction surfaces the
// upgrade backfill needs. SQLDB satisfies it for production wiring.
type GraphNodeOwnerBackfillDB interface {
	ExecQueryer
	Beginner
}

// GraphNodeOwnerBackfillStore seeds pre-ledger graph rows through the same
// sorted per-uid locks and monotonic max-upsert as reducer ownership writes.
type GraphNodeOwnerBackfillStore struct {
	db GraphNodeOwnerBackfillDB
}

// NewGraphNodeOwnerBackfillStore constructs the owner-ledger upgrade store.
func NewGraphNodeOwnerBackfillStore(db GraphNodeOwnerBackfillDB) GraphNodeOwnerBackfillStore {
	return GraphNodeOwnerBackfillStore{db: db}
}

// IsCloudResourceBackfillComplete reports whether the full existing-graph
// enumeration committed and recorded its durable completion marker.
func (s GraphNodeOwnerBackfillStore) IsCloudResourceBackfillComplete(ctx context.Context) (bool, error) {
	if s.db == nil {
		return false, fmt.Errorf("graph node owner backfill database is required")
	}
	rows, err := s.db.QueryContext(ctx, isGraphNodeOwnerBackfillCompleteSQL, cloudResourceOwnerBackfillKey)
	if err != nil {
		return false, fmt.Errorf("check graph node owner backfill completion: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, rows.Err()
	}
	var complete bool
	if err := rows.Scan(&complete); err != nil {
		return false, fmt.Errorf("scan graph node owner backfill completion: %w", err)
	}
	return complete, nil
}

// SeedExistingGraphNodeOwners inserts existing graph rows in bounded
// transactions. Every entry must carry the minimum upgrade key; accepting a
// normal reducer key here would let a read-side snapshot race overwrite a
// newer graph write without writing that winner back to the graph.
func (s GraphNodeOwnerBackfillStore) SeedExistingGraphNodeOwners(
	ctx context.Context,
	entries []GraphNodeOwnerEntry,
	updatedAt time.Time,
) error {
	if s.db == nil {
		return fmt.Errorf("graph node owner backfill database is required")
	}
	if updatedAt.IsZero() {
		return fmt.Errorf("graph node owner backfill updated_at is required")
	}
	entries = dedupeOwnerEntries(entries)
	for _, entry := range entries {
		if !strings.HasPrefix(entry.SourceOrderKey, GraphNodeOwnerBackfillMinimumOrderKeyPrefix) {
			return fmt.Errorf("graph node owner backfill entry %q does not use the minimum upgrade order key", entry.UID)
		}
	}
	ownerStore := NewGraphNodeOwnerStore()
	for start := 0; start < len(entries); start += graphNodeOwnerBackfillBatchSize {
		end := start + graphNodeOwnerBackfillBatchSize
		if end > len(entries) {
			end = len(entries)
		}
		if err := s.seedChunk(ctx, ownerStore, entries[start:end], updatedAt.UTC()); err != nil {
			return err
		}
	}
	return nil
}

func (s GraphNodeOwnerBackfillStore) seedChunk(
	ctx context.Context,
	ownerStore GraphNodeOwnerStore,
	entries []GraphNodeOwnerEntry,
	updatedAt time.Time,
) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin graph node owner backfill chunk: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := ownerStore.acquireLocks(ctx, tx, entries); err != nil {
		return err
	}
	if err := ownerStore.upsert(ctx, tx, entries, updatedAt); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit graph node owner backfill chunk: %w", err)
	}
	committed = true
	return nil
}

// MarkCloudResourceBackfillComplete records successful enumeration. The marker
// is idempotent so concurrent API and MCP startups can converge safely.
func (s GraphNodeOwnerBackfillStore) MarkCloudResourceBackfillComplete(ctx context.Context, at time.Time) error {
	if s.db == nil {
		return fmt.Errorf("graph node owner backfill database is required")
	}
	if at.IsZero() {
		return fmt.Errorf("graph node owner backfill completion time is required")
	}
	if _, err := s.db.ExecContext(ctx, markGraphNodeOwnerBackfillCompleteSQL, cloudResourceOwnerBackfillKey, at.UTC()); err != nil {
		return fmt.Errorf("mark graph node owner backfill complete: %w", err)
	}
	return nil
}
