// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/keys"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Purge refusal sentinels. An unfenced repository and a tombstone the
// owning runner moved past pending/fenced both refuse, never purge
// partially; the sentinels let callers match the cause with errors.Is
// instead of reading error text. The operator trigger reports a purge
// error as a failed outcome retried with the same idempotency key.
// ErrRetirementNoOpenTombstone reports a purge for a repository with no
// open tombstone row: the repository was never fenced.
var ErrRetirementNoOpenTombstone = errors.New("retirement purge: no open tombstone")

// ErrRetirementTombstoneNotFenced reports a purge for a tombstone the
// owning runner moved past pending/fenced.
var ErrRetirementTombstoneNotFenced = errors.New("retirement purge: tombstone is not pending/fenced")

// PurgeMaxRepositories caps one purge request. It matches the fence's
// request cap so one operator request keeps one shape end to end.
const PurgeMaxRepositories = RetirementFenceMaxRepositories

// Purge blocked reasons. terminal_not_reached is the designed observable
// exit: a generation went live after the fence (or the fence never ran),
// and only a new fence can move it. scope_lock_busy is ordinary
// contention: the reducer retention pass holds a scope lock, and the
// operator's re-POST retries the same fenced row (PurgeRetired has no runner
// caller today; the only caller is the admin retirement trigger).
// derive_lock_busy is the same kind of exit for the two derive advisory locks
// (#10820): a derive held one past the purge's wait for it (10 s per lock and
// 30 s per request, then 100 ms attempts), nothing was written, and the re-POST
// retries.
const (
	// PurgeReasonTerminalNotReached reports generations still live.
	PurgeReasonTerminalNotReached = "terminal_not_reached"
	// PurgeReasonScopeLockBusy reports a NOWAIT scope-lock miss.
	PurgeReasonScopeLockBusy = "scope_lock_busy"
	// PurgeReasonDeriveLockBusy reports a derive advisory lock still held past
	// the purge's wait for it: 10 s per lock, 30 s across one request, then
	// 100 ms attempts (#10820). Nothing was written; retry the fenced row.
	PurgeReasonDeriveLockBusy = "derive_lock_busy"
)

// PurgeRequest is the operator's purge input: 1 to 25 repositories whose
// fenced tombstones the purge completes. One transaction per repository, so
// a blocked repository never holds back the rest.
type PurgeRequest struct {
	RepoIDs []string
}

// PurgeBlocked is one repository's designed exit: the purge wrote nothing
// for it and the fenced tombstone stays open for a later pass.
type PurgeBlocked struct {
	RepoID       string
	RetirementID string
	Reason       string
	Detail       string
	// Statuses is the terminal-gate readback ("active:1, pending:2"),
	// empty for non-gate exits.
	Statuses string
}

// PurgeResult reports the request's per-repository outcomes.
type PurgeResult struct {
	// Completed are the purged repositories, sorted.
	Completed []string
	// Blocked are the repositories the purge skipped without writing.
	Blocked []PurgeBlocked
}

// PurgeOptions tunes the purge. Zero values select the designed defaults.
type PurgeOptions struct {
	Logger *slog.Logger
	// Now is the clock the request's derive-lock budget spends against; tests
	// inject a fake one.
	Now func() time.Time
}

