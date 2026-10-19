// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	lockstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/lock"
)

// fenceAttempt runs steps 3 to 8 in one transaction. It reports restart
// when a live lease or a lock timeout rolled the attempt back and the
// caller must restart at step 1 inside the deadline.
func (f RetirementFencer) fenceAttempt(
	ctx context.Context,
	opts FenceOptions,
	deadline time.Time,
	req FenceRequest,
	repoIDs []string,
) (FenceResult, bool, error) {
	tx, err := f.beginner.Begin(ctx)
	if err != nil {
		return FenceResult{}, false, fmt.Errorf("retirement fence begin: %w", err)
	}
	rolledBack := false
	rollback := func() {
		if !rolledBack {
			rolledBack = true
			_ = tx.Rollback()
		}
	}
	// Every return below funnels through this deferred rollback: helpers
	// report restart or error without rolling back themselves, and the
	// commit marks the transaction handed over.
	defer rollback()

	advisoryBudget := time.Until(deadline) - retirementFenceReserve
	if advisoryBudget <= 0 {
		return FenceResult{}, false, &FenceBlockedError{
			Reason: FenceReasonLockTimeout, RepoIDs: repoIDs,
			Detail: "no remaining deadline for the advisory wait plus the fence reserve",
		}
	}
	acquired, restart, err := f.fenceAcquire(ctx, tx, req, repoIDs, advisoryBudget)
	if err != nil || restart {
		return FenceResult{}, restart, err
	}
	marked, restart, err := f.fenceApplyMarks(ctx, tx, repoIDs, acquired)
	if err != nil || restart {
		return FenceResult{}, restart, err
	}

	records, err := f.writeMarkers(ctx, tx, req)
	if err != nil {
		if isLockTimeoutError(err) {
			return FenceResult{}, true, nil
		}
		return FenceResult{}, false, err
	}

	if err := assertRetirementFenced(ctx, tx, acquired.scopeIDs); err != nil {
		if isLockTimeoutError(err) {
			return FenceResult{}, true, nil
		}
		return FenceResult{}, false, err
	}
	if err := tx.Commit(); err != nil {
		if isLockTimeoutError(err) {
			return FenceResult{}, true, nil
		}
		return FenceResult{}, false, fmt.Errorf("retirement fence commit: %w", err)
	}
	rolledBack = true

	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.RetirementID)
	}
	result := FenceResult{
		RetirementIDs:       ids,
		RepoIDs:             repoIDs,
		ScopeIDs:            acquired.scopeIDs,
		GenerationsFenced:   marked.generations,
		ProjectorRowsMarked: marked.projectorRows,
		ReducerRowsMarked:   marked.reducerRows,
	}
	opts.Logger.InfoContext(ctx, "repository retirement fenced",
		slog.String("outcome", "fenced"),
		slog.Any("repo_ids", repoIDs),
		slog.Any("scope_ids", acquired.scopeIDs),
		slog.Int64("generations_fenced", marked.generations),
		slog.Int64("projector_rows_marked", marked.projectorRows),
		slog.Int64("reducer_rows_marked", marked.reducerRows),
	)
	return result, false, nil
}

// fenceAcquired carries the authoritative under-lock scope set plus the
// materialized step-4 pair set the marks bind.
type fenceAcquired struct {
	scopeIDs []string
	pairs    generationPairs
}

