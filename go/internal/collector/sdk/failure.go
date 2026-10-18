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
// credential-bearing request details from Error. The optional operation token
// names the bounded provider step that failed (for example "search_issues");
// it must stay a short identifier and never carry a URL, body fragment, or
// credential.
type ProviderFailure struct {
	provider     string
	operation    string
	failureClass FailureClass
	terminal     bool
	cause        error
}

// maxFailureOperationLength bounds the persisted operation token so a caller
// cannot smuggle high-cardinality or unbounded values into status rows.
const maxFailureOperationLength = 64

// NewProviderFailure builds a bounded provider failure.
func NewProviderFailure(provider string, class FailureClass, terminal bool, cause error) ProviderFailure {
	return ProviderFailure{
		provider:     strings.TrimSpace(provider),
		failureClass: class,
		terminal:     terminal,
		cause:        cause,
	}
}

// WithOperation returns a copy of the failure carrying a bounded operation
// token. The token is sanitized to [a-z0-9_] and truncated; non-blank input
// with no surviving token falls back to "unknown_operation", while blank
// input stays blank so claim handling can use its stage operation. The field
// is always safe for status rows, telemetry dimensions, and logs: it never
// carries URLs, response bodies, or credentials.
func (f ProviderFailure) WithOperation(operation string) ProviderFailure {
	f.operation = sanitizeFailureOperation(operation)
	return f
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

// FailureOperation returns the bounded provider operation token, or "" when
// the failure carries none. The value is already sanitized at set time, so it
// is safe for status rows, telemetry dimensions, and logs.
func (f ProviderFailure) FailureOperation() string {
	return f.operation
}

// FailureStatusCode returns the bounded provider HTTP status code preserved
// from the wrapped HTTPError, or 0 when no HTTP status is known. Only
// 100-599 survives; anything else is reported as unknown.
func (f ProviderFailure) FailureStatusCode() int {
	var httpErr HTTPError
	if errors.As(f.cause, &httpErr) {
		if httpErr.StatusCode >= 100 && httpErr.StatusCode <= 599 {
			return httpErr.StatusCode
		}
	}
	return 0
}

// sanitizeFailureOperation reduces an operation token to lowercase [a-z0-9_]
// of bounded length. URLs, hostnames, query strings, and credential fragments
// collapse to underscores and are then trimmed. Blank input stays blank so
// claim handling can fall back to its stage operation; non-blank input with
// no surviving token becomes "unknown_operation".
func sanitizeFailureOperation(operation string) string {
	trimmed := strings.TrimSpace(operation)
	if trimmed == "" {
		return ""
	}
	lowered := strings.ToLower(trimmed)
	var builder strings.Builder
	builder.Grow(len(lowered))
	for _, r := range lowered {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
			builder.WriteRune(r)
		} else {
			builder.WriteByte('_')
		}
	}
	token := strings.Trim(builder.String(), "_")
	if token == "" {
		return "unknown_operation"
	}
	if len(token) > maxFailureOperationLength {
		token = strings.Trim(token[:maxFailureOperationLength], "_")
		if token == "" {
			return "unknown_operation"
		}
	}
	return token
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
// An endpoint token carried by the cause (HTTPError.Operation or any wrapped
// FailureOperation) is preserved onto the returned failure via WithOperation,
// so per-endpoint tokens threaded through the client layer reach
// failureOperationOf claim handling. A cause with no token keeps a blank
// operation and claim handling falls back to its stage token.
func ClassifyProviderFailure(provider string, err error, policy StatusPolicy, fallback FailureClass) ProviderFailure {
	var failure ProviderFailure
	if errors.As(err, &failure) {
		return failure
	}
	var httpErr HTTPError
	if errors.As(err, &httpErr) {
		classified := policy.ClassifyStatus(httpErr.StatusCode)
		return withCarriedOperation(NewProviderFailure(provider, classified.Class, classified.Terminal, err), err)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return withCarriedOperation(NewProviderFailure(provider, FailureRetryable, false, err), err)
	}
	if fallback == "" {
		fallback = FailureRetryable
	}
	return withCarriedOperation(NewProviderFailure(provider, fallback, fallback == FailureTerminal, err), err)
}

// withCarriedOperation copies the first endpoint token found in the cause
// chain onto the failure. HTTPError.Operation wins over a generic carried
// token so the endpoint that failed stays authoritative; a cause with no
// token leaves the failure operation blank for the stage fallback.
func withCarriedOperation(failure ProviderFailure, err error) ProviderFailure {
	if operation := carriedFailureOperation(err); operation != "" {
		return failure.WithOperation(operation)
	}
	return failure
}

// carriedFailureOperation returns the first endpoint token in the cause
// chain: the first non-blank HTTPError.Operation, else the first non-blank
// wrapped FailureOperation. It returns "" when the chain carries no token.
func carriedFailureOperation(err error) string {
	var httpErr HTTPError
	if errors.As(err, &httpErr) && strings.TrimSpace(httpErr.Operation) != "" {
		return httpErr.Operation
	}
	var carried interface{ FailureOperation() string }
	if errors.As(err, &carried) && strings.TrimSpace(carried.FailureOperation()) != "" {
		return carried.FailureOperation()
	}
	return ""
}
