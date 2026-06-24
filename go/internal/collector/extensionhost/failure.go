// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"fmt"
	"regexp"
	"strings"

	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
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

var failureClassPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,95}$`)

type extensionFailure struct {
	class    string
	terminal bool
	cause    error
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
