// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

// deferredBackfillPartitionMemoSchemaSQL creates the memo table the deferred
// relationship backfill's partition-skip gate (issue #3624 Track 1 / B') reads
// and writes. It is a dedicated table rather than an extra column on
// graph_projection_phase_state (issue #3624 design note): the memo's identity
// (scope_id, generation_id) is coarser than the phase table's six-column primary
// key, and overloading that PK would either duplicate rows per
// (acceptance_unit_id, source_run_id, keyspace, phase) or force a fragile
// application-side "pick one row" convention. A dedicated table keeps the memo's
// own lifecycle (ON DELETE CASCADE from ingestion_scopes and scope_generations,
// exactly like graph_projection_phase_state) independent of the phase table's.
const deferredBackfillPartitionMemoSchemaSQL = `
CREATE TABLE IF NOT EXISTS deferred_backfill_partition_memo (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    catalog_fingerprint TEXT NOT NULL,
    committed_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id)
);
CREATE INDEX IF NOT EXISTS deferred_backfill_partition_memo_committed_idx
    ON deferred_backfill_partition_memo (committed_at DESC);
`

const upsertDeferredBackfillPartitionMemoBatchPrefix = `
INSERT INTO deferred_backfill_partition_memo (
    scope_id, generation_id, catalog_fingerprint, committed_at
) VALUES `

const upsertDeferredBackfillPartitionMemoBatchSuffix = `
ON CONFLICT (scope_id, generation_id) DO UPDATE
SET catalog_fingerprint = EXCLUDED.catalog_fingerprint,
    committed_at = EXCLUDED.committed_at
`

const deferredBackfillPartitionMemoColumnsPerRow = 4

// deferredBackfillPartitionMemoBatchSize bounds one memo upsert statement's row
// count, matching the batching convention graphProjectionPhaseStateBatchSize
// uses for the sibling phase-state table.
const deferredBackfillPartitionMemoBatchSize = 250

// lookupDeferredBackfillPartitionMemosQuery batch-resolves every requested
// (scope_id, generation_id) partition's memoized catalog fingerprint in ONE
// query via unnest-based row construction, never an N+1 per-partition probe
// (the fan-out this pass already runs can cover hundreds of partitions).
const lookupDeferredBackfillPartitionMemosQuery = `
SELECT memo.scope_id, memo.generation_id, memo.catalog_fingerprint
FROM deferred_backfill_partition_memo AS memo
JOIN (
    SELECT * FROM unnest($1::text[], $2::text[]) AS requested(scope_id, generation_id)
) AS requested
  ON requested.scope_id = memo.scope_id
 AND requested.generation_id = memo.generation_id
`

// DeferredBackfillPartitionMemoSchemaSQL returns the DDL for the partition memo
// gate's durable store.
func DeferredBackfillPartitionMemoSchemaSQL() string {
	return deferredBackfillPartitionMemoSchemaSQL
}

// deferredBackfillPartitionMemoStore persists and looks up the deferred
// backfill's per-partition (scope_id, generation_id) -> catalog_fingerprint
// memo (issue #3624 Track 1 / B').
type deferredBackfillPartitionMemoStore struct {
	db ExecQueryer
}

// newDeferredBackfillPartitionMemoStore constructs a store backed by the
// provided database handle or transaction.
func newDeferredBackfillPartitionMemoStore(db ExecQueryer) *deferredBackfillPartitionMemoStore {
	return &deferredBackfillPartitionMemoStore{db: db}
}

// EnsureSchema applies the partition memo DDL.
func (s *deferredBackfillPartitionMemoStore) EnsureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, deferredBackfillPartitionMemoSchemaSQL)
	return err
}

// deferredBackfillPartitionMemoRow is one durable partition-memo record: the
// catalog shape (fingerprint) that was in effect when (ScopeID, GenerationID)'s
// backward evidence last committed.
type deferredBackfillPartitionMemoRow struct {
	ScopeID            string
	GenerationID       string
	CatalogFingerprint string
	CommittedAt        time.Time
}

// Upsert writes partition memo rows in bounded batches, matching the batching
// convention the sibling graph_projection_phase_state store uses.
func (s *deferredBackfillPartitionMemoStore) Upsert(
	ctx context.Context,
	rows []deferredBackfillPartitionMemoRow,
) error {
	if len(rows) == 0 {
		return nil
	}

	for i := 0; i < len(rows); i += deferredBackfillPartitionMemoBatchSize {
		end := i + deferredBackfillPartitionMemoBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := upsertDeferredBackfillPartitionMemoBatch(ctx, s.db, rows[i:end]); err != nil {
			return err
		}
	}

	return nil
}

// LookupMany batch-resolves the memoized catalog fingerprint for every
// requested partition in ONE query. The returned map omits any partition with
// no memo row (bootstrap / never-committed partitions), so callers must treat
// a missing key as "not memoized" rather than as an empty-string fingerprint.
func (s *deferredBackfillPartitionMemoStore) LookupMany(
	ctx context.Context,
	partitions []scopeGenerationPartition,
) (map[scopeGenerationPartition]string, error) {
	if len(partitions) == 0 {
		return nil, nil
	}

	scopeIDs := make([]string, 0, len(partitions))
	generationIDs := make([]string, 0, len(partitions))
	for _, partition := range partitions {
		scopeIDs = append(scopeIDs, partition.ScopeID)
		generationIDs = append(generationIDs, partition.GenerationID)
	}

	rows, err := s.db.QueryContext(
		ctx,
		lookupDeferredBackfillPartitionMemosQuery,
		pq.StringArray(scopeIDs),
		pq.StringArray(generationIDs),
	)
	if err != nil {
		return nil, fmt.Errorf("lookup deferred backfill partition memos: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[scopeGenerationPartition]string, len(partitions))
	for rows.Next() {
		var partition scopeGenerationPartition
		var fingerprint string
		if err := rows.Scan(&partition.ScopeID, &partition.GenerationID, &fingerprint); err != nil {
			return nil, fmt.Errorf("scan deferred backfill partition memo: %w", err)
		}
		result[partition] = fingerprint
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deferred backfill partition memos: %w", err)
	}

	return result, nil
}

func upsertDeferredBackfillPartitionMemoBatch(
	ctx context.Context,
	db ExecQueryer,
	batch []deferredBackfillPartitionMemoRow,
) error {
	if len(batch) == 0 {
		return nil
	}

	args := make([]any, 0, len(batch)*deferredBackfillPartitionMemoColumnsPerRow)
	var values strings.Builder

	for i, row := range batch {
		if strings.TrimSpace(row.ScopeID) == "" {
			return fmt.Errorf("deferred backfill partition memo row %d: scope_id must not be blank", i)
		}
		if strings.TrimSpace(row.GenerationID) == "" {
			return fmt.Errorf("deferred backfill partition memo row %d: generation_id must not be blank", i)
		}

		committedAt := row.CommittedAt.UTC()
		if committedAt.IsZero() {
			committedAt = time.Now().UTC()
		}

		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * deferredBackfillPartitionMemoColumnsPerRow
		fmt.Fprintf(&values, "($%d, $%d, $%d, $%d)", offset+1, offset+2, offset+3, offset+4)
		args = append(
			args,
			strings.TrimSpace(row.ScopeID),
			strings.TrimSpace(row.GenerationID),
			strings.TrimSpace(row.CatalogFingerprint),
			committedAt,
		)
	}

	query := upsertDeferredBackfillPartitionMemoBatchPrefix + values.String() + upsertDeferredBackfillPartitionMemoBatchSuffix
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert deferred backfill partition memo batch (%d rows): %w", len(batch), err)
	}
	return nil
}
