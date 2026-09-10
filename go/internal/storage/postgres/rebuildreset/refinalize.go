// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq
//
// Refinalize coordination: the ordered prelude a rebuild-from-facts runs
// inside its transaction before the dedup resets in reset.go.
//
// The caller (postgres.RecoveryStore today) owns the transaction and the
// result assembly; this package owns the statement order and the in-flight
// reducer fence, so every statement binds the one generation set read first.
// Queryer and Rows are narrow local interfaces so this package never imports
// its caller: any QueryContext source whose rows offer Next/Scan/Err/Close
// satisfies them implicitly.
package rebuildreset

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

const (
	// DefaultRefinalizeDrainTimeout bounds the in-flight reducer drain wait.
	// A live reducer claim holds for about its lease TTL (60s by default)
	// plus heartbeat extensions, so five minutes lets a slow-but-live
	// resolver finish while still failing fast enough for an operator to
	// notice and retry. The abort is safe to retry: the refinalize
	// transaction rolls back, so nothing is half-retired.
	DefaultRefinalizeDrainTimeout = 5 * time.Minute

	// DefaultRefinalizeDrainPollInterval is the poll interval of the drain
	// wait. The poll is one indexed COUNT on the recovery path only; it never
	// runs on the hot claim/drain path.
	DefaultRefinalizeDrainPollInterval = 500 * time.Millisecond
)

// Rows is the narrow read surface the coordination queries need.
type Rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close() error
}

// Queryer is the narrow query surface the coordination reads need.
type Queryer interface {
	QueryContext(context.Context, string, ...any) (Rows, error)
}

// refinalizeScopeProjectionsQuery re-enqueues projector work by inserting one
// pending work item per generation in the set the refinalize already
// materialized. $1 is the timestamp; $2 and $3 are the index-aligned scope-id
// and generation-id arrays. It reads no table, which is the point: see
// ReadAffectedGenerations.
//
// ON CONFLICT (work_item_id) DO UPDATE is what makes the rebuild restartable.
// The work_item_id is derived from scope_id and generation_id, so a second
// rebuild over the same generations resets the same rows to pending instead of
// inserting duplicates. An interrupted rebuild is re-runnable with the same
// command.
const refinalizeScopeProjectionsQuery = `
INSERT INTO fact_work_items (
    work_item_id,
    scope_id,
    generation_id,
    stage,
    domain,
    status,
    attempt_count,
    lease_owner,
    claim_until,
    visible_at,
    last_attempt_at,
    next_attempt_at,
    failure_class,
    failure_message,
    failure_details,
    payload,
    created_at,
    updated_at
)
SELECT
    'refinalize_' || scope.scope_id || '_' || scope.generation_id,
    scope.scope_id,
    scope.generation_id,
    'projector',
    'source_local',
    'pending',
    0,
    NULL,
    NULL,
    $1,
    NULL,
    NULL,
    NULL,
    NULL,
    NULL,
    '{}'::jsonb,
    $1,
    $1
FROM unnest($2::text[], $3::text[]) AS scope(scope_id, generation_id)
ON CONFLICT (work_item_id) DO UPDATE
SET status = 'pending',
    attempt_count = 0,
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = EXCLUDED.visible_at,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL,
    updated_at = EXCLUDED.updated_at
RETURNING scope_id
`

// refinalizeInflightReducersQuery counts reducer work items holding a live
// lease on the refinalized pairs. The live-lease predicate mirrors the claim
// system: claim_until > now() means a worker is, or may still be, executing,
// while NULL or expired means the row is reclaimable and whoever reclaims it
// resolves post-retirement. now() is the transaction start, so every poll in
// one refinalize sees the same lease clock as the retirement statement's
// atomic guard in reset.go.
const refinalizeInflightReducersQuery = `
SELECT COUNT(*)
FROM fact_work_items AS w
WHERE w.stage = 'reducer'
  AND w.status IN ('claimed', 'running')
  AND w.claim_until > now()
  AND (w.scope_id, w.generation_id) IN (
    SELECT * FROM unnest($1::text[], $2::text[]) AS affected(scope_id, generation_id)
  )
`

