// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"errors"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
)

// ProviderFailure is a bounded provider failure returned to claim handling.
// operation names the bounded endpoint token for the call that failed (for
// example "list_repo_alerts"); it is preserved from the inner cause and never
// carries a repository, organization, URL, or credential. Empty means the
// cause carried no token and claim handling falls back to its stage
// operation.
type ProviderFailure struct {
	failureClass string
	terminal     bool
	rateLimit    securityalerts.GitHubRateLimitInfo
	operation    string
	// alertsDisabled carries the provider's bounded "Dependabot alerts are
	// disabled for this repository" fact (#8805). It never holds the message.
	alertsDisabled bool
	cause          error
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

// FailureOperation returns the bounded endpoint token preserved from the
// inner cause, or "" when the cause carries none.
func (f ProviderFailure) FailureOperation() string {
	return f.operation
}

// AlertsDisabled reports whether the provider answered that Dependabot alerts
// are disabled for the repository read. The failure class is still the
// provider's (auth_denied); only the org fan-out uses the marker to continue.
func (f ProviderFailure) AlertsDisabled() bool {
	return f.alertsDisabled
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
			failureClass:   class,
			terminal:       ghErr.TerminalFailure(),
			rateLimit:      ghErr.RateLimit,
			operation:      ghErr.FailureOperation(),
			alertsDisabled: ghErr.AlertsDisabled,
			cause:          err,
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
		operation:    failure.FailureOperation(),
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
