// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// applyScopeGateOutcome applies the folded retirement marker gate's answer
// inside the commit transaction (#7766). It reports admitted: true means
// the commit proceeds; false means the commit was refused and fully wound
// down (rolled back, stream drained, WARN logged), so the caller returns
// nil on the finalized-skip path. A refused commit is not a commit
// failure.
func (s IngestionStore) applyScopeGateOutcome(
	ctx context.Context,
	tx db.Transaction,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	gateOutcome scopeGateOutcome,
	retirementID string,
	factStream <-chan facts.Envelope,
	factStreamErr func() error,
	transactionClosed *bool,
) (bool, error) {
	switch gateOutcome {
	case scopeGateRefusedRetiring:
		// An open non-complete tombstone covers the repository. Roll back
		// before draining the stream, so no fact, generation, or
		// projector work from the refused commit can land.
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return false, fmt.Errorf("rollback retiring scope commit: %w", rollbackErr)
		}
		*transactionClosed = true
		if streamErr := drainFactsAndCheckStream(factStream, factStreamErr); streamErr != nil {
			return false, fmt.Errorf("read refused retiring scope stream: %w", streamErr)
		}
		s.logRetirementRefusal(ctx, scopeValue, generation)
		return false, nil
	case scopeGateReadmitted:
		// The open tombstone is complete: this commit re-admits the
		// repository as a fresh first generation. The stamp lands in the
		// same transaction, so a rollback undoes both. Concurrent
		// re-admitting commits serialize on the row; only the winner
		// logs.
		stamped, err := stampRetirementReadmitted(ctx, tx, retirementID, s.now())
		if err != nil {
			return false, fmt.Errorf("stamp retirement readmission: %w", err)
		}
		if stamped {
			s.logRetirementReadmission(ctx, scopeValue, generation, retirementID)
		}
		return true, nil
	default:
		return true, nil
	}
}

// logRetirementRefusal emits the operator WARN for a commit the folded
// retirement marker gate refused (#7766). The refusal returns nil on the
// finalized-skip path, so this log is the signal a retiring repository
// stopped ingesting. Writer attribution (collector, webhook, backfill,
// recover, reindex) lands with the ingest-refusals counter wiring; the
// commit boundary does not know its writer.
func (s IngestionStore) logRetirementRefusal(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
) {
	if s.Logger == nil {
		return
	}
	s.Logger.WarnContext(ctx, "repository ingest refused: retiring",
		slog.String("scope_id", scopeValue.ScopeID),
		slog.String("repo_id", scopeValue.PartitionKey),
		slog.String("generation_id", generation.GenerationID),
		slog.String("outcome", "repository_retiring"),
		telemetry.PhaseAttr(telemetry.PhaseEmission),
	)
}

// logRetirementReadmission emits the operator WARN for a commit that
// re-admitted a retired repository as a fresh first generation (#7766).
// Only the commit that wins the readmitted_at stamp logs.
func (s IngestionStore) logRetirementReadmission(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	retirementID string,
) {
	if s.Logger == nil {
		return
	}
	s.Logger.WarnContext(ctx, "repository readmitted after retirement",
		slog.String("scope_id", scopeValue.ScopeID),
		slog.String("repo_id", scopeValue.PartitionKey),
		slog.String("generation_id", generation.GenerationID),
		slog.String("retirement_id", retirementID),
		slog.String("outcome", "readmitted_after_retirement"),
		telemetry.PhaseAttr(telemetry.PhaseEmission),
	)
}
