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

// Purge refusal sentinels. The operator trigger maps them to 409 without
// reading error text: an unfenced repository and a tombstone the owning
// runner moved past pending/fenced both refuse, never purge partially.
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
// contention: the reducer retention pass holds a scope lock, and the next
// runner pass retries the same fenced row.
const (
	// PurgeReasonTerminalNotReached reports generations still live.
	PurgeReasonTerminalNotReached = "terminal_not_reached"
	// PurgeReasonScopeLockBusy reports a NOWAIT scope-lock miss.
	PurgeReasonScopeLockBusy = "scope_lock_busy"
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
	Now    func() time.Time
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
// returns an error.
func (p RetirementPurger) PurgeRetired(ctx context.Context, req PurgeRequest) (PurgeResult, error) {
	opts := p.Options.withDefaults()
	repoIDs, err := validatedPurgeRepos(req)
	if err != nil {
		return PurgeResult{}, err
	}
	if p.beginner == nil {
		return PurgeResult{}, fmt.Errorf("retirement purge: beginner is required")
	}
	var result PurgeResult
	for _, repoID := range repoIDs {
		completed, blocked, err := p.purgeOne(ctx, opts, repoID)
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
	repoID string,
) (string, *PurgeBlocked, error) {
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
	ledger, err := deletePurgeLeftovers(ctx, tx, repoID, scopeIDs)
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
	if err := completePurgeTombstone(ctx, tx, record.RetirementID, ledger); err != nil {
		return "", nil, err
	}
	if err := tx.Commit(); err != nil {
		return "", nil, fmt.Errorf("retirement purge commit: %w", err)
	}
	rolledBack = true

	recordRetirementPurged(ctx, p.Instruments)
	opts.Logger.InfoContext(ctx, "repository retirement purged",
		slog.String("outcome", "purged"),
		slog.String("repo_id", repoID),
		slog.String("retirement_id", record.RetirementID),
		slog.Int64("scopes_deleted", scopes),
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
	return scopeIDs, false, rows.Err()
}

// generationBlock is one live status the terminal gate read back.
type generationBlock struct {
	status string
	count  int64
}

// readBlockingGenerations returns the live generation statuses the
// terminal gate must refuse on. Empty means every generation is terminal
// (superseded, failed, or any other non-live status) and the purge may run.
func readBlockingGenerations(ctx context.Context, tx db.Transaction, scopeIDs []string) ([]generationBlock, error) {
	rows, err := tx.QueryContext(ctx, purgeBlockingGenerationsQuery, scopeIDs)
	if err != nil {
		return nil, fmt.Errorf("retirement purge terminal gate: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var blocking []generationBlock
	for rows.Next() {
		var block generationBlock
		if err := rows.Scan(&block.status, &block.count); err != nil {
			return nil, fmt.Errorf("retirement purge terminal gate scan: %w", err)
		}
		blocking = append(blocking, block)
	}
	return blocking, rows.Err()
}

// formatGenerationReadback renders the gate readback for the WARN log and
// the blocked exit ("active:1, pending:2", statuses already ordered).
func formatGenerationReadback(blocking []generationBlock) string {
	parts := make([]string, 0, len(blocking))
	for _, block := range blocking {
		parts = append(parts, fmt.Sprintf("%s:%d", block.status, block.count))
	}
	return strings.Join(parts, ", ")
}

// deletePurgeLeftovers deletes the no-FK keyed leftover rows and returns
// the deleted-row ledger. Grant rows cascade with the scope delete, so the
// ledger pre-counts them here; everything else reports its own delete
// count.
func deletePurgeLeftovers(
	ctx context.Context,
	tx db.Transaction,
	repoID string,
	scopeIDs []string,
) (map[string]int64, error) {
	ledger := make(map[string]int64)
	grants, err := scanGrantCounts(ctx, tx, scopeIDs)
	if err != nil {
		return nil, err
	}
	for table, count := range grants {
		ledger[table] = count
	}
	for _, keyed := range []struct {
		table string
		query string
		args  []any
	}{
		{"repository_reindex_requests", purgeDeleteReindexRequestsQuery, []any{scopeIDs}},
		{"repository_selection_observations", purgeDeleteSelectionObservationsQuery, []any{scopeIDs}},
		{"content_files", purgeDeleteContentFilesQuery, []any{repoID}},
		{"content_entities", purgeDeleteContentEntitiesQuery, []any{repoID}},
		{"content_file_references", purgeDeleteContentReferencesQuery, []any{repoID}},
		{"repository_refs", purgeDeleteRepositoryRefsQuery, []any{repoID}},
		{"shared_projection_intents", purgeDeleteSharedIntentsQuery, []any{repoID, scopeIDs}},
		{"shared_projection_unroutable_intents", purgeDeleteUnroutableIntentsQuery, []any{repoID, scopeIDs}},
		{"collector_generation_dead_letters", purgeDeleteDeadLettersQuery, []any{scopeIDs}},
	} {
		deleted, err := execCount(ctx, tx, keyed.query, keyed.args...)
		if err != nil {
			return ledger, fmt.Errorf("retirement purge delete %s: %w", keyed.table, err)
		}
		ledger[keyed.table] = deleted
	}
	return ledger, nil
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

// scanGrantCounts pre-counts the four scope-FK grant tables the scope
// delete cascades, in one bounded statement.
func scanGrantCounts(ctx context.Context, tx db.Transaction, scopeIDs []string) (map[string]int64, error) {
	rows, err := tx.QueryContext(ctx, purgeGrantCountsQuery, scopeIDs)
	if err != nil {
		return nil, fmt.Errorf("retirement purge grant counts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("retirement purge grant counts read: %w", err)
		}
		return nil, fmt.Errorf("retirement purge grant counts: no row returned")
	}
	var scope, repository, roleScope, roleRepository int64
	if err := rows.Scan(&scope, &repository, &roleScope, &roleRepository); err != nil {
		return nil, fmt.Errorf("retirement purge grant counts scan: %w", err)
	}
	return map[string]int64{
		"tenant_scope_grants":              scope,
		"tenant_repository_grants":         repository,
		"identity_role_scope_targets":      roleScope,
		"identity_role_repository_targets": roleRepository,
	}, rows.Err()
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

// recordRetirementPurged counts one completed operator retirement on
// eshu_dp_repository_retirements_purged_total (#7766). Nil instruments are
// a no-op. The count lands after the purge commit, so a crash between
// commit and emission under-counts rather than double-counts.
func recordRetirementPurged(ctx context.Context, instruments *telemetry.Instruments) {
	if instruments == nil || instruments.OperatorRepositoryRetirementsPurged == nil {
		return
	}
	instruments.OperatorRepositoryRetirementsPurged.Add(ctx, 1)
}
