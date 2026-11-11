// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import "time"

const (
	defaultClaimLeaseTTL            = 60 * time.Second
	defaultHeartbeatInterval        = 20 * time.Second
	defaultReapInterval             = 20 * time.Second
	defaultExpiredClaimLimit        = 100
	defaultExpiredClaimRequeueDelay = 5 * time.Second
	// defaultClaimMaxAttempts bounds collector retries on one workflow work
	// item. The runtime escalates a retryable failure to terminal once the
	// item's AttemptCount reaches this value. The default is intentionally
	// generous so transient throttles and timeouts still recover; the guard
	// is for the runaway loop in issue #612 where a permanent failure
	// (orphaned stale fence, IAM gap, unsupported target) drove
	// workflow_claims.failed_retryable into the millions.
	defaultClaimMaxAttempts = 10
	// defaultExpiredLeaseMaxRequeues caps expired-lease requeues on one
	// workflow work item (eshu-hq/eshu#10113). The reaper dead-letters the
	// item instead of requeueing once its AttemptCount has reached this
	// value without a returned failure verdict, so a run that never returns
	// (for example a pod evicted over its ephemeral allowance) cannot loop
	// forever. The value intentionally equals defaultClaimMaxAttempts so one
	// mental model bounds both a run that keeps failing retryably and a run
	// that never returns; normal lease churn of one or two expiries keeps
	// working as today.
	defaultExpiredLeaseMaxRequeues = 10
)

// FailureClassExpiredLeaseBudgetExhausted labels workflow work items the
// expired-lease reaper dead-lettered after the item's AttemptCount reached
// DefaultExpiredLeaseMaxRequeues without a returned failure verdict
// (eshu-hq/eshu#10113). It lives in the workflow package (not the collector
// package) because the writer is the storage reaper, not a collector runner.
const FailureClassExpiredLeaseBudgetExhausted = "expired_lease_budget_exhausted"

// FailureClassOperatorRetiredUnclaimed labels workflow work items the
// sanctioned never-claimed retirement path moved to failed_terminal:
// pending rows with attempt_count 0 and no claim for a deconfigured
// collector instance (eshu-hq/eshu#10532). It lives in the workflow package
// because the writer is the storage layer on an operator action, not a
// collector runner. Failure-reporting reads attribute it like any other
// terminal class; status reads bind only enabled, non-deactivated instances,
// so a retired row of a deconfigured instance never surfaces as a serving
// failure.
const FailureClassOperatorRetiredUnclaimed = "operator_retired_unclaimed"

// FailureOperationOperatorRetire is the bounded claim-stage token recorded
// on rows the sanctioned never-claimed retirement path retires
// (eshu-hq/eshu#10532). It carries only the operation, never provider or
// row text, so failure-reporting selects stay safe to expose.
const FailureOperationOperatorRetire = "operator_retire"

func DefaultClaimLeaseTTL() time.Duration {
	return defaultClaimLeaseTTL
}

func DefaultHeartbeatInterval() time.Duration {
	return defaultHeartbeatInterval
}

func DefaultReapInterval() time.Duration {
	return defaultReapInterval
}

func DefaultExpiredClaimLimit() int {
	return defaultExpiredClaimLimit
}

func DefaultExpiredClaimRequeueDelay() time.Duration {
	return defaultExpiredClaimRequeueDelay
}

// DefaultClaimMaxAttempts returns the bounded retry budget collector runners
// MUST apply to ClaimedService.MaxAttempts unless the deployment overrides
// it. See issue #612 for the runtime symptom this guard prevents.
func DefaultClaimMaxAttempts() int {
	return defaultClaimMaxAttempts
}

// DefaultExpiredLeaseMaxRequeues returns the bounded expired-lease requeue
// budget the workflow reaper enforces per work item (eshu-hq/eshu#10113).
// See defaultExpiredLeaseMaxRequeues for the value rationale.
func DefaultExpiredLeaseMaxRequeues() int {
	return defaultExpiredLeaseMaxRequeues
}
