// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"time"
)

// writeContainerImageIdentityRows dispatches one write across the identity
// writer's four atomic shapes -- the completed-cutover single-round-trip
// path, the transactional completed-cutover-oversized-batch path, the
// transactional first-cutover path, and the empty (nothing to publish or
// clean up) path -- and runs the container_image_identity_write_admission CAS
// (#5874) as the first statement of whichever one this write takes. Split out
// of container_image_identity_writer.go, which otherwise passed the
// repository's 500-line file cap; see that file's WriteContainerImageIdentityDecisions
// for the caller. The completed-cutover-fast-path and empty-write branches
// are further split into their own methods below to stay under the funlen
// gate.
func (w PostgresContainerImageIdentityWriter) writeContainerImageIdentityRows(
	ctx context.Context,
	write ContainerImageIdentityWrite,
	rows []reducerFactRow,
	fencingToken int64,
	now time.Time,
) (int, error) {
	if len(rows) == 0 && len(write.LegacyFactIDs) == 0 {
		return w.writeContainerImageIdentityRowsEmpty(ctx, write, rows, fencingToken, now)
	}

	rollbackNeeded := false
	legacyRowsDeleted := 0

	cutoverComplete := false
	var err error
	if w.CutoverLookup != nil {
		cutoverComplete, err = w.CutoverLookup.ContainerImageIdentityCutoverExists(
			ctx,
			write.ScopeID,
			write.GenerationID,
		)
		if err != nil {
			return 0, fmt.Errorf(
				"read container image identity cutover: %w",
				err,
			)
		}
	}
	skipLegacyCleanup := false
	if cutoverComplete && w.LegacyCleanupLookup != nil {
		skipLegacyCleanup, err = w.LegacyCleanupLookup.ContainerImageIdentityLegacyCleanupComplete(
			ctx,
			write.ScopeID,
			write.GenerationID,
		)
		if err != nil {
			return 0, fmt.Errorf(
				"read container image identity legacy cleanup state: %w",
				err,
			)
		}
	}
	if cutoverComplete && len(rows) <= reducerFactBatchSize {
		return w.writeContainerImageIdentityRowsFastPath(
			ctx, write, rows, fencingToken, now, skipLegacyCleanup,
		)
	}
	if w.Beginner == nil {
		return 0, fmt.Errorf(
			"container image identity transaction beginner is required for v2 publication or legacy cleanup",
		)
	}
	tx, err := w.Beginner.BeginContainerImageIdentityTx(ctx)
	if err != nil {
		return 0, fmt.Errorf(
			"begin container image identity write: %w",
			err,
		)
	}
	exec := tx
	rollbackNeeded = true
	defer func() {
		if rollbackNeeded {
			_ = tx.Rollback()
		}
	}()
	// The admission CAS (#5874) MUST be the first statement in this
	// transaction, before the claim-epoch check or the pre-cutover fence
	// below: an admission-rejected pass must not mutate the claim state,
	// publish, or clean up legacy rows at all. Mirrors
	// aws_cloud_runtime_drift_writer.go's begin-before-mutate ordering.
	admitted, err := tryAdmitContainerImageIdentityWrite(
		ctx, tx, write.ScopeID, write.GenerationID, fencingToken, now,
	)
	if err != nil {
		return 0, err
	}
	if !admitted {
		return 0, containerImageIdentityWriteSupersededError{
			scopeID:      write.ScopeID,
			generationID: write.GenerationID,
		}
	}
	if cutoverComplete {
		claimedExecer, ok := tx.(ContainerImageIdentityClaimedExecer)
		if !ok {
			return 0, fmt.Errorf(
				"container image identity transaction cannot validate completed-cutover claim",
			)
		}
		claimValid, err := lockContainerImageIdentityCompletedCutoverClaim(
			ctx,
			claimedExecer,
			write.ScopeID,
			write.GenerationID,
			write.IntentID,
			write.ClaimEpoch,
		)
		if err != nil {
			return 0, err
		}
		if !claimValid {
			return 0, ErrContainerImageIdentityClaimRejected
		}
	} else if err := execContainerImageIdentityFirstCutover(
		ctx,
		exec,
		write,
		fencingToken,
	); err != nil {
		return 0, err
	}
	if cutoverComplete && skipLegacyCleanup {
		err = reducerBatchInsertFacts(ctx, exec, rows)
	} else {
		legacyRowsDeleted, err = execContainerImageIdentityPublicationsAndCleanup(
			ctx,
			exec,
			rows,
			write.LegacyFactIDs,
			write.ScopeID,
			write.GenerationID,
			fencingToken,
		)
	}
	if err != nil {
		return 0, err
	}
	if tx != nil {
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf(
				"commit container image identity write: %w",
				err,
			)
		}
		rollbackNeeded = false
	}
	return legacyRowsDeleted, nil
}

