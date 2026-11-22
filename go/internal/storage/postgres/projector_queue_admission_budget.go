// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	queuestore "github.com/eshu-hq/eshu/go/internal/storage/postgres/queue"
)

// This file holds the projector admission-wait budget (#10671): the split
// of the shared attempt fence into a projection budget and a dedicated,
// finite admission budget for backpressure admission-timeout waits. It is
// split out of projector_queue.go to keep both files under the 500-line
// cap; Fail calls retryDecision and the queue accessor below.

// defaultProjectorAdmissionMaxAttempts bounds admission-timeout waits per
// projector work item. A budget of N endures N waits: the first N-1 retry
// and the Nth dead-letters, matching the sibling projection fence (the Nth
// failed claim dead-letters). Derivation under defaults (5-minute
// admission wait per cycle from reducer_admission_timeout.go, 30s
// retry-delay base with the shared exponential backoff capped at 1h):
// each of the 48 cycles waits up to 5 minutes in admission (48 x 5m = 4h)
// plus a backoff delay of 30s, 1m, 2m, ... capped at 1h once the
// exponential reaches it after ~7 cycles, so the remaining ~41 cycles add
// ~41h. Total wall-clock to exhaust is roughly 44-46h — more than 6x the
// ~7h episode that killed chunk-2/chunk-3 on the shared budget of 3,
// while a permanently stalled queue still dead-letters inside 2 days per
// #10485.
const defaultProjectorAdmissionMaxAttempts = 48

// admissionMaxAttempts returns the configured admission-wait budget,
// falling back to defaultProjectorAdmissionMaxAttempts when unset. There
// is no disable mode: an unbounded admission budget would reintroduce the
// infinite worker parking #10485 removed.
func (q ProjectorQueue) admissionMaxAttempts() int {
	if q.AdmissionMaxAttempts > 0 {
		return q.AdmissionMaxAttempts
	}

	return defaultProjectorAdmissionMaxAttempts
}

// isAdmissionTimeoutCause reports whether err is a backpressure
// admission-timeout failure: an enqueue that waited for queue depth to
// fall below the admission marks past its wait bound. It matches the
// failure.AdmissionTimeoutError narrow interface through any %w
// wrapping (the projector runtime wraps the ingester Enqueue error as
// "enqueue reducer intents: %w"), so the production wrap chain is
// covered without importing the ingester main package.
func isAdmissionTimeoutCause(err error) bool {
	var admissionErr failure.AdmissionTimeoutError
	return errors.As(err, &admissionErr) && admissionErr.IsAdmissionTimeout()
}

// retryKind names which budget a retry path spends: the shared projection
// budget, the finite admission-wait budget, or neither (a source-paced
// release, #10882).
type retryKind int

const (
	// retryKindProjection spends the projection attempt budget.
	retryKindProjection retryKind = iota
	// retryKindAdmission spends the finite admission-wait budget.
	retryKindAdmission
	// retryKindSourcePaced spends no budget: the wait is refunded from the
	// projection budget and has no admission budget to exhaust.
	retryKindSourcePaced
)

// isSourcePacedCause reports whether err is a per-source backpressure
// release (#10882) through any %w wrapping, matching the narrow
// failure.SourcePacedError interface.
func isSourcePacedCause(err error) bool {
	var pacedErr failure.SourcePacedError
	return errors.As(err, &pacedErr) && pacedErr.IsSourcePaced()
}

// projectionAttempts is the number of claims since the last operator replay
// that spent the projection budget: AttemptCount minus the waits refunded to the
// admission and source-paced counters (AdmissionWaitAttempts,
// SourcePacedWaits) minus ReplayAttemptBaseline (#11182). Replay sets the
// baseline to the row's AttemptCount and resets both refund counters, so the
// first claim after a replay counts 1, as a new row's first claim does, while
// AttemptCount keeps growing as the claim fence. A row never replayed has
// baseline 0, which is the #10671 rule unchanged. There is no clamp: every
// writer that lowers AttemptCount (reproject, refinalize) resets the baseline
// and both refund counters to 0 in the same statement.
func projectionAttempts(work projector.ScopeGenerationWork) int {
	return work.AttemptCount - work.AdmissionWaitAttempts - work.SourcePacedWaits - work.ReplayAttemptBaseline
}

// retryDecision splits the shared attempt fence into three budgets. A
// source-paced release (#10882) always retries: pacing defers a source's
// work and must never dead-letter it, so it is bounded by the source's
// latch (Stuck stays loud), not an attempt count. An admission-timeout
// failure retries iff the item's admission waits are under the admission
// budget, independent of the projection budget; any other retryable
// failure retries iff the claims that spent the projection budget
// (AttemptCount minus the admission and source-paced refunds) are under
// the projection budget, so crash/expired-lease claims keep today's
// budget-burning semantics. The second return value names which counter
// and telemetry the retry follows.
func (q ProjectorQueue) retryDecision(work projector.ScopeGenerationWork, cause error) (willRetry bool, kind retryKind) {
	if !failure.IsRetryable(cause) {
		return false, retryKindProjection
	}
	if isSourcePacedCause(cause) {
		return true, retryKindSourcePaced
	}
	if isAdmissionTimeoutCause(cause) {
		// +1 counts the wait being failed, matching the sibling fence
		// (AttemptCount counts the claim being failed, retrying while
		// AttemptCount < MaxAttempts). A budget of N endures N waits
		// (N-1 granted retries, death on the Nth).
		return work.AdmissionWaitAttempts+1 < q.admissionMaxAttempts(), retryKindAdmission
	}
	return projectionAttempts(work) < q.maxAttempts(), retryKindProjection
}

// retryBackoff is the retry delay for one failure. A source-paced release
// (#10882) is scheduled at max(grant delay, min(paced exponential, resume
// bound)) plus jitter: the exponential backs off on source_paced_waits but is
// capped at the cause's resume bound (the release age), so a long engagement
// cannot park an item for the full retry cap and a released source's paced
// backlog is visible again within about the bound. A cause with no bound keeps
// the queue cap, the grant delay wins when larger, and the item is never
// claimable before it.
func (q ProjectorQueue) retryBackoff(kind retryKind, attempt int, cause error) time.Duration {
	maxDelay := q.retryMaxDelay()
	if kind == retryKindSourcePaced {
		if bound := failure.ResumeBoundHint(cause); bound > 0 && (maxDelay <= 0 || bound < maxDelay) {
			maxDelay = bound
		}
	}
	delay := queuestore.ComputeRetryDelay(q.retryDelay(), maxDelay, q.JitterFraction, attempt, q.jitterSource())
	if kind == retryKindSourcePaced {
		delay = max(delay, failure.RetryAfterHint(cause))
	}
	return delay
}
