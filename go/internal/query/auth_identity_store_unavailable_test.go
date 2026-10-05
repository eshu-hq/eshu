// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestAuthMiddlewareIdentityStoreUnavailableAnswersRetryable503 proves a
// credential that could not be evaluated because the identity store was
// unreachable (#7586) is told to retry, not told it is unauthenticated. A flat
// 401 reads as a credential problem, so an operator looking at auth never looks
// at PostgreSQL, and a client that treats 401 as terminal never retries.
func TestAuthMiddlewareIdentityStoreUnavailableAnswersRetryable503(t *testing.T) {
	t.Parallel()

	audit := &fakeGovernanceAuditAppender{}
	resolver := &fakeScopedTokenResolver{
		err: fmt.Errorf("resolve identity token: %w", querycontract.ErrIdentityStoreUnavailable),
	}
	reached := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })
	handler := AuthMiddlewareWithScopedTokensGovernanceAuditAndEnforcement("", resolver, next, audit, true)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/governance", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.Header.Set("Authorization", "Bearer some-credential")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
	if got, want := rec.Header().Get("Retry-After"), strconv.Itoa(querycontract.BackendUnavailableRetryAfterSeconds); got != want {
		t.Fatalf("Retry-After = %q, want %q", got, want)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "" {
		t.Fatalf("WWW-Authenticate = %q, want none: the credential was never judged, so no auth challenge", got)
	}
	if reached {
		t.Fatal("handler ran: an unevaluated credential must never be admitted")
	}
	body := rec.Body.String()
	if !strings.Contains(body, string(ErrorCodeBackendUnavailable)) {
		t.Fatalf("body = %s, want error code %q", body, ErrorCodeBackendUnavailable)
	}
	if strings.Contains(body, "resolve identity token") {
		t.Fatalf("body = %s leaks the internal error chain", body)
	}
	if got, want := len(audit.events), 1; got != want {
		t.Fatalf("len(audit.events) = %d, want %d", got, want)
	}
	if got, want := audit.events[0].Decision, governanceaudit.DecisionUnavailable; got != want {
		t.Fatalf("event.Decision = %q, want %q: an outage is not a denial", got, want)
	}
	if got, want := audit.events[0].ReasonCode, "identity_store_unavailable"; got != want {
		t.Fatalf("event.ReasonCode = %q, want %q", got, want)
	}
}

// TestAuthMiddlewareOtherResolverErrorStaysBare401 pins the fail-safe the 503
// mapping must not widen: only the identity-store-unavailable marker changes the
// answer. Any other resolver failure still answers a bare 401, so a client is
// never steered to OAuth discovery by an infrastructure error (the
// anthropics/claude-code#59467 guard in authMiddlewareWithRoutePolicy).
func TestAuthMiddlewareOtherResolverErrorStaysBare401(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{err: errors.New("some other resolver failure")}
	handler := AuthMiddlewareWithScopedTokensGovernanceAuditAndEnforcement(
		"", resolver, mockHandler(), &fakeGovernanceAuditAppender{}, true,
	)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/governance", nil)
	req.Header.Set("Authorization", "Bearer some-credential")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if got := rec.Header().Get("Retry-After"); got != "" {
		t.Fatalf("Retry-After = %q, want none on a 401", got)
	}
}
