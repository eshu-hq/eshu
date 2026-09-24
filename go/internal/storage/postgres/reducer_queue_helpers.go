// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/payload"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/queue"
	"github.com/eshu-hq/eshu/go/internal/telemetry"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/metric"
)

// reducerLiveLeaseUniqueConstraint is the partial unique index (migration 005)
// that allows at most one live reducer lease per (conflict_domain, conflict_key).
const reducerLiveLeaseUniqueConstraint = "fact_work_items_reducer_live_lease_uniq"

// isReducerLiveLeaseConflict reports whether err is the unique-index violation
// that fences a second concurrent live lease on a conflict key (#4137,
// completing #3558). Under READ COMMITTED two genuinely simultaneous claimers
// can each pick a different pending sibling row before either commits; the
// database rejects the second claim with this specific constraint, and the
// claim path treats it as "no claimable work this call" — the sibling stays
// pending and the holder's lease governs the conflict key. The constraint name
// is matched explicitly so an unrelated unique violation is never swallowed.
func isReducerLiveLeaseConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == "23505" &&
		pgErr.ConstraintName == reducerLiveLeaseUniqueConstraint
}

func (q ReducerQueue) claimDomainFilters() []string {
	domains := q.effectiveClaimDomains()
	if len(domains) == 0 {
		return nil
	}
	values := make([]string, 0, len(domains))
	for _, domain := range domains {
		values = append(values, string(domain))
	}
	return values
}

func (q ReducerQueue) effectiveClaimDomains() []reducer.Domain {
	if len(q.ClaimDomains) > 0 {
		return q.ClaimDomains
	}
	if q.ClaimDomain != "" {
		return []reducer.Domain{q.ClaimDomain}
	}
	return nil
}

