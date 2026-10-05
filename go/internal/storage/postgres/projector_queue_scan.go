// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/payload"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// scanProjectorWork decodes one claimed projector work row (scope, active
// generation metadata, and payload-derived scope metadata) into a
// projector.ScopeGenerationWork. Split out of projector_queue.go to keep that
// file under the repo's 500-line cap (see also projector_queue_config_state_drift_trigger_hook.go).
func scanProjectorWork(rows db.Rows) (projector.ScopeGenerationWork, error) {
	var work projector.ScopeGenerationWork
	var scopeKind string
	var collectorKind string
	var generationStatus string
	var triggerKind string
	var rawPayload []byte

	if err := rows.Scan(
		&work.Scope.ScopeID,
		&work.Scope.SourceSystem,
		&scopeKind,
		&work.Scope.ParentScopeID,
		&work.Scope.ActiveGenerationID,
		&work.Scope.PreviousGenerationExists,
		&collectorKind,
		&work.Scope.PartitionKey,
		&work.Generation.GenerationID,
		&work.AttemptCount,
		&work.Generation.ObservedAt,
		&work.Generation.IngestedAt,
		&generationStatus,
		&triggerKind,
		&work.Generation.FreshnessHint,
		&rawPayload,
	); err != nil {
		return projector.ScopeGenerationWork{}, err
	}

	work.Scope.ScopeKind = scope.ScopeKind(scopeKind)
	work.Scope.CollectorKind = scope.CollectorKind(collectorKind)
	work.Generation.ScopeID = work.Scope.ScopeID
	work.Generation.Status = scope.GenerationStatus(generationStatus)
	work.Generation.TriggerKind = scope.TriggerKind(triggerKind)
	work.Generation.ObservedAt = work.Generation.ObservedAt.UTC()
	work.Generation.IngestedAt = work.Generation.IngestedAt.UTC()
	work.Scope.Metadata = projectorScopeMetadata(rawPayload)

	return work, nil
}

// projectorWorkItemID is the projector work item id of a scope generation.
// activation/backlog.go's catchUpQuery builds the same string in SQL
// ('projector_' || scope_id || '_' || generation_id) for owed obligations;
// change both together (TestActivationObligationCatchUpLive asserts they agree).
func projectorWorkItemID(scopeID string, generationID string) string {
	return fmt.Sprintf("projector_%s_%s", scopeID, generationID)
}

func projectorScopeMetadata(rawPayload []byte) map[string]string {
	payload, err := payloadstore.UnmarshalPayload(rawPayload)
	if err != nil || len(payload) == 0 {
		return nil
	}

	metadata := make(map[string]string, len(payload))
	for key, value := range payload {
		switch typed := value.(type) {
		case string:
			if typed != "" {
				metadata[key] = typed
			}
		case fmt.Stringer:
			text := typed.String()
			if text != "" {
				metadata[key] = text
			}
		}
	}
	if len(metadata) == 0 {
		return nil
	}

	return metadata
}

// projectorClaimConflictAttempts bounds how many times Claim runs the claim
// statement when Postgres aborts it with a deadlock or serialization failure.
// The statement is atomic, so an aborted attempt changed nothing.
const projectorClaimConflictAttempts = 3

// defaultProjectorClaimConflictBackoff is the base retry delay when
// ProjectorQueue.ClaimConflictBackoff is unset.
const defaultProjectorClaimConflictBackoff = 25 * time.Millisecond

// claimOnce runs the claim statement once and scans at most one work item.
func (q ProjectorQueue) claimOnce(ctx context.Context) (projector.ScopeGenerationWork, bool, error) {
	now := q.now()
	rows, err := q.database.QueryContext(
		ctx,
		claimProjectorWorkQuery,
		now,
		q.LeaseOwner,
		now.Add(q.LeaseDuration),
		q.ClaimSourceSystem,
	)
	if err != nil {
		return projector.ScopeGenerationWork{}, false, fmt.Errorf("claim projector work: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return projector.ScopeGenerationWork{}, false, fmt.Errorf("claim projector work: %w", err)
		}
		return projector.ScopeGenerationWork{}, false, nil
	}

	work, err := scanProjectorWork(rows)
	if err != nil {
		return projector.ScopeGenerationWork{}, false, fmt.Errorf("claim projector work: %w", err)
	}
	if err := rows.Err(); err != nil {
		return projector.ScopeGenerationWork{}, false, fmt.Errorf("claim projector work: %w", err)
	}

	return work, true, nil
}

