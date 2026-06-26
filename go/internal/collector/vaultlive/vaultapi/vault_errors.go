// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vaultapi

import (
	"errors"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

// Vault error sentinel values for errors.Is matching.
var (
	ErrVaultTimeout   = errors.New("vault request timed out")
	ErrVaultAuth      = errors.New("vault authentication failed (check read-only policy)")
	ErrVaultNotFound  = errors.New("vault resource not found")
	ErrVaultTransport = errors.New("vault transport failure")
)

// VaultTimeoutError wraps a timeout or deadline-exceeded error from the
// underlying HTTP transport. Carry no token, address, or path.
type VaultTimeoutError struct {
	Operation string
	Cause     error
}

func (e VaultTimeoutError) Error() string {
	return "vaultapi: request timed out: " + e.Cause.Error()
}

func (e VaultTimeoutError) Unwrap() error { return e.Cause }

func (e VaultTimeoutError) Is(target error) bool { return target == ErrVaultTimeout }

// VaultAuthError wraps a 403 Forbidden response from Vault, typically indicating
// an expired or insufficient token. Carry no token, address, or path.
type VaultAuthError struct {
	Operation  string
	StatusCode int
	Message    string
}

func (e VaultAuthError) Error() string {
	return "vaultapi: authentication failed (HTTP " + http.StatusText(e.StatusCode) + ")"
}

func (e VaultAuthError) Is(target error) bool { return target == ErrVaultAuth }

// VaultNotFoundError wraps a 404 response for a Vault metadata path. It signals
// that the requested mount, role, or metadata path does not exist — a partial
// coverage outcome, not a fatal failure.
type VaultNotFoundError struct {
	Operation string
}

func (e VaultNotFoundError) Error() string {
	return "vaultapi: resource not found"
}

func (e VaultNotFoundError) Is(target error) bool { return target == ErrVaultNotFound }

// VaultTransportError wraps a network or TLS error from the HTTP transport layer
// (connection refused, DNS failure, TLS handshake failure). Carry no address or
// token.
type VaultTransportError struct {
	Operation string
	Cause     error
}

func (e VaultTransportError) Error() string {
	return "vaultapi: transport failure"
}

func (e VaultTransportError) Unwrap() error { return e.Cause }

func (e VaultTransportError) Is(target error) bool { return target == ErrVaultTransport }

// classifyError maps an error from doRequest to a bounded metric result label.
// The result set is: success, timeout, auth_error, not_found, transport_error.
func classifyError(err error) string {
	if err == nil {
		return "success"
	}
	var (
		timeout   VaultTimeoutError
		auth      VaultAuthError
		notFound  VaultNotFoundError
		transport VaultTransportError
	)
	switch {
	case errors.As(err, &timeout):
		return "timeout"
	case errors.As(err, &auth):
		return "auth_error"
	case errors.As(err, &notFound):
		return "not_found"
	case errors.As(err, &transport):
		return "transport_error"
	default:
		return "transport_error"
	}
}

// wrapTransportError wraps a raw HTTP transport error as a VaultTransportError
// when the cause is a net.Error with Timeout() true, wrap as VaultTimeoutError
// instead.
func wrapTransportError(operation string, err error) error {
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return VaultTimeoutError{Operation: operation, Cause: err}
	}
	return VaultTransportError{Operation: operation, Cause: err}
}

// classifyHTTPStatus maps an HTTP status code to a typed vault error.
func classifyHTTPStatus(operation string, resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusForbidden:
		return VaultAuthError{
			Operation:  operation,
			StatusCode: resp.StatusCode,
			Message:    http.StatusText(resp.StatusCode),
		}
	case http.StatusNotFound:
		return VaultNotFoundError{Operation: operation}
	default:
		return sdk.HTTPError{
			Provider:   "vault",
			StatusCode: resp.StatusCode,
			Message:    http.StatusText(resp.StatusCode),
			RetryAfter: sdk.ParseRetryAfterHeader(resp.Header.Get("Retry-After")),
		}
	}
}
