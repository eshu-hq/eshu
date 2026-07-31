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
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	reducerEnqueueBatchSize  = 500
	columnsPerReducerEnqueue = 8
)

const enqueueReducerBatchPrefix = `INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    conflict_domain, conflict_key,
    attempt_count, lease_owner, claim_until, visible_at, last_attempt_at,
    next_attempt_at, failure_class, failure_message, failure_details,
    payload, created_at, updated_at
) VALUES `

const enqueueReducerBatchSuffix = `
ON CONFLICT (work_item_id) DO NOTHING
`

const ackReducerWorkQuery = `
UPDATE fact_work_items
SET status = 'succeeded',
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = NULL,
    updated_at = $1,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL
WHERE work_item_id = $2
  AND stage = 'reducer'
  AND lease_owner = $3
  AND status IN ('claimed', 'running')
`

const ackContainerImageIdentityReducerWorkQuery = `
UPDATE fact_work_items
SET status = 'succeeded',
    container_image_identity_v2_authorized_status = CASE
        WHEN container_image_identity_v2_required THEN 'succeeded'
        ELSE ''
    END,
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = NULL,
    updated_at = $1,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL
WHERE work_item_id = $2
  AND stage = 'reducer'
  AND lease_owner = $3
  AND status IN ('claimed', 'running')
  AND container_image_identity_claim_epoch = $4
`

const heartbeatReducerWorkQuery = `
UPDATE fact_work_items
SET claim_until = $1,
    updated_at = $2
WHERE work_item_id = $3
  AND stage = 'reducer'
  AND lease_owner = $4
  AND status IN ('claimed', 'running')
`

const failReducerWorkQuery = `
UPDATE fact_work_items
SET status = 'dead_letter',
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = NULL,
    updated_at = $1,
    failure_class = $2,
    failure_message = $3,
    failure_details = $4
WHERE work_item_id = $5
  AND stage = 'reducer'
  AND lease_owner = $6
  AND status IN ('claimed', 'running')
`

const failContainerImageIdentityReducerWorkQuery = `
UPDATE fact_work_items
SET status = 'dead_letter',
    container_image_identity_v2_authorized_status = CASE
        WHEN container_image_identity_v2_required THEN 'dead_letter'
        ELSE ''
    END,
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = NULL,
    updated_at = $1,
    failure_class = $2,
    failure_message = $3,
    failure_details = $4
WHERE work_item_id = $5
  AND stage = 'reducer'
  AND lease_owner = $6
  AND status IN ('claimed', 'running')
  AND container_image_identity_claim_epoch = $7
`

const retryReducerWorkQuery = `
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
WHERE work_item_id = $6
  AND stage = 'reducer'
  AND lease_owner = $7
  AND status IN ('claimed', 'running')
`

const retryContainerImageIdentityReducerWorkQuery = `
UPDATE fact_work_items
SET status = 'retrying',
    container_image_identity_v2_authorized_status = CASE
        WHEN container_image_identity_v2_required THEN 'retrying'
        ELSE ''
    END,
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = $5,
    next_attempt_at = $5,
    updated_at = $1,
    failure_class = $2,
    failure_message = $3,
    failure_details = $4
WHERE work_item_id = $6
  AND stage = 'reducer'
  AND lease_owner = $7
  AND status IN ('claimed', 'running')
  AND container_image_identity_claim_epoch = $8
`

