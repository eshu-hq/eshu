package collector

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const (
	// RegistryFailureAuthDenied reports authentication or authorization denial
	// without exposing host, repository, package, account, or credential values.
	RegistryFailureAuthDenied = "registry_auth_denied"
	// RegistryFailureNotFound reports a missing registry repository, package, or
	// metadata endpoint without exposing the private object name.
	RegistryFailureNotFound = "registry_not_found"
	// RegistryFailureRateLimited reports registry throttling.
	RegistryFailureRateLimited = "registry_rate_limited"
	// RegistryFailureRetryable reports a transient registry or transport failure.
	RegistryFailureRetryable = "registry_retryable_failure"
	// RegistryFailureCanceled reports collector cancellation before completion.
	RegistryFailureCanceled = "registry_canceled"
	// RegistryFailureTerminal reports a non-retryable registry failure class that
	// is not auth-denied, not-found, rate-limited, or canceled.
	RegistryFailureTerminal = "registry_terminal_failure"
)

// RegistryFailure carries bounded registry failure metadata into workflow and
// queue status surfaces. Error text and details intentionally avoid registry
// hosts, repositories, package names, paths, tags, digests, accounts, and
// credential references so operators can diagnose failure class without
// leaking private registry material.
type RegistryFailure struct {
	Class   string
	Message string
	Details string
	Cause   error
}

// Error returns the bounded operator-facing message.
func (e RegistryFailure) Error() string {
	if message := strings.TrimSpace(e.Message); message != "" {
		return message
	}
	if class := strings.TrimSpace(e.Class); class != "" {
		return "registry collector failed: " + class
	}
	return "registry collector failed"
}

// Unwrap returns the underlying cause for errors.Is and errors.As checks.
func (e RegistryFailure) Unwrap() error {
	return e.Cause
}

// FailureClass returns the bounded status failure class.
func (e RegistryFailure) FailureClass() string {
	return strings.TrimSpace(e.Class)
}

// FailureDetails returns bounded key/value context for operator status.
func (e RegistryFailure) FailureDetails() string {
	return strings.TrimSpace(e.Details)
}

// RegistryHTTPFailure creates a bounded registry failure from an HTTP status.
func RegistryHTTPFailure(provider string, ecosystem string, operation string, statusCode int, cause error) RegistryFailure {
	class := RegistryFailureClassForHTTPStatus(statusCode)
	return RegistryFailure{
		Class:   class,
		Message: "registry collector failed: " + class,
		Details: registryFailureDetails(provider, ecosystem, operation, statusCode),
		Cause:   cause,
	}
}

// RegistryTransportFailure creates a bounded registry failure from a transport
// or context error.
func RegistryTransportFailure(provider string, ecosystem string, operation string, cause error) RegistryFailure {
	class := RegistryFailureRetryable
	if errors.Is(cause, context.Canceled) {
		class = RegistryFailureCanceled
	}
	return RegistryFailure{
		Class:   class,
		Message: "registry collector failed: " + class,
		Details: registryFailureDetails(provider, ecosystem, operation, 0),
		Cause:   cause,
	}
}

// RegistryFailureClassForHTTPStatus maps HTTP status codes to stable registry
// failure classes used by status and recovery tooling.
func RegistryFailureClassForHTTPStatus(statusCode int) string {
	switch {
	case statusCode == 401 || statusCode == 403:
		return RegistryFailureAuthDenied
	case statusCode == 404:
		return RegistryFailureNotFound
	case statusCode == 429:
		return RegistryFailureRateLimited
	case statusCode == 408 || statusCode >= 500:
		return RegistryFailureRetryable
	default:
		return RegistryFailureTerminal
	}
}

func registryFailureDetails(provider string, ecosystem string, operation string, statusCode int) string {
	parts := []string{
		"provider=" + boundedRegistryValue(provider, "unknown"),
	}
	if ecosystem = strings.TrimSpace(ecosystem); ecosystem != "" {
		parts = append(parts, "ecosystem="+boundedRegistryValue(ecosystem, "unknown"))
	}
	parts = append(parts, "operation="+boundedRegistryValue(operation, "unknown"))
	if statusCode > 0 {
		parts = append(parts, fmt.Sprintf("status_code=%d", statusCode))
	}
	return strings.Join(parts, " ")
}

func boundedRegistryValue(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	replacer := strings.NewReplacer(" ", "_", "\t", "_", "\n", "_", "\r", "_")
	return replacer.Replace(value)
}