// fenceAcquire runs fence steps 3 to 6: the advisory key wait, the scope
// row locks with the marker-scope check, the under-lock budget recheck,
// and the 1s LOCK TABLE with the live-lease recheck. A lock timeout or a
// live lease reports restart; anything else is terminal for the attempt.
func (f RetirementFencer) fenceAcquire(
	ctx context.Context,
	tx db.Transaction,
	req FenceRequest,
	repoIDs []string,
	advisoryBudget time.Duration,
) (fenceAcquired, bool, error) {
	if _, err := tx.ExecContext(ctx, "SELECT set_config('lock_timeout', $1, true)", lockTimeoutSetting(advisoryBudget)); err != nil {
		return fenceAcquired{}, false, fmt.Errorf("retirement fence advisory lock timeout: %w", err)
	}
	if err := lockstore.AcquireDeferredMaintenanceRepoExclusiveLocks(ctx, tx, repoIDs); err != nil {
		if isLockTimeoutError(err) {
			return fenceAcquired{}, true, nil
		}
		return fenceAcquired{}, false, fmt.Errorf("retirement fence advisory key: %w", err)
	}

	scopeIDs, err := scanScopeIDs(ctx, tx, fenceLockScopesQuery, repoIDs)
	if err != nil {
		return fenceAcquired{}, false, err
	}
	locked := make(map[string]struct{}, len(scopeIDs))
	for _, scopeID := range scopeIDs {
		locked[scopeID] = struct{}{}
	}
	for _, retirement := range req.Retirements {
		if _, ok := locked[retirement.ScopeID]; !ok {
			return fenceAcquired{}, false, fmt.Errorf(
				"retirement fence: marker scope %q is not a locked scope of %q",
				retirement.ScopeID, retirement.RepoID)
		}
	}
	pairs, err := scanGenerationPairs(ctx, tx, fenceGenerationPairsQuery, scopeIDs)
	if err != nil {
		return fenceAcquired{}, false, err
	}
	if int64(len(pairs)) > RetirementFenceMaxGenerations {
		budgets, generations, reducerRows, err := scanUnderLockBudgets(ctx, tx, scopeIDs)
		if err != nil {
			return fenceAcquired{}, false, err
		}
		return fenceAcquired{}, false, &FenceBlockedError{
			Reason: FenceReasonRequestTooLarge, RepoIDs: repoIDs,
			Generations: generations, ReducerRows: reducerRows, PerRepo: budgets,
			Detail: "under-lock generation set exceeds the request budget",
		}
	}
	reducerRows, err := scanSingleCount(ctx, tx, fenceReducerRowCountQuery, scopeIDs)
	if err != nil {
		return fenceAcquired{}, false, err
	}
	if reducerRows > RetirementFenceMaxReducerRows {
		budgets, generations, budgetRows, err := scanUnderLockBudgets(ctx, tx, scopeIDs)
		if err != nil {
			return fenceAcquired{}, false, err
		}
		return fenceAcquired{}, false, &FenceBlockedError{
			Reason: FenceReasonRequestTooLarge, RepoIDs: repoIDs,
			Generations: generations, ReducerRows: budgetRows, PerRepo: budgets,
			Detail: "under-lock reducer row set exceeds the request budget",
		}
	}

	// The 1s LOCK TABLE wait plus retry: a waiting EXCLUSIVE blocks every
	// later ROW EXCLUSIVE requester behind it in queue order, so the wait
	// stays short and the retry restarts at step 1, never a convoy.
	if _, err := tx.ExecContext(ctx, "SELECT set_config('lock_timeout', '1s', true)"); err != nil {
		return fenceAcquired{}, false, fmt.Errorf("retirement fence lock timeout: %w", err)
	}
	if _, err := tx.ExecContext(ctx, fenceLockTableQuery); err != nil {
		if isLockTimeoutError(err) {
			return fenceAcquired{}, true, nil
		}
		return fenceAcquired{}, false, fmt.Errorf("retirement fence lock table: %w", err)
	}
	live, err := hasTxRows(ctx, tx, fenceRecheckLiveLeasesQuery, scopeIDs)
	if err != nil {
		if isLockTimeoutError(err) {
			return fenceAcquired{}, true, nil
		}
		return fenceAcquired{}, false, err
	}
	if live {
		// A lease went live after the drain: restart at step 1 inside
		// the deadline so the drain waits for it.
		return fenceAcquired{}, true, nil
	}
	return fenceAcquired{scopeIDs: scopeIDs, pairs: pairs}, false, nil
}

// fenceMarked counts the step-7 writes.
type fenceMarked struct {
	generations   int64
	projectorRows int64
	reducerRows   int64
}

