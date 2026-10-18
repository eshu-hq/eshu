// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membershipstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/membership"
)

// Bounds of one DeleteExpiredObservations call: at most
// expiredSweepMaxBatches statements of expiredSweepBatchSize rows each, so a
// large backlog drains across collector cycles instead of in one long pass.
const (
	expiredSweepBatchSize  = 500
	expiredSweepMaxBatches = 20
)

// deleteExpiredObservationsQuery deletes one batch of rows, by primary key,
// whose own liveness window plus the grace period ended before $1, and
// returns each deleted row's state so the caller can count reclaimed
// not_listed rows. A $4 (not_listed) row is deleted only when its selector
// is abandoned -- no repository_selectors marker, or a marker older than the
// row's own window plus grace (#7840): membership.Evaluate reads that
// history to keep scopes already confirmed missing out of the mass-miss
// guard, so deleting it for a selector that still cycles (a guard-tripped
// cycle writes no rows, only the marker) could trip the guard forever once
// access returns. The marker is read from the statement snapshot, so a
// selector silent for window plus grace that revives in the same instant as
// a sweep can lose its history; its next evaluation then re-confirms from
// pending instead of tripping (the cycle that read the priors rewrites the
// rows). That revive-in-the-same-instant race is bounded and accepted, not
// prevented (P3-1). Each batch is its own statement, so locks last one
// batch. SKIP LOCKED passes over a row an upsert holds, and FOR UPDATE rechecks the
// expiry predicate against the newest row version, so a row renewed before
// the lock is never deleted. The batch is unordered on purpose: the sweep
// never waits on a lock, so no lock order is needed, and an ORDER BY forces
// a full primary-key index scan on every steady-state probe that finds
// nothing to delete.
const deleteExpiredObservationsQuery = `
WITH doomed AS (
    SELECT o.scope_id, o.selector_id
    FROM repository_selection_observations o
    LEFT JOIN repository_selectors s ON s.selector_id = o.selector_id
    WHERE o.evaluated_at + make_interval(secs => o.liveness_window_seconds) + make_interval(secs => $2::bigint) < $1::timestamptz
      AND (o.state <> $4 OR s.selector_id IS NULL
           OR s.last_cycle_at + make_interval(secs => o.liveness_window_seconds) + make_interval(secs => $2::bigint) < $1::timestamptz)
    LIMIT $3
    FOR UPDATE OF o SKIP LOCKED
)
DELETE FROM repository_selection_observations o
USING doomed d
WHERE o.scope_id = d.scope_id AND o.selector_id = d.selector_id
RETURNING o.state
`

// DeleteExpiredObservations deletes rows of every selector whose evaluated_at
// plus their own liveness window plus grace is before now. It runs batches of
// expiredSweepBatchSize rows, stops at the first short batch or after
// expiredSweepMaxBatches, and returns the rows deleted by the batches that
// completed before an error; a failed batch contributes nothing (see
// deleteExpiredBatch). A row still inside its window or the grace after
// it is never deleted; a not_listed row is deleted only for an abandoned
// selector (see deleteExpiredObservationsQuery, #7840) and is counted in
// NotListedReclaimed.
func (s ObservationStore) DeleteExpiredObservations(ctx context.Context, now time.Time, grace time.Duration) (membership.SweepCounts, error) {
	switch {
	case s.database == nil:
		return membership.SweepCounts{}, errors.New("repository selection store database is required")
	case now.IsZero():
		return membership.SweepCounts{}, errors.New("delete expired repository selection observations: now is required")
	case grace < 0:
		return membership.SweepCounts{}, fmt.Errorf("delete expired repository selection observations: grace must not be negative, got %v", grace)
	}
	var counts membership.SweepCounts
	for range expiredSweepMaxBatches {
		batch, err := s.deleteExpiredBatch(ctx, now, grace)
		counts.Deleted += batch.Deleted
		counts.NotListedReclaimed += batch.NotListedReclaimed
		if err != nil {
			return counts, err
		}
		if batch.Deleted < expiredSweepBatchSize {
			break
		}
	}
	return counts, nil
}

// deleteExpiredBatch deletes one sweep batch and counts its rows by the
// states RETURNING reports. The batch is one atomic DELETE ... RETURNING
// statement: on a scan or iteration error it returns zero counts, because a
// statement that fails mid-stream rolls back, so no row it streamed is
// known-deleted. Counting the streamed prefix would overcount Deleted and
// NotListedReclaimed by up to one batch; database truth is unaffected either
// way, since the statement itself is all-or-nothing.
func (s ObservationStore) deleteExpiredBatch(ctx context.Context, now time.Time, grace time.Duration) (membership.SweepCounts, error) {
	rows, err := s.database.QueryContext(ctx, deleteExpiredObservationsQuery,
		now.UTC(), int64(grace/time.Second), expiredSweepBatchSize, string(membership.StateNotListed))
	if err != nil {
		return membership.SweepCounts{}, fmt.Errorf("delete expired repository selection observations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var counts membership.SweepCounts
	for rows.Next() {
		var state string
		if err := rows.Scan(&state); err != nil {
			return membership.SweepCounts{}, fmt.Errorf("delete expired repository selection observations: %w", err)
		}
		counts.Deleted++
		if membership.State(state) == membership.StateNotListed {
			counts.NotListedReclaimed++
		}
	}
	if err := rows.Err(); err != nil {
		return membership.SweepCounts{}, fmt.Errorf("delete expired repository selection observations: %w", err)
	}
	return counts, nil
}
