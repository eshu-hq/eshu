// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// TestSAMLHandlerACSAuditsSuccessfulLogin proves issue #5601's fix for the
// SAML path: a successful ACS callback now writes a durable
// identity_authentication governance-audit row, matching the GitHub/OIDC
// callback paths' new behavior.
func TestSAMLHandlerACSAuditsSuccessfulLogin(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 16, 0, 0, 0, time.UTC)
	audit := &fakeGovernanceAuditAppender{}
	store := &fakeSAMLStore{
		provider:  testSAMLProvider(),
		requestOK: true,
		resolveOK: true,
		replayOK:  true,
		sessionAuth: AuthContext{
			Mode:          AuthModeBrowserSession,
			TenantID:      "tenant_a",
			WorkspaceID:   "workspace_a",
			SubjectClass:  "external_saml",
			SubjectIDHash: "sha256:abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234",
		},
	}
	handler := &SAMLHandler{
		Store:     store,
		Sessions:  store,
		Verifier:  fakeSAMLVerifier{},
		NewSecret: sequenceSecrets("session-secret", "csrf-secret"),
		Now:       func() time.Time { return now },
		Audit:     audit,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	form := url.Values{}
	form.Set("RelayState", "relay-secret")
	form.Set("SAMLResponse", testSAMLResponseForRequest("request-1"))
	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/saml/providers/provider_a/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want 1: %#v", len(audit.events), audit.events)
	}
	event := audit.events[0]
	if event.Type != governanceaudit.EventTypeIdentityAuthentication {
		t.Fatalf("event type = %q, want identity_authentication", event.Type)
	}
	if event.Decision != governanceaudit.DecisionAllowed || event.ReasonCode != "sso_login_authenticated" {
		t.Fatalf("decision/reason = %q/%q, want allowed/sso_login_authenticated", event.Decision, event.ReasonCode)
	}
	if event.ActorIDHash != "sha256:abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234" {
		t.Fatalf("actor id hash = %q, want the resolved SAML subject hash", event.ActorIDHash)
	}
	if event.TenantID != "tenant_a" {
		t.Fatalf("tenant id = %q, want tenant_a", event.TenantID)
	}
	if event.WorkspaceID != "workspace_a" {
		t.Fatalf("workspace id = %q, want workspace_a", event.WorkspaceID)
	}
}

// TestSAMLHandlerACSAuditsReplayDenial proves a denied ACS outcome (replay
// detected) is now audited — previously the SAML ACS path recorded nothing
// at all for any denial branch.
func TestSAMLHandlerACSAuditsReplayDenial(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 16, 0, 0, 0, time.UTC)
	audit := &fakeGovernanceAuditAppender{}
	store := &fakeSAMLStore{
		provider:  testSAMLProvider(),
		requestOK: true,
		replayOK:  false,
	}
	handler := &SAMLHandler{
		Store:     store,
		Sessions:  store,
		Verifier:  fakeSAMLVerifier{},
		NewSecret: sequenceSecrets("session-secret", "csrf-secret"),
		Now:       func() time.Time { return now },
		Audit:     audit,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	form := url.Values{}
	form.Set("RelayState", "relay-secret")
	form.Set("SAMLResponse", testSAMLResponseForRequest("request-1"))
	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/saml/providers/provider_a/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want 1: %#v", len(audit.events), audit.events)
	}
	event := audit.events[0]
	if event.Decision != governanceaudit.DecisionDenied {
		t.Fatalf("decision = %q, want denied", event.Decision)
	}
	if event.ReasonCode != "replay_detected" {
		t.Fatalf("reason code = %q, want replay_detected", event.ReasonCode)
	}
	if event.ActorIDHash != "" {
		t.Fatalf("actor id hash = %q, want empty", event.ActorIDHash)
	}
}

