// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Activation sources written to changed_since_activations.source.
const (
	// SourceSweeper marks an active generation journaled by the sweeper,
	// with an unknown (NULL) prior.
	SourceSweeper = "sweeper"
	// SourceBackfill marks a retained generation journaled by the backfill.
	SourceBackfill = "backfill"
)

// JournalResult summarizes one journal pass.
type JournalResult struct {
	// Skipped is true when another replica held the journal lock; nothing
	// else is set then.
	Skipped bool
	// BackfillScopes is the number of scopes whose retained chain was
	// journaled; BackfillRows the rows it inserted.
	BackfillScopes int
	BackfillRows   int64
	// SweeperRows is the number of active generations journaled with an
	// unknown prior.
	SweeperRows int64
}

// JournalStore journals activations for the link writer and reads the
// ledger's backlog and size. It is safe for concurrent use; a journal pass
// is exclusive across replicas through a try-only advisory lock.
type JournalStore struct {
	database db.ExecQueryer
	// Now supplies timestamps; nil uses time.Now.
	Now func() time.Time
}

// NewJournalStore returns a JournalStore over database, which must
// implement db.Beginner for Journal and DeleteOrphanScope.
func NewJournalStore(database db.ExecQueryer) *JournalStore {
	return &JournalStore{database: database}
}

func (s *JournalStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *JournalStore) begin(ctx context.Context) (db.Transaction, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("changed-since journal database is required")
	}
	beginner, ok := s.database.(db.Beginner)
	if !ok {
		return nil, errors.New("changed-since journal database must support Begin")
	}
	return beginner.Begin(ctx)
}

