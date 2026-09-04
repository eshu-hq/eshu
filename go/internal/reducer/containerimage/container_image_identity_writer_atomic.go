// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"context"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

const containerImageIdentityCutoverFenceQuery = `
INSERT INTO container_image_identity_cutovers (
    scope_id,
    generation_id,
    activated_by_work_item_id,
    activated_by_claim_epoch
)
VALUES ($1, $2, $3, $4)
ON CONFLICT (scope_id, generation_id) DO NOTHING
`

const containerImageIdentityLegacyPrelockQuery = `
SELECT fact_id
FROM fact_records
WHERE fact_id = ANY($1::text[])
  AND fact_kind = 'reducer_container_image_identity'
  AND is_tombstone = FALSE
  AND scope_id = $2
  AND generation_id = $3
  AND fencing_token <= $4
ORDER BY fact_id
FOR UPDATE NOWAIT
`

const containerImageIdentityPublishAndLegacyCleanupQuery = `WITH published AS (
` + factwrite.BatchInsertQuery + `
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

const containerImageIdentityCompletedCutoverWriteQuery = `
WITH current_claim AS MATERIALIZED (
    UPDATE fact_work_items AS work_item
    SET status = 'running',
        container_image_identity_v2_authorized_status = 'running'
    WHERE work_item.work_item_id = $21
      AND work_item.scope_id = $18
      AND work_item.generation_id = $19
      AND work_item.stage = 'reducer'
      AND work_item.domain = 'container_image_identity'
      AND work_item.status IN ('claimed', 'running')
      AND work_item.container_image_identity_claim_epoch = $22
      AND work_item.container_image_identity_v2_required
      AND EXISTS (
          SELECT 1
          FROM container_image_identity_cutovers AS cutover
          WHERE cutover.scope_id = work_item.scope_id
            AND cutover.generation_id = work_item.generation_id
      )
      AND work_item.container_image_identity_v2_authorized_status =
          work_item.status
    RETURNING 1
),
published AS (
` + factwrite.BatchInsertPrefix +
	factwrite.BatchInsertSource + `
WHERE EXISTS (SELECT 1 FROM current_claim)
` + factwrite.BatchInsertConflict + `
RETURNING 1
),
deleted AS (
    DELETE FROM fact_records AS fact
    WHERE fact.fact_id = ANY($17::text[])
      AND fact.fact_kind = 'reducer_container_image_identity'
      AND fact.is_tombstone = FALSE
      AND fact.scope_id = $18
      AND fact.generation_id = $19
      AND fact.fencing_token <= $20
      AND EXISTS (SELECT 1 FROM current_claim)
    RETURNING 1
)
SELECT COALESCE((SELECT count(*) FROM deleted), 0)
FROM current_claim
`

const containerImageIdentityCompletedCutoverPublishOnlyQuery = `
WITH current_claim AS MATERIALIZED (
    UPDATE fact_work_items AS work_item
    SET status = 'running',
        container_image_identity_v2_authorized_status = 'running'
    WHERE work_item.work_item_id = $21
      AND work_item.scope_id = $18
      AND work_item.generation_id = $19
      AND work_item.stage = 'reducer'
      AND work_item.domain = 'container_image_identity'
      AND work_item.status IN ('claimed', 'running')
      AND work_item.container_image_identity_claim_epoch = $22
      AND work_item.container_image_identity_v2_required
      AND EXISTS (
          SELECT 1
          FROM container_image_identity_cutovers AS cutover
          WHERE cutover.scope_id = work_item.scope_id
            AND cutover.generation_id = work_item.generation_id
      )
      AND work_item.container_image_identity_v2_authorized_status =
          work_item.status
    RETURNING 1
),
legacy_cleanup_input AS MATERIALIZED (
    SELECT $17::text[] AS fact_ids, $20::bigint AS fencing_token
),
published AS (
` + factwrite.BatchInsertPrefix +
	factwrite.BatchInsertSource + `
WHERE EXISTS (SELECT 1 FROM current_claim)
` + factwrite.BatchInsertConflict + `
RETURNING 1
)
SELECT 0
FROM current_claim, legacy_cleanup_input
`

const containerImageIdentityCompletedCutoverClaimLockQuery = `
WITH current_claim AS MATERIALIZED (
    UPDATE fact_work_items AS work_item
    SET status = 'running',
        container_image_identity_v2_authorized_status = 'running'
    WHERE work_item.work_item_id = $3
      AND work_item.scope_id = $1
      AND work_item.generation_id = $2
      AND work_item.stage = 'reducer'
      AND work_item.domain = 'container_image_identity'
      AND work_item.status IN ('claimed', 'running')
      AND work_item.container_image_identity_claim_epoch = $4
      AND work_item.container_image_identity_v2_required
      AND EXISTS (
          SELECT 1
          FROM container_image_identity_cutovers AS cutover
          WHERE cutover.scope_id = work_item.scope_id
            AND cutover.generation_id = work_item.generation_id
      )
      AND work_item.container_image_identity_v2_authorized_status =
          work_item.status
    RETURNING 1
)
SELECT 0
FROM current_claim
`

// ErrContainerImageIdentityClaimRejected reports that a completed-cutover
// publication no longer owns the exact active claim epoch.
var ErrContainerImageIdentityClaimRejected = errors.New(
	"container image identity claim rejected",
)

type containerImageIdentityCutoverLockBusyError struct {
	err error
}

func (e *containerImageIdentityCutoverLockBusyError) Error() string {
	return fmt.Sprintf("container image identity cutover legacy row is busy: %v", e.err)
}

func (e *containerImageIdentityCutoverLockBusyError) Unwrap() error {
	return e.err
}

func (*containerImageIdentityCutoverLockBusyError) Retryable() bool {
	return true
}

func (*containerImageIdentityCutoverLockBusyError) FailureClass() string {
	return "container_image_identity_cutover_lock_busy"
}

// execContainerImageIdentityCutoverFence serializes the format transition for
// one scope generation and commits its durable marker in the same transaction
// as the image-reference-keyed publications and legacy cleanup.
//
// This must be a separate statement before cleanup. The marker trigger locks
// the active work item and scope-generation advisory key. The caller then uses
// a NOWAIT prelock for every exact legacy row cleanup can delete, so it never
// waits on a fact row while holding the advisory lock. Under Read Committed, an
// absent legacy insert that commits before the marker remains visible to the
// later prelock and cleanup; one that reaches the legacy guard after the marker
// commits is rejected.
func execContainerImageIdentityCutoverFence(
	ctx context.Context,
	db factwrite.Execer,
	scopeID string,
	generationID string,
	workItemID string,
	claimEpoch int64,
) error {
	if _, err := db.ExecContext(
		ctx,
		containerImageIdentityCutoverFenceQuery,
		scopeID,
		generationID,
		workItemID,
		claimEpoch,
	); err != nil {
		return fmt.Errorf("fence container image identity format cutover: %w", err)
	}
	return nil
}

func execContainerImageIdentityFirstCutover(
	ctx context.Context,
	db factwrite.Execer,
	write ContainerImageIdentityWrite,
	fencingToken int64,
) error {
	if err := execContainerImageIdentityCutoverFence(
		ctx,
		db,
		write.ScopeID,
		write.GenerationID,
		write.IntentID,
		write.ClaimEpoch,
	); err != nil {
		return err
	}
	return execContainerImageIdentityLegacyPrelock(
		ctx,
		db,
		write.LegacyFactIDs,
		write.ScopeID,
		write.GenerationID,
		fencingToken,
	)
}

func execContainerImageIdentityLegacyPrelock(
	ctx context.Context,
	db factwrite.Execer,
	legacyFactIDs []string,
	scopeID string,
	generationID string,
	fencingToken int64,
) error {
	if len(legacyFactIDs) == 0 {
		return nil
	}
	if _, err := db.ExecContext(
		ctx,
		containerImageIdentityLegacyPrelockQuery,
		legacyFactIDs,
		scopeID,
		generationID,
		fencingToken,
	); err != nil {
		var sqlState interface{ SQLState() string }
		if errors.As(err, &sqlState) && sqlState.SQLState() == "55P03" {
			return &containerImageIdentityCutoverLockBusyError{err: err}
		}
		return fmt.Errorf(
			"prelock legacy container image identity facts: %w",
			err,
		)
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
	db factwrite.Execer,
	rows []factwrite.Row,
	legacyFactIDs []string,
	scopeID string,
	generationID string,
	fencingToken int64,
) (int, error) {
	rows = factwrite.DedupeRowsByFactID(rows, func(row factwrite.Row) string {
		return row.FactID
	})
	for start := 0; start < len(rows); start += factwrite.BatchSize {
		end := start + factwrite.BatchSize
		if end > len(rows) {
			end = len(rows)
		}
		if end < len(rows) {
			if err := factwrite.ExecChunk(ctx, db, rows[start:end]); err != nil {
				return 0, err
			}
			continue
		}
		args := append(
			factwrite.ChunkArgs(rows[start:end]),
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
		factwrite.ChunkArgs(nil),
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

func execContainerImageIdentityCompletedCutoverWrite(
	ctx context.Context,
	db ContainerImageIdentityClaimedExecer,
	rows []factwrite.Row,
	legacyFactIDs []string,
	skipLegacyCleanup bool,
	scopeID string,
	generationID string,
	fencingToken int64,
	workItemID string,
	claimEpoch int64,
) (int, bool, error) {
	args := append(
		factwrite.ChunkArgs(rows),
		legacyFactIDs,
		scopeID,
		generationID,
		fencingToken,
		workItemID,
		claimEpoch,
	)
	query := containerImageIdentityCompletedCutoverWriteQuery
	if skipLegacyCleanup {
		query = containerImageIdentityCompletedCutoverPublishOnlyQuery
	}
	deleted, claimValid, err := db.ExecContainerImageIdentityClaimed(
		ctx,
		query,
		args...,
	)
	if err != nil {
		return 0, false, fmt.Errorf(
			"publish completed-cutover container image identities: %w",
			err,
		)
	}
	return deleted, claimValid, nil
}

func lockContainerImageIdentityCompletedCutoverClaim(
	ctx context.Context,
	db ContainerImageIdentityClaimedExecer,
	scopeID string,
	generationID string,
	workItemID string,
	claimEpoch int64,
) (bool, error) {
	_, claimValid, err := db.ExecContainerImageIdentityClaimed(
		ctx,
		containerImageIdentityCompletedCutoverClaimLockQuery,
		scopeID,
		generationID,
		workItemID,
		claimEpoch,
	)
	if err != nil {
		return false, fmt.Errorf(
			"lock completed-cutover container image identity claim: %w",
			err,
		)
	}
	return claimValid, nil
}
