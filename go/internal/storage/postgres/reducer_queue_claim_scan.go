// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"database/sql"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/payload"
)

// Claim-row scanning for the reducer queue: the single-claim and
// batch-claim queries share one column shape and one scanner, so a widened
// claim output stays byte-identical across both paths.

func claimedAtValue(intent reducercontract.Intent) time.Time {
	if intent.ClaimedAt == nil {
		return time.Time{}
	}
	return intent.ClaimedAt.UTC()
}

func scanReducerIntent(rows db.Rows) (reducercontract.Intent, error) {
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
	// #11075: the claim carries the prior verdict's failure evidence so a
	// deferred handler keeps its deferral anchor across retries. COALESCE
	// in the claim queries keeps all three non-NULL.
	var failureClass string
	var failureMessage string
	var failureDetails string
	// #11615: the last requeue reason rides the claim read-only. NULL means
	// the row was never requeued and reads as legacy_unclassified.
	var requeueReason sql.NullString
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
		&failureClass,
		&failureMessage,
		&failureDetails,
		&requeueReason,
		&rawPayload,
	); err != nil {
		return reducercontract.Intent{}, err
	}

	payload, err := payloadstore.UnmarshalPayload(rawPayload)
	if err != nil {
		return reducercontract.Intent{}, err
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
		return reducercontract.Intent{}, err
	}
	claimedAt = claimedAt.UTC()

	intent := reducercontract.Intent{
		// #11075: a prior verdict's failure evidence rides the claim so a
		// deferred handler keeps its deferral anchor across retries. Empty
		// means no prior verdict: the handler treats the evaluation as the
		// cycle's first deferral.
		Failure: func() *reducercontract.FailureRecord {
			if failureClass == "" {
				return nil
			}
			return &reducercontract.FailureRecord{
				FailureClass: failureClass,
				Message:      failureMessage,
				Details:      failureDetails,
			}
		}(),
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
		RequeueReason:   requeueReason.String,
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
		return reducercontract.Intent{}, err
	}

	return intent, nil
}
