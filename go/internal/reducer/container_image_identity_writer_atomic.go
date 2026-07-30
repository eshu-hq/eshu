// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
)

const containerImageIdentityCutoverFenceQuery = `
WITH locked AS MATERIALIZED (
    SELECT pg_advisory_xact_lock(
        hashtextextended($1 || E'\x1f' || $2, 5854)
    )
)
INSERT INTO container_image_identity_cutovers (
    scope_id,
    generation_id
)
SELECT $1, $2
FROM locked
ON CONFLICT (scope_id, generation_id) DO NOTHING
`

const containerImageIdentityPublishAndLegacyCleanupQuery = `WITH published AS (
` + reducerFactBatchInsertQuery + `
RETURNING 1
)
DELETE FROM fact_records AS fact
WHERE fact.fact_id = ANY($17::text[])
  AND fact.fact_kind = 'reducer_container_image_identity'
  AND fact.is_tombstone = FALSE
  AND fact.scope_id = $18
  AND fact.generation_id = $19
  AND fact.fencing_token <= $20
`

// execContainerImageIdentityCutoverFence serializes the format transition for
// one scope generation and commits its durable marker in the same transaction
// as the image-reference-keyed publications and legacy cleanup.
//
// This must be a separate statement before cleanup. Under Read Committed, that
// gives the later DELETE a fresh snapshot after the advisory lock is acquired,
// so it sees a legacy insert that committed immediately before the lock. The
// trigger installed by migration 088 takes the same lock and suppresses legacy
// inserts after this marker commits.
func execContainerImageIdentityCutoverFence(
	ctx context.Context,
	db workloadIdentityExecer,
	scopeID string,
	generationID string,
) error {
	if _, err := db.ExecContext(
		ctx,
		containerImageIdentityCutoverFenceQuery,
		scopeID,
		generationID,
	); err != nil {
		return fmt.Errorf("fence container image identity format cutover: %w", err)
	}
	return nil
}

// execContainerImageIdentityPublicationsAndCleanup publishes bounded chunks
// and fuses exact legacy cleanup into the last chunk. The caller passes the
// explicit transaction that already acquired the scope-generation cutover
// fence, so every publication, the cleanup, and the durable marker commit
// atomically.
func execContainerImageIdentityPublicationsAndCleanup(
	ctx context.Context,
	db workloadIdentityExecer,
	rows []reducerFactRow,
	legacyFactIDs []string,
	scopeID string,
	generationID string,
	fencingToken int64,
) (int, error) {
	rows = dedupeReducerFactRowsByFactID(rows, func(row reducerFactRow) string {
		return row.FactID
	})
	for start := 0; start < len(rows); start += reducerFactBatchSize {
		end := start + reducerFactBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		if end < len(rows) {
			if err := execReducerFactChunk(ctx, db, rows[start:end]); err != nil {
				return 0, err
			}
			continue
		}
		args := append(
			reducerFactChunkArgs(rows[start:end]),
			legacyFactIDs,
			scopeID,
			generationID,
			fencingToken,
		)
		result, err := db.ExecContext(
			ctx,
			containerImageIdentityPublishAndLegacyCleanupQuery,
			args...,
		)
		if err != nil {
			return 0, fmt.Errorf("publish container image identities and delete legacy facts: %w", err)
		}
		if result == nil {
			return 0, nil
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("count deleted legacy container image identity facts: %w", err)
		}
		return int(affected), nil
	}

	// A defensive empty-publication path still executes cleanup atomically.
	args := append(
		reducerFactChunkArgs(nil),
		legacyFactIDs,
		scopeID,
		generationID,
		fencingToken,
	)
	result, err := db.ExecContext(
		ctx,
		containerImageIdentityPublishAndLegacyCleanupQuery,
		args...,
	)
	if err != nil {
		return 0, fmt.Errorf("delete legacy container image identity facts: %w", err)
	}
	if result == nil {
		return 0, nil
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count deleted legacy container image identity facts: %w", err)
	}
	return int(affected), nil
}
