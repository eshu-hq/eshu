// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

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
	intentPayload := make(map[string]any, len(payload))
	for key, value := range payload {
		intentPayload[key] = value
	}

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
		Payload:         intentPayload,
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
	return failureClass == reducer.SecretsIAMEndpointNotReadyFailureClass
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
	// queueFailureMetadata only overrides the fallback when the error implements
	// FailureClass(), so a returned value other than the sentinel means the cause
	// curated its own class.
	const unclassifiedRetrySentinel = "reducer_failed"
	probeClass, _, _ := queueFailureMetadata(cause, unclassifiedRetrySentinel)
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
		_, failureMessage, failureDetails := queueFailureMetadata(cause, retryFailureClass)
		args := []any{
			now,
			retryFailureClass,
			failureMessage,
			failureDetails,
			now.Add(q.retryDelay()),
			intent.IntentID,
			q.LeaseOwner,
		}
		if _, err := q.db.ExecContext(ctx, retryReducerWorkQuery, args...); err != nil {
			return fmt.Errorf("fail reducer work: %w", err)
		}
		return nil
	}

	// Dead-letter path: enrich the durable failure_class with an operator-facing
	// triage class. Retryable() stays the retry-decision authority; the triage
	// metadata only labels the outcome (issue #3514). A self-classifying error
	// still wins over the triage fallback class.
	failureClass, failureMessage, failureDetails := deadLetterTriageMetadata(cause, "reduce_intent", reducer.IsRetryable(cause))
	args := []any{
		now,
		failureClass,
		failureMessage,
		failureDetails,
		intent.IntentID,
		q.LeaseOwner,
	}
	if _, err := q.db.ExecContext(ctx, failReducerWorkQuery, args...); err != nil {
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