// claimConflictClass returns the failure_class for a retryable claim
// conflict, or "" when err is nil or not a deadlock/serialization failure.
func claimConflictClass(err error) string {
	var pgErr *pgconn.PgError
	if err == nil || !errors.As(err, &pgErr) {
		return ""
	}
	switch pgErr.Code {
	case "40P01":
		return "deadlock"
	case "40001":
		return "serialization_failure"
	default:
		return ""
	}
}

// claimConflictDelay returns attempt*base plus jitter in [0, base).
func (q ProjectorQueue) claimConflictDelay(attempt int) time.Duration {
	base := q.ClaimConflictBackoff
	if base <= 0 {
		base = defaultProjectorClaimConflictBackoff
	}
	jitter := time.Duration(q.jitterSource()() * float64(base))
	return time.Duration(attempt)*base + jitter
}

// recordClaimConflictRetry emits the retry counter and a warning log that an
// operator can join to the Postgres deadlock report by failure_class.
func (q ProjectorQueue) recordClaimConflictRetry(ctx context.Context, class string, attempt int, err error) {
	if q.Instruments != nil && q.Instruments.QueueClaimConflictRetries != nil {
		q.Instruments.QueueClaimConflictRetries.Add(ctx, 1, metric.WithAttributes(
			attribute.String("queue", "projector"),
			attribute.String(telemetry.MetricDimensionFailureClass, class),
		))
	}
	slog.WarnContext(ctx, "projector claim conflict; retrying claim",
		slog.String("queue", "projector"),
		slog.String(telemetry.LogKeyFailureClass, class),
		slog.Int("attempt", attempt),
		slog.Int("max_attempts", projectorClaimConflictAttempts),
		slog.String("lease_owner", q.LeaseOwner),
		slog.String("error", err.Error()),
	)
}

