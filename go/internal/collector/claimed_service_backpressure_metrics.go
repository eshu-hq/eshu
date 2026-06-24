// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	claimThrottleOutcomeRetryAfterHonored = "retry_after_honored"
	claimThrottleOutcomePollBackoff       = "poll_backoff"
)

// recordAttemptBudgetExhausted records one retryable claim escalated to terminal
// because the work item exhausted its bounded retry budget, labeled by
// collector_kind and source_system.
func (s ClaimedService) recordAttemptBudgetExhausted(ctx context.Context, item workflow.WorkItem) {
	if s.Instruments == nil || s.Instruments.WorkflowClaimAttemptBudgetExhausted == nil {
		return
	}
	s.Instruments.WorkflowClaimAttemptBudgetExhausted.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrCollectorKind(string(s.CollectorKind)),
		telemetry.AttrSourceSystem(item.SourceSystem),
	))
}

// recordClaimRetry records one retryable claim re-queue for the collector
// family, labeled by collector_kind, source_system, and failure_class. It uses
// only bounded labels so an operator can attribute retry pressure to a family
// and cause without high-cardinality provider targets, accounts, URLs, or
// instance identity.
func (s ClaimedService) recordClaimRetry(ctx context.Context, item workflow.WorkItem, failureClass string) {
	if s.Instruments == nil || s.Instruments.WorkflowClaimRetries == nil {
		return
	}
	s.Instruments.WorkflowClaimRetries.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrCollectorKind(string(s.CollectorKind)),
		telemetry.AttrSourceSystem(item.SourceSystem),
		telemetry.AttrFailureClass(failureClass),
	))
}

// recordProviderThrottle records one provider-backpressure outcome for the
// collector family when a retryable failure is provider rate-limiting or
// carries a provider Retry-After delay. The outcome distinguishes a honored
// provider Retry-After from a default poll backoff so an operator can tell
// provider-driven pacing from local backoff.
func (s ClaimedService) recordProviderThrottle(ctx context.Context, item workflow.WorkItem, failureClass string, err error) {
	if s.Instruments == nil || s.Instruments.WorkflowClaimProviderThrottles == nil {
		return
	}
	if !isProviderThrottle(failureClass, err) {
		return
	}
	s.Instruments.WorkflowClaimProviderThrottles.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrCollectorKind(string(s.CollectorKind)),
		telemetry.AttrSourceSystem(item.SourceSystem),
		telemetry.AttrOutcome(s.providerThrottleOutcome(err)),
	))
}

// recordClaimLeaseAge records how long the active claim has been held at the
// current heartbeat, labeled by collector_kind and source_system. A lease age
// trending toward the lease TTL is the operator signal that a collector family
// is stalling under load before the lease is reaped.
func (s ClaimedService) recordClaimLeaseAge(ctx context.Context, item workflow.WorkItem, ageSeconds float64) {
	if s.Instruments == nil || s.Instruments.WorkflowClaimLeaseAge == nil {
		return
	}
	if ageSeconds < 0 {
		ageSeconds = 0
	}
	s.Instruments.WorkflowClaimLeaseAge.Record(ctx, ageSeconds, metric.WithAttributes(
		telemetry.AttrCollectorKind(string(s.CollectorKind)),
		telemetry.AttrSourceSystem(item.SourceSystem),
	))
}

// providerThrottleOutcome reports whether a retryable failure's backoff used a
// provider-supplied Retry-After delay or fell back to the local poll interval.
func (s ClaimedService) providerThrottleOutcome(err error) string {
	var retryAfter retryAfterFailure
	if errors.As(err, &retryAfter) && retryAfter.RetryAfterDelay() > s.PollInterval {
		return claimThrottleOutcomeRetryAfterHonored
	}
	return claimThrottleOutcomePollBackoff
}

// isProviderThrottle reports whether a retryable failure represents provider
// backpressure. It must NOT fire for ordinary retryable failures (5xx,
// transport errors, context deadlines): claim-aware SDK collectors wrap those
// in sdk.ProviderFailure, whose RetryAfterDelay() returns 0 when the provider
// gave no Retry-After, so an errors.As check alone would count every generic
// outage as throttling. A failure is throttling only when its class is an
// explicit rate-limited class, or when the provider supplied a positive
// Retry-After delay.
func isProviderThrottle(failureClass string, err error) bool {
	if failureClass == RegistryFailureRateLimited || failureClass == string(sdk.FailureRateLimited) {
		return true
	}
	var retryAfter retryAfterFailure
	return errors.As(err, &retryAfter) && retryAfter.RetryAfterDelay() > 0
}
