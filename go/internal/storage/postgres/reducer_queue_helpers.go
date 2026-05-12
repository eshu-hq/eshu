package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

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
