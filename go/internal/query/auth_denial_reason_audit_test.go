// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// bearerDenialOutcomeError is a resolver error that carries a bounded denial
// outcome, the shape internal/oidcbearer's denial errors implement.
type bearerDenialOutcomeError struct {
	outcome string
	wrapped error
}

func (e bearerDenialOutcomeError) Error() string {
	return fmt.Sprintf("bearer token denied: %s", e.outcome)
}

func (e bearerDenialOutcomeError) Unwrap() error { return e.wrapped }

func (e bearerDenialOutcomeError) DenialOutcome() string { return e.outcome }

// TestAuthMiddlewareRecordsDistinctBearerDenialReasonCodes proves that a bearer
// credential rejected for a specific reason lands in governance_audit_events
// with that reason, instead of collapsing every failure into the generic
// authentication_required (#5567).
//
// Before this, an operator asking "who tried to authenticate and why were they
// denied" could not tell an expired token from a wrong-audience one from a
// forged signature through the audit trail — those distinctions existed only in
// structured logs. A spike of bad_signature is a very different security
// signal from a spike of expired, and the audit table could not separate them.
func TestAuthMiddlewareRecordsDistinctBearerDenialReasonCodes(t *testing.T) {
	t.Parallel()

	for _, outcome := range []string{
		"expired",
		"wrong_audience",
		"unknown_issuer",
		"bad_signature",
		"malformed",
		"no_grants",
		"jwks_fetch_failure",
	} {
		t.Run(outcome, func(t *testing.T) {
			t.Parallel()

			audit := &fakeGovernanceAuditAppender{}
			resolver := &fakeScopedTokenResolver{
				err: bearerDenialOutcomeError{outcome: outcome},
			}
			handler := AuthMiddlewareWithScopedTokensGovernanceAuditAndEnforcement(
				"", resolver, mockHandler(), audit, true,
			)
			req := httptest.NewRequest(http.MethodGet, "/api/v0/status/governance", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			req.Header.Set("Authorization", "Bearer some-credential")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusUnauthorized; got != want {
				t.Fatalf("status = %d, want %d", got, want)
			}
			if got, want := len(audit.events), 1; got != want {
				t.Fatalf("len(audit.events) = %d, want %d", got, want)
			}
			event := audit.events[0]
			if got, want := event.Decision, governanceaudit.DecisionDenied; got != want {
				t.Fatalf("event.Decision = %q, want %q", got, want)
			}
			if got, want := event.ReasonCode, outcome; got != want {
				t.Fatalf("event.ReasonCode = %q, want %q", got, want)
			}
		})
	}
}

// TestAuthMiddlewareRejectsUnboundedBearerDenialReasonCode proves an outcome the
// audit contract does not recognize falls back to authentication_required rather
// than writing an unbounded string into reason_code. reason_code is a bounded
// enum an operator filters on; letting a resolver widen it at will would make
// those filters silently incomplete.
func TestAuthMiddlewareRejectsUnboundedBearerDenialReasonCode(t *testing.T) {
	t.Parallel()

	audit := &fakeGovernanceAuditAppender{}
	resolver := &fakeScopedTokenResolver{
		err: bearerDenialOutcomeError{outcome: "something_new_from_a_future_provider"},
	}
	handler := AuthMiddlewareWithScopedTokensGovernanceAuditAndEnforcement(
		"", resolver, mockHandler(), audit, true,
	)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/governance", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.Header.Set("Authorization", "Bearer some-credential")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got, want := len(audit.events), 1; got != want {
		t.Fatalf("len(audit.events) = %d, want %d", got, want)
	}
	if got, want := audit.events[0].ReasonCode, "authentication_required"; got != want {
		t.Fatalf("event.ReasonCode = %q, want %q", got, want)
	}
}

// TestAuthMiddlewarePlainResolverErrorKeepsGenericReasonCode proves a resolver
// error carrying no outcome still audits as authentication_required, so the
// existing denial paths are unchanged.
func TestAuthMiddlewarePlainResolverErrorKeepsGenericReasonCode(t *testing.T) {
	t.Parallel()

	audit := &fakeGovernanceAuditAppender{}
	resolver := &fakeScopedTokenResolver{err: errors.New("resolver unavailable")}
	handler := AuthMiddlewareWithScopedTokensGovernanceAuditAndEnforcement(
		"", resolver, mockHandler(), audit, true,
	)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/governance", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.Header.Set("Authorization", "Bearer some-credential")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got, want := len(audit.events), 1; got != want {
		t.Fatalf("len(audit.events) = %d, want %d", got, want)
	}
	if got, want := audit.events[0].ReasonCode, "authentication_required"; got != want {
		t.Fatalf("event.ReasonCode = %q, want %q", got, want)
	}
}
