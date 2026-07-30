// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
)

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

// execContainerImageIdentityPublicationsAndCleanup publishes bounded chunks
// and fuses exact legacy cleanup into the last chunk. A one-chunk call is one
// implicit Postgres transaction; a multi-chunk caller must pass an explicit
// transaction so all publications and cleanup still commit atomically.
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
