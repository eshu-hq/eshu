// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factkeys

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// DeleteMarker opens every key sidecar delete statement so a statement
// recorder can tell them apart from other retention statements.
const DeleteMarker = "-- retention: fact key delete"

// Table names the three per-fact key sidecars, in delete order.
const (
	RelationshipReferenceTable = "relationship_reference_candidate_keys"
	ManifestConsumptionTable   = "package_manifest_consumption_keys"
	RegistryIdentityTable      = "package_registry_identity_keys"
)

// Delete is one key sidecar table and its two delete statements: Generation
// deletes one generation's rows ($1 scope id, $2 generation id) and Page
// deletes one chunk page's rows ($1 scope id, $2 generation id, $3 the page's
// fact ids).
type Delete struct {
	Table      string
	Generation string
	Page       string
}

// Deletes returns the three key sidecars in the order every retention
// transaction deletes them: relationship keys, then manifest consumption
// keys, then registry identity keys. The package key scope rebuild clears
// manifest keys before registry keys, so both writers take those row locks in
// one order; the reversed order deadlocks against it. Every statement leads
// with (scope_id, generation_id), the prefix of each table's (scope_id,
// generation_id, fact_id) partition index (migrations 058, 182 and 183): the
// measured plans stay on that index at 5% and 20% generation shares, under
// stale statistics and under a forced generic plan, with buffers that track
// the generation instead of the table. A fact_id-only page delete was
// rejected: it chose a sequential scan of the package keys and a primary key
// walk whose buffers grow with the table
// (docs/internal/evidence/7420-key-sidecar-explicit-deletes.md).
func Deletes() []Delete {
	return []Delete{
		statements(RelationshipReferenceTable),
		statements(ManifestConsumptionTable),
		statements(RegistryIdentityTable),
	}
}

func statements(table string) Delete {
	return Delete{
		Table: table,
		Generation: DeleteMarker + " (generation)\nDELETE FROM " + table + "\n" +
			"WHERE scope_id = $1 AND generation_id = $2\n",
		Page: DeleteMarker + " (chunk page)\nDELETE FROM " + table + "\n" +
			"WHERE scope_id = $1 AND generation_id = $2 AND fact_id = ANY($3::text[])\n",
	}
}

// DeleteGenerations deletes the key sidecar rows of each (scopeIDs[i],
// generationIDs[i]) generation, one statement per generation and table,
// table by table in Deletes order, and sets rowsPruned[table] to the rows it
// deleted. Facts are generation-scoped (a fact id derives from scope,
// generation and stable key) and every key writer copies the fact's scope and
// generation, so this removes exactly the rows the per-fact foreign key
// cascade would. The statements are idempotent: a retried transaction
// deletes nothing twice.
func DeleteGenerations(
	ctx context.Context,
	exec db.Executor,
	scopeIDs, generationIDs []string,
	rowsPruned map[string]int64,
) error {
	if len(scopeIDs) != len(generationIDs) {
		return fmt.Errorf("delete fact keys: %d scope ids for %d generation ids", len(scopeIDs), len(generationIDs))
	}
	for _, keys := range Deletes() {
		var deleted int64
		for i := range generationIDs {
			affected, err := rowsAffected(ctx, exec, keys.Generation, scopeIDs[i], generationIDs[i])
			if err != nil {
				return fmt.Errorf("delete %s: %w", keys.Table, err)
			}
			deleted += affected
		}
		rowsPruned[keys.Table] = deleted
	}
	return nil
}

// DeletePage deletes the key sidecar rows of one over-limit chunk page before
// the page's facts are deleted, in Deletes order. The page is ordered by
// (observed_at, fact_id), not a contiguous fact_id range, so the statement
// binds the page ids as the third column of the partition index under the
// chunk's (scope_id, generation_id). It returns the deleted rows by table.
func DeletePage(
	ctx context.Context,
	exec db.Executor,
	scopeID, generationID string,
	factIDs []string,
) (map[string]int64, error) {
	deleted := make(map[string]int64, 3)
	for _, keys := range Deletes() {
		affected, err := rowsAffected(ctx, exec, keys.Page, scopeID, generationID, factIDs)
		if err != nil {
			return nil, fmt.Errorf("delete chunk page %s: %w", keys.Table, err)
		}
		deleted[keys.Table] = affected
	}
	return deleted, nil
}

func rowsAffected(ctx context.Context, exec db.Executor, query string, args ...any) (int64, error) {
	result, err := exec.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
