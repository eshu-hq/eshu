// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"errors"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
)

// ProviderFailure is a bounded provider failure returned to claim handling.
type ProviderFailure struct {
	failureClass string
	terminal     bool
	rateLimit    securityalerts.GitHubRateLimitInfo
	cause        error
}

// Error returns a bounded failure string safe for logs and status.
func (f ProviderFailure) Error() string {
	if f.failureClass == "" {
		return "security alert provider failure"
	}
	return "security alert provider failure: " + f.failureClass
}

// Unwrap returns the underlying provider error.
func (f ProviderFailure) Unwrap() error {
	return f.cause
}

// FailureClass returns the workflow retry class for this provider failure.
func (f ProviderFailure) FailureClass() string {
	if f.failureClass == "" {
		return FailureRetryable
	}
	return f.failureClass
}

// TerminalFailure reports whether workflow should stop retrying this claim.
func (f ProviderFailure) TerminalFailure() bool {
	return f.terminal
}

// RateLimit returns bounded provider retry metadata for tests and callers.
func (f ProviderFailure) RateLimit() securityalerts.GitHubRateLimitInfo {
	return f.rateLimit
}

func classifiedProviderFailure(err error) ProviderFailure {
	var ghErr securityalerts.GitHubDependabotError
	if errors.As(err, &ghErr) {
		class := mapGitHubFailureClass(ghErr.FailureClass())
		return ProviderFailure{
			failureClass: class,
			terminal:     ghErr.TerminalFailure(),
			rateLimit:    ghErr.RateLimit,
			cause:        err,
		}
	}
	failure := sdk.ClassifyProviderFailure(
		ProviderGitHubDependabot,
		err,
		sdk.StatusPolicy{},
		sdk.FailureRetryable,
	)
	return ProviderFailure{
		failureClass: failure.FailureClass(),
		terminal:     failure.TerminalFailure(),
		cause:        failure,
	}
}

func mapGitHubFailureClass(class string) string {
	switch class {
	case securityalerts.GitHubDependabotFailureAuthDenied:
		return FailureAuthDenied
	case securityalerts.GitHubDependabotFailureNotFound:
		return FailureNotFound
	case securityalerts.GitHubDependabotFailureRateLimited:
		return FailureRateLimited
	case securityalerts.GitHubDependabotFailureTerminal:
		return FailureTerminal
	default:
		return FailureRetryable
	}
}
