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
	superseded, err := q.supersedeRunningWorkIfNewerGenerationExists(ctx, work, now)
	if err != nil {
		return err
	}
	if superseded {
		return projector.ErrWorkSuperseded
	}

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

func (q ProjectorQueue) supersedeRunningWorkIfNewerGenerationExists(
	ctx context.Context,
	work projector.ScopeGenerationWork,
	now time.Time,
) (bool, error) {
	result, err := q.db.ExecContext(
		ctx,
		supersedeRunningProjectorWorkQuery,
		now,
		work.Scope.ScopeID,
		work.Generation.GenerationID,
		q.LeaseOwner,
	)
	if err != nil {
		return false, fmt.Errorf("supersede running projector work: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("supersede running projector work: rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return false, nil
	}
	return true, nil
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