// refinalizeInflightReducersSampleQuery names the busiest in-flight pair for
// the abort error, so the operator knows which scope and generation to wait
// for. It runs only on the abort path, never in the poll loop.
const refinalizeInflightReducersSampleQuery = `
SELECT w.scope_id, w.generation_id, COUNT(*)
FROM fact_work_items AS w
WHERE w.stage = 'reducer'
  AND w.status IN ('claimed', 'running')
  AND w.claim_until > now()
  AND (w.scope_id, w.generation_id) IN (
    SELECT * FROM unnest($1::text[], $2::text[]) AS affected(scope_id, generation_id)
  )
GROUP BY w.scope_id, w.generation_id
ORDER BY COUNT(*) DESC
LIMIT 1
`

// InflightReducersError reports a refinalize aborted because reducer work
// still held live leases on the generations it was about to retire. Retiring
// under a running resolver lets it re-activate the retired generation with
// stale rows while its success ack dedupes the re-emitted intent (Codex #6184
// P1), so the abort rolls the whole refinalize back and the operator retries
// after the in-flight work drains. Match it with errors.As; the message
// carries the busiest scope and generation.
type InflightReducersError struct {
	// ScopeID and GenerationID sample the busiest in-flight pair. Empty when
	// the pair could not be sampled.
	ScopeID      string
	GenerationID string

	// Inflight counts reducer rows holding live leases on the refinalized set.
	Inflight int

	// Waited reports how long the drain wait ran before giving up. Zero when
	// the atomic retirement guard tripped instead: a claim committed between
	// the last drain poll and the retirement statement.
	Waited time.Duration

	// Timeout is the drain bound that was exceeded. Zero on a guard trip.
	Timeout time.Duration
}

// Error describes which in-flight work blocked the refinalize and what to do.
func (e *InflightReducersError) Error() string {
	if e.Timeout > 0 {
		return fmt.Sprintf(
			"refinalize aborted: %d reducer work item(s) still hold live leases after waiting %s "+
				"(e.g. scope %q generation %q); retirement would strand their in-flight resolution — "+
				"retry after they drain",
			e.Inflight, e.Waited.Round(time.Second), e.ScopeID, e.GenerationID,
		)
	}
	return fmt.Sprintf(
		"refinalize aborted: %d reducer work item(s) claimed with live leases during commit "+
			"(e.g. scope %q generation %q); the retirement guard held the generation active — "+
			"retry after they drain",
		e.Inflight, e.ScopeID, e.GenerationID,
	)
}

