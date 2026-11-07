// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package investigation_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/investigation"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
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
		code    querycontract.ErrorCode
		want    packet.PacketRefusalState
		wantHit bool
	}{
		{"not_found", querycontract.ErrorCodeNotFound, packet.PacketRefusalScopeNotFound, true},
		{"scope_not_found", querycontract.ErrorCodeScopeNotFound, packet.PacketRefusalScopeNotFound, true},
		{"service_not_found", querycontract.ErrorCodeServiceNotFound, packet.PacketRefusalScopeNotFound, true},
		{"unsupported_capability", querycontract.ErrorCodeUnsupportedCapability, packet.PacketRefusalProfileUnsupported, true},
		{"capability_degraded", querycontract.ErrorCodeCapabilityDegraded, packet.PacketRefusalProfileUnsupported, true},
		{"backend_unavailable", querycontract.ErrorCodeBackendUnavailable, packet.PacketRefusalBackendUnavailable, true},
		{"index_building", querycontract.ErrorCodeIndexBuilding, packet.PacketRefusalBackendUnavailable, true},
		{"unmapped code stays a CLI error", querycontract.ErrorCodeInvalidArgument, packet.PacketRefusalNone, false},
		{"empty code stays a CLI error", querycontract.ErrorCode(""), packet.PacketRefusalNone, false},
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
		want    packet.PacketRefusalState
		wantHit bool
	}{
		{"404 is scope_not_found", &statusError{code: 404}, packet.PacketRefusalScopeNotFound, true},
		{"501 is profile_unsupported", &statusError{code: 501}, packet.PacketRefusalProfileUnsupported, true},
		{"503 is backend_unavailable", &statusError{code: 503}, packet.PacketRefusalBackendUnavailable, true},
		{"wrapped 503 still classifies", fmt.Errorf("fetch: %w", &statusError{code: 503}), packet.PacketRefusalBackendUnavailable, true},
		{"500 surfaces as a CLI error", &statusError{code: 500}, packet.PacketRefusalNone, false},
		{"400 surfaces as a CLI error", &statusError{code: 400}, packet.PacketRefusalNone, false},
		{"transport error without a status", errors.New("dial tcp: connection refused"), packet.PacketRefusalNone, false},
		{"nil error", nil, packet.PacketRefusalNone, false},
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
	if !ok || got != packet.PacketRefusalBackendUnavailable {
		t.Fatalf("RefusalFromFetchError(503 + refused text) = (%q, %t), want (backend_unavailable, true)", got, ok)
	}
}

func TestRefusalFromEnvelopeError(t *testing.T) {
	t.Parallel()

	t.Run("nil envelope error is not a refusal", func(t *testing.T) {
		t.Parallel()

		refusal, refused, err := investigation.RefusalFromEnvelopeError(nil)
		if err != nil || refused || refusal != packet.PacketRefusalNone {
			t.Fatalf("got (%q, %t, %v), want (none, false, nil)", refusal, refused, err)
		}
	})

	t.Run("mapped code becomes a refusal", func(t *testing.T) {
		t.Parallel()

		refusal, refused, err := investigation.RefusalFromEnvelopeError(
			&querycontract.ErrorEnvelope{Code: querycontract.ErrorCodeNotFound, Message: "no finding"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !refused || refusal != packet.PacketRefusalScopeNotFound {
			t.Fatalf("got (%q, %t), want (scope_not_found, true)", refusal, refused)
		}
	})

	t.Run("unmapped code becomes a CLI error naming code and message", func(t *testing.T) {
		t.Parallel()

		_, refused, err := investigation.RefusalFromEnvelopeError(
			&querycontract.ErrorEnvelope{Code: querycontract.ErrorCodeInvalidArgument, Message: "bad scope"})
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
