// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// refuseFactIDOwnerConflict refuses a generation whose save found a fact_id
// held by another scope or generation (#11203). It follows the same channel as
// refuseDestructiveSuccessor: the transaction rolls back, so no fact row, no
// generation row, no projector work item, and no reference-key row of this
// generation lands and the scope keeps serving its prior generation; the
// commit disposition is marked refused with the closed reason
// fact_id_owner_conflict, so the collector records the outcome as refused and
// returns nil instead of failing the run; and the refusal is reported once on
// the generation outcomes counter, the fact id scope conflict counter, and one
// ERROR log.
//
// Why a refusal and not a returned error or a skip. A returned error ends the
// unclaimed collector run loop, or retries without bound when the attempt
// budget is off, for one deterministic collision. Skipping the fact and
// activating the generation would publish a graph with a fact missing that
// nothing downstream can detect. The refusal is not retried with a hold: the
// next collection tick re-collects the scope, hits the same collision, and
// refuses again with a fresh ERROR log, so a recurring log and a rising
// counter are the alarm until the producer's fact id is fixed.
//
// The caller marks its transaction closed (via transactionClosed) and returns
// the result: nil on a refusal.
func (s IngestionStore) refuseFactIDOwnerConflict(
	ctx context.Context,
	tx db.Transaction,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	prior successorPrior,
	conflict *FactIDOwnerConflictError,
	factStream <-chan facts.Envelope,
	factStreamErr func() error,
	transactionClosed *bool,
) error {
	if rollbackErr := tx.Rollback(); rollbackErr != nil {
		return fmt.Errorf("rollback fact_id owner conflict: %w", rollbackErr)
	}
	*transactionClosed = true
	// The save stopped reading at the conflicting batch; drain the rest so the
	// producer is never left blocked on a full channel.
	if streamErr := drainFactsAndCheckStream(factStream, factStreamErr); streamErr != nil {
		return fmt.Errorf("read refused fact_id owner conflict stream: %w", streamErr)
	}
	recordFactIDOwnerConflict(ctx, s.Instruments, s.Logger, scopeValue.SourceSystem, conflict)
	s.backfillStore().RecordSkippedRefresh(ctx, scopeValue, generation, prior.candidate.GenerationID,
		telemetry.RefreshSkipReasonFactIDOwnerConflict)
	collector.MarkCommitRefused(ctx, collector.CommitRefusal{
		Reason: telemetry.RefreshSkipReasonFactIDOwnerConflict,
	})
	return nil
}
