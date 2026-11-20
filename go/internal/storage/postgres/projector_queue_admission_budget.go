// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
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

// retryDecision splits the shared attempt fence into two budgets. An
// admission-timeout failure retries iff the item's admission waits are
// under the admission budget, independent of the projection budget; any
// other retryable failure retries iff the non-admission claims
// (AttemptCount minus AdmissionWaitAttempts) are under the projection
// budget, refunding admission waits while crash/expired-lease claims keep
// today's budget-burning semantics. The second return value reports
// whether the retry is an admission-wait retry (counter increment,
// backoff input, and telemetry follow it).
func (q ProjectorQueue) retryDecision(work projector.ScopeGenerationWork, cause error) (willRetry bool, admissionRetry bool) {
	retryable := failure.IsRetryable(cause)
	if !retryable {
		return false, false
	}
	if isAdmissionTimeoutCause(cause) {
		// +1 counts the wait being failed, matching the sibling fence
		// (AttemptCount counts the claim being failed, retrying while
		// AttemptCount < MaxAttempts). A budget of N endures N waits
		// (N-1 granted retries, death on the Nth).
		return work.AdmissionWaitAttempts+1 < q.admissionMaxAttempts(), true
	}
	return work.AttemptCount-work.AdmissionWaitAttempts < q.maxAttempts(), false
}
