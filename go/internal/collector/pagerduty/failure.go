package pagerduty

import "errors"

const (
	// FailureAuthDenied marks PagerDuty credential or permission failures as terminal.
	FailureAuthDenied = "auth_denied"
	// FailureNotFound marks missing PagerDuty resources as terminal.
	FailureNotFound = "not_found"
	// FailureRateLimited marks PagerDuty rate limiting as retryable.
	FailureRateLimited = "rate_limited"
	// FailureRetryable marks transient PagerDuty transport/provider failures.
	FailureRetryable = "retryable"
	// FailureTerminal marks malformed or otherwise non-retryable failures.
	FailureTerminal = "terminal"
)

// PagerDutyError carries bounded PagerDuty HTTP failure details.
type PagerDutyError struct {
	StatusCode int
	Message    string
}

// Error returns a bounded provider error string.
func (e PagerDutyError) Error() string {
	if e.StatusCode == 0 {
		return "pagerduty request failed"
	}
	return "pagerduty request failed with status"
}

// ProviderFailure is a bounded PagerDuty failure returned to claim handling.
type ProviderFailure struct {
	failureClass string
	terminal     bool
	cause        error
}

// Error returns a bounded failure string safe for logs and status.
func (f ProviderFailure) Error() string {
	if f.failureClass == "" {
		return "pagerduty provider failure"
	}
	return "pagerduty provider failure: " + f.failureClass
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

func classifiedProviderFailure(err error) ProviderFailure {
	var pdErr PagerDutyError
	if errors.As(err, &pdErr) {
		class, terminal := pagerDutyStatusClass(pdErr.StatusCode)
		return ProviderFailure{failureClass: class, terminal: terminal, cause: err}
	}
	return ProviderFailure{failureClass: FailureRetryable, cause: err}
}

func pagerDutyStatusClass(status int) (string, bool) {
	switch status {
	case 401, 403:
		return FailureAuthDenied, true
	case 404:
		return FailureNotFound, true
	case 408, 409, 425, 429:
		return FailureRateLimited, false
	default:
		if status >= 500 {
			return FailureRetryable, false
		}
		if status >= 400 {
			return FailureTerminal, true
		}
		return FailureRetryable, false
	}
}
