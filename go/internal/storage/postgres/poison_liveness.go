// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const defaultPoisonLivenessMaxRecoverAttempts = 1

// PoisonLivenessPolicy bounds the poison dead-letter recovery arm. Unlike
// GenerationLivenessPolicy (which re-drives source-local projector work for a
// wedged ACTIVE generation), this policy governs re-enqueuing a fresh pending
// attempt for a fact_work_items row that is already terminally 'dead_letter'
// with no newer scope generation to supersede it.
type PoisonLivenessPolicy struct {
	// MaxRecoverAttempts bounds the automated re-drive budget per work item so a
	// genuinely poison item cannot loop forever; once exhausted the item is left
	// dead_letter for an operator to inspect. Defaults to 1 when <= 0.
	MaxRecoverAttempts int
	// BatchLimit caps how many dead-letter rows one sweep re-drives. Defaults to
	// defaultGenerationLivenessBatchLimit (200) when <= 0.
	BatchLimit int
}

// Normalize fills zero or negative fields with documented defaults.
func (p PoisonLivenessPolicy) Normalize() PoisonLivenessPolicy {
	if p.MaxRecoverAttempts <= 0 {
		p.MaxRecoverAttempts = defaultPoisonLivenessMaxRecoverAttempts
	}
	if p.BatchLimit <= 0 {
		p.BatchLimit = defaultGenerationLivenessBatchLimit
	}
	return p
}

// PoisonStuckCounts reports the current dead-letter/poison class: fact_work_items
// rows whose status is 'dead_letter' and whose scope has no strictly-newer
// scope_generations row. This is the class the #4727 projector-claimer fix and
// the generation-liveness sweep do not reach — dead_letter is terminal and
// unclaimable, and the liveness sweep's NOT EXISTS guard treats same-generation
// dead_letter reducer work as still "in progress", excluding it from the wedged
// re-drive path.
type PoisonStuckCounts struct {
	// PoisonScopes is the distinct scope_id count in the poison class.
	PoisonScopes int64
	// PoisonItems is the total fact_work_items row count in the poison class.
	PoisonItems int64
	// OldestPoisonAgeSeconds is the age, in seconds, of the oldest poison row's
	// updated_at (when the row was dead-lettered). Zero when the class is empty.
	OldestPoisonAgeSeconds float64
}

// PoisonRecoveryResult summarizes one bounded poison-recovery sweep.
type PoisonRecoveryResult struct {
	// Recovered counts dead-letter rows re-enqueued to pending this cycle.
	Recovered int
	// RecoveredScopeIDs names the distinct scopes touched, for logging.
	RecoveredScopeIDs []string
}

// PoisonLivenessStore detects and bounds-recovers the dead-letter/poison class
// against Postgres. All writes are idempotent under concurrent reducer workers:
// the conflict domain is work_item_id (the fact_work_items primary key), and
// the recovery UPDATE re-verifies status = 'dead_letter' at write time so a
// concurrent reclaim of the same row is never clobbered.
type PoisonLivenessStore struct {
	db ExecQueryer
}

// NewPoisonLivenessStore constructs a Postgres-backed poison-liveness store.
func NewPoisonLivenessStore(db ExecQueryer) PoisonLivenessStore {
	return PoisonLivenessStore{db: db}
}

// CountPoisonDeadLetters buckets the current poison dead-letter class into
// scope count, item count, and the oldest item's age. The query is read-only
// and bounded by fact_work_items_dead_letter_poison_idx (partial index on
// status = 'dead_letter'), so cost is proportional to the dead_letter subset,
// not the full fact_work_items table.
func (s PoisonLivenessStore) CountPoisonDeadLetters(ctx context.Context, now time.Time) (PoisonStuckCounts, error) {
	if s.db == nil {
		return PoisonStuckCounts{}, errors.New("poison liveness database is required")
	}
	now = now.UTC()

	rows, err := s.db.QueryContext(ctx, countPoisonDeadLettersQuery, now)
	if err != nil {
		return PoisonStuckCounts{}, fmt.Errorf("count poison dead letters: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var counts PoisonStuckCounts
	if rows.Next() {
		if scanErr := rows.Scan(&counts.PoisonScopes, &counts.PoisonItems, &counts.OldestPoisonAgeSeconds); scanErr != nil {
			return PoisonStuckCounts{}, fmt.Errorf("count poison dead letters: %w", scanErr)
		}
	}
	if err := rows.Err(); err != nil {
		return PoisonStuckCounts{}, fmt.Errorf("count poison dead letters: %w", err)
	}
	return counts, nil
}

// RecoverPoisonDeadLetters re-enqueues a bounded batch of poison dead-letter
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
// surface-only via CountPoisonDeadLetters / the exported gauge.
func (s PoisonLivenessStore) RecoverPoisonDeadLetters(
	ctx context.Context,
	policy PoisonLivenessPolicy,
	now time.Time,
) (PoisonRecoveryResult, error) {
	if s.db == nil {
		return PoisonRecoveryResult{}, errors.New("poison liveness database is required")
	}
	policy = policy.Normalize()
	now = now.UTC()

	rows, err := s.db.QueryContext(ctx, recoverPoisonDeadLettersQuery, now, policy.MaxRecoverAttempts, policy.BatchLimit)
	if err != nil {
		return PoisonRecoveryResult{}, fmt.Errorf("recover poison dead letters: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var scopeIDs []string
	recovered := 0
	for rows.Next() {
		var workItemID, scopeID, generationID string
		if scanErr := rows.Scan(&workItemID, &scopeID, &generationID); scanErr != nil {
			return PoisonRecoveryResult{}, fmt.Errorf("recover poison dead letters: %w", scanErr)
		}
		recovered++
		scopeIDs = append(scopeIDs, scopeID)
	}
	if err := rows.Err(); err != nil {
		return PoisonRecoveryResult{}, fmt.Errorf("recover poison dead letters: %w", err)
	}

	return PoisonRecoveryResult{
		Recovered:         recovered,
		RecoveredScopeIDs: scopeIDs,
	}, nil
}