// ReducerQueue provides reducer-stage queue behavior over fact_work_items.
type ReducerQueue struct {
	db            ExecQueryer
	LeaseOwner    string
	LeaseDuration time.Duration
	RetryDelay    time.Duration
	MaxAttempts   int
	Now           func() time.Time

	// MaxRetryDelay caps the exponential-backoff retry term computed by
	// failIntent. Zero/unset falls back to defaultRetryMaxDelayFallback (1
	// hour), matching runtime.RetryPolicyConfig's default.
	MaxRetryDelay time.Duration
	// JitterFraction scales the random jitter added on top of the
	// exponential backoff term, relative to RetryDelay: jitter is drawn
	// uniformly from [0, RetryDelay*JitterFraction). Zero means no jitter
	// (legacy fixed-delay behavior); callers wired through
	// runtime.LoadRetryPolicyConfig get 0.1 by default (#4450).
	JitterFraction float64
	// JitterSource draws jitter in [0, 1); nil defaults to
	// defaultJitterSource (math/rand/v2). Tests inject a seeded or fixed
	// source for deterministic, non-flaky assertions.
	JitterSource func() float64
	// Instruments records operator-facing retry telemetry
	// (eshu_dp_reducer_retry_surge_total, #4450). Nil is safe (no-op) so
	// existing callers that do not wire it keep working.
	Instruments *telemetry.Instruments

	// ClaimDomain optionally restricts this queue instance to one reducer domain.
	// Prefer ClaimDomains for new multi-domain reducer lanes.
	ClaimDomain reducer.Domain

	// ClaimDomains optionally restricts this queue instance to a reducer domain
	// allowlist. Empty keeps the default all-domain reducer behavior.
	ClaimDomains []reducer.Domain

	// RequireProjectorDrainBeforeClaim keeps reducer graph writes from
	// contending with same-scope source-local projection. It is intended for
	// NornicDB local_authoritative evaluation, where canonical projector
	// writes and reducer writes share one embedded graph backend.
	RequireProjectorDrainBeforeClaim bool

	// ExpectedSourceLocalProjectors optionally requires semantic reducers to
	// wait until local-host has completed the discovered source-local corpus.
	ExpectedSourceLocalProjectors int

	// SemanticEntityClaimLimit caps concurrent semantic entity reducer claims
	// under the NornicDB local-authoritative drain gate. Values <= 0 disable
	// the cross-scope semantic cap; conflict-domain fencing still serializes
	// same-scope code graph work.
	SemanticEntityClaimLimit int
}

// ErrReducerClaimRejected means the claimed reducer work item no longer belongs
// to the current lease owner, so heartbeat/ack/fail must stop.
var ErrReducerClaimRejected = errors.New("reducer work claim rejected")

// NewReducerQueue constructs a Postgres-backed reducer work queue.
func NewReducerQueue(
	db ExecQueryer,
	leaseOwner string,
	leaseDuration time.Duration,
) ReducerQueue {
	return ReducerQueue{
		db:            db,
		LeaseOwner:    leaseOwner,
		LeaseDuration: leaseDuration,
	}
}

// Enqueue implements projector.ReducerIntentWriter over fact_work_items.
// Uses batched multi-row INSERT to reduce round trips from N to N/500.
func (q ReducerQueue) Enqueue(
	ctx context.Context,
	intents []projector.ReducerIntent,
) (projector.IntentResult, error) {
	if err := q.validateEnqueue(); err != nil {
		return projector.IntentResult{}, err
	}

	if len(intents) == 0 {
		return projector.IntentResult{Count: 0}, nil
	}

	// Validate all intents before batching
	for _, intent := range intents {
		if err := intent.Domain.Validate(); err != nil {
			return projector.IntentResult{}, fmt.Errorf("enqueue reducer intent: %w", err)
		}
	}

	now := q.now()

	// Enqueue in batches, summing each batch's actual RowsAffected -- not
	// len(intents) -- so the returned Count reflects what the DB really
	// admitted through ON CONFLICT (work_item_id) DO NOTHING (issue #5593;
	// see IntentResult's doc comment in internal/projector/runtime.go).
	var inserted int64
	for i := 0; i < len(intents); i += reducerEnqueueBatchSize {
		end := i + reducerEnqueueBatchSize
		if end > len(intents) {
			end = len(intents)
		}
		batchInserted, err := q.enqueueReducerBatch(ctx, intents[i:end], now)
		if err != nil {
			return projector.IntentResult{}, err
		}
		inserted += batchInserted
	}

	return projector.IntentResult{Count: int(inserted)}, nil
}

// enqueueReducerBatch inserts one batch of reducer intents using a multi-row
// INSERT and returns the number of rows the DB actually inserted (excluding
// rows skipped by ON CONFLICT (work_item_id) DO NOTHING), read from the
// ExecContext result's RowsAffected -- not the batch size, which is only an
// attempt count (issue #5593).
func (q ReducerQueue) enqueueReducerBatch(
	ctx context.Context,
	batch []projector.ReducerIntent,
	now time.Time,
) (int64, error) {
	if len(batch) == 0 {
		return 0, nil
	}

	args := make([]any, 0, len(batch)*columnsPerReducerEnqueue)
	var values strings.Builder

	for i, intent := range batch {
		payload := make(map[string]any, len(intent.Payload)+4)
		for key, value := range intent.Payload {
			payload[key] = value
		}
		payload["entity_key"] = intent.EntityKey
		payload["reason"] = intent.Reason
		payload["fact_id"] = intent.FactID
		payload["source_system"] = intent.SourceSystem
		payloadJSON, err := marshalPayload(payload)
		if err != nil {
			return 0, fmt.Errorf("marshal reducer payload: %w", err)
		}

		if i > 0 {
			values.WriteString(", ")
		}
		conflictDomain, conflictKey := reducerConflictDomainKey(intent)
		offset := i * columnsPerReducerEnqueue
		fmt.Fprintf(
			&values,
			"($%d, $%d, $%d, 'reducer', $%d, 'pending', $%d, $%d, 0, NULL, NULL, $%d, NULL, NULL, NULL, NULL, NULL, $%d::jsonb, $%d, $%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5, offset+6, offset+7, offset+8, offset+7, offset+7,
		)

		args = append(
			args,
			reducerWorkItemID(intent),
			intent.ScopeID,
			intent.GenerationID,
			string(intent.Domain),
			conflictDomain,
			conflictKey,
			now,
			payloadJSON,
		)
	}

	query := enqueueReducerBatchPrefix + values.String() + enqueueReducerBatchSuffix

	result, err := q.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("enqueue reducer batch (%d intents): %w", len(batch), err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("enqueue reducer batch (%d intents): rows affected: %w", len(batch), err)
	}

	return inserted, nil
}

