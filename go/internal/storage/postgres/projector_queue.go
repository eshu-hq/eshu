package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

const enqueueProjectorWorkQuery = `
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
) VALUES (
    $1, $2, $3, 'projector', $4, 'pending', 0, NULL, NULL, $5, NULL, NULL, NULL, NULL, NULL, '{}'::jsonb, $5, $5
)
ON CONFLICT (work_item_id) DO NOTHING
`

const claimProjectorWorkQuery = `
WITH candidate AS (
    SELECT work.work_item_id
    FROM fact_work_items AS work
    WHERE stage = 'projector'
      AND work.status IN ('pending', 'retrying', 'claimed', 'running')
      AND (work.visible_at IS NULL OR work.visible_at <= $1)
      AND (work.claim_until IS NULL OR work.claim_until <= $1)
      AND NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS inflight
          WHERE inflight.stage = 'projector'
            AND inflight.scope_id = work.scope_id
            AND inflight.work_item_id <> work.work_item_id
            AND inflight.status IN ('claimed', 'running')
            AND inflight.claim_until > $1
      )
      -- Every concurrent projector claimer for a scope must target the same
      -- oldest ready row. Otherwise FOR UPDATE SKIP LOCKED lets workers skip a
      -- locked older row and start a newer generation for the same repository.
      AND work.work_item_id = (
          SELECT same.work_item_id
          FROM fact_work_items AS same
          WHERE same.stage = 'projector'
            AND same.scope_id = work.scope_id
            AND same.status IN ('pending', 'retrying', 'claimed', 'running')
            AND (same.visible_at IS NULL OR same.visible_at <= $1)
            AND (same.claim_until IS NULL OR same.claim_until <= $1)
          ORDER BY
            CASE
              WHEN same.status IN ('claimed', 'running') AND same.claim_until <= $1 THEN 0
              ELSE 1
            END,
            same.updated_at ASC,
            same.work_item_id ASC
          LIMIT 1
      )
    ORDER BY
      CASE
        WHEN work.status IN ('claimed', 'running') AND work.claim_until <= $1 THEN 0
        ELSE 1
      END,
      work.updated_at ASC,
      work.work_item_id ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
),
claimed AS (
    UPDATE fact_work_items AS work
    SET status = 'claimed',
        attempt_count = work.attempt_count + 1,
        lease_owner = $2,
        claim_until = $3,
        last_attempt_at = $1,
        updated_at = $1
    FROM candidate
    WHERE work.work_item_id = candidate.work_item_id
    RETURNING work.scope_id, work.generation_id, work.attempt_count
)
SELECT
    scope.scope_id,
    scope.source_system,
    scope.scope_kind,
    COALESCE(scope.parent_scope_id, ''),
    COALESCE(scope.active_generation_id, ''),
    EXISTS (
        SELECT 1
        FROM scope_generations AS prior_generation
        WHERE prior_generation.scope_id = scope.scope_id
          AND prior_generation.generation_id <> claimed.generation_id
    ),
    scope.collector_kind,
    scope.partition_key,
    generation.generation_id,
    claimed.attempt_count,
    generation.observed_at,
    generation.ingested_at,
    generation.status,
    generation.trigger_kind,
    COALESCE(generation.freshness_hint, ''),
    COALESCE(scope.payload, '{}'::jsonb)
FROM claimed
JOIN ingestion_scopes AS scope
  ON scope.scope_id = claimed.scope_id
JOIN scope_generations AS generation
  ON generation.generation_id = claimed.generation_id
`

const supersedeProjectorActiveGenerationQuery = `
UPDATE scope_generations
SET status = 'superseded',
    superseded_at = $1
WHERE scope_id = $2
  AND generation_id <> $3
  AND status = 'active'
`

const activateProjectorGenerationQuery = `
UPDATE scope_generations
SET status = 'active',
    activated_at = COALESCE(activated_at, $1),
    superseded_at = NULL
WHERE scope_id = $2
  AND generation_id = $3
`

const updateProjectorScopeGenerationQuery = `
UPDATE ingestion_scopes
SET status = 'active',
    active_generation_id = $3,
    ingested_at = $1
WHERE scope_id = $2
`

const ackProjectorWorkItemQuery = `
UPDATE fact_work_items
SET status = 'succeeded',
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = NULL,
    updated_at = $1,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL
WHERE stage = 'projector'
  AND scope_id = $2
  AND generation_id = $3
  AND lease_owner = $4
  AND status IN ('claimed', 'running')
`

