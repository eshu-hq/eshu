// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package poison

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

const (
	defaultMaxRecoverAttempts = 1
	// DefaultBatchLimit matches the generation-liveness sweep batch default
	// (defaultGenerationLivenessBatchLimit in the parent postgres package):
	// one sweep re-drives at most this many dead-letter rows. Exported so
	// the parent package pins the equality in a test without production
	// code on either side importing the other.
	DefaultBatchLimit = 200
)

// Policy bounds the poison dead-letter recovery arm. Unlike
// GenerationLivenessPolicy (which re-drives source-local projector work for a
// wedged ACTIVE generation), this policy governs re-enqueuing a fresh pending
// attempt for a fact_work_items row that is already terminally 'dead_letter'
// with no newer scope generation to supersede it.
type Policy struct {
	// MaxRecoverAttempts bounds the automated re-drive budget per work item so a
	// genuinely poison item cannot loop forever; once exhausted the item is left
	// dead_letter for an operator to inspect. Defaults to 1 when <= 0.
	MaxRecoverAttempts int
	// BatchLimit caps how many dead-letter rows one sweep re-drives. Defaults to
	// DefaultBatchLimit (200) when <= 0.
	BatchLimit int
}

// Normalize fills zero or negative fields with documented defaults.
func (p Policy) Normalize() Policy {
	if p.MaxRecoverAttempts <= 0 {
		p.MaxRecoverAttempts = defaultMaxRecoverAttempts
	}
	if p.BatchLimit <= 0 {
		p.BatchLimit = DefaultBatchLimit
	}
	return p
}

// StuckCounts reports the current dead-letter/poison class: fact_work_items
// rows whose status is 'dead_letter' and whose scope has no strictly-newer
// scope_generations row. This is the class the #4727 projector-claimer fix and
// the generation-liveness sweep do not reach — dead_letter is terminal and
// unclaimable, and the liveness sweep's NOT EXISTS guard treats same-generation
// dead_letter reducer work as still "in progress", excluding it from the wedged
// re-drive path.
type StuckCounts struct {
	// PoisonScopes is the distinct scope_id count in the poison class.
	PoisonScopes int64
	// PoisonItems is the total fact_work_items row count in the poison class.
	PoisonItems int64
	// OldestPoisonAgeSeconds is the age, in seconds, of the oldest poison row's
	// updated_at (when the row was dead-lettered). Zero when the class is empty.
	OldestPoisonAgeSeconds float64
}

// RecoveryResult summarizes one bounded poison-recovery sweep.
type RecoveryResult struct {
	// Recovered counts dead-letter rows re-enqueued to pending this cycle.
	Recovered int
	// RecoveredScopeIDs names the distinct scopes touched, for logging.
	RecoveredScopeIDs []string
}

// Store detects and bounds-recovers the dead-letter/poison class
// against Postgres. All writes are idempotent under concurrent reducer workers:
// the conflict domain is work_item_id (the fact_work_items primary key), and
// the recovery UPDATE re-verifies status = 'dead_letter' at write time so a
// concurrent reclaim of the same row is never clobbered.
type Store struct {
	database db.ExecQueryer
}

// NewStore constructs a Postgres-backed poison-liveness store.
func NewStore(database db.ExecQueryer) Store {
	return Store{database: database}
}

// CountDeadLetters buckets the current poison dead-letter class into
// scope count, item count, and the oldest item's age. The query is read-only
// and bounded by fact_work_items_dead_letter_poison_idx (partial index on
// status = 'dead_letter'), so cost is proportional to the dead_letter subset,
// not the full fact_work_items table.
func (s Store) CountDeadLetters(ctx context.Context, now time.Time) (StuckCounts, error) {
	if s.database == nil {
		return StuckCounts{}, errors.New("poison liveness database is required")
	}
	now = now.UTC()

	rows, err := s.database.QueryContext(ctx, CountDeadLettersQuery, now)
	if err != nil {
		return StuckCounts{}, fmt.Errorf("count poison dead letters: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var counts StuckCounts
	if rows.Next() {
		if scanErr := rows.Scan(&counts.PoisonScopes, &counts.PoisonItems, &counts.OldestPoisonAgeSeconds); scanErr != nil {
			return StuckCounts{}, fmt.Errorf("count poison dead letters: %w", scanErr)
		}
	}
	if err := rows.Err(); err != nil {
		return StuckCounts{}, fmt.Errorf("count poison dead letters: %w", err)
	}
	return counts, nil
}

// RecoverDeadLetters re-enqueues a bounded batch of poison dead-letter
// rows to a fresh pending attempt, incrementing each row's
// poison_recovery_attempts budget counter (capped by policy.MaxRecoverAttempts
// via LEAST, mirroring recoverWedgedActiveGenerationsQuery). A row already at
// or past the budget ceiling is excluded by the candidate CTE. The UPDATE's
// write-time WHERE re-verifies status = 'dead_letter' so a row a concurrent
// worker has already reclaimed is left untouched (affects zero rows for that
// row), never overwritten.
//
// Callers must only invoke this when bounded auto-retry is enabled
// (ESHU_POISON_LIVENESS_AUTO_RETRY_ENABLED); the default posture is
// surface-only via CountDeadLetters / the exported gauge.
func (s Store) RecoverDeadLetters(
	ctx context.Context,
	policy Policy,
	now time.Time,
) (RecoveryResult, error) {
	if s.database == nil {
		return RecoveryResult{}, errors.New("poison liveness database is required")
	}
	policy = policy.Normalize()
	now = now.UTC()

	rows, err := s.database.QueryContext(ctx, RecoverDeadLettersQuery, now, policy.MaxRecoverAttempts, policy.BatchLimit)
	if err != nil {
		return RecoveryResult{}, fmt.Errorf("recover poison dead letters: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var scopeIDs []string
	recovered := 0
	for rows.Next() {
		var workItemID, scopeID, generationID string
		if scanErr := rows.Scan(&workItemID, &scopeID, &generationID); scanErr != nil {
			return RecoveryResult{}, fmt.Errorf("recover poison dead letters: %w", scanErr)
		}
		recovered++
		scopeIDs = append(scopeIDs, scopeID)
	}
	if err := rows.Err(); err != nil {
		return RecoveryResult{}, fmt.Errorf("recover poison dead letters: %w", err)
	}

	return RecoveryResult{
		Recovered:         recovered,
		RecoveredScopeIDs: scopeIDs,
	}, nil
}
