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
