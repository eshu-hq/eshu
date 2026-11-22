// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// The old-writer fence (migration 336) marks the scope of every finding fact a
// writer without the maintainer touches. The backfill drains one dirty scope per
// step: it re-reads every generation of the scope that holds finding facts, chunk
// by chunk under each pair's conflict-domain lock, and deletes the mark only if
// no writer re-marked it meanwhile, so a mark that lands during the drain
// survives for the next step.

// supplyChainImpactSupportsDirtyScopeSQL reads the oldest dirty scope.
const supplyChainImpactSupportsDirtyScopeSQL = `
SELECT scope_id, dirty_version
FROM supply_chain_impact_finding_supports_dirty
ORDER BY marked_at, scope_id
LIMIT 1`

// supplyChainImpactSupportsDirtyGenerationsSQL lists every generation of a scope
// that holds a finding fact, live or tombstoned, so a generation with only
// tombstones still has its orphaned supports removed.
const supplyChainImpactSupportsDirtyGenerationsSQL = `
SELECT DISTINCT generation_id
FROM fact_records
WHERE scope_id = $1 AND fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'
ORDER BY generation_id`

// supplyChainImpactSupportsDirtyClearSQL clears a drained mark unless a writer
// marked the scope again after the drain read it.
const supplyChainImpactSupportsDirtyClearSQL = `
DELETE FROM supply_chain_impact_finding_supports_dirty
WHERE scope_id = $1 AND dirty_version = $2`

// supplyChainImpactSupportsDirtyCountSQL counts dirty scopes, capped so the read
// stays bounded when an old writer marks many.
const supplyChainImpactSupportsDirtyCountSQL = `
SELECT count(*) FROM (
    SELECT 1 FROM supply_chain_impact_finding_supports_dirty LIMIT 1000
) AS dirty`

// dirtyScopes returns the fence's depth, capped at 1000.
func (b SupplyChainImpactSupportsBackfill) dirtyScopes(ctx context.Context) (int64, error) {
	rows, err := b.DB.QueryContext(ctx, supplyChainImpactSupportsDirtyCountSQL)
	if err != nil {
		return 0, fmt.Errorf("count dirty supply chain impact supports scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var n int64
	if rows.Next() {
		if err := rows.Scan(&n); err != nil {
			return 0, fmt.Errorf("scan dirty supply chain impact supports scopes: %w", err)
		}
	}
	return n, rows.Err()
}

// drainOneDirtyScope drains the oldest dirty scope and records what it wrote in
// report. It returns false when no scope is dirty.
func (b SupplyChainImpactSupportsBackfill) drainOneDirtyScope(
	ctx context.Context,
	beginner db.Beginner,
	chunk int,
	report *SupplyChainImpactSupportsBackfillReport,
) (bool, error) {
	scope, version, found, err := b.oldestDirtyScope(ctx)
	if err != nil || !found {
		return false, err
	}
	generations, err := b.dirtyGenerations(ctx, scope)
	if err != nil {
		return false, err
	}
	for _, generation := range generations {
		after, entering := "", true
		for {
			written, unchanged, removed, last, err := b.writeChunk(ctx, beginner, chunkRequest{
				scope: scope, generation: generation, after: after, limit: chunk, cleanOrphans: entering, skipCursor: true,
			})
			report.Written += written
			report.Unchanged += unchanged
			report.Removed += removed
			if err != nil {
				return false, err
			}
			if last == "" {
				break
			}
			after, entering = last, false
		}
	}
	if _, err := b.DB.ExecContext(ctx, supplyChainImpactSupportsDirtyClearSQL, scope, version); err != nil {
		return false, fmt.Errorf("clear drained supply chain impact supports scope: %w", err)
	}
	report.Drained++
	return true, nil
}

func (b SupplyChainImpactSupportsBackfill) oldestDirtyScope(ctx context.Context) (scope string, version int64, found bool, err error) {
	rows, err := b.DB.QueryContext(ctx, supplyChainImpactSupportsDirtyScopeSQL)
	if err != nil {
		return "", 0, false, fmt.Errorf("read the oldest dirty supply chain impact supports scope: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return "", 0, false, rows.Err()
	}
	if err := rows.Scan(&scope, &version); err != nil {
		return "", 0, false, fmt.Errorf("scan the oldest dirty supply chain impact supports scope: %w", err)
	}
	return scope, version, true, nil
}

func (b SupplyChainImpactSupportsBackfill) dirtyGenerations(ctx context.Context, scope string) ([]string, error) {
	rows, err := b.DB.QueryContext(ctx, supplyChainImpactSupportsDirtyGenerationsSQL, scope)
	if err != nil {
		return nil, fmt.Errorf("list generations of a dirty supply chain impact supports scope: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var generations []string
	for rows.Next() {
		var generation string
		if err := rows.Scan(&generation); err != nil {
			return nil, fmt.Errorf("scan generation of a dirty supply chain impact supports scope: %w", err)
		}
		generations = append(generations, generation)
	}
	return generations, rows.Err()
}