// Claim implements reducer.WorkSource over fact_work_items.
func (q ReducerQueue) Claim(ctx context.Context) (reducer.Intent, bool, error) {
	if err := q.validateClaim(); err != nil {
		return reducer.Intent{}, false, err
	}

	now := q.now()
	rows, err := q.db.QueryContext(
		ctx,
		claimReducerWorkQuery,
		now,
		q.claimDomainFilters(),
		q.LeaseOwner,
		now.Add(q.LeaseDuration),
		q.RequireProjectorDrainBeforeClaim,
		q.ExpectedSourceLocalProjectors,
		q.semanticEntityClaimLimit(),
	)
	if err != nil {
		if isReducerLiveLeaseConflict(err) {
			// A concurrent claimer already holds the live lease on this
			// conflict key; defer rather than double-claim (#4137).
			return reducer.Intent{}, false, nil
		}
		return reducer.Intent{}, false, fmt.Errorf("claim reducer work: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			if isReducerLiveLeaseConflict(err) {
				return reducer.Intent{}, false, nil
			}
			return reducer.Intent{}, false, fmt.Errorf("claim reducer work: %w", err)
		}
		return reducer.Intent{}, false, nil
	}

	intent, err := scanReducerIntent(rows)
	if err != nil {
		return reducer.Intent{}, false, fmt.Errorf("claim reducer work: %w", err)
	}
	if err := rows.Err(); err != nil {
		if isReducerLiveLeaseConflict(err) {
			return reducer.Intent{}, false, nil
		}
		return reducer.Intent{}, false, fmt.Errorf("claim reducer work: %w", err)
	}

	return intent, true, nil
}

// Heartbeat extends the claim on one reducer work item owned by this queue.
func (q ReducerQueue) Heartbeat(ctx context.Context, intent reducer.Intent) error {
	if err := q.validateClaim(); err != nil {
		return err
	}

	now := q.now()
	result, err := q.db.ExecContext(
		ctx,
		heartbeatReducerWorkQuery,
		now.Add(q.LeaseDuration),
		now,
		intent.IntentID,
		q.LeaseOwner,
	)
	if err != nil {
		return fmt.Errorf("heartbeat reducer work: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("heartbeat reducer work: rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrReducerClaimRejected
	}
	return nil
}

// Ack marks one claimed reducer work item as succeeded.
func (q ReducerQueue) Ack(ctx context.Context, intent reducer.Intent, _ reducer.Result) error {
	if err := q.validateClaim(); err != nil {
		return err
	}

	query := ackReducerWorkQuery
	if intent.Domain == reducer.DomainContainerImageIdentity {
		query = ackContainerImageIdentityReducerWorkQuery
	}
	args := []any{q.now(), intent.IntentID, q.LeaseOwner}
	if intent.Domain == reducer.DomainContainerImageIdentity {
		args = append(args, intent.ClaimEpoch)
	}
	result, err := q.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("ack reducer work: %w", err)
	}
	if intent.Domain == reducer.DomainContainerImageIdentity {
		rowsAffected, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return fmt.Errorf("ack reducer work: rows affected: %w", rowsErr)
		}
		if rowsAffected == 0 {
			return ErrReducerClaimRejected
		}
	}

	return nil
}

// Fail marks one claimed reducer work item as failed.
func (q ReducerQueue) Fail(ctx context.Context, intent reducer.Intent, cause error) error {
	if err := q.validateClaim(); err != nil {
		return err
	}
	if cause == nil {
		return errors.New("reducer failure cause is required")
	}

	if err := q.failIntent(ctx, intent, cause); err != nil {
		return err
	}

	return nil
}
