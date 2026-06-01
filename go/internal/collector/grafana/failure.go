package grafana

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

// GrafanaError carries a bounded HTTP provider failure.
type GrafanaError struct {
	StatusCode int
	Message    string
}

func (e GrafanaError) Error() string {
	if strings.TrimSpace(e.Message) == "" {
		return fmt.Sprintf("grafana provider returned status %d", e.StatusCode)
	}
	return fmt.Sprintf("grafana provider returned status %d: %s", e.StatusCode, e.Message)
}

// ProviderFailure wraps a Grafana failure with a bounded failure class.
type ProviderFailure struct {
	failureClass string
	cause        error
}

func (f ProviderFailure) Error() string {
	if f.cause == nil {
		return f.failureClass
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
	var grafanaErr GrafanaError
	if errors.As(err, &grafanaErr) {
		switch grafanaErr.StatusCode {
		case http.StatusTooManyRequests:
			return ProviderFailure{failureClass: FailureRateLimited, cause: err}
		case http.StatusUnauthorized, http.StatusForbidden:
			return ProviderFailure{failureClass: FailureAuthDenied, cause: err}
		case http.StatusBadRequest, http.StatusNotFound:
			return ProviderFailure{failureClass: FailureTerminal, cause: err}
		default:
			if grafanaErr.StatusCode >= 500 {
				return ProviderFailure{failureClass: FailureRetryable, cause: err}
			}
		}
	}
	return ProviderFailure{failureClass: FailureRetryable, cause: err}
}