// writeContainerImageIdentityRowsFastPath runs the completed-cutover
// single-round-trip write: the container_image_identity_write_admission CAS
// (#5874) is woven into the SAME combined SQL statement as the claim-epoch
// check (see containerImageIdentityCompletedCutoverAdmissionCTE), so there is
// no separate Go-level admission call here -- unlike the transactional and
// empty-write paths, this shape has no Go BEGIN/COMMIT to run a second
// statement inside.
func (w PostgresContainerImageIdentityWriter) writeContainerImageIdentityRowsFastPath(
	ctx context.Context,
	write ContainerImageIdentityWrite,
	rows []reducerFactRow,
	fencingToken int64,
	now time.Time,
	skipLegacyCleanup bool,
) (int, error) {
	if w.ClaimedExecer == nil {
		return 0, fmt.Errorf(
			"container image identity claimed executor is required for completed cutover",
		)
	}
	legacyRowsDeleted, admitted, claimValid, err := execContainerImageIdentityCompletedCutoverWrite(
		ctx,
		w.ClaimedExecer,
		rows,
		write.LegacyFactIDs,
		skipLegacyCleanup,
		write.ScopeID,
		write.GenerationID,
		fencingToken,
		write.IntentID,
		write.ClaimEpoch,
		now,
	)
	if err != nil {
		return 0, err
	}
	if !claimValid {
		return 0, ErrContainerImageIdentityClaimRejected
	}
	// claimValid==true tells us only that this pass still owns its claim
	// epoch -- admitted is the SEPARATE signal that this pass's evidence-read
	// watermark was not superseded by a fresher one for this (scope,
	// generation). Checked AFTER claimValid because a claim-rejected pass is
	// not this worker's write to retry at all; an admission-rejected pass
	// with a still-valid claim IS retryable with a fresh token.
	if !admitted {
		return 0, containerImageIdentityWriteSupersededError{
			scopeID:      write.ScopeID,
			generationID: write.GenerationID,
		}
	}
	return legacyRowsDeleted, nil
}

// writeContainerImageIdentityRowsEmpty handles a pass with nothing to
// publish or clean up (rows and write.LegacyFactIDs both empty). The
// container_image_identity_write_admission CAS (#5874) is the ONLY statement
// this branch issues -- reducerBatchInsertFacts is a no-op for an empty
// rows slice. That is deliberate, not incidental: a pass with zero decisions
// still has to advance the admission watermark, or a fully-negative
// observation (this pass looked at every image reference and found nothing
// worth writing) could never outrank a fresher pass that also found nothing,
// and a genuinely stale later pass could stay unopposed because nothing
// durable ever recorded that a fresher pass already looked. A single
// ExecContext call is trivially atomic on its own, so no transaction is
// needed for this branch specifically.
func (w PostgresContainerImageIdentityWriter) writeContainerImageIdentityRowsEmpty(
	ctx context.Context,
	write ContainerImageIdentityWrite,
	rows []reducerFactRow,
	fencingToken int64,
	now time.Time,
) (int, error) {
	admitted, err := tryAdmitContainerImageIdentityWrite(
		ctx, w.DB, write.ScopeID, write.GenerationID, fencingToken, now,
	)
	if err != nil {
		return 0, err
	}
	if !admitted {
		return 0, containerImageIdentityWriteSupersededError{
			scopeID:      write.ScopeID,
			generationID: write.GenerationID,
		}
	}
	if err := reducerBatchInsertFacts(ctx, w.DB, rows); err != nil {
		return 0, fmt.Errorf("write container image identity fact: %w", err)
	}
	return 0, nil
}