func (o PurgeOptions) withDefaults() PurgeOptions {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

// RetirementPurger runs the narrowed purge: for each fenced tombstone whose
// scope generations are all terminal, it deletes the scope-keyed leftover
// rows, deletes the scope rows (generations cascade), and completes the
// tombstone with the deleted-row ledger. Useful concurrency is preserved:
// repositories purge in independent transactions with sorted lock order,
// and the NOWAIT scope lock never queues behind retention.
type RetirementPurger struct {
	queryer  db.Queryer
	beginner db.Beginner
	// Instruments carries the operator retirement counter. Nil is a
	// no-op: unit fakes without telemetry never panic.
	Instruments *telemetry.Instruments
	// Options tunes logging and the clock.
	Options PurgeOptions
}

// NewRetirementPurger binds the purge to the tombstone claim transaction.
func NewRetirementPurger(queryer db.Queryer, beginner db.Beginner) RetirementPurger {
	return RetirementPurger{queryer: queryer, beginner: beginner}
}

// PurgeRetired purges every fenced repository in the request, one
// transaction each. A blocked repository lands in Result.Blocked; only a
// malformed request, a wrong-phase tombstone, or a failed transaction
// returns an error. The repositories share one budget for derive-lock waiting
// (purgeRequestDeriveLockBudget), so a request waits at most 30 s on those
// locks plus a floor attempt per lock statement once the budget is spent.
func (p RetirementPurger) PurgeRetired(ctx context.Context, req PurgeRequest) (PurgeResult, error) {
	opts := p.Options.withDefaults()
	repoIDs, err := validatedPurgeRepos(req)
	if err != nil {
		return PurgeResult{}, err
	}
	if p.beginner == nil {
		return PurgeResult{}, fmt.Errorf("retirement purge: beginner is required")
	}
	// The budget belongs to this call: the purger is shared by concurrent
	// requests, so it must not live on the purger.
	budget := newDeriveLockBudget(opts.Now)
	var result PurgeResult
	for _, repoID := range repoIDs {
		completed, blocked, err := p.purgeOne(ctx, opts, budget, repoID)
		if err != nil {
			return PurgeResult{}, err
		}
		if blocked != nil {
			result.Blocked = append(result.Blocked, *blocked)
			continue
		}
		result.Completed = append(result.Completed, completed)
	}
	return result, nil
}

// validatedPurgeRepos checks the request shape: 1 to 25 repositories, every
// id present, no duplicate repository. The returned ids are sorted so
// concurrent purgers take tombstone locks in the same order.
func validatedPurgeRepos(req PurgeRequest) ([]string, error) {
	if len(req.RepoIDs) == 0 || len(req.RepoIDs) > PurgeMaxRepositories {
		return nil, fmt.Errorf("retirement purge: want 1 to %d repositories, got %d",
			PurgeMaxRepositories, len(req.RepoIDs))
	}
	repoIDs := make([]string, 0, len(req.RepoIDs))
	seen := make(map[string]struct{}, len(req.RepoIDs))
	for _, repoID := range req.RepoIDs {
		if strings.TrimSpace(repoID) == "" {
			return nil, fmt.Errorf("retirement purge: empty repo id")
		}
		if _, dup := seen[repoID]; dup {
			return nil, fmt.Errorf("retirement purge: duplicate repo id %q", repoID)
		}
		seen[repoID] = struct{}{}
		repoIDs = append(repoIDs, repoID)
	}
	sort.Strings(repoIDs)
	return repoIDs, nil
}

// purgeOne purges one repository in one transaction. It reports the
// completed repo id, or the blocked exit when the purge wrote nothing.
func (p RetirementPurger) purgeOne(
	ctx context.Context,
	opts PurgeOptions,
	budget *deriveLockBudget,
	repoID string,
) (string, *PurgeBlocked, error) {
	purgeStarted := time.Now()
	tx, err := p.beginner.Begin(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("retirement purge begin: %w", err)
	}
	rolledBack := false
	rollback := func() {
		if !rolledBack {
			rolledBack = true
			_ = tx.Rollback()
		}
	}
	defer rollback()

	record, err := claimPurgeTombstone(ctx, tx, repoID)
	if err != nil {
		return "", nil, err
	}
	if record.State == RetirementStateComplete {
		// Crash-restart idempotency: the tombstone already reached
		// complete, so there is nothing left to delete and nothing to
		// count again.
		if err := tx.Commit(); err != nil {
			return "", nil, fmt.Errorf("retirement purge commit: %w", err)
		}
		rolledBack = true
		return repoID, nil, nil
	}
	if record.State != RetirementStatePending || record.Phase != RetirementPhaseFenced {
		return "", nil, fmt.Errorf(
			"retirement purge %s: tombstone %s is %s/%s, want pending/fenced (the owning runner phase purges it): %w",
			repoID, record.RetirementID, record.State, record.Phase, ErrRetirementTombstoneNotFenced)
	}
	scopeIDs, busy, err := lockPurgeScopes(ctx, tx, repoID)
	if err != nil {
		return "", nil, err
	}
	if busy {
		return "", &PurgeBlocked{
			RepoID: repoID, RetirementID: record.RetirementID,
			Reason: PurgeReasonScopeLockBusy,
			Detail: "scope rows locked by a concurrent pass; retry the fenced row",
		}, nil
	}
	blocking, err := readBlockingGenerations(ctx, tx, scopeIDs)
	if err != nil {
		return "", nil, err
	}
	if len(blocking) > 0 {
		readback := formatGenerationReadback(blocking)
		opts.Logger.WarnContext(ctx, "repository retirement blocked: terminal generations not reached",
			slog.String("outcome", PurgeReasonTerminalNotReached),
			slog.String("repo_id", repoID),
			slog.String("retirement_id", record.RetirementID),
			slog.Any("scope_ids", scopeIDs),
			slog.String("blocking_statuses", readback),
			telemetry.PhaseAttr(telemetry.PhaseEmission),
		)
		return "", &PurgeBlocked{
			RepoID: repoID, RetirementID: record.RetirementID,
			Reason:   PurgeReasonTerminalNotReached,
			Detail:   "scope generations still live; a new fence must move them before the purge",
			Statuses: readback,
		}, nil
	}
	ledger, deriveLockWait, err := deletePurgeLeftovers(ctx, tx, repoID, scopeIDs, budget)
	var lockBusy *deriveLockBusyError
	if errors.As(err, &lockBusy) {
		opts.Logger.WarnContext(ctx, "repository retirement blocked: derive lock busy",
			slog.String("outcome", PurgeReasonDeriveLockBusy),
			slog.String("repo_id", repoID),
			slog.String("retirement_id", record.RetirementID),
			slog.String("lock", lockBusy.lock),
			slog.Int64("derive_lock_wait_ms", lockBusy.wait.Milliseconds()),
			slog.Int64("derive_lock_timeout_ms", lockBusy.timeout.Milliseconds()),
			telemetry.PhaseAttr(telemetry.PhaseEmission),
		)
		return "", &PurgeBlocked{
			RepoID: repoID, RetirementID: record.RetirementID,
			Reason: PurgeReasonDeriveLockBusy,
			Detail: "a derive holds the " + lockBusy.lock + " advisory lock; retry the fenced row",
		}, nil
	}
	if err != nil {
		return "", nil, err
	}
	derived, err := addDerivedLedgerCounts(ctx, tx, repoID, scopeIDs, ledger)
	if err != nil {
		return "", nil, err
	}
	// The scope delete cascades generations and their fact rows, but the
	// per-fact key sidecars do not cascade: prune them first, in Deletes
	// order, so no orphaned sidecar row survives the retirement. The
	// pruned counts join the tombstone ledger beside the cascade
	// pre-counts. This DeleteGenerations call must stay textually before
	// the scope delete in this function: the fact-delete guard pairs
	// purgeDeleteScopesQuery with DeleteGenerations by call order.
	genScopeIDs, generationIDs, err := listPurgeGenerationIDs(ctx, tx, scopeIDs)
	if err != nil {
		return "", nil, err
	}
	pruned := make(map[string]int64)
	if err := factkeys.DeleteGenerations(ctx, tx, genScopeIDs, generationIDs, pruned); err != nil {
		return "", nil, fmt.Errorf("retirement purge prune fact keys: %w", err)
	}
	for table, count := range pruned {
		ledger[table] += count
	}
	scopes, err := execCount(ctx, tx, purgeDeleteScopesQuery, scopeIDs)
	if err != nil {
		return "", nil, fmt.Errorf("retirement purge delete scopes: %w", err)
	}
	ledger["ingestion_scopes"] = scopes
	// Pass-state rows have no FK to the scope, so the cascade misses them.
	// This stays after the scope delete: see purgeDeleteScopePassStateQuery
	// for the writer lock order it matches.
	passState, err := execCount(ctx, tx, purgeDeleteScopePassStateQuery, scopeIDs)
	if err != nil {
		return "", nil, fmt.Errorf("retirement purge delete impact pass state: %w", err)
	}
	ledger["supply_chain_impact_scope_pass_state"] = passState
	// The #10820 tables whose rows the cascade or the writer order puts after
	// the scope delete: see deletePurgeAfterCascade.
	if err := deletePurgeAfterCascade(ctx, tx, scopeIDs, ledger); err != nil {
		return "", nil, err
	}
	if err := completePurgeTombstone(ctx, tx, record.RetirementID, ledger); err != nil {
		return "", nil, err
	}
	if err := tx.Commit(); err != nil {
		return "", nil, fmt.Errorf("retirement purge commit: %w", err)
	}
	rolledBack = true

	recordRetirementPurged(ctx, p.Instruments)
	recordDerivedScopesPurged(ctx, p.Instruments, int64(len(derived)))
	opts.Logger.InfoContext(ctx, "repository retirement purged",
		slog.String("outcome", "purged"),
		slog.String("repo_id", repoID),
		slog.String("retirement_id", record.RetirementID),
		slog.Int64("scopes_deleted", scopes),
		slog.Int("derived_scopes_deleted", len(derived)),
		slog.Int64("derive_lock_wait_ms", deriveLockWait.Milliseconds()),
		slog.Int64("purge_duration_ms", time.Since(purgeStarted).Milliseconds()),
		telemetry.PhaseAttr(telemetry.PhaseEmission),
	)
	return repoID, nil, nil
}

// claimPurgeTombstone locks and returns the repository's open tombstone
// row, or nil when the repository was never fenced.
func claimPurgeTombstone(ctx context.Context, tx db.Transaction, repoID string) (*RetirementRecord, error) {
	rows, err := tx.QueryContext(ctx, purgeClaimTombstoneQuery, repoID)
	if err != nil {
		return nil, fmt.Errorf("retirement purge claim tombstone: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("retirement purge claim tombstone: %w", err)
		}
		return nil, fmt.Errorf("retirement purge %s: %w", repoID, ErrRetirementNoOpenTombstone)
	}
	var record RetirementRecord
	if err := rows.Scan(&record.RetirementID, &record.RepoID, &record.ScopeID, &record.State, &record.Phase); err != nil {
		return nil, fmt.Errorf("retirement purge claim scan: %w", err)
	}
	return &record, rows.Err()
}

// lockPurgeScopes locks the repository's scope rows without waiting. It
// reports busy when a concurrent pass (reducer retention) holds a scope
// lock: the purge retries the fenced row instead of queueing behind it.
func lockPurgeScopes(ctx context.Context, tx db.Transaction, repoID string) ([]string, bool, error) {
	rows, err := tx.QueryContext(ctx, purgeLockScopesQuery, repoID)
	if err != nil {
		if isLockTimeoutError(err) {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("retirement purge lock scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var scopeIDs []string
	for rows.Next() {
		var scopeID string
		if err := rows.Scan(&scopeID); err != nil {
			return nil, false, fmt.Errorf("retirement purge lock scopes scan: %w", err)
		}
		scopeIDs = append(scopeIDs, scopeID)
	}
	// The ordered NOWAIT lock streams the scopes it could lock first, so a
	// held later scope reports 55P03 through iteration, not the query call.
	if err := rows.Err(); err != nil {
		if isLockTimeoutError(err) {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("retirement purge lock scopes: %w", err)
	}
	return scopeIDs, false, nil
}

// listPurgeGenerationIDs returns the parallel (scope, generation) pairs
// covering every generation of the purge scopes, in scope then generation
// order. The terminal gate already passed, so these are all terminal; the
// sidecar prune needs every pair regardless of status.
func listPurgeGenerationIDs(
	ctx context.Context,
	tx db.Transaction,
	scopeIDs []string,
) ([]string, []string, error) {
	rows, err := tx.QueryContext(ctx, purgeScopeGenerationIDsQuery, scopeIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("retirement purge list generations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var genScopeIDs, generationIDs []string
	for rows.Next() {
		var scopeID, generationID string
		if err := rows.Scan(&scopeID, &generationID); err != nil {
			return nil, nil, fmt.Errorf("retirement purge list generations scan: %w", err)
		}
		genScopeIDs = append(genScopeIDs, scopeID)
		generationIDs = append(generationIDs, generationID)
	}
	return genScopeIDs, generationIDs, rows.Err()
}

// completePurgeTombstone writes the tombstone: complete/done, the
// retirement timestamp, and the deleted-row ledger. Zero affected rows
// means a concurrent completion won, and this attempt must not count the
// retirement twice.
func completePurgeTombstone(
	ctx context.Context,
	tx db.Transaction,
	retirementID string,
	ledger map[string]int64,
) error {
	ledgerJSON, err := json.Marshal(ledger)
	if err != nil {
		return fmt.Errorf("retirement purge ledger: %w", err)
	}
	result, err := tx.ExecContext(ctx, purgeCompleteTombstoneQuery, retirementID, string(ledgerJSON))
	if err != nil {
		return fmt.Errorf("retirement purge complete tombstone: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("retirement purge complete tombstone rows: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("retirement purge complete tombstone: %d rows, want 1 (concurrent completion)", affected)
	}
	return nil
}