// fenceApplyMarks runs fence steps 7a to 7e: supersede generations, null
// the active pointers, mark projector rows by the step-4 pairs, mark
// reducer rows by scope, and drop reindex requests. A lock timeout
// reports restart; anything else is terminal for the attempt.
func (f RetirementFencer) fenceApplyMarks(
	ctx context.Context,
	tx db.Transaction,
	repoIDs []string,
	acquired fenceAcquired,
) (fenceMarked, bool, error) {
	generations, err := execCount(ctx, tx, fenceSupersedeGenerationsQuery, acquired.scopeIDs)
	if err != nil {
		if isLockTimeoutError(err) {
			return fenceMarked{}, true, nil
		}
		return fenceMarked{}, false, fmt.Errorf("retirement fence supersede generations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, fenceNullActivePointerQuery, repoIDs); err != nil {
		if isLockTimeoutError(err) {
			return fenceMarked{}, true, nil
		}
		return fenceMarked{}, false, fmt.Errorf("retirement fence null active pointer: %w", err)
	}
	var projectorRows int64
	if len(acquired.pairs) > 0 {
		projectorRows, err = execCount(ctx, tx, fenceMarkProjectorRowsQuery, acquired.pairs.scopeIDs(), acquired.pairs.generationIDs())
		if err != nil {
			if isLockTimeoutError(err) {
				return fenceMarked{}, true, nil
			}
			return fenceMarked{}, false, fmt.Errorf("retirement fence mark projector rows: %w", err)
		}
	}
	reducerRows, err := execCount(ctx, tx, fenceMarkReducerRowsQuery, acquired.scopeIDs)
	if err != nil {
		if isLockTimeoutError(err) {
			return fenceMarked{}, true, nil
		}
		return fenceMarked{}, false, fmt.Errorf("retirement fence mark reducer rows: %w", err)
	}
	if _, err := tx.ExecContext(ctx, fenceDeleteReindexRequestsQuery, acquired.scopeIDs); err != nil {
		if isLockTimeoutError(err) {
			return fenceMarked{}, true, nil
		}
		return fenceMarked{}, false, fmt.Errorf("retirement fence delete reindex requests: %w", err)
	}
	return fenceMarked{generations: generations, projectorRows: projectorRows, reducerRows: reducerRows}, false, nil
}

// writeMarkers commits one pending/fenced tombstone row per repository
// (7g). A repeated idempotency key resumes the existing open row instead
// of opening a second retirement.
func (f RetirementFencer) writeMarkers(
	ctx context.Context,
	tx db.Transaction,
	req FenceRequest,
) ([]RetirementRecord, error) {
	records := make([]RetirementRecord, 0, len(req.Retirements))
	for _, retirement := range req.Retirements {
		record, err := insertRetirementMarker(ctx, tx, RetirementRequest{
			RetirementID:       retirement.RetirementID,
			RepoID:             retirement.RepoID,
			ScopeID:            retirement.ScopeID,
			ReasonCode:         req.ReasonCode,
			ReasonHash:         req.ReasonHash,
			ActorClass:         req.ActorClass,
			ActorIDHash:        req.ActorIDHash,
			IdempotencyKeyHash: req.IdempotencyKeyHash,
		})
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

// insertRetirementMarker opens one pending/fenced tombstone row inside the
// fence transaction. It shares the insert statement with InsertPending,
// so the fence marker and the operator tombstone can never drift apart. A
// repeated idempotency key returns the existing open row.
func insertRetirementMarker(
	ctx context.Context,
	tx db.Transaction,
	req RetirementRequest,
) (RetirementRecord, error) {
	rows, err := tx.QueryContext(ctx, retirementInsertPendingSQL,
		req.RetirementID, req.RepoID, req.ScopeID, req.ReasonCode,
		req.ReasonHash, req.ActorClass, req.ActorIDHash, req.IdempotencyKeyHash)
	if err != nil {
		if strings.Contains(err.Error(), "repository_retirements_open_repo_idx") {
			return RetirementRecord{}, fmt.Errorf("retirement insert %s: %w", req.RepoID, ErrRetirementAlreadyOpen)
		}
		return RetirementRecord{}, fmt.Errorf("retirement insert: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return RetirementRecord{}, fmt.Errorf("retirement insert: %w", err)
		}
		return RetirementRecord{}, fmt.Errorf("retirement insert: no row returned")
	}
	var rec RetirementRecord
	if err := rows.Scan(&rec.RetirementID, &rec.RepoID, &rec.ScopeID, &rec.State, &rec.Phase); err != nil {
		return RetirementRecord{}, fmt.Errorf("retirement insert scan: %w", err)
	}
	return rec, nil
}

// assertRetirementFenced is the step-8 fence assertion: after the marks,
// no live generation and no markable reducer row of the scopes may remain.
// A mismatch rolls the fence back instead of committing a half fence.
func assertRetirementFenced(ctx context.Context, tx db.Transaction, scopeIDs []string) error {
	live, err := scanSingleCount(ctx, tx, fenceAssertLiveGenerationsQuery, scopeIDs)
	if err != nil {
		return err
	}
	if live != 0 {
		return fmt.Errorf("retirement fence assertion: %d live generations remain", live)
	}
	markable, err := scanSingleCount(ctx, tx, fenceReducerRowCountQuery, scopeIDs)
	if err != nil {
		return err
	}
	if markable != 0 {
		return fmt.Errorf("retirement fence assertion: %d markable reducer rows remain", markable)
	}
	return nil
}
