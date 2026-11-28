// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// ErrExecutionClaimRejected means a side effect was fenced because the exact
// reducer claim that began the execution is no longer live. The service must
// not acknowledge or fail the queue row: it may already belong to a newer
// claimant.
var ErrExecutionClaimRejected = errors.New("reducer execution claim rejected")

// IntentStatus captures the durable reducer intent lifecycle state.
type IntentStatus string

const (
	// IntentStatusPending means the intent is ready to be claimed.
	IntentStatusPending IntentStatus = "pending"
	// IntentStatusClaimed means the intent has been leased for execution.
	IntentStatusClaimed IntentStatus = "claimed"
	// IntentStatusRunning means the reducer is actively processing the intent.
	IntentStatusRunning IntentStatus = "running"
	// IntentStatusSucceeded means the intent finished successfully.
	IntentStatusSucceeded IntentStatus = "succeeded"
	// IntentStatusFailed means the intent is terminally failed.
	IntentStatusFailed IntentStatus = "failed"
)

// ResultStatus captures the terminal outcome of one reducer execution.
type ResultStatus string

const (
	// ResultStatusSucceeded means the execution completed successfully.
	ResultStatusSucceeded ResultStatus = "succeeded"
	// ResultStatusFailed means the execution failed.
	ResultStatusFailed ResultStatus = "failed"
	// ResultStatusSuperseded means a newer scope generation replaced the intent.
	ResultStatusSuperseded ResultStatus = "superseded"
)

// FailureRecord captures the durable reducer failure classification.
type FailureRecord struct {
	FailureClass string
	Message      string
	Details      string
}

// SharedEdgeTargetNotReadyFailureClass classifies a shared-edge batch write
// deferred because one of its runtime targets is not yet in the graph. It
// lives in the contract package because every producer and consumer of the
// class already imports it: the shared-edge writer's target-presence guard
// (go/internal/storage/cypher/edge/writer/unroutable.go), the code-call
// matched-count shortfall (go/internal/storage/cypher/edge/writer/code_call_matched.go),
// the queue readiness SQL
// (go/internal/storage/postgres/reducer_queue_readiness_sql.go), and the
// code-call projection shortfall bound
// (go/internal/reducer/code/call/projection/bound.go), which must never
// import the reducer root. A single definition keeps the writer and the
// bound from drifting apart: if the writer's class ever stops matching the
// bound's, shortfall parks never expire (#10947).
//
// Registered as a non-counting reducer retry class
// (nonCountingReducerRetryFailureClasses in
// go/internal/storage/postgres/reducer_queue_readiness_sql.go): the target
// node is committed by another scope's materialization with no happens-before
// against the batch, so the miss is a timing state, not a payload defect.
const SharedEdgeTargetNotReadyFailureClass = "shared_edge_target_not_ready"

// RetryableError marks reducer failures that should re-enter the durable queue.
type RetryableError interface {
	error
	Retryable() bool
}

// IsRetryable reports whether an error explicitly opts into bounded retries.
func IsRetryable(err error) bool {
	var retryable RetryableError
	if !errors.As(err, &retryable) {
		return false
	}
	return retryable.Retryable()
}

// ExpectedWaitError marks a retryable reducer failure that is a designed wait
// for upstream evidence rather than a defect (security_alert_reconciliation
// waiting for package impact findings). The queue still counts it toward the
// retry budget; the Service only uses the marker to record the attempt as
// deferred at WARN instead of failed at ERROR while the attempt is below the
// budget (#8826). A handler may stop waiting before the budget ends; the
// security alert reconciliation does at attempt 3 and writes instead.
type ExpectedWaitError interface {
	error
	ExpectedWait() bool
}

// IsExpectedWait reports whether an error is both retryable and declares itself
// an expected wait. A non-retryable error never qualifies, so a terminal
// failure cannot hide behind the marker.
func IsExpectedWait(err error) bool {
	if !IsRetryable(err) {
		return false
	}
	var wait ExpectedWaitError
	if !errors.As(err, &wait) {
		return false
	}
	return wait.ExpectedWait()
}