// Journal runs one pass in one transaction: first the backfill of at most
// backfillScopes scopes that have no activation row, in chain order, then
// one sweeper row for every active generation still missing from the
// journal, then a cursor row for every journaled scope that has none. It
// enqueues nothing. Backfill goes first so a scope's retained history takes lower
// activation_seq values than its active generation. The pass holds the
// journal advisory lock so two replicas cannot interleave one scope's rows.
func (s *JournalStore) Journal(ctx context.Context, backfillScopes int) (JournalResult, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return JournalResult{}, fmt.Errorf("changed-since journal: begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var locked bool
	if err := queryOne(ctx, tx, trySlotQuery, []any{SlotLockClass, journalLockSlot}, &locked); err != nil {
		return JournalResult{}, fmt.Errorf("changed-since journal: lock: %w", err)
	}
	if !locked {
		return JournalResult{Skipped: true}, nil
	}

	var result JournalResult
	if backfillScopes > 0 {
		if err := s.backfill(ctx, tx, backfillScopes, &result); err != nil {
			return JournalResult{}, err
		}
	}
	res, err := tx.ExecContext(ctx, journalActiveGenerationsQuery, s.now())
	if err != nil {
		return JournalResult{}, fmt.Errorf("changed-since journal: sweep active generations: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil {
		result.SweeperRows = n
	}
	if _, err := tx.ExecContext(ctx, createCursorsQuery, DigestVersion, s.now()); err != nil {
		return JournalResult{}, fmt.Errorf("changed-since journal: create cursors: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return JournalResult{}, fmt.Errorf("changed-since journal: commit: %w", err)
	}
	committed = true
	return result, nil
}

type chainRow struct {
	scopeID      string
	generationID string
	priorID      string
	activatedAt  time.Time
}

func (s *JournalStore) backfill(ctx context.Context, tx db.Transaction, limit int, result *JournalResult) error {
	rows, err := tx.QueryContext(ctx, backfillChainsQuery, limit)
	if err != nil {
		return fmt.Errorf("changed-since journal: backfill chains: %w", err)
	}
	var chain []chainRow
	for rows.Next() {
		var row chainRow
		if err := rows.Scan(&row.scopeID, &row.generationID, &row.priorID, &row.activatedAt); err != nil {
			_ = rows.Close()
			return fmt.Errorf("changed-since journal: scan backfill chain: %w", err)
		}
		chain = append(chain, row)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return fmt.Errorf("changed-since journal: backfill chains: %w", err)
	}
	// A row that inserts nothing (its generation is held by retention or
	// already pruned, or the row exists) ends its scope's chain for this pass
	// (arbiter ruling arb-7127-3d, C3): a later row would name it as its
	// prior. The sweeper still journals the active generation.
	lastScope, ended := "", ""
	for _, row := range chain {
		if row.scopeID == ended {
			continue
		}
		res, err := tx.ExecContext(ctx, insertActivationQuery,
			row.scopeID, row.generationID, row.priorID, SourceBackfill, row.activatedAt)
		if err != nil {
			return fmt.Errorf("changed-since journal: insert backfill activation: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("changed-since journal: insert backfill activation: %w", err)
		}
		if n == 0 {
			ended = row.scopeID
			continue
		}
		result.BackfillRows += n
		if row.scopeID != lastScope {
			result.BackfillScopes++
			lastScope = row.scopeID
		}
	}
	return nil
}

// BacklogScopes returns up to limit scopes that have an activation above
// their cursor and are not backing off, oldest pending activation first. It
// is a hint: LinkNext re-reads the head under the cursor lock.
func (s *JournalStore) BacklogScopes(ctx context.Context, limit int) ([]string, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("changed-since journal database is required")
	}
	rows, err := s.database.QueryContext(ctx, backlogScopesQuery, limit, s.now())
	if err != nil {
		return nil, fmt.Errorf("changed-since journal: backlog scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var scopes []string
	for rows.Next() {
		var scopeID string
		if err := rows.Scan(&scopeID); err != nil {
			return nil, fmt.Errorf("changed-since journal: scan backlog scope: %w", err)
		}
		scopes = append(scopes, scopeID)
	}
	return scopes, rows.Err()
}

// LedgerStats is a point-in-time reading of the ledger for the gauges.
type LedgerStats struct {
	// BacklogRows is the number of activation rows above their scope cursor.
	BacklogRows int64
	// LagSeconds is the age of the oldest of them, 0 when there is none.
	LagSeconds float64
	// StateBytes and StateRows describe changed_since_key_state; StateRows is
	// the planner's estimate.
	StateBytes int64
	StateRows  int64
	// DeltaBytes and DeltaRows describe changed_since_link_deltas; DeltaRows
	// is the planner's estimate.
	DeltaBytes int64
	DeltaRows  int64
	// RetryingScopes have a counted failure pending on their head activation;
	// PoisonedScopes carry the link_poisoned marker until their next full link.
	RetryingScopes int64
	PoisonedScopes int64
}

// Stats reads the backlog, the lag and the ledger's size.
func (s *JournalStore) Stats(ctx context.Context) (LedgerStats, error) {
	if s == nil || s.database == nil {
		return LedgerStats{}, errors.New("changed-since journal database is required")
	}
	var stats LedgerStats
	if err := queryOne(ctx, s.database, backlogStatsQuery, []any{s.now()},
		&stats.BacklogRows, &stats.LagSeconds, &stats.RetryingScopes, &stats.PoisonedScopes); err != nil {
		return LedgerStats{}, fmt.Errorf("changed-since journal: backlog stats: %w", err)
	}
	if err := queryOne(ctx, s.database, ledgerSizeQuery, nil,
		&stats.StateBytes, &stats.StateRows, &stats.DeltaBytes, &stats.DeltaRows); err != nil {
		return LedgerStats{}, fmt.Errorf("changed-since journal: ledger size: %w", err)
	}
	return stats, nil
}

// LedgerOrphans is one reading of the orphan probe (arbiter ruling
// arb-7127-3d): ledger rows that name a generation with no scope_generations
// row, and bucket-count groups with no link row. With every ledger writer
// fencing the generations it names (PR-3e), all three stay zero outside the
// short window between a scope's deletion and the orphan-scope purge.
type LedgerOrphans struct {
	// Links are links whose generation or non-empty prior generation has no
	// scope_generations row.
	Links int64
	// Activations are activation rows whose generation has no
	// scope_generations row.
	Activations int64
	// HeadlessBucketGroups are (scope, generation, prior) groups of bucket
	// counts with no link row.
	HeadlessBucketGroups int64
}

// Orphans runs the orphan probe. It scans the link, activation and
// bucket-count tables, so callers sample it sparingly.
func (s *JournalStore) Orphans(ctx context.Context) (LedgerOrphans, error) {
	if s == nil || s.database == nil {
		return LedgerOrphans{}, errors.New("changed-since journal database is required")
	}
	var orphans LedgerOrphans
	if err := queryOne(ctx, s.database, ledgerOrphansQuery, nil,
		&orphans.Links, &orphans.Activations, &orphans.HeadlessBucketGroups); err != nil {
		return LedgerOrphans{}, fmt.Errorf("changed-since journal: orphan probe: %w", err)
	}
	return orphans, nil
}

// OrphanScopes returns up to limit scopes with ledger rows and no
// ingestion_scopes row.
func (s *JournalStore) OrphanScopes(ctx context.Context, limit int) ([]string, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("changed-since journal database is required")
	}
	rows, err := s.database.QueryContext(ctx, orphanScopesQuery, limit)
	if err != nil {
		return nil, fmt.Errorf("changed-since journal: orphan scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var scopes []string
	for rows.Next() {
		var scopeID string
		if err := rows.Scan(&scopeID); err != nil {
			return nil, fmt.Errorf("changed-since journal: scan orphan scope: %w", err)
		}
		scopes = append(scopes, scopeID)
	}
	return scopes, rows.Err()
}

// DeleteOrphanScope removes a deleted scope's ledger rows in one
// transaction. It returns false without deleting when a link writer holds
// the scope's cursor; the next pass retries.
func (s *JournalStore) DeleteOrphanScope(ctx context.Context, scopeID string) (bool, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return false, fmt.Errorf("changed-since orphan delete: begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	held, err := rowExists(ctx, tx, lockOrphanCursorQuery, scopeID)
	if err != nil {
		return false, fmt.Errorf("changed-since orphan delete: lock cursor: %w", err)
	}
	if !held {
		exists, err := rowExists(ctx, tx, orphanCursorExistsQuery, scopeID)
		if err != nil {
			return false, fmt.Errorf("changed-since orphan delete: read cursor: %w", err)
		}
		if exists {
			return false, nil
		}
	}
	for _, statement := range deleteOrphanScopeStatements {
		if _, err := tx.ExecContext(ctx, statement, scopeID); err != nil {
			return false, fmt.Errorf("changed-since orphan delete: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("changed-since orphan delete: commit: %w", err)
	}
	committed = true
	return true, nil
}

func rowExists(ctx context.Context, q db.Queryer, query string, args ...any) (bool, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	found := rows.Next()
	return found, rows.Err()
}