// ReadAffectedGenerations reads the (scope_id, generation_id) set one
// refinalize covers, once, so the enqueue, the drain fence, and the four
// resets all bind the same rows. Re-deriving the set per statement would give
// each one its own READ COMMITTED snapshot, and an ingester activating a
// generation mid-refinalize could then leave the enqueue rebuilding G1 while
// a reset cleared G2.
//
// It takes no row locks: a concurrent activation wins and falls outside this
// refinalize, which is cheaper than putting an ingester behind a rebuild.
func ReadAffectedGenerations(
	ctx context.Context,
	q Queryer,
	filter recovery.RefinalizeFilter,
) (Generations, error) {
	query, args := AffectedGenerationsQuery(filter)

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return Generations{}, fmt.Errorf("refinalize affected generations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var generations Generations
	for rows.Next() {
		var scopeID, generationID string
		if scanErr := rows.Scan(&scopeID, &generationID); scanErr != nil {
			return Generations{}, fmt.Errorf("refinalize affected generations: %w", scanErr)
		}
		generations.Append(scopeID, generationID)
	}
	if err := rows.Err(); err != nil {
		return Generations{}, fmt.Errorf("refinalize affected generations: %w", err)
	}

	return generations, nil
}

// EnqueueProjectorWork runs the projector re-enqueue and returns the scope
// IDs it queued. The rows are fully consumed and closed before the reset
// statements run, because database/sql forbids a second statement on a
// transaction while its Rows are open.
func EnqueueProjectorWork(
	ctx context.Context,
	q Queryer,
	generations Generations,
	now time.Time,
) ([]string, error) {
	args := append([]any{now.UTC()}, generations.Args()...)

	rows, err := q.QueryContext(ctx, refinalizeScopeProjectionsQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("refinalize scope projections: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var scopeIDs []string
	for rows.Next() {
		var id string
		if scanErr := rows.Scan(&id); scanErr != nil {
			return nil, fmt.Errorf("refinalize scope projections: %w", scanErr)
		}
		scopeIDs = append(scopeIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("refinalize scope projections: %w", err)
	}

	return scopeIDs, nil
}

// WaitForReducerDrain blocks until no reducer work item holds a live lease on
// the refinalized generations, or the drain bound expires, or the context
// ends. Call it inside the refinalize transaction, after
// ReadAffectedGenerations and before any write statement: the transaction
// holds no row locks at this point (the generation read takes none), so the
// wait blocks no ingester or worker — it only holds one pooled connection,
// for at most the drain bound. Non-positive timeout or poll interval selects
// the defaults. An empty generation set returns immediately.
func WaitForReducerDrain(
	ctx context.Context,
	q Queryer,
	generations Generations,
	timeout, poll time.Duration,
) error {
	if generations.Len() == 0 {
		return nil
	}
	if timeout <= 0 {
		timeout = DefaultRefinalizeDrainTimeout
	}
	if poll <= 0 {
		poll = DefaultRefinalizeDrainPollInterval
	}
	deadline := time.Now().Add(timeout)

	for {
		inflight, err := countInflightReducers(ctx, q, generations)
		if err != nil {
			return fmt.Errorf("refinalize reducer drain: %w", err)
		}
		if inflight == 0 {
			return nil
		}
		if now := time.Now(); !now.Before(deadline) {
			return newInflightReducersError(ctx, q, generations, inflight, timeout, timeout)
		}
		timer := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("refinalize reducer drain: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

// AssertRetirementFenced distinguishes a tripped atomic retirement guard from
// a genuine no-op. The retirement UPDATE in reset.go retires nothing when its
// NOT EXISTS guard sees a live lease, so a zero retired count with live
// leases outstanding means a claim committed between the drain wait and the
// retirement statement: abort rather than commit a refinalize that re-enqueued
// work over a generation it did not retire. A zero count with no live leases
// is the convergent re-run and commits, as does any positive count.
func AssertRetirementFenced(
	ctx context.Context,
	q Queryer,
	generations Generations,
	retired int,
) error {
	if retired != 0 || generations.Len() == 0 {
		return nil
	}
	inflight, err := countInflightReducers(ctx, q, generations)
	if err != nil {
		return fmt.Errorf("refinalize retirement fence: %w", err)
	}
	if inflight == 0 {
		return nil
	}
	return newInflightReducersError(ctx, q, generations, inflight, 0, 0)
}

// newInflightReducersError samples the busiest in-flight pair for the abort
// error. A sampling failure still aborts with the count: the fence must never
// pass silently because its own diagnostics failed.
func newInflightReducersError(
	ctx context.Context,
	q Queryer,
	generations Generations,
	inflight int,
	waited, timeout time.Duration,
) error {
	fenceErr := &InflightReducersError{
		Inflight: inflight,
		Waited:   waited,
		Timeout:  timeout,
	}
	rows, err := q.QueryContext(ctx, refinalizeInflightReducersSampleQuery, generations.Args()...)
	if err != nil {
		return fenceErr
	}
	defer func() { _ = rows.Close() }()
	var sampleCount int
	if rows.Next() {
		if scanErr := rows.Scan(&fenceErr.ScopeID, &fenceErr.GenerationID, &sampleCount); scanErr != nil {
			return fenceErr
		}
	}
	return fenceErr
}

// countInflightReducers counts reducer rows holding live leases on the
// refinalized pairs. It shares its predicate with the retirement statement's
// atomic guard by construction: both filter stage, live-lease statuses,
// claim_until against the transaction clock, and the same generation arrays.
func countInflightReducers(
	ctx context.Context,
	q Queryer,
	generations Generations,
) (int, error) {
	rows, err := q.QueryContext(ctx, refinalizeInflightReducersQuery, generations.Args()...)
	if err != nil {
		return 0, fmt.Errorf("count in-flight reducers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var count int
	if rows.Next() {
		if scanErr := rows.Scan(&count); scanErr != nil {
			return 0, fmt.Errorf("scan in-flight reducer count: %w", scanErr)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate in-flight reducer count: %w", err)
	}
	return count, nil
}
