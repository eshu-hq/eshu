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
)

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
