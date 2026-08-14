// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package investigation_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/investigation"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// statusError stands in for go/cmd/eshu's apiHTTPError, which this package
// cannot import: cmd/eshu is package main. It implements the same
// apierr.HTTPStatusError contract the real transport error satisfies.
type statusError struct {
	code int
}

func (e *statusError) Error() string { return fmt.Sprintf("API error %d: body", e.code) }

func (e *statusError) HTTPStatusCode() int { return e.code }

func TestRefusalFromErrorCode(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		code    query.ErrorCode
		want    query.PacketRefusalState
		wantHit bool
	}{
		{"not_found", query.ErrorCodeNotFound, query.PacketRefusalScopeNotFound, true},
		{"scope_not_found", query.ErrorCodeScopeNotFound, query.PacketRefusalScopeNotFound, true},
		{"service_not_found", query.ErrorCodeServiceNotFound, query.PacketRefusalScopeNotFound, true},
		{"unsupported_capability", query.ErrorCodeUnsupportedCapability, query.PacketRefusalProfileUnsupported, true},
		{"capability_degraded", query.ErrorCodeCapabilityDegraded, query.PacketRefusalProfileUnsupported, true},
		{"backend_unavailable", query.ErrorCodeBackendUnavailable, query.PacketRefusalBackendUnavailable, true},
		{"index_building", query.ErrorCodeIndexBuilding, query.PacketRefusalBackendUnavailable, true},
		{"unmapped code stays a CLI error", query.ErrorCodeInvalidArgument, query.PacketRefusalNone, false},
		{"empty code stays a CLI error", query.ErrorCode(""), query.PacketRefusalNone, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := investigation.RefusalFromErrorCode(tc.code)
			if ok != tc.wantHit {
				t.Fatalf("RefusalFromErrorCode(%q) ok = %t, want %t", tc.code, ok, tc.wantHit)
			}
			if got != tc.want {
				t.Fatalf("RefusalFromErrorCode(%q) = %q, want %q", tc.code, got, tc.want)
			}
		})
	}
}

func TestRefusalFromFetchError(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		err     error
		want    query.PacketRefusalState
		wantHit bool
	}{
		{"404 is scope_not_found", &statusError{code: 404}, query.PacketRefusalScopeNotFound, true},
		{"501 is profile_unsupported", &statusError{code: 501}, query.PacketRefusalProfileUnsupported, true},
		{"503 is backend_unavailable", &statusError{code: 503}, query.PacketRefusalBackendUnavailable, true},
		{"wrapped 503 still classifies", fmt.Errorf("fetch: %w", &statusError{code: 503}), query.PacketRefusalBackendUnavailable, true},
		{"500 surfaces as a CLI error", &statusError{code: 500}, query.PacketRefusalNone, false},
		{"400 surfaces as a CLI error", &statusError{code: 400}, query.PacketRefusalNone, false},
		{"transport error without a status", errors.New("dial tcp: connection refused"), query.PacketRefusalNone, false},
		{"nil error", nil, query.PacketRefusalNone, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := investigation.RefusalFromFetchError(tc.err)
			if ok != tc.wantHit {
				t.Fatalf("RefusalFromFetchError(%v) ok = %t, want %t", tc.err, ok, tc.wantHit)
			}
			if got != tc.want {
				t.Fatalf("RefusalFromFetchError(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// TestRefusalFromFetchErrorClassifiesByStatusNotMessage pins the one behavior
// that separates this family from `eshu trace`. trace inspects the error text
// for "connection refused" BEFORE it looks at any status, so a 400 whose body
// mentions a refused connection classifies as backend_unavailable there. This
// family has no message check at all: only the status decides. A 400 carrying
// that text must therefore stay a CLI error, not a refusal.
func TestRefusalFromFetchErrorClassifiesByStatusNotMessage(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("request failed: dial tcp 127.0.0.1:8080: connect: connection refused: %w", &statusError{code: 400})
	got, ok := investigation.RefusalFromFetchError(err)
	if ok {
		t.Fatalf("RefusalFromFetchError classified a 400 as %q; this family reads the status only", got)
	}

	// The same text on a 503 is a refusal, which proves the status is what the
	// classifier read, not the message.
	got, ok = investigation.RefusalFromFetchError(
		fmt.Errorf("request failed: connection refused: %w", &statusError{code: 503}))
	if !ok || got != query.PacketRefusalBackendUnavailable {
		t.Fatalf("RefusalFromFetchError(503 + refused text) = (%q, %t), want (backend_unavailable, true)", got, ok)
	}
}

func TestRefusalFromEnvelopeError(t *testing.T) {
	t.Parallel()

	t.Run("nil envelope error is not a refusal", func(t *testing.T) {
		t.Parallel()

		refusal, refused, err := investigation.RefusalFromEnvelopeError(nil)
		if err != nil || refused || refusal != query.PacketRefusalNone {
			t.Fatalf("got (%q, %t, %v), want (none, false, nil)", refusal, refused, err)
		}
	})

	t.Run("mapped code becomes a refusal", func(t *testing.T) {
		t.Parallel()

		refusal, refused, err := investigation.RefusalFromEnvelopeError(
			&query.ErrorEnvelope{Code: query.ErrorCodeNotFound, Message: "no finding"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !refused || refusal != query.PacketRefusalScopeNotFound {
			t.Fatalf("got (%q, %t), want (scope_not_found, true)", refusal, refused)
		}
	})

	t.Run("unmapped code becomes a CLI error naming code and message", func(t *testing.T) {
		t.Parallel()

		_, refused, err := investigation.RefusalFromEnvelopeError(
			&query.ErrorEnvelope{Code: query.ErrorCodeInvalidArgument, Message: "bad scope"})
		if refused {
			t.Fatal("an unmapped envelope code must not become a refusal packet")
		}
		if err == nil {
			t.Fatal("expected a CLI error for an unmapped envelope code")
		}
		if got, want := err.Error(), "read failed: invalid_argument: bad scope"; got != want {
			t.Fatalf("error = %q, want %q", got, want)
		}
	})
}
