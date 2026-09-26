// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/queue"
	"github.com/eshu-hq/eshu/go/internal/telemetry"

	"go.opentelemetry.io/otel/metric"
)

// ProjectorQueue provides projector-stage queue claim and ack behavior.
type ProjectorQueue struct {
	database          db.ExecQueryer
	LeaseOwner        string
	LeaseDuration     time.Duration
	RetryDelay        time.Duration
	MaxAttempts       int
	ClaimSourceSystem string
	Now               func() time.Time

	// MaxRetryDelay caps the exponential-backoff retry term computed by
	// Fail. Zero/unset falls back to a 1-hour cap (see retryMaxDelay).
	MaxRetryDelay time.Duration
	// JitterFraction scales the random jitter added on top of the
	// exponential backoff term, relative to RetryDelay: jitter is drawn
	// uniformly from [0, RetryDelay*JitterFraction). Zero means no jitter
	// (legacy fixed-delay behavior). Callers wired through
	// runtime.LoadRetryPolicyConfig get 0.1 by default (#4450); a caller
	// that constructs ProjectorQueue directly and leaves this at its Go
	// zero value keeps the pre-#4450 fixed-delay retry schedule.
	JitterFraction float64
	// JitterSource draws jitter in [0, 1); nil defaults to
	// queuestore.DefaultJitterSource (math/rand/v2). Tests inject a seeded or
	// fixed source for deterministic, non-flaky assertions.
	JitterSource func() float64
	// Instruments records operator-facing retry telemetry. Nil is safe
	// (no-op) so existing callers that do not wire it keep working.
	Instruments *telemetry.Instruments
	// CrossplaneRedrive re-drives cross-scope Claim SATISFIED_BY correlations
	// after Ack activates a generation carrying an active CrossplaneXRD
	// (issue #5476). Nil is safe (no-op): existing callers that do not wire a
	// sweeper keep today's behavior exactly. See runCrossplaneRedriveHook's
	// doc comment for why this runs AFTER Ack's own transaction commits, not
	// inside it.
	CrossplaneRedrive CrossplaneRedriveSweeper
	// ConfigStateDriftTrigger enqueues one config_state_drift reducer intent
	// after Ack activates a state_snapshot:* scope generation (issue #5593),
	// so drift evaluation follows the data instead of waiting for the next
	// bootstrap-index Phase 3.5 sweep. Nil is safe (no-op). MUST NOT be wired
	// on bootstrap-index's ProjectorQueue -- see
	// runConfigStateDriftTriggerHook's doc comment for why.
	ConfigStateDriftTrigger ConfigStateDriftTrigger
	// AckScopeLockTimeout bounds how long Ack waits for a row lock, mainly the
	// scope row an ingestion commit holds while streaming facts. It must stay
	// below the caller's Ack budget. Zero uses defaultProjectorAckLockTimeout.
	AckScopeLockTimeout time.Duration
	// ClaimConflictBackoff is the base delay between Claim attempts after a
	// deadlock or serialization failure; each retry waits attempt*base plus
	// up to one base of jitter. Zero uses defaultProjectorClaimConflictBackoff.
	ClaimConflictBackoff time.Duration
}

// defaultProjectorAckLockTimeout leaves room inside the projector service's
// 5 s Ack budget for the transaction's other statements, and
// maxProjectorAckLockTimeout caps a configured value below that budget.
const (
	defaultProjectorAckLockTimeout = 2 * time.Second
	maxProjectorAckLockTimeout     = 4 * time.Second
)

// ErrProjectorClaimRejected means the projector work item's owner, attempt,
// or claimable status changed, so heartbeat, Ack, or Fail must stop. It wraps
// failure.ErrWorkClaimLost so the projector service drops the stale attempt
// instead of stopping its other workers.
var ErrProjectorClaimRejected = fmt.Errorf("projector work claim rejected: %w", failure.ErrWorkClaimLost)

// NewProjectorQueue constructs a Postgres-backed projector work queue.
func NewProjectorQueue(
	database db.ExecQueryer,
	leaseOwner string,
	leaseDuration time.Duration,
) ProjectorQueue {
	return ProjectorQueue{
		database:      database,
		LeaseOwner:    leaseOwner,
		LeaseDuration: leaseDuration,
	}
}

// WithClaimSourceSystem scopes Claim to projector work owned by one source
// system while leaving enqueue, ack, heartbeat, and failure behavior unchanged.
func (q ProjectorQueue) WithClaimSourceSystem(sourceSystem string) ProjectorQueue {
	q.ClaimSourceSystem = strings.TrimSpace(sourceSystem)
	return q
}

