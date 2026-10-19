// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossrepo

import (
	"context"
	"fmt"
	"log/slog"

	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// ResolvedUpsertResult reports what one UpsertResolved call wrote and the
// repository scope it covered. Inserted counts the rows the ON CONFLICT DO
// NOTHING insert added (conflicted rows report RowsAffected 0). RepoIDs
// holds the distinct non-empty source/target repository IDs of every row in
// the upsert set, in first-seen order, inserted or not: the
// completion-driven deployable_unit_correlation wake (#8294) runs over this
// scope on every pass, including a retry that appends nothing, and its
// fingerprint-mismatch predicate — not Inserted — decides what reopens, so
// a wake lost to a crash or a failed first pass still converges on retry.
type ResolvedUpsertResult struct {
	Inserted int64
	RepoIDs  []string
}

// CompletionWakeResult reports what one completion wake transaction did:
// Reopened succeeded items returned to pending, and ReplayFlagged
// claimed/running items marked to replay once their ACK lands.
type CompletionWakeResult struct {
	Reopened      int64
	ReplayFlagged int64
}

// DeployableUnitCorrelationWaker reopens the
// deployable_unit_correlation items whose recorded input no longer holds
// after a resolution pass over the given repositories: succeeded items
// return to pending, claimed/running items with a recorded fingerprint are
// flagged to replay after their ACK. A claimed/running item with no recorded
// fingerprint is mid-first-execution and is left alone: there is no recorded
// input to prove stale. The wake is fingerprint-gated, so re-running it over
// the same repositories reopens nothing further.
//
// Ownership: the trigger lives in this package (the resolution flow knows
// when it appended), the queue write lives in go/internal/storage/postgres
// (it owns fact_work_items and the reopen fingerprint table; postgres already
// imports this package, and this package must never import postgres). The
// RelationshipStore implements this interface; the handler holds it as an
// optional port so hermetic resolution tests need no database.
type DeployableUnitCorrelationWaker interface {
	ReopenDeployableUnitCorrelationsForRepos(ctx context.Context, repoIDs []string) (CompletionWakeResult, error)
}

// wakeDeployableUnitCorrelations reopens the deployable_unit_correlation
// items whose recorded input no longer holds over this pass's repository
// scope (#8294). It runs on every pass that resolved rows, never gated on
// Inserted: the per-row inserts autocommit before the wake, so a pass whose
// wake failed (or that crashed between upsert and wake) retries with
// Inserted 0, and only re-running the wake lets that retry converge. The
// wake's fingerprint-mismatch predicate makes the repeat a no-op once the
// items have re-read the appended rows. It is a no-op without a waker or
// without resolved repos. A wake failure fails the pass so the item retries
// on its ordinary budget.
func (h *CrossRepoRelationshipHandler) wakeDeployableUnitCorrelations(
	ctx context.Context,
	scopeID string,
	generationID string,
	result ResolvedUpsertResult,
) error {
	if h.CorrelationWake == nil || len(result.RepoIDs) == 0 {
		return nil
	}
	woken, err := h.CorrelationWake.ReopenDeployableUnitCorrelationsForRepos(ctx, result.RepoIDs)
	if err != nil {
		return fmt.Errorf("reopen deployable_unit_correlation items for appended resolved rows: %w", err)
	}
	slog.InfoContext(
		ctx, "cross-repo completion wake reopened deployable_unit_correlation items",
		log.ScopeID(scopeID),
		log.GenerationID(generationID),
		log.Domain("cross_repo_resolution"),
		slog.Int64("inserted_resolved_rows", result.Inserted),
		slog.Int64("reopened_items", woken.Reopened),
		slog.Int64("replay_flagged_items", woken.ReplayFlagged),
	)
	return nil
}