func (q ReducerQueue) semanticEntityClaimLimit() int {
	if !q.RequireProjectorDrainBeforeClaim {
		return 0
	}
	if q.SemanticEntityClaimLimit > 0 {
		return q.SemanticEntityClaimLimit
	}
	return 0
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

// retryMaxDelay caps the exponential backoff term computed by failIntent.
// Zero/unset falls back to queuestore.DefaultRetryMaxDelayFallback (1 hour),
// matching runtime.RetryPolicyConfig's default.
func (q ReducerQueue) retryMaxDelay() time.Duration {
	if q.MaxRetryDelay > 0 {
		return q.MaxRetryDelay
	}

	return queuestore.DefaultRetryMaxDelayFallback
}

// jitterSource returns the configured JitterSource, defaulting to
// queuestore.DefaultJitterSource (math/rand/v2's global source) in
// production.
func (q ReducerQueue) jitterSource() func() float64 {
	if q.JitterSource != nil {
		return q.JitterSource
	}

	return queuestore.DefaultJitterSource
}

func (q ReducerQueue) maxAttempts() int {
	if q.MaxAttempts > 0 {
		return q.MaxAttempts
	}

	return 3
}

func claimedAtValue(intent reducer.Intent) time.Time {
	if intent.ClaimedAt == nil {
		return time.Time{}
	}
	return intent.ClaimedAt.UTC()
}

func scanReducerIntent(rows db.Rows) (reducer.Intent, error) {
	var intentID string
	var scopeID string
	var generationID string
	var domain string
	var attemptCount int
	var claimEpoch int64
	var enqueuedAt time.Time
	var availableAt time.Time
	var cycleStartedAt time.Time
	var claimedAt time.Time
	var rawPayload []byte

	if err := rows.Scan(
		&intentID,
		&scopeID,
		&generationID,
		&domain,
		&attemptCount,
		&claimEpoch,
		&enqueuedAt,
		&availableAt,
		&cycleStartedAt,
		&claimedAt,
		&rawPayload,
	); err != nil {
		return reducer.Intent{}, err
	}

	payload, err := payloadstore.UnmarshalPayload(rawPayload)
	if err != nil {
		return reducer.Intent{}, err
	}

	entityKey, _ := payload["entity_key"].(string)
	reason, _ := payload["reason"].(string)
	factID, _ := payload["fact_id"].(string)
	sourceSystem, _ := payload["source_system"].(string)
	intentPayload := make(map[string]any, len(payload))
	for key, value := range payload {
		intentPayload[key] = value
	}

	domainValue, err := reducer.ParseDomain(domain)
	if err != nil {
		return reducer.Intent{}, err
	}
	claimedAt = claimedAt.UTC()

	intent := reducer.Intent{
		IntentID:        intentID,
		ScopeID:         scopeID,
		GenerationID:    generationID,
		SourceSystem:    sourceSystem,
		Domain:          domainValue,
		Cause:           reason,
		AttemptCount:    attemptCount,
		ClaimEpoch:      claimEpoch,
		EntityKeys:      nil,
		RelatedScopeIDs: []string{scopeID},
		Payload:         intentPayload,
		Status:          reducer.IntentStatusClaimed,
		EnqueuedAt:      enqueuedAt.UTC(),
		AvailableAt:     availableAt.UTC(),
		CycleStartedAt:  cycleStartedAt.UTC(),
		ClaimedAt:       &claimedAt,
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

func (q ReducerQueue) retryable(cause error, failureClass string, attemptCount int) bool {
	if !reducer.IsRetryable(cause) {
		return false
	}
	if isNonCountingReducerRetryFailureClass(failureClass) {
		return true
	}
	return attemptCount < q.maxAttempts()
}

func isNonCountingReducerRetryFailureClass(failureClass string) bool {
	for _, class := range nonCountingReducerRetryFailureClasses {
		if failureClass == class {
			return true
		}
	}
	return false
}

func (q ReducerQueue) failIntent(
	ctx context.Context,
	intent reducer.Intent,
	cause error,
) error {
	now := q.now()

	// retryable() consults both the canonical Retryable() authority and the
	// non-counting readiness class. Probe with a sentinel fallback so a
	// self-classifying cause is distinguishable from one that does not classify:
	// queuestore.QueueFailureMetadata only overrides the fallback when the
	// error implements FailureClass(), so a returned value other than the
	// sentinel means the cause curated its own class.
	const unclassifiedRetrySentinel = "reducer_failed"
	probeClass, _, _ := queuestore.QueueFailureMetadata(cause, unclassifiedRetrySentinel)
	willRetry := q.retryable(cause, probeClass, intent.AttemptCount)

	if willRetry {
		// Preserve the cause's self-classified failure class on the retrying row.
		// Graph-write timeouts keep graph_write_timeout and readiness misses keep
		// their *_not_ready / *_n class, so producer write-timeout backpressure
		// (#3560) can scope its pressure signal to the graph-write class and never
		// throttle on a readiness backlog. A cause that does not self-classify
		// falls back to the generic reducer_retryable label.
		retryFailureClass := "reducer_retryable"
		if probeClass != unclassifiedRetrySentinel {
			retryFailureClass = probeClass
		}
		_, failureMessage, failureDetails := queuestore.QueueFailureMetadata(cause, retryFailureClass)
		delay := queuestore.ComputeRetryDelay(q.retryDelay(), q.retryMaxDelay(), q.JitterFraction, intent.AttemptCount, q.jitterSource())
		args := []any{
			now,
			retryFailureClass,
			failureMessage,
			failureDetails,
			now.Add(delay),
			intent.IntentID,
			q.LeaseOwner,
		}
		query := retryReducerWorkQuery
		target := intent.Domain == reducer.DomainContainerImageIdentity
		if target {
			query = retryContainerImageIdentityReducerWorkQuery
			args = append(args, intent.ClaimEpoch)
		}
		args = append(args, claimedAtValue(intent))
		result, err := q.database.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("fail reducer work: %w", err)
		}
		rowsAffected, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return fmt.Errorf("fail reducer work: rows affected: %w", rowsErr)
		}
		if rowsAffected != 1 {
			return ErrReducerClaimRejected
		}
		if q.Instruments != nil && q.Instruments.ReducerRetrySurge != nil {
			q.Instruments.ReducerRetrySurge.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrFailureClass(retryFailureClass),
			))
		}
		return nil
	}

	// Dead-letter path: enrich the durable failure_class with an operator-facing
	// triage class. Retryable() stays the retry-decision authority; the triage
	// metadata only labels the outcome (issue #3514). A self-classifying error
	// still wins over the triage fallback class.
	failureClass, failureMessage, failureDetails := queuestore.DeadLetterTriageMetadata(cause, "reduce_intent", reducer.IsRetryable(cause))
	args := []any{
		now,
		failureClass,
		failureMessage,
		failureDetails,
		intent.IntentID,
		q.LeaseOwner,
	}
	query := failReducerWorkQuery
	target := intent.Domain == reducer.DomainContainerImageIdentity
	if target {
		query = failContainerImageIdentityReducerWorkQuery
		args = append(args, intent.ClaimEpoch)
	}
	args = append(args, claimedAtValue(intent))
	result, err := q.database.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("fail reducer work: %w", err)
	}
	rowsAffected, rowsErr := result.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("fail reducer work: rows affected: %w", rowsErr)
	}
	if rowsAffected != 1 {
		return ErrReducerClaimRejected
	}

	return nil
}

