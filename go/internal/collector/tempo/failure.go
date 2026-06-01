package tempo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// FailureClass is the bounded provider failure label stored in workflow status
// and telemetry.
type FailureClass string

const (
	// FailureAuthDenied marks missing or unauthorized Tempo credentials.
	FailureAuthDenied FailureClass = "auth_denied"
	// FailureRateLimited marks Tempo rate limiting as retryable.
	FailureRateLimited FailureClass = "rate_limited"
	// FailureRetryable marks transport or server-side failures as retryable.
	FailureRetryable FailureClass = "retryable"
	// FailureTerminal marks malformed provider responses or unsupported setup.
	FailureTerminal FailureClass = "terminal"
)

// ProviderFailure carries a bounded failure class without provider response
// bodies or credential-bearing request details.
type ProviderFailure struct {
	failureClass FailureClass
	cause        error
}

func (f ProviderFailure) Error() string {
	if f.failureClass == "" {
		return "tempo provider failure: terminal"
	}
	return "tempo provider failure: " + string(f.failureClass)
}

// Unwrap returns the underlying bounded cause.
func (f ProviderFailure) Unwrap() error {
	return f.cause
}

// FailureClass returns the bounded provider failure class.
func (f ProviderFailure) FailureClass() FailureClass {
	if f.failureClass == "" {
		return FailureTerminal
	}
	return f.failureClass
}

// ProviderHTTPError describes a bounded non-success HTTP response.
type ProviderHTTPError struct {
	StatusCode int
	Message    string
}

func (e ProviderHTTPError) Error() string {
	return fmt.Sprintf("tempo provider returned HTTP %d", e.StatusCode)
}

func classifiedProviderFailure(err error) ProviderFailure {
	var failure ProviderFailure
	if errors.As(err, &failure) {
		return failure
	}
	var httpErr ProviderHTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return ProviderFailure{failureClass: FailureAuthDenied, cause: err}
		case http.StatusTooManyRequests:
			return ProviderFailure{failureClass: FailureRateLimited, cause: err}
		default:
			if httpErr.StatusCode >= 500 {
				return ProviderFailure{failureClass: FailureRetryable, cause: err}
			}
			return ProviderFailure{failureClass: FailureTerminal, cause: err}
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ProviderFailure{failureClass: FailureRetryable, cause: err}
	}
	return ProviderFailure{failureClass: FailureTerminal, cause: err}
}
