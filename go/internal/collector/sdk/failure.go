// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Version is the first-party collector SDK contract version.
const Version = "v0.1.0"

// FailureClass is a bounded workflow retry class safe for status rows,
// telemetry dimensions, and logs.
type FailureClass string

const (
	// FailureAuthDenied marks missing, invalid, or unauthorized credentials.
	FailureAuthDenied FailureClass = "auth_denied"
	// FailurePermissionHidden marks source data hidden by provider permissions.
	FailurePermissionHidden FailureClass = "permission_hidden"
	// FailureNotFound marks missing provider resources.
	FailureNotFound FailureClass = "not_found"
	// FailureDeleted marks provider resources that were removed or hidden by absence.
	FailureDeleted FailureClass = "deleted"
	// FailureArchived marks provider resources no longer active but still known.
	FailureArchived FailureClass = "archived"
	// FailureRateLimited marks provider throttling as retryable.
	FailureRateLimited FailureClass = "rate_limited"
	// FailureRetryable marks transient transport or provider failures.
	FailureRetryable FailureClass = "retryable"
	// FailureTerminal marks malformed input or non-retryable provider failures.
	FailureTerminal FailureClass = "terminal"
)

// Classification is the bounded failure decision for a provider status.
type Classification struct {
	Class    FailureClass
	Terminal bool
}

// StatusPolicy maps common HTTP statuses to collector-specific failure classes.
type StatusPolicy struct {
	AuthDeniedClass FailureClass
	NotFoundClass   FailureClass
	GoneClass       FailureClass
}

// ClassifyStatus returns the bounded failure decision for an HTTP status code.
func (p StatusPolicy) ClassifyStatus(status int) Classification {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return Classification{Class: p.classOrDefault(p.AuthDeniedClass, FailureAuthDenied), Terminal: true}
	case http.StatusNotFound:
		return Classification{Class: p.classOrDefault(p.NotFoundClass, FailureNotFound), Terminal: true}
	case http.StatusGone:
		return Classification{Class: p.classOrDefault(p.GoneClass, FailureTerminal), Terminal: true}
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusTooEarly, http.StatusTooManyRequests:
		return Classification{Class: FailureRateLimited}
	default:
		if status >= http.StatusInternalServerError {
			return Classification{Class: FailureRetryable}
		}
		if status >= http.StatusBadRequest {
			return Classification{Class: FailureTerminal, Terminal: true}
		}
		return Classification{Class: FailureRetryable}
	}
}

func (p StatusPolicy) classOrDefault(class FailureClass, fallback FailureClass) FailureClass {
	if class != "" {
		return class
	}
	return fallback
}

// ProviderFailure is a bounded source-provider failure returned to claim
// handling. It deliberately omits provider response bodies, raw URLs, and
// credential-bearing request details from Error.
type ProviderFailure struct {
	provider     string
	failureClass FailureClass
	terminal     bool
	cause        error
}

// NewProviderFailure builds a bounded provider failure.
func NewProviderFailure(provider string, class FailureClass, terminal bool, cause error) ProviderFailure {
	return ProviderFailure{
		provider:     strings.TrimSpace(provider),
		failureClass: class,
		terminal:     terminal,
		cause:        cause,
	}
}

// Error returns a bounded failure string safe for logs and status rows.
func (f ProviderFailure) Error() string {
	provider := strings.TrimSpace(f.provider)
	if provider == "" {
		provider = "collector"
	}
	class := f.FailureClass()
	if class == "" {
		class = string(FailureRetryable)
	}
	return fmt.Sprintf("%s provider failure: %s", provider, class)
}

// Unwrap returns the underlying provider or transport error.
func (f ProviderFailure) Unwrap() error {
	return f.cause
}

// FailureClass returns the bounded provider failure class.
func (f ProviderFailure) FailureClass() string {
	if f.failureClass == "" {
		return string(FailureRetryable)
	}
	return string(f.failureClass)
}

// TerminalFailure reports whether workflow should stop retrying this claim.
func (f ProviderFailure) TerminalFailure() bool {
	return f.terminal
}

// RetryAfterDelay returns provider retry guidance preserved from the wrapped cause.
func (f ProviderFailure) RetryAfterDelay() time.Duration {
	var retryAfter interface {
		RetryAfterDelay() time.Duration
	}
	if errors.As(f.cause, &retryAfter) {
		return retryAfter.RetryAfterDelay()
	}
	return 0
}

// ClassifyProviderFailure maps HTTP and context failures to ProviderFailure.
func ClassifyProviderFailure(provider string, err error, policy StatusPolicy, fallback FailureClass) ProviderFailure {
	var failure ProviderFailure
	if errors.As(err, &failure) {
		return failure
	}
	var httpErr HTTPError
	if errors.As(err, &httpErr) {
		classified := policy.ClassifyStatus(httpErr.StatusCode)
		return NewProviderFailure(provider, classified.Class, classified.Terminal, err)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return NewProviderFailure(provider, FailureRetryable, false, err)
	}
	if fallback == "" {
		fallback = FailureRetryable
	}
	return NewProviderFailure(provider, fallback, fallback == FailureTerminal, err)
}
