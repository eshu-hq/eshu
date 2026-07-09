// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestSAMLHandlerCreateSessionIssuesSessionWithPerTenantTimeoutOverride is an
// end-to-end proof (through SAMLHandler.createSession, the session issuer
// handleACS calls after a validated SAML assertion) that a tenant's stored
// idle_timeout_seconds/absolute_timeout_seconds override reaches both the
// persisted session row AND the cookie the browser receives (issue #4968,
// epic #4962). The local login path already has this proof
// (TestLocalLoginIssuesSessionWithPerTenantTimeoutOverride in
// session_timeout_policy_test.go); this pins the same fix on the SAML
// session-issuing path.
func TestSAMLHandlerCreateSessionIssuesSessionWithPerTenantTimeoutOverride(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	sessions := &fakeBrowserSessionStore{}
	policy := &fakeSignInPolicyReadStore{policy: SignInPolicy{
		IdleTimeoutSeconds:     600,  // 10m, overrides the 30m process default
		AbsoluteTimeoutSeconds: 7200, // 2h, overrides the 12h process default
	}}
	handler := &SAMLHandler{
		Sessions:        sessions,
		SignInPolicy:    policy,
		NewSecret:       sequenceSecrets("session-secret", "csrf-secret"),
		Now:             func() time.Time { return now },
		IdleTimeout:     30 * time.Minute,
		AbsoluteTimeout: 12 * time.Hour,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/saml/providers/provider_a/acs", nil)
	rec := httptest.NewRecorder()
	auth := AuthContext{
		Mode:         AuthModeScoped,
		TenantID:     "tenant_saml",
		WorkspaceID:  "workspace_saml",
		SubjectClass: "external_saml",
		AllScopes:    false,
	}

	handler.createSession(rec, req, auth, "", now)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if len(sessions.created) != 1 {
		t.Fatalf("created sessions = %d, want 1", len(sessions.created))
	}
	created := sessions.created[0]
	wantIdle := now.Add(10 * time.Minute)
	wantAbsolute := now.Add(2 * time.Hour)
	if !created.IdleExpiresAt.Equal(wantIdle) {
		t.Fatalf("IdleExpiresAt = %v, want %v (per-tenant override, not the 30m process default)", created.IdleExpiresAt, wantIdle)
	}
	if !created.AbsoluteExpiresAt.Equal(wantAbsolute) {
		t.Fatalf("AbsoluteExpiresAt = %v, want %v (per-tenant override, not the 12h process default)", created.AbsoluteExpiresAt, wantAbsolute)
	}

	sessionCookie := requireCookie(t, rec.Result(), BrowserSessionCookieName)
	wantMaxAge := int(2 * time.Hour / time.Second)
	if sessionCookie.MaxAge != wantMaxAge {
		t.Fatalf("session cookie MaxAge = %d, want %d (per-tenant absolute override in seconds, not the 12h process default)", sessionCookie.MaxAge, wantMaxAge)
	}
}