// TestSAMLHandlerACSAuditsNoGrantsDenial proves the "no principal resolved"
// branch (the closest SAML analog to GitHub/OIDC's no_grants) is audited
// with a matching reason code.
func TestSAMLHandlerACSAuditsNoGrantsDenial(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 16, 0, 0, 0, time.UTC)
	audit := &fakeGovernanceAuditAppender{}
	store := &fakeSAMLStore{
		provider:  testSAMLProvider(),
		requestOK: true,
		replayOK:  true,
		resolveOK: false,
	}
	handler := &SAMLHandler{
		Store:     store,
		Sessions:  store,
		Verifier:  fakeSAMLVerifier{},
		NewSecret: sequenceSecrets("session-secret", "csrf-secret"),
		Now:       func() time.Time { return now },
		Audit:     audit,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	form := url.Values{}
	form.Set("RelayState", "relay-secret")
	form.Set("SAMLResponse", testSAMLResponseForRequest("request-1"))
	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/saml/providers/provider_a/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want 1: %#v", len(audit.events), audit.events)
	}
	if got := audit.events[0].ReasonCode; got != "no_grants" {
		t.Fatalf("reason code = %q, want no_grants", got)
	}
}

// TestSAMLHandlerACSAuditsUnavailableLogin proves a technical failure during
// the SAML ACS callback (here a ConsumeSAMLRequest store error) is audited as
// DecisionUnavailable with the branch's reason code, mirroring the GitHub and
// OIDC UnavailableLogin tests. It covers one of the SAML handler's four
// DecisionUnavailable branches (provider_lookup_error, request_consume_error,
// replay_reserve_error, principal_resolve_error).
func TestSAMLHandlerACSAuditsUnavailableLogin(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 16, 0, 0, 0, time.UTC)
	audit := &fakeGovernanceAuditAppender{}
	store := &fakeSAMLStore{
		provider:   testSAMLProvider(),
		consumeErr: errors.New("consume failed"),
	}
	handler := &SAMLHandler{
		Store:     store,
		Sessions:  store,
		Verifier:  fakeSAMLVerifier{},
		NewSecret: sequenceSecrets("session-secret", "csrf-secret"),
		Now:       func() time.Time { return now },
		Audit:     audit,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	form := url.Values{}
	form.Set("RelayState", "relay-secret")
	form.Set("SAMLResponse", testSAMLResponseForRequest("request-1"))
	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/saml/providers/provider_a/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want 1: %#v", len(audit.events), audit.events)
	}
	if got := audit.events[0].Decision; got != governanceaudit.DecisionUnavailable {
		t.Fatalf("decision = %q, want unavailable", got)
	}
	if got := audit.events[0].ReasonCode; got != "request_consume_error" {
		t.Fatalf("reason code = %q, want request_consume_error", got)
	}
}

// TestSAMLHandlerACSNilAuditIsSafe proves a handler with no Audit wired
// (e.g. an older deployment) never panics, mirroring
// TestGitHubLoginHandlerCallbackNilAuditIsSafe and
// TestOIDCLoginHandlerCallbackNilAuditIsSafe. nil Audit is a valid
// deployment configuration (adminRecoveryAuditAppender can return nil), and
// recordSSOLoginAuthentication already guards on it.
func TestSAMLHandlerACSNilAuditIsSafe(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 16, 0, 0, 0, time.UTC)
	store := &fakeSAMLStore{
		provider:  testSAMLProvider(),
		requestOK: true,
		resolveOK: true,
		replayOK:  true,
		sessionAuth: AuthContext{
			Mode:          AuthModeBrowserSession,
			TenantID:      "tenant_a",
			WorkspaceID:   "workspace_a",
			SubjectClass:  "external_saml",
			SubjectIDHash: "sha256:abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234",
		},
	}
	handler := &SAMLHandler{
		Store:     store,
		Sessions:  store,
		Verifier:  fakeSAMLVerifier{},
		NewSecret: sequenceSecrets("session-secret", "csrf-secret"),
		Now:       func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	form := url.Values{}
	form.Set("RelayState", "relay-secret")
	form.Set("SAMLResponse", testSAMLResponseForRequest("request-1"))
	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/saml/providers/provider_a/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}
