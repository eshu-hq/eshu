// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossrepo

import (
	"context"
	"fmt"
	"log/slog"

	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// ResolvedUpsertResult reports what one UpsertResolved call actually wrote.
// Inserted counts the rows the ON CONFLICT DO NOTHING insert added (conflicted
// rows report RowsAffected 0); RepoIDs holds the distinct non-empty
// source/target repository IDs of exactly those inserted rows, in first-seen
// order. The completion-driven deployable_unit_correlation wake (#8294) keys
// off this set so a re-run that appends nothing reopens nothing.
type ResolvedUpsertResult struct {
	Inserted int64
	RepoIDs  []string
}

// DeployableUnitCorrelationWaker reopens succeeded
// deployable_unit_correlation items whose recorded input could have changed
// because resolution appended rows.
//
// Ownership: the trigger lives in this package (the resolution flow knows
// when it appended), the queue write lives in go/internal/storage/postgres
// (it owns fact_work_items and the reopen fingerprint table; postgres already
// imports this package, and this package must never import postgres). The
// RelationshipStore implements this interface; the handler holds it as an
// optional port so hermetic resolution tests need no database.
type DeployableUnitCorrelationWaker interface {
	ReopenDeployableUnitCorrelationsForRepos(ctx context.Context, repoIDs []string) (int64, error)
}

// wakeDeployableUnitCorrelations reopens succeeded
// deployable_unit_correlation items whose recorded input overlaps the rows
// this pass appended (#8294). It is a no-op without a waker, without an
// insert, or without inserted repos. A wake failure fails the pass so the
// item retries on its ordinary budget; the wake is idempotent, so a retry
// that re-appends nothing reopens nothing further.
func (h *CrossRepoRelationshipHandler) wakeDeployableUnitCorrelations(
	ctx context.Context,
	scopeID string,
	generationID string,
	result ResolvedUpsertResult,
) error {
	if h.CorrelationWake == nil || result.Inserted == 0 || len(result.RepoIDs) == 0 {
		return nil
	}
	reopened, err := h.CorrelationWake.ReopenDeployableUnitCorrelationsForRepos(ctx, result.RepoIDs)
	if err != nil {
		return fmt.Errorf("reopen deployable_unit_correlation items for appended resolved rows: %w", err)
	}
	slog.InfoContext(
		ctx, "cross-repo completion wake reopened deployable_unit_correlation items",
		log.ScopeID(scopeID),
		log.GenerationID(generationID),
		log.Domain("cross_repo_resolution"),
		slog.Int64("inserted_resolved_rows", result.Inserted),
		slog.Int64("reopened_items", reopened),
	)
	return nil
}
