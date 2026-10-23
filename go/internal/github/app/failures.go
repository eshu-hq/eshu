// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package app

import (
	"time"
)

// Failure classes reported by a mint that outlasted its retry budget. They
// reuse the bounded vocabulary of the repository-discovery REST calls so both
// runtimes classify the same fault the same way.
const (
	// ClassServerError marks repeated 5xx or transport faults.
	ClassServerError = "server_error"
	// ClassRateLimited marks a deferred rate-limit wait.
	ClassRateLimited = "rate_limited"
)

// Retry bounds shared by every minter. A hint longer than inCallWaitCap is
// handed back to the caller instead of slept in-call, so one mint never
// stalls its runtime past a minute.
const (
	inCallWaitCap   = 60 * time.Second
	retryAfterCap   = time.Hour
	rateLimitFloor  = time.Minute
	minimumHintWait = time.Second
	defaultBaseURL  = "https://api.github.com"
)

// RetryableError reports a mint that failed for a reason the provider is
// expected to clear: repeated 5xx or transport faults, or a rate limit the
// caller must wait out. It implements Retryable() so collector.IsRetryable
// accepts it without this package importing the collector core.
type RetryableError struct {
	// Class is ClassServerError, ClassRateLimited, or a bounded transport
	// class from TransportFailureClass.
	Class string
	// Attempts is how many requests the in-call retry made before giving up.
	Attempts int
	// RetryAfter is how long the caller should wait before the next attempt.
	// Zero means the caller's own poll interval.
	RetryAfter time.Duration

	cause error
}

// NewConfigError builds a ConfigError with a bounded validation message.
// The constructor exists for callers and tests that classify a fault
// themselves; MintInstallationToken builds its own.
func NewConfigError(message string) *ConfigError {
	if message == "" {
		message = "github app token mint misconfigured"
	}
	return &ConfigError{message: message}
}

// NewRetryableError builds a RetryableError for a retryable fault that
// outlasted the in-call budget. MintInstallationToken builds its own; the
// constructor exists for tests.
func NewRetryableError(class string, attempts int, retryAfter time.Duration, cause error) *RetryableError {
	return &RetryableError{Class: class, Attempts: attempts, RetryAfter: retryAfter, cause: cause}
}

// Error describes the failure with its class and attempt count. It carries
// no credential, key, token, or URL.
func (e *RetryableError) Error() string {
	return "github app token mint retryable failure"
}

// Unwrap returns the underlying bounded cause.
func (e *RetryableError) Unwrap() error { return e.cause }

// Retryable reports true: the provider is expected to clear this fault.
func (e *RetryableError) Retryable() bool { return true }

// AuthDeniedError reports a final 401 or a 403 without a rate-limit signal:
// the App credentials are wrong, revoked, or lack access. It is terminal.
type AuthDeniedError struct {
	// StatusCode is the mint response status (401 or 403).
	StatusCode int
}

// Error returns a bounded message with the status only.
func (e *AuthDeniedError) Error() string {
	return "github app token mint denied"
}

// Retryable reports false: retrying with the same credentials cannot clear
// this fault.
func (e *AuthDeniedError) Retryable() bool { return false }

// NotFoundError reports a final 404: the installation does not exist under
// the configured App. Callers map it to their auth-denied class because the
// fix is operator configuration, not a retry.
type NotFoundError struct{}

// Error returns a bounded message.
func (e *NotFoundError) Error() string { return "github app installation not found" }

// Retryable reports false.
func (e *NotFoundError) Retryable() bool { return false }

// ConfigError reports a mint that never reached the provider: missing
// credentials, an unparseable private key, or an unreadable token response.
// It never echoes the offending value.
type ConfigError struct {
	message string
}

// Error returns the bounded validation message.
func (e *ConfigError) Error() string {
	if e.message == "" {
		return "github app token mint misconfigured"
	}
	return e.message
}

// Retryable reports false: the operator must fix the configuration.
func (e *ConfigError) Retryable() bool { return false }