func reducerWorkItemID(intent runtime.ReducerIntent) string {
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

// reducerAckBatchSplit groups one ack batch by the ack statement each domain
// family needs, and records how many acks were dropped because a later claim
// of the same work item is present in the same batch.
type reducerAckBatchSplit struct {
	target    []reducer.Intent
	cicd      []reducer.Intent
	unrelated []reducer.Intent

	// supersededClaims counts acks dropped in favour of a newer claim of the
	// same work item. A dropped ack carries a result that was never applied,
	// so AckBatch reports the batch as claim-rejected rather than succeeded.
	supersededClaims int

	// keptResultIndexByID maps each retained work item to the position its
	// surviving intent held in the input batch, so the caller can pair it with
	// the result that intent's handler produced. A batch carrying two claims of
	// one work item also carries two results, and the value-flow refresh emit
	// gate (#6785) must read the surviving claim's.
	keptResultIndexByID map[string]int
}

// splitReducerAckBatchIntents routes one ack batch to the statement each
// domain family needs, collapsing repeats of a work item to a single ack.
//
// A batch can legitimately carry the same work item twice under two claim
// identities: a lease that expires while its handler is still running is
// re-claimed by this same process, both handlers finish, and both hand the
// acker a result. Only the newest claim still owns the row — the ack statements
// fence on last_attempt_at, the container-image epoch, and a live claim_until —
// so the older ack would match zero rows. This keeps the newest claim, drops
// the superseded one, and counts it, which makes AckBatch report the batch as
// claim-rejected. Failing the whole batch instead would discard the surviving
// ack too, and its error would not wrap reducer.ErrExecutionClaimRejected, so
// the batch acker would treat an ordinary lease race as fatal and stop the
// reducer (issue #6162).
//
// A repeat under two different domains is not a lease race. A work item id
// encodes its domain, so that pairing is an invariant violation and stays an
// error.
func splitReducerAckBatchIntents(
	intents []reducer.Intent,
) (reducerAckBatchSplit, error) {
	indexByID := make(map[string]int, len(intents))
	keptResultIndexByID := make(map[string]int, len(intents))
	kept := make([]reducer.Intent, 0, len(intents))
	supersededClaims := 0
	for position, intent := range intents {
		index, ok := indexByID[intent.IntentID]
		if !ok {
			indexByID[intent.IntentID] = len(kept)
			keptResultIndexByID[intent.IntentID] = position
			kept = append(kept, intent)
			continue
		}
		prior := kept[index]
		if prior.Domain != intent.Domain {
			return reducerAckBatchSplit{}, fmt.Errorf(
				"batch ack reducer work item %q has conflicting domains %q and %q",
				intent.IntentID,
				prior.Domain,
				intent.Domain,
			)
		}
		if prior.ClaimEpoch == intent.ClaimEpoch &&
			sameClaimedAt(prior.ClaimedAt, intent.ClaimedAt) {
			continue
		}
		supersededClaims++
		if newerReducerClaim(intent, prior) {
			kept[index] = intent
			keptResultIndexByID[intent.IntentID] = position
		}
	}

	split := reducerAckBatchSplit{
		target:              make([]reducer.Intent, 0, len(kept)),
		cicd:                make([]reducer.Intent, 0, len(kept)),
		unrelated:           make([]reducer.Intent, 0, len(kept)),
		supersededClaims:    supersededClaims,
		keptResultIndexByID: keptResultIndexByID,
	}
	for _, intent := range kept {
		switch intent.Domain {
		case reducer.DomainContainerImageIdentity:
			split.target = append(split.target, intent)
		case reducer.DomainCICDRunCorrelation:
			split.cicd = append(split.cicd, intent)
		default:
			split.unrelated = append(split.unrelated, intent)
		}
	}
	return split, nil
}

// newerReducerClaim reports whether candidate holds a later claim of the same
// work item than current. Claim epoch leads because it is the monotonic fence
// for the domains that opt into it; claim timestamp settles the domains whose
// epoch stays zero, and matches what the ack statements compare against
// last_attempt_at.
func newerReducerClaim(candidate, current reducer.Intent) bool {
	if candidate.ClaimEpoch != current.ClaimEpoch {
		return candidate.ClaimEpoch > current.ClaimEpoch
	}
	return claimedAtValue(candidate).After(claimedAtValue(current))
}

func sameClaimedAt(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