const heartbeatProjectorWorkQuery = `
UPDATE fact_work_items
SET status = 'running',
    claim_until = $1,
    updated_at = $2
WHERE stage = 'projector'
  AND scope_id = $3
  AND generation_id = $4
  AND lease_owner = $5
  AND status IN ('claimed', 'running')
`

const retryProjectorWorkQuery = `
UPDATE fact_work_items
SET status = 'retrying',
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = $5,
    next_attempt_at = $5,
    updated_at = $1,
    failure_class = $2,
    failure_message = $3,
    failure_details = $4
WHERE stage = 'projector'
  AND scope_id = $6
  AND generation_id = $7
  AND lease_owner = $8
  AND status IN ('claimed', 'running')
`

const failProjectorWorkQuery = `
WITH failed_generation AS (
    UPDATE scope_generations
    SET status = 'failed'
    WHERE generation_id = $6
      AND status IN ('pending', 'active')
),
scope_update AS (
    UPDATE ingestion_scopes
    SET status = CASE
            WHEN active_generation_id = $6 OR active_generation_id IS NULL THEN 'failed'
            ELSE status
        END,
        active_generation_id = CASE
            WHEN active_generation_id = $6 THEN NULL
            ELSE active_generation_id
        END,
        ingested_at = CASE
            WHEN active_generation_id = $6 OR active_generation_id IS NULL THEN $1
            ELSE ingested_at
        END
    WHERE scope_id = $5
)
UPDATE fact_work_items
SET status = 'dead_letter',
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = NULL,
    updated_at = $1,
    failure_class = $2,
    failure_message = $3,
    failure_details = $4
WHERE stage = 'projector'
  AND scope_id = $5
  AND generation_id = $6
  AND lease_owner = $7
  AND status IN ('claimed', 'running')
`

// ProjectorQueue provides projector-stage queue claim and ack behavior.
type ProjectorQueue struct {
	db            ExecQueryer
	LeaseOwner    string
	LeaseDuration time.Duration
	RetryDelay    time.Duration
	MaxAttempts   int
	Now           func() time.Time
}

// ErrProjectorClaimRejected means the claimed projector work item no longer
// belongs to the current lease owner, so heartbeat/ack/fail must stop.
var ErrProjectorClaimRejected = errors.New("projector work claim rejected")

// NewProjectorQueue constructs a Postgres-backed projector work queue.
func NewProjectorQueue(
	db ExecQueryer,
	leaseOwner string,
	leaseDuration time.Duration,
) ProjectorQueue {
	return ProjectorQueue{
		db:            db,
		LeaseOwner:    leaseOwner,
		LeaseDuration: leaseDuration,
	}
}

// Enqueue inserts one durable source-local projection work item.
func (q ProjectorQueue) Enqueue(
	ctx context.Context,
	scopeID string,
	generationID string,
) error {
	if q.db == nil {
		return errors.New("projector queue database is required")
	}
	if scopeID == "" {
		return errors.New("projector queue scope_id is required")
	}
	if generationID == "" {
		return errors.New("projector queue generation_id is required")
	}

	now := q.now()
	_, err := q.db.ExecContext(
		ctx,
		enqueueProjectorWorkQuery,
		projectorWorkItemID(scopeID, generationID),
		scopeID,
		generationID,
		"source_local",
		now,
	)
	if err != nil {
		return fmt.Errorf("enqueue projector work: %w", err)
	}

	return nil
}

