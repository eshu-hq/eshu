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
		work.Generation.GenerationID, failure.ErrWorkSuperseded)
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
