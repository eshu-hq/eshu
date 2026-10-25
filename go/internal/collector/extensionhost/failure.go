// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
)

const (
	// FailureClassInvalidClaim marks corrupt or unsupported host claim input.
	FailureClassInvalidClaim = "invalid_claim"
	// FailureClassInvalidResult marks SDK output rejected before fact commit.
	FailureClassInvalidResult = "invalid_result"
	// FailureClassIdentityMismatch marks output for a different claim identity.
	FailureClassIdentityMismatch = "identity_mismatch"
	// FailureClassLaunchFailure marks host-side extension launch failure.
	FailureClassLaunchFailure = "launch_failure"
	// FailureClassStatusRecord marks host failure while recording SDK status.
	FailureClassStatusRecord = "status_record"
)

// MaxRetryAfterDelay caps the provider retry delay an extension can request
// through status.retry_after_seconds on a retryable result. The SDK only
// requires the value to be non-negative, so without a cap one result could
// hide its work item for years (or overflow time.Duration) while the claim
// row still counts the attempt. One hour matches the GitHub App token-mint
// Retry-After cap; a provider that needs longer is retried hourly until its
// attempt budget runs out.
const MaxRetryAfterDelay = time.Hour

var failureClassPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,95}$`)

type extensionFailure struct {
	class    string
	terminal bool
	// retryAfter is the provider-requested delay before the next attempt,
	// already clamped to MaxRetryAfterDelay. Zero means no request.
	retryAfter time.Duration
	cause      error
}

func (e extensionFailure) Error() string {
	if e.cause == nil {
		return "collector extension failure: " + e.class
	}
	return "collector extension failure: " + e.class + ": " + e.cause.Error()
}

func (e extensionFailure) Unwrap() error {
	return e.cause
}

// FailureClass returns the workflow failure class for ClaimedService.
func (e extensionFailure) FailureClass() string {
	return e.class
}

// TerminalFailure reports whether ClaimedService should stop retrying.
func (e extensionFailure) TerminalFailure() bool {
	return e.terminal
}

// RetryAfterDelay returns the provider-requested retry delay.
// ClaimedService uses max(PollInterval, this) as the re-queue delay of a
// retryable claim, and none once the claim's attempt budget is spent. It is the
// largest status.retry_after_seconds on the result, clamped to
// MaxRetryAfterDelay, or zero when the result requested none.
func (e extensionFailure) RetryAfterDelay() time.Duration {
	return e.retryAfter
}

// retryAfterFromStatuses returns the largest retry_after_seconds across the
// result's statuses as a duration clamped to MaxRetryAfterDelay. The clamp is
// applied in seconds before the multiply so no int value can overflow
// time.Duration.
func retryAfterFromStatuses(result sdkcollector.Result) time.Duration {
	largest := 0
	for _, status := range result.Statuses {
		largest = max(largest, status.RetryAfterSeconds)
	}
	if largest <= 0 {
		return 0
	}
	if capSeconds := int(MaxRetryAfterDelay / time.Second); largest > capSeconds {
		return MaxRetryAfterDelay
	}
	return time.Duration(largest) * time.Second
}

func validateBoundedStatuses(result sdkcollector.Result) error {
	for _, status := range result.Statuses {
		failureClass := strings.TrimSpace(status.FailureClass)
		if failureClass == "" {
			continue
		}
		if failureClass != status.FailureClass || !failureClassPattern.MatchString(failureClass) {
			return fmt.Errorf("status.failure_class %q is not bounded", status.FailureClass)
		}
	}
	return nil
}

func failureClassFromStatus(result sdkcollector.Result, fallback string) string {
	for _, status := range result.Statuses {
		if status.Class == sdkcollector.StatusFailure {
			if value := strings.TrimSpace(status.FailureClass); value != "" {
				return value
			}
		}
	}
	return fallback
}