// Claim implements projector.ProjectorWorkSource over fact_work_items.
func (q ProjectorQueue) Claim(ctx context.Context) (projector.ScopeGenerationWork, bool, error) {
	if err := q.validate(); err != nil {
		return projector.ScopeGenerationWork{}, false, err
	}

	now := q.now()
	rows, err := q.db.QueryContext(ctx, claimProjectorWorkQuery, now, q.LeaseOwner, now.Add(q.LeaseDuration))
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

// Ack marks one claimed projector work item as succeeded.
func (q ProjectorQueue) Ack(
	ctx context.Context,
	work projector.ScopeGenerationWork,
	_ projector.Result,
) error {
	if err := q.validate(); err != nil {
		return err
	}

	beginner, ok := q.db.(Beginner)
	if !ok {
		return errors.New("projector queue database must support Begin for ack")
	}

	tx, err := beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("ack projector work: begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	now := q.now()
	steps := []struct {
		query string
		op    string
		args  []any
	}{
		{
			query: supersedeProjectorActiveGenerationQuery,
			op:    "supersede active generation",
			args:  []any{now, work.Scope.ScopeID, work.Generation.GenerationID},
		},
		{
			query: activateProjectorGenerationQuery,
			op:    "activate target generation",
			args:  []any{now, work.Scope.ScopeID, work.Generation.GenerationID},
		},
		{
			query: updateProjectorScopeGenerationQuery,
			op:    "update scope active generation",
			args:  []any{now, work.Scope.ScopeID, work.Generation.GenerationID},
		},
		{
			query: ackProjectorWorkItemQuery,
			op:    "mark projector work succeeded",
			args:  []any{now, work.Scope.ScopeID, work.Generation.GenerationID, q.LeaseOwner},
		},
	}
	for _, step := range steps {
		if _, err := tx.ExecContext(ctx, step.query, step.args...); err != nil {
			return fmt.Errorf("ack projector work: %s: %w", step.op, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ack projector work: commit: %w", err)
	}
	committed = true

	return nil
}

// Heartbeat renews one claimed projector work item so long-running projection
// work keeps exclusive ownership until Ack or Fail completes.
func (q ProjectorQueue) Heartbeat(ctx context.Context, work projector.ScopeGenerationWork) error {
	if err := q.validate(); err != nil {
		return err
	}

	now := q.now()
	result, err := q.db.ExecContext(
		ctx,
		heartbeatProjectorWorkQuery,
		now.Add(q.LeaseDuration),
		now,
		work.Scope.ScopeID,
		work.Generation.GenerationID,
		q.LeaseOwner,
	)
	if err != nil {
		return fmt.Errorf("heartbeat projector work: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("heartbeat projector work: rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrProjectorClaimRejected
	}
	return nil
}

// Fail marks one claimed projector work item as failed.
func (q ProjectorQueue) Fail(
	ctx context.Context,
	work projector.ScopeGenerationWork,
	cause error,
) error {
	if err := q.validate(); err != nil {
		return err
	}
	if cause == nil {
		return errors.New("projector failure cause is required")
	}

	query := failProjectorWorkQuery
	failureClass, failureMessage, failureDetails := queueFailureMetadata(cause, "projection_failed")
	args := []any{
		q.now(),
		failureClass,
		failureMessage,
		failureDetails,
		work.Scope.ScopeID,
		work.Generation.GenerationID,
		q.LeaseOwner,
	}

	if projector.IsRetryable(cause) && work.AttemptCount < q.maxAttempts() {
		query = retryProjectorWorkQuery
		failureClass = "projection_retryable"
		args = []any{
			q.now(),
			failureClass,
			failureMessage,
			failureDetails,
			q.now().Add(q.retryDelay()),
			work.Scope.ScopeID,
			work.Generation.GenerationID,
			q.LeaseOwner,
		}
	}

	_, err := q.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("fail projector work: %w", err)
	}

	return nil
}

func sanitizeFailureText(text string) string {
	if text == "" {
		return text
	}
	sanitized := strings.ToValidUTF8(text, "")
	return strings.ReplaceAll(sanitized, "\x00", "")
}

func (q ProjectorQueue) validate() error {
	if q.db == nil {
		return errors.New("projector queue database is required")
	}
	if q.LeaseOwner == "" {
		return errors.New("projector queue lease owner is required")
	}
	if q.LeaseDuration <= 0 {
		return errors.New("projector queue lease duration must be positive")
	}

	return nil
}

func (q ProjectorQueue) now() time.Time {
	if q.Now != nil {
		return q.Now().UTC()
	}

	return time.Now().UTC()
}

func (q ProjectorQueue) retryDelay() time.Duration {
	if q.RetryDelay > 0 {
		return q.RetryDelay
	}

	return 30 * time.Second
}

func (q ProjectorQueue) maxAttempts() int {
	if q.MaxAttempts > 0 {
		return q.MaxAttempts
	}

	return 3
}

func scanProjectorWork(rows Rows) (projector.ScopeGenerationWork, error) {
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

func projectorWorkItemID(scopeID string, generationID string) string {
	return fmt.Sprintf("projector_%s_%s", scopeID, generationID)
}

func projectorScopeMetadata(rawPayload []byte) map[string]string {
	payload, err := unmarshalPayload(rawPayload)
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