// Enqueue inserts one durable source-local projection work item.
func (q ProjectorQueue) Enqueue(
	ctx context.Context,
	scopeID string,
	generationID string,
) error {
	if q.database == nil {
		return errors.New("projector queue database is required")
	}
	if scopeID == "" {
		return errors.New("projector queue scope_id is required")
	}
	if generationID == "" {
		return errors.New("projector queue generation_id is required")
	}

	now := q.now()
	_, err := q.database.ExecContext(
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
//
// The claim statement takes every row lock with SKIP LOCKED, so it should not
// deadlock (#7108). A 40P01 or 40001 from Postgres is still retried a bounded
// number of times with jittered backoff as defense in depth; each retry
// increments eshu_dp_queue_claim_conflict_retries_total. When every attempt
// conflicts, the returned error wraps failure.ErrWorkClaimConflict so the
// projector service keeps its workers running.
func (q ProjectorQueue) Claim(ctx context.Context) (projector.ScopeGenerationWork, bool, error) {
	if err := q.validate(); err != nil {
		return projector.ScopeGenerationWork{}, false, err
	}
	for attempt := 1; ; attempt++ {
		work, ok, err := q.claimOnce(ctx)
		class := claimConflictClass(err)
		if class == "" {
			return work, ok, err
		}
		if attempt >= projectorClaimConflictAttempts {
			return projector.ScopeGenerationWork{}, false, fmt.Errorf("%w after %d attempts: %w",
				failure.ErrWorkClaimConflict, attempt, err)
		}
		q.recordClaimConflictRetry(ctx, class, attempt, err)
		if waitErr := sleepContext(ctx, q.claimConflictDelay(attempt)); waitErr != nil {
			return projector.ScopeGenerationWork{}, false, fmt.Errorf("claim projector work: %w", waitErr)
		}
	}
}

// Ack marks one claimed projector work item as succeeded and publishes its
// generation. It never revives a superseded generation (#7130): the work item
// is then marked superseded and Ack returns failure.ErrWorkSuperseded, which
// callers treat like a superseded heartbeat.
func (q ProjectorQueue) Ack(
	ctx context.Context,
	work projector.ScopeGenerationWork,
	_ runtime.Result,
) (err error) {
	if err := q.validate(); err != nil {
		return err
	}
	// A lock timeout rolled the transaction back, so nothing changed and the
	// attempt still owns the work; the caller renews the lease and retries.
	defer func() {
		if isPostgresLockNotAvailable(err) {
			err = fmt.Errorf("%w: %w", failure.ErrWorkAckDeferred, err)
		}
	}()

	beginner, ok := q.database.(db.Beginner)
	if !ok {
		return errors.New("projector queue database must support Begin for ack")
	}

	tx, err := beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("ack projector work: begin: %w", err)
	}
	txDone := false
	defer func() {
		if !txDone {
			_ = tx.Rollback()
		}
	}()

	lockTimeout := min(q.AckScopeLockTimeout, maxProjectorAckLockTimeout)
	if lockTimeout < time.Millisecond { // "0ms" would disable lock_timeout
		lockTimeout = defaultProjectorAckLockTimeout
	}
	// PostgreSQL accepts "2000ms" but not Go's "1m30s" duration syntax.
	lockTimeoutSetting := fmt.Sprintf("%dms", lockTimeout.Milliseconds())
	if _, err := tx.ExecContext(ctx, "SELECT set_config('lock_timeout', $1, true)", lockTimeoutSetting); err != nil {
		return fmt.Errorf("ack projector work: set lock timeout: %w", err)
	}
	now := q.now()
	// Ingestion commits lock scope, then generation, then work. Locking the
	// scope first serializes same-scope commits; work precedes generation so
	// heartbeat and claim operations cannot invert the remaining lock order.
	if _, err := tx.ExecContext(ctx, updateProjectorScopeGenerationQuery,
		now, work.Scope.ScopeID, work.Generation.GenerationID); err != nil {
		return fmt.Errorf("ack projector work: update scope active generation: %w", err)
	}
	ackResult, err := tx.ExecContext(ctx, ackProjectorWorkItemQuery,
		now, work.Scope.ScopeID, work.Generation.GenerationID, q.LeaseOwner, work.AttemptCount)
	if err != nil {
		return fmt.Errorf("ack projector work: mark work succeeded: %w", err)
	}
	ackRows, err := ackResult.RowsAffected()
	if err != nil {
		return fmt.Errorf("ack projector work: rows affected: %w", err)
	}
	if ackRows != 1 {
		return fmt.Errorf("ack projector work: %w", ErrProjectorClaimRejected)
	}
	steps := []struct {
		query string
		op    string
		args  []any
	}{
		{
			query: supersedeProjectorObsoleteGenerationsQuery,
			op:    "supersede obsolete terminal generations",
			args:  []any{now, work.Scope.ScopeID, work.Generation.GenerationID},
		},
		{
			query: supersedeProjectorActiveGenerationQuery,
			op:    "supersede active generation",
			args:  []any{now, work.Scope.ScopeID, work.Generation.GenerationID},
		},
	}
	for _, step := range steps {
		if _, err := tx.ExecContext(ctx, step.query, step.args...); err != nil {
			return fmt.Errorf("ack projector work: %s: %w", step.op, err)
		}
	}
	activated, err := q.activateAckGeneration(ctx, tx, work, now)
	if err != nil {
		return err
	}
	if !activated {
		txDone = true // refuseSupersededAck rolls the transaction back.
		return q.refuseSupersededAck(ctx, tx, work, now)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ack projector work: commit: %w", err)
	}
	txDone = true

	q.runCrossplaneRedriveHook(ctx, work)
	q.runConfigStateDriftTriggerHook(ctx, work)

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
		return failure.ErrWorkSuperseded
	}

	result, err := q.database.ExecContext(
		ctx,
		heartbeatProjectorWorkQuery,
		now.Add(q.LeaseDuration),
		now,
		work.Scope.ScopeID,
		work.Generation.GenerationID,
		q.LeaseOwner,
		work.AttemptCount,
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
	result, err := q.database.ExecContext(
		ctx,
		supersedeRunningProjectorWorkQuery,
		now,
		work.Scope.ScopeID,
		work.Generation.GenerationID,
		q.LeaseOwner,
		work.AttemptCount,
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

	retryable := failure.IsRetryable(cause)
	willRetry := retryable && work.AttemptCount < q.maxAttempts()

	if willRetry {
		// Retry path: keep the existing retryable class and preserve any
		// detailed failure context the error carries for diagnosis.
		_, failureMessage, failureDetails := queuestore.QueueFailureMetadata(cause, "projection_retryable")
		now := q.now()
		delay := queuestore.ComputeRetryDelay(q.retryDelay(), q.retryMaxDelay(), q.JitterFraction, work.AttemptCount, q.jitterSource())
		args := []any{
			now,
			"projection_retryable",
			failureMessage,
			failureDetails,
			now.Add(delay),
			work.Scope.ScopeID,
			work.Generation.GenerationID,
			q.LeaseOwner,
			work.AttemptCount,
		}
		result, err := q.database.ExecContext(ctx, retryProjectorWorkQuery, args...)
		if err != nil {
			return fmt.Errorf("fail projector work: %w", err)
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("fail projector work: rows affected: %w", err)
		}
		if rowsAffected != 1 {
			return ErrProjectorClaimRejected
		}
		if q.Instruments != nil && q.Instruments.ProjectorRetrySurge != nil {
			q.Instruments.ProjectorRetrySurge.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrFailureClass("projection_retryable"),
			))
		}
		return nil
	}

	// Dead-letter path: enrich the durable failure_class with an operator-facing
	// triage class so an operator can tell why the item died (transient that
	// exhausted retries, terminal invalid input, or a poison projection bug) and
	// whether replaying it unchanged is safe. The retry decision stays with
	// IsRetryable; the triage metadata only labels the outcome (issue #3514).
	failureClass, failureMessage, failureDetails := queuestore.DeadLetterTriageMetadata(cause, "project_work_item", retryable)
	args := []any{
		q.now(),
		failureClass,
		failureMessage,
		failureDetails,
		work.Scope.ScopeID,
		work.Generation.GenerationID,
		q.LeaseOwner,
		work.AttemptCount,
	}

	result, err := q.database.ExecContext(ctx, failProjectorWorkQuery, args...)
	if err != nil {
		return fmt.Errorf("fail projector work: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("fail projector work: rows affected: %w", err)
	}
	if rowsAffected != 1 {
		return ErrProjectorClaimRejected
	}

	return nil
}

func (q ProjectorQueue) validate() error {
	if q.database == nil {
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

// retryMaxDelay caps the exponential backoff term computed by Fail. Zero/
// unset falls back to queuestore.DefaultRetryMaxDelayFallback (1 hour),
// matching runtime.RetryPolicyConfig's default.
func (q ProjectorQueue) retryMaxDelay() time.Duration {
	if q.MaxRetryDelay > 0 {
		return q.MaxRetryDelay
	}

	return queuestore.DefaultRetryMaxDelayFallback
}

// jitterSource returns the configured JitterSource, defaulting to
// queuestore.DefaultJitterSource (math/rand/v2's global source) in
// production.
func (q ProjectorQueue) jitterSource() func() float64 {
	if q.JitterSource != nil {
		return q.JitterSource
	}

	return queuestore.DefaultJitterSource
}

func (q ProjectorQueue) maxAttempts() int {
	if q.MaxAttempts > 0 {
		return q.MaxAttempts
	}

	return 3
}

// isPostgresLockNotAvailable reports SQLSTATE 55P03, raised when lock_timeout
// expires while a statement waits for a lock.
func isPostgresLockNotAvailable(err error) bool {
	return coordination.IsLockNotAvailable(err)
}
