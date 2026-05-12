package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
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

const claimReducerWorkQuery = `
WITH candidate AS (
    SELECT work_item_id
    FROM fact_work_items
    WHERE stage = 'reducer'
      AND status IN ('pending', 'retrying', 'claimed', 'running')
      AND (visible_at IS NULL OR visible_at <= $1)
      AND (claim_until IS NULL OR claim_until <= $1)
      AND ($2 = '' OR domain = $2)
      -- NornicDB local_authoritative first-generation runs must not let
      -- reducer graph writes contend with source-local canonical projection
      -- for the same scope. Unrelated scopes can continue draining.
      AND ($5 = false OR NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS projector_work
          WHERE projector_work.stage = 'projector'
            AND projector_work.scope_id = fact_work_items.scope_id
            AND projector_work.status IN ('pending', 'retrying', 'claimed', 'running')
      ))
      -- Semantic entity materialization writes labels onto source-local
      -- content-entity nodes. On NornicDB, running those writes while any
      -- source-local projection is still active causes cross-scope graph
      -- backend contention and retry storms; non-semantic reducer domains can
      -- still drain once their own scope has passed the gate above.
      AND ($5 = false OR domain <> 'semantic_entity_materialization' OR NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS projector_any
          WHERE projector_any.stage = 'projector'
            AND projector_any.domain = 'source_local'
            AND projector_any.status IN ('pending', 'retrying', 'claimed', 'running')
      ))
      -- In local-host watch mode the ingester discovers and enqueues source
      -- projector work incrementally. A temporary enqueue gap is not proof
      -- that the whole corpus has drained, so semantic reducers can also wait
      -- for the owner-discovered source-local success count.
      AND ($5 = false OR domain <> 'semantic_entity_materialization' OR $6 <= 0 OR (
          SELECT count(*)
          FROM fact_work_items AS projector_done
          WHERE projector_done.stage = 'projector'
            AND projector_done.domain = 'source_local'
            AND projector_done.status = 'succeeded'
      ) >= $6)
      -- NornicDB's semantic label update path is backed by the same label/uid
      -- indexes touched by source-local canonical entities. Eight concurrent
      -- semantic writers have repeatedly timed out on tiny row sets, so cap
      -- only this reducer domain while preserving concurrency for unrelated
      -- reducer domains.
      AND ($5 = false OR domain <> 'semantic_entity_materialization' OR (
          SELECT count(*)
          FROM fact_work_items AS semantic_inflight
          WHERE semantic_inflight.stage = 'reducer'
            AND semantic_inflight.domain = 'semantic_entity_materialization'
            AND semantic_inflight.work_item_id <> fact_work_items.work_item_id
            AND semantic_inflight.status IN ('claimed', 'running')
            AND semantic_inflight.claim_until > $1
      ) < $7)
      -- Reducer domains can touch the same graph nodes for a scope. Fence by
      -- explicit conflict key so unrelated graph families can still overlap.
      AND NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS inflight
          WHERE inflight.stage = 'reducer'
            AND inflight.conflict_domain = fact_work_items.conflict_domain
            AND COALESCE(inflight.conflict_key, inflight.scope_id) = COALESCE(fact_work_items.conflict_key, fact_work_items.scope_id)
            AND inflight.work_item_id <> fact_work_items.work_item_id
            AND inflight.status IN ('claimed', 'running')
            AND inflight.claim_until > $1
      )
    ORDER BY updated_at ASC, work_item_id ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
),
claimed AS (
    UPDATE fact_work_items AS work
    SET status = 'claimed',
        attempt_count = work.attempt_count + 1,
        lease_owner = $3,
        claim_until = $4,
        last_attempt_at = $1,
        updated_at = $1
    FROM candidate
    WHERE work.work_item_id = candidate.work_item_id
    RETURNING
        work.work_item_id,
        work.scope_id,
        work.generation_id,
        work.domain,
        work.attempt_count,
        work.created_at,
        COALESCE(work.visible_at, work.created_at) AS available_at,
        work.payload
)
SELECT
    work_item_id,
    scope_id,
    generation_id,
    domain,
    attempt_count,
    created_at,
    available_at,
    payload
FROM claimed
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

// ReducerQueue provides reducer-stage queue behavior over fact_work_items.
type ReducerQueue struct {
	db            ExecQueryer
	LeaseOwner    string
	LeaseDuration time.Duration
	RetryDelay    time.Duration
	MaxAttempts   int
	Now           func() time.Time

	// ClaimDomain optionally restricts this queue instance to one reducer
	// domain. Empty keeps the default all-domain reducer behavior.
	ClaimDomain reducer.Domain

	// RequireProjectorDrainBeforeClaim keeps reducer graph writes from
	// contending with same-scope source-local projection. It is intended for
	// NornicDB local_authoritative evaluation, where canonical projector
	// writes and reducer writes share one embedded graph backend.
	RequireProjectorDrainBeforeClaim bool

	// ExpectedSourceLocalProjectors optionally requires semantic reducers to
	// wait until local-host has completed the discovered source-local corpus.
	ExpectedSourceLocalProjectors int

	// SemanticEntityClaimLimit caps concurrent semantic entity reducer claims
	// under the NornicDB local-authoritative drain gate. Values <= 0 keep the
	// conservative one-claim default when the gate is enabled.
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

	// Enqueue in batches
	for i := 0; i < len(intents); i += reducerEnqueueBatchSize {
		end := i + reducerEnqueueBatchSize
		if end > len(intents) {
			end = len(intents)
		}
		if err := q.enqueueReducerBatch(ctx, intents[i:end], now); err != nil {
			return projector.IntentResult{}, err
		}
	}

	return projector.IntentResult{Count: len(intents)}, nil
}

// enqueueReducerBatch inserts one batch of reducer intents using a multi-row INSERT.
func (q ReducerQueue) enqueueReducerBatch(
	ctx context.Context,
	batch []projector.ReducerIntent,
	now time.Time,
) error {
	if len(batch) == 0 {
		return nil
	}

	args := make([]any, 0, len(batch)*columnsPerReducerEnqueue)
	var values strings.Builder

	for i, intent := range batch {
		payloadJSON, err := marshalPayload(map[string]any{
			"entity_key":    intent.EntityKey,
			"reason":        intent.Reason,
			"fact_id":       intent.FactID,
			"source_system": intent.SourceSystem,
		})
		if err != nil {
			return fmt.Errorf("marshal reducer payload: %w", err)
		}

		if i > 0 {
			values.WriteString(", ")
		}
		conflictDomain, conflictKey := reducerConflictDomainKey(intent)
		offset := i * columnsPerReducerEnqueue
		fmt.Fprintf(&values,
			"($%d, $%d, $%d, 'reducer', $%d, 'pending', $%d, $%d, 0, NULL, NULL, $%d, NULL, NULL, NULL, NULL, NULL, $%d::jsonb, $%d, $%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5, offset+6, offset+7, offset+8, offset+7, offset+7,
		)

		args = append(args,
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

	if _, err := q.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("enqueue reducer batch (%d intents): %w", len(batch), err)
	}

	return nil
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
		q.claimDomainFilter(),
		q.LeaseOwner,
		now.Add(q.LeaseDuration),
		q.RequireProjectorDrainBeforeClaim,
		q.ExpectedSourceLocalProjectors,
		q.semanticEntityClaimLimit(),
	)
	if err != nil {
		return reducer.Intent{}, false, fmt.Errorf("claim reducer work: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return reducer.Intent{}, false, fmt.Errorf("claim reducer work: %w", err)
		}
		return reducer.Intent{}, false, nil
	}

	intent, err := scanReducerIntent(rows)
	if err != nil {
		return reducer.Intent{}, false, fmt.Errorf("claim reducer work: %w", err)
	}
	if err := rows.Err(); err != nil {
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

	_, err := q.db.ExecContext(ctx, ackReducerWorkQuery, q.now(), intent.IntentID, q.LeaseOwner)
	if err != nil {
		return fmt.Errorf("ack reducer work: %w", err)
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

// validateShared runs the checks both enqueue and claim paths require, with
// error messages that name the caller's side. Both validateEnqueue and
// validateClaim delegate here so a db-nil or ClaimDomain failure carries the
// correct side marker in error strings and wrapped stack traces.
//
// The earlier shape composed validateClaim on top of validateEnqueue, which
// produced enqueue-marked errors on the claim path for shared-check failures
// (see Copilot review of PR #196). Routing through validateShared with an
// explicit side label fixes that without duplicating the check bodies.
func (q ReducerQueue) validateShared(side string) error {
	if q.db == nil {
		return fmt.Errorf("reducer queue database is required for %s", side)
	}
	if q.ClaimDomain != "" {
		if err := q.ClaimDomain.Validate(); err != nil {
			return fmt.Errorf("reducer queue claim domain invalid for %s: %w", side, err)
		}
	}
	return nil
}

// validateEnqueue checks the inputs Enqueue needs to insert a reducer
// fact_work_items row. The enqueue SQL writes NULL for lease_owner and
// claim_until (see enqueueReducerBatchPrefix and the VALUES tuple in
// enqueueReducerBatch), so LeaseOwner and LeaseDuration are not part of the
// enqueue contract. Splitting the check off from validateClaim removes the
// historical smell at drift_enqueue.go where producers had to fabricate
// placeholder lease values just to construct a struct used for enqueue only.
//
// Every error returned here carries the side marker "for enqueue" so wrapped
// errors and stack traces remain self-locating, including the shared checks
// delegated to validateShared.
func (q ReducerQueue) validateEnqueue() error {
	return q.validateShared("enqueue")
}

// validateClaim checks the inputs Claim, Heartbeat, Ack, and Fail need to
// fence reducer work by lease owner. LeaseOwner identifies the worker on the
// fact_work_items UPDATE statements (claim_until = $1, lease_owner = $3 in
// claimReducerWorkQuery; lease_owner = $3 in ackReducerWorkQuery and the
// heartbeat/fail variants). LeaseDuration sets claim_until on claim and
// renews it on heartbeat.
//
// validateClaim delegates the shared db != nil and ClaimDomain.Validate()
// checks to validateShared with the claim-side marker, so shared-check
// failures on the claim path are labeled "for claim/ack/heartbeat/fail"
// instead of inheriting the enqueue-side marker.
//
// Every error returned here carries the side marker
// "for claim/ack/heartbeat/fail" so wrapped errors remain self-locating.
func (q ReducerQueue) validateClaim() error {
	if err := q.validateShared("claim/ack/heartbeat/fail"); err != nil {
		return err
	}
	if q.LeaseOwner == "" {
		return errors.New("reducer queue lease owner is required for claim/ack/heartbeat/fail")
	}
	if q.LeaseDuration <= 0 {
		return errors.New("reducer queue lease duration must be positive for claim/ack/heartbeat/fail")
	}
	return nil
}

func (q ReducerQueue) claimDomainFilter() string {
	if q.ClaimDomain == "" {
		return ""
	}
	return string(q.ClaimDomain)
}

func (q ReducerQueue) semanticEntityClaimLimit() int {
	if !q.RequireProjectorDrainBeforeClaim {
		return 0
	}
	if q.SemanticEntityClaimLimit > 0 {
		return q.SemanticEntityClaimLimit
	}
	return 1
}

func (q ReducerQueue) now() time.Time {
	if q.Now != nil {
		return q.Now().UTC()
	}

	return time.Now().UTC()
}

func (q ReducerQueue) retryDelay() time.Duration {
	if q.RetryDelay > 0 {
		return q.RetryDelay
	}

	return 30 * time.Second
}

func (q ReducerQueue) maxAttempts() int {
	if q.MaxAttempts > 0 {
		return q.MaxAttempts
	}

	return 3
}

func scanReducerIntent(rows Rows) (reducer.Intent, error) {
	var intentID string
	var scopeID string
	var generationID string
	var domain string
	var attemptCount int
	var enqueuedAt time.Time
	var availableAt time.Time
	var rawPayload []byte

	if err := rows.Scan(
		&intentID,
		&scopeID,
		&generationID,
		&domain,
		&attemptCount,
		&enqueuedAt,
		&availableAt,
		&rawPayload,
	); err != nil {
		return reducer.Intent{}, err
	}

	payload, err := unmarshalPayload(rawPayload)
	if err != nil {
		return reducer.Intent{}, err
	}

	entityKey, _ := payload["entity_key"].(string)
	reason, _ := payload["reason"].(string)
	factID, _ := payload["fact_id"].(string)
	sourceSystem, _ := payload["source_system"].(string)

	domainValue, err := reducer.ParseDomain(domain)
	if err != nil {
		return reducer.Intent{}, err
	}

	intent := reducer.Intent{
		IntentID:        intentID,
		ScopeID:         scopeID,
		GenerationID:    generationID,
		SourceSystem:    sourceSystem,
		Domain:          domainValue,
		Cause:           reason,
		AttemptCount:    attemptCount,
		EntityKeys:      nil,
		RelatedScopeIDs: []string{scopeID},
		Status:          reducer.IntentStatusClaimed,
		EnqueuedAt:      enqueuedAt.UTC(),
		AvailableAt:     availableAt.UTC(),
	}
	if entityKey != "" {
		intent.EntityKeys = []string{entityKey}
	}
	if reason == "" {
		intent.Cause = "projector emitted shared work"
	}
	if sourceSystem == "" {
		intent.SourceSystem = "unknown"
	}
	if factID != "" && len(intent.EntityKeys) == 0 {
		intent.EntityKeys = []string{factID}
	}
	if err := intent.Validate(); err != nil {
		return reducer.Intent{}, err
	}

	return intent, nil
}

func (q ReducerQueue) retryable(cause error, attemptCount int) bool {
	return reducer.IsRetryable(cause) && attemptCount < q.maxAttempts()
}

func (q ReducerQueue) failIntent(
	ctx context.Context,
	intent reducer.Intent,
	cause error,
) error {
	now := q.now()
	failureClass, failureMessage, failureDetails := queueFailureMetadata(cause, "reducer_failed")
	query := failReducerWorkQuery
	args := []any{
		now,
		failureClass,
		failureMessage,
		failureDetails,
		intent.IntentID,
		q.LeaseOwner,
	}

	if q.retryable(cause, intent.AttemptCount) {
		failureClass = "reducer_retryable"
		query = retryReducerWorkQuery
		args = []any{
			now,
			failureClass,
			failureMessage,
			failureDetails,
			now.Add(q.retryDelay()),
			intent.IntentID,
			q.LeaseOwner,
		}
	}

	_, err := q.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("fail reducer work: %w", err)
	}

	return nil
}

func reducerWorkItemID(intent projector.ReducerIntent) string {
	parts := []string{
		intent.ScopeID,
		intent.GenerationID,
		string(intent.Domain),
		intent.EntityKey,
	}
	sanitized := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		part = strings.ReplaceAll(part, ":", "_")
		part = strings.ReplaceAll(part, "/", "_")
		sanitized = append(sanitized, part)
	}
	return "reducer_" + strings.Join(sanitized, "_")
}
