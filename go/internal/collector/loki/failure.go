package loki

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const (
	// FailureRetryable marks transient provider or transport failures.
	FailureRetryable = "retryable"
	// FailureRateLimited marks provider throttling as retryable.
	FailureRateLimited = "rate_limited"
	// FailureAuthDenied marks authentication or authorization failures.
	FailureAuthDenied = "auth_denied"
	// FailureTerminal marks malformed or unsupported terminal failures.
	FailureTerminal = "terminal"
)

// ProviderHTTPError carries a bounded HTTP provider failure.
type ProviderHTTPError struct {
	StatusCode int
	Message    string
}

func (e ProviderHTTPError) Error() string {
	if strings.TrimSpace(e.Message) == "" {
		return fmt.Sprintf("loki provider returned status %d", e.StatusCode)
	}
	return fmt.Sprintf("loki provider returned status %d: %s", e.StatusCode, e.Message)
}

// ProviderAPIError carries a bounded Loki API status failure.
type ProviderAPIError struct {
	Status    string
	ErrorType string
}

func (e ProviderAPIError) Error() string {
	status := strings.TrimSpace(e.Status)
	if status == "" {
		status = "error"
	}
	errorType := strings.TrimSpace(e.ErrorType)
	if errorType == "" {
		return fmt.Sprintf("loki provider returned API status %q", status)
	}
	return fmt.Sprintf("loki provider returned API status %q: %s", status, errorType)
}

// ProviderFailure wraps a Loki provider failure with a bounded failure class.
type ProviderFailure struct {
	failureClass string
	cause        error
}

func (f ProviderFailure) Error() string {
	if f.cause == nil {
		return f.FailureClass()
	}
	return f.cause.Error()
}

func (f ProviderFailure) Unwrap() error {
	return f.cause
}

// FailureClass returns the bounded provider failure class.
func (f ProviderFailure) FailureClass() string {
	if f.failureClass == "" {
		return FailureRetryable
	}
	return f.failureClass
}

func classifiedProviderFailure(err error) ProviderFailure {
	if err == nil {
		return ProviderFailure{failureClass: FailureRetryable}
	}
	var providerErr ProviderHTTPError
	if errors.As(err, &providerErr) {
		switch providerErr.StatusCode {
		case http.StatusTooManyRequests:
			return ProviderFailure{failureClass: FailureRateLimited, cause: err}
		case http.StatusUnauthorized, http.StatusForbidden:
			return ProviderFailure{failureClass: FailureAuthDenied, cause: err}
		case http.StatusBadRequest, http.StatusNotFound:
			return ProviderFailure{failureClass: FailureTerminal, cause: err}
		default:
			if providerErr.StatusCode >= 500 {
				return ProviderFailure{failureClass: FailureRetryable, cause: err}
			}
		}
	}
	var apiErr ProviderAPIError
	if errors.As(err, &apiErr) {
		if strings.TrimSpace(apiErr.ErrorType) == "bad_data" {
			return ProviderFailure{failureClass: FailureTerminal, cause: err}
		}
		return ProviderFailure{failureClass: FailureRetryable, cause: err}
	}
	return ProviderFailure{failureClass: FailureRetryable, cause: err}
}
