// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"errors"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type retryAfterFailure interface {
	RetryAfterDelay() time.Duration
}

// ClaimRetryDelay is the delay ClaimedService applies before a retryable
// claim is visible again: the larger of its poll interval and the provider's
// requested delay (zero when none was requested). It is exported so a
// component that reports the retry time, such as the extension host's
// vulnerability source-state recorder (#9420), uses the same arithmetic and
// cannot drift from the queue.
func ClaimRetryDelay(pollInterval, requested time.Duration) time.Duration {
	return max(pollInterval, requested)
}

// ClaimAttemptBudgetExhausted reports whether a retryable failure on item is
// escalated to terminal under a retry budget of maxAttempts: true once the
// work item's AttemptCount has reached a positive budget. maxAttempts <= 0
// disables the guard. ClaimedService and the extension host's source-state
// recorder (#9420) share it so a row and its claim agree on terminality.
func ClaimAttemptBudgetExhausted(maxAttempts int, item workflow.WorkItem) bool {
	return maxAttempts > 0 && item.AttemptCount >= maxAttempts
}

func (s ClaimedService) retryableVisibleAt(err error) time.Time {
	var requested time.Duration
	var retryAfter retryAfterFailure
	if errors.As(err, &retryAfter) {
		requested = retryAfter.RetryAfterDelay()
	}
	return s.now().Add(ClaimRetryDelay(s.PollInterval, requested))
}