// sleepContext waits for d or until ctx ends, returning ctx.Err() on cancel.
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// activateAckGeneration runs Ack's activation statement and reports whether
// the target generation was activated. It is the last statement in Ack's lock
// order (scope row, work row, other generation rows, target generation row)
// and adds no lock of its own. false means the generation is superseded: the
// status predicate on the locked row failed, either on this statement's
// snapshot or on the EvalPlanQual recheck after a concurrent supersede
// committed.
func (q ProjectorQueue) activateAckGeneration(
	ctx context.Context,
	tx db.Transaction,
	work projector.ScopeGenerationWork,
	now time.Time,
) (bool, error) {
	result, err := tx.ExecContext(ctx, activateProjectorGenerationQuery,
		now, work.Scope.ScopeID, work.Generation.GenerationID)
	if err != nil {
		return false, fmt.Errorf("ack projector work: activate target generation: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("ack projector work: activate target generation: rows affected: %w", err)
	}
	return rows == 1, nil
}

// refuseSupersededAck handles an Ack whose generation is already superseded.
// It rolls tx back, which undoes the scope repoint, the work succeeded mark,
// and any supersede of the published generation the earlier statements made.
// It then marks the work item superseded in one statement and returns
// failure.ErrWorkSuperseded.
//
// Rolling back instead of using a savepoint keeps subtransactions off the Ack
// hot path; this branch only runs when replayed or raced work reaches Ack. If
// the mark statement fails, the item stays claimed until its lease expires and
// the next attempt's Ack refuses again, so the outcome converges.
func (q ProjectorQueue) refuseSupersededAck(
	ctx context.Context,
	tx db.Transaction,
	work projector.ScopeGenerationWork,
	now time.Time,
) error {
	if err := tx.Rollback(); err != nil {
		return fmt.Errorf("ack projector work: roll back superseded generation: %w", err)
	}
	result, err := q.database.ExecContext(ctx, markProjectorAckSupersededQuery,
		now, work.Scope.ScopeID, work.Generation.GenerationID, q.LeaseOwner, work.AttemptCount)
	if err != nil {
		return fmt.Errorf("ack projector work: mark superseded: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("ack projector work: mark superseded: rows affected: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("ack projector work: %w", ErrProjectorClaimRejected)
	}
	recordSupersededGenerationFence(ctx, q.Instruments, projectorAckGenerationSupersededClass, 1)
	return fmt.Errorf("ack projector work: generation %s is superseded: %w",
		work.Generation.GenerationID, projectorWorkSupersededError{failureClass: projectorAckGenerationSupersededClass})
}

// recordSupersededGenerationFence counts work a superseded-generation fence
// stopped, labeled by its closed failure_class. Nil instruments are a no-op.
func recordSupersededGenerationFence(
	ctx context.Context,
	instruments *telemetry.Instruments,
	failureClass string,
	count int,
) {
	if instruments == nil || instruments.SupersededGenerationFence == nil || count <= 0 {
		return
	}
	instruments.SupersededGenerationFence.Add(context.WithoutCancel(ctx), int64(count),
		metric.WithAttributes(telemetry.AttrFailureClass(failureClass)))
}

// projectorWorkSupersededError is failure.ErrWorkSuperseded carrying the
// failure_class the queue wrote on the work row. The projector service logs
// that class, so a log search for a row's class finds its refusal.
type projectorWorkSupersededError struct {
	failureClass string
}

// Error reports the superseded outcome and its failure class.
func (e projectorWorkSupersededError) Error() string {
	return fmt.Sprintf("%s (failure_class=%s)", failure.ErrWorkSuperseded, e.failureClass)
}

// Unwrap keeps errors.Is(err, failure.ErrWorkSuperseded) true for callers.
func (e projectorWorkSupersededError) Unwrap() error { return failure.ErrWorkSuperseded }

// FailureClass returns the bounded failure_class recorded on the work row.
func (e projectorWorkSupersededError) FailureClass() string { return e.failureClass }

// supersedeRunningWork runs Heartbeat's supersede statement. It returns nil
// when the work may keep running, and an error wrapping
// failure.ErrWorkSuperseded when the statement ended it: either a newer
// generation replaces it, or its own generation is already superseded (#7130).
// The second case counts on eshu_dp_superseded_generation_fence_total.
func (q ProjectorQueue) supersedeRunningWork(
	ctx context.Context,
	work projector.ScopeGenerationWork,
	now time.Time,
) error {
	rows, err := q.database.QueryContext(
		ctx,
		supersedeRunningProjectorWorkQuery,
		now,
		work.Scope.ScopeID,
		work.Generation.GenerationID,
		q.LeaseOwner,
		work.AttemptCount,
	)
	if err != nil {
		return fmt.Errorf("supersede running projector work: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return fmt.Errorf("supersede running projector work: %w", err)
		}
		return nil
	}
	var generationStatus string
	if err := rows.Scan(&generationStatus); err != nil {
		return fmt.Errorf("supersede running projector work: scan: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("supersede running projector work: %w", err)
	}

	class := "projector_superseded_by_newer_generation"
	if generationStatus == "superseded" {
		class = projectorHeartbeatGenerationSupersededClass
		recordSupersededGenerationFence(ctx, q.Instruments, class, 1)
	}
	return fmt.Errorf("heartbeat projector work: generation %s: %w",
		work.Generation.GenerationID, projectorWorkSupersededError{failureClass: class})
}

// supersedeRunningProjectorWorkQuery runs on every heartbeat. It never waits:
// ingestion holds the scope row while it streams facts, and a waiting heartbeat
// would let the lease lapse, so every lock is SKIP LOCKED and a held row defers
// supersession to a later heartbeat. NO KEY UPDATE does not conflict with
// foreign-key KEY SHARE locks from unrelated child inserts.
//
// Two triggers stop the running work. A newer pending or active generation
// replaces a pending or active one only while it has not started writing the
// graph (projection_write_started_at IS NULL, #7389): the heartbeat never
// abandons a started projection write to a newer generation, so the attempt
// runs to Ack and a stale delta is refused at the #7319 preflight. Every other
// retirement of a started write (claim-path or Ack obsolete supersede, dead
// letter, Ack refusal) is healed by a forced full snapshot, the collector's
// graph_dirty reconciliation. And the work's own generation is already
// superseded (#7130), marker or not: a newer Ack retired it, typically while
// this worker's lease had expired. The statement returns one row, carrying the
// generation status the work was stopped under, when it superseded the work.
//
// The gate lives in locked_generation, on the locked row: EvalPlanQual
// rechecks only the rows a statement locks or updates, and reads every other
// row from the snapshot, so a gate on an unlocked joined row missed a marker
// committed after the snapshot and let both win (341 of 1,000 races). An
// uncommitted marker holds the row, and SKIP LOCKED makes this a no-op. The
// full predicate keeps the steady-state heartbeat off the generation row.
//
// Lock order: scope row, own work row (full lease fence), own generation row,
// each FOR NO KEY UPDATE SKIP LOCKED, then the UPDATEs of the rows it holds.
// The heartbeat takes no lock it can wait on, so it cannot join a wait cycle.
// Every blocking path takes the scope row first; the delta-baseline refusal
// and Ack share work-then-generation. Generation-before-work deadlocked
// (40P01) with the refusal, which takes no scope row, and a heartbeat error
// stops the worker pool (TestProjectorHeartbeatNeverDeadlocksWithBaselineRefusal).
// The lease renew that follows, heartbeatProjectorWorkQuery, is a single-row
// blocking UPDATE that holds nothing else.
const supersedeRunningProjectorWorkQuery = `
WITH locked_scope AS MATERIALIZED (
    SELECT scope_id
    FROM ingestion_scopes
    WHERE scope_id = $2
    FOR NO KEY UPDATE SKIP LOCKED
),
locked_work AS MATERIALIZED (
    SELECT work.work_item_id, work.scope_id, work.generation_id
    FROM locked_scope AS scope
    JOIN fact_work_items AS work ON work.scope_id = scope.scope_id
    WHERE work.stage = 'projector'
      AND work.generation_id = $3
      AND work.lease_owner = $4
      AND work.attempt_count = $5
      AND work.status IN ('claimed', 'running')
    FOR NO KEY UPDATE OF work SKIP LOCKED
),
locked_generation AS MATERIALIZED (
    SELECT generation.generation_id
    FROM locked_work AS owned
    JOIN scope_generations AS generation ON generation.scope_id = owned.scope_id
    WHERE generation.generation_id = $3
      AND (
          generation.status = 'superseded'
          OR (
              generation.status IN ('pending', 'active')
              AND generation.projection_write_started_at IS NULL
              AND EXISTS (
                  SELECT 1
                  FROM scope_generations AS newer
                  WHERE newer.scope_id = generation.scope_id
                    AND newer.generation_id <> generation.generation_id
                    AND newer.status IN ('pending', 'active')
                    AND (
                        newer.ingested_at > generation.ingested_at
                        OR (
                            newer.ingested_at = generation.ingested_at
                            AND newer.generation_id > generation.generation_id
                        )
                    )
              )
          )
      )
    FOR NO KEY UPDATE OF generation SKIP LOCKED
),
superseded_work AS (
UPDATE fact_work_items AS work
SET status = 'superseded',
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = NULL,
    next_attempt_at = NULL,
    updated_at = $1,
    failure_class = CASE
        WHEN current_generation.status = 'superseded' THEN '` + projectorHeartbeatGenerationSupersededClass + `'
        ELSE 'projector_superseded_by_newer_generation'
    END,
    failure_message = CASE
        WHEN current_generation.status = 'superseded' THEN 'running projector work stopped: generation already superseded'
        ELSE 'running projector work superseded by newer same-scope generation'
    END,
    failure_details = jsonb_build_object(
        'scope_id', work.scope_id,
        'work_item_id', work.work_item_id,
        'generation_id', work.generation_id,
        'generation_status', current_generation.status
    ) || ` + priorFailureWorkSQL + `
FROM locked_work AS owned,
     locked_generation AS locked_generation,
     scope_generations AS current_generation
WHERE work.work_item_id = owned.work_item_id
  AND work.stage = 'projector'
  AND current_generation.generation_id = locked_generation.generation_id
  AND work.scope_id = owned.scope_id
  AND work.generation_id = $3
  AND work.lease_owner = $4
  AND work.attempt_count = $5
  AND work.status IN ('claimed', 'running')
  AND current_generation.scope_id = work.scope_id
  AND current_generation.generation_id = work.generation_id
  AND (
      current_generation.status = 'superseded'
      OR (
          current_generation.status IN ('pending', 'active')
          AND current_generation.projection_write_started_at IS NULL
          AND EXISTS (
              SELECT 1
              FROM scope_generations AS newer
              WHERE newer.scope_id = current_generation.scope_id
                AND newer.generation_id <> current_generation.generation_id
                AND newer.status IN ('pending', 'active')
                AND (
                    newer.ingested_at > current_generation.ingested_at
                    OR (
                        newer.ingested_at = current_generation.ingested_at
                        AND newer.generation_id > current_generation.generation_id
                    )
                )
          )
      )
  )
  RETURNING work.generation_id, current_generation.status AS generation_status
)
UPDATE scope_generations AS generation
-- An active generation remains published until successor Ack changes the scope
-- pointer in the same transaction, and a superseded one is terminal. Still
-- update this row so the statement returns the superseded work to Heartbeat
-- for every generation status; only a pending generation changes.
SET status = CASE WHEN generation.status = 'pending' THEN 'superseded' ELSE generation.status END,
    superseded_at = CASE WHEN generation.status = 'pending' THEN $1 ELSE generation.superseded_at END
FROM superseded_work
WHERE generation.generation_id = superseded_work.generation_id
  AND generation.status IN ('pending', 'active', 'superseded')
RETURNING superseded_work.generation_status
`