// Intent describes one durable reducer follow-up action keyed by scope generation.
type Intent struct {
	IntentID     string
	ScopeID      string
	GenerationID string
	SourceSystem string
	Domain       Domain
	Cause        string
	Priority     int
	AttemptCount int
	// ClaimEpoch is a monotonic execution fence for queue domains whose retry
	// attempt count may be reset by replay. Zero means the domain has not
	// opted into claim-epoch fencing.
	ClaimEpoch      int64
	EntityKeys      []string
	RelatedScopeIDs []string
	Payload         map[string]any
	Status          IntentStatus
	EnqueuedAt      time.Time
	AvailableAt     time.Time
	ClaimedAt       *time.Time
	CompletedAt     *time.Time
	Failure         *FailureRecord

	// CycleStartedAt anchors "how long has this row been in its current
	// repair cycle", as opposed to EnqueuedAt's "when was this row first
	// created". It equals EnqueuedAt for a row that has never been reopened;
	// ReopenSucceeded/ReplayDomain (go/internal/storage/postgres/reducer_queue_replay.go)
	// reset it to the reopen timestamp, the same moment they reset
	// AttemptCount to 0, so a maintenance-triggered reopen gets a genuinely
	// fresh window instead of inheriting an already-expired one. Unlike
	// AvailableAt, ordinary claim/retry/fail never touch it (see
	// reducer_queue.go's retryReducerWorkQuery), so it is immune to the
	// AttemptCount freeze that a non-counting retry failure class causes
	// (nonCountingReducerRetryFailureClasses). A bounded readiness defer that
	// needs a per-reopen grace window (e.g. AWS cloud runtime drift's
	// awsCloudRuntimeDriftStatePendingMaxWait) should compare elapsed time
	// against CycleStartedAt, not EnqueuedAt.
	CycleStartedAt time.Time

	// RequeueReason names why the row last returned to an outstanding
	// state, from the closed #11615 vocabulary (retry_backoff,
	// readiness_wait, waiting_wake, maintenance_reopen,
	// producer_activation_settle, completion_wake, cross_scope_replay,
	// fanout_holder, retouch_new_evidence, admin_replay, admin_reopen,
	// recovery_redrive). Empty means the row was never requeued (or
	// predates the column) and reads as legacy_unclassified. Writers
	// validate the vocabulary at the queue boundary; this field carries
	// the stored value through without re-validating so rolling upgrades
	// keep reading rows stamped by newer writers.
	RequeueReason string
}

// ScopeGenerationKey returns the durable scope-generation boundary for the intent.
func (i Intent) ScopeGenerationKey() string {
	return fmt.Sprintf("%s:%s", i.ScopeID, i.GenerationID)
}

// Validate checks the durable intent contract.
func (i Intent) Validate() error {
	if strings.TrimSpace(i.IntentID) == "" {
		return errors.New("intent_id must not be blank")
	}
	if strings.TrimSpace(i.ScopeID) == "" {
		return errors.New("scope_id must not be blank")
	}
	if strings.TrimSpace(i.GenerationID) == "" {
		return errors.New("generation_id must not be blank")
	}
	if strings.TrimSpace(i.SourceSystem) == "" {
		return errors.New("source_system must not be blank")
	}
	if err := i.Domain.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(i.Cause) == "" {
		return errors.New("cause must not be blank")
	}
	if i.EnqueuedAt.IsZero() {
		return errors.New("enqueued_at must not be zero")
	}
	if i.AvailableAt.IsZero() {
		return errors.New("available_at must not be zero")
	}
	if len(i.RelatedScopeIDs) == 0 {
		return errors.New("related_scope_ids must not be empty")
	}
	if err := i.Status.Validate(); err != nil {
		return err
	}

	for _, key := range i.EntityKeys {
		if strings.TrimSpace(key) == "" {
			return errors.New("entity_keys must not contain blank values")
		}
	}
	var seenRelatedScopes map[string]struct{}
	for _, scopeID := range i.RelatedScopeIDs {
		normalizedScopeID := strings.TrimSpace(scopeID)
		if normalizedScopeID == "" {
			return errors.New("related_scope_ids must not contain blank values")
		}
		if seenRelatedScopes == nil {
			seenRelatedScopes = make(map[string]struct{}, len(i.RelatedScopeIDs))
		}
		if _, exists := seenRelatedScopes[normalizedScopeID]; exists {
			return errors.New("related_scope_ids must not contain duplicate values")
		}
		seenRelatedScopes[normalizedScopeID] = struct{}{}
	}

	return nil
}

// Clone detaches the intent's slices, top-level payload map, timestamps, and
// failure record. Values nested inside Payload remain shared.
func (i Intent) Clone() Intent {
	cloned := i
	cloned.EntityKeys = slices.Clone(i.EntityKeys)
	cloned.RelatedScopeIDs = slices.Clone(i.RelatedScopeIDs)
	if i.Payload != nil {
		cloned.Payload = make(map[string]any, len(i.Payload))
		for key, value := range i.Payload {
			cloned.Payload[key] = value
		}
	}
	if i.ClaimedAt != nil {
		claimedAt := *i.ClaimedAt
		cloned.ClaimedAt = &claimedAt
	}
	if i.CompletedAt != nil {
		completedAt := *i.CompletedAt
		cloned.CompletedAt = &completedAt
	}
	if i.Failure != nil {
		failure := *i.Failure
		cloned.Failure = &failure
	}
	return cloned
}

// Validate checks that the lifecycle state is a known durable value.
func (status IntentStatus) Validate() error {
	switch status {
	case IntentStatusPending, IntentStatusClaimed, IntentStatusRunning, IntentStatusSucceeded, IntentStatusFailed:
		return nil
	default:
		return fmt.Errorf("unknown intent status %q", status)
	}
}

// Terminal reports whether the status represents a final state.
func (status IntentStatus) Terminal() bool {
	switch status {
	case IntentStatusSucceeded, IntentStatusFailed:
		return true
	default:
		return false
	}
}

// WithStatus returns a clone of the intent with the status and timestamp.
func (i Intent) WithStatus(status IntentStatus, at time.Time) Intent {
	cloned := i.Clone()
	cloned.Status = status
	switch status {
	case IntentStatusPending:
	case IntentStatusClaimed, IntentStatusRunning:
		cloned.ClaimedAt = &at
	case IntentStatusSucceeded, IntentStatusFailed:
		cloned.CompletedAt = &at
	}
	return cloned
}
