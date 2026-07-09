// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestResolveSessionTimeoutsUsesPerTenantOverride(t *testing.T) {
	t.Parallel()

	policy := &fakeSignInPolicyReadStore{policy: SignInPolicy{
		IdleTimeoutSeconds:     900,
		AbsoluteTimeoutSeconds: 3600,
	}}
	idle, absolute := resolveSessionTimeouts(context.Background(), policy, "tenant_a", 30*time.Minute, 12*time.Hour)
	if idle != 15*time.Minute {
		t.Fatalf("idle = %v, want 15m", idle)
	}
	if absolute != time.Hour {
		t.Fatalf("absolute = %v, want 1h", absolute)
	}
}

func TestResolveSessionTimeoutsFallsBackToDefaultWhenUnset(t *testing.T) {
	t.Parallel()

	policy := &fakeSignInPolicyReadStore{policy: SignInPolicy{}}
	idle, absolute := resolveSessionTimeouts(context.Background(), policy, "tenant_a", 30*time.Minute, 12*time.Hour)
	if idle != 30*time.Minute || absolute != 12*time.Hour {
		t.Fatalf("idle=%v absolute=%v, want defaults 30m/12h", idle, absolute)
	}
}

func TestResolveSessionTimeoutsFailsOpenToDefaultOnReadError(t *testing.T) {
	t.Parallel()

	policy := &fakeSignInPolicyReadStore{err: context.DeadlineExceeded}
	idle, absolute := resolveSessionTimeouts(context.Background(), policy, "tenant_a", 30*time.Minute, 12*time.Hour)
	if idle != 30*time.Minute || absolute != 12*time.Hour {
		t.Fatalf("idle=%v absolute=%v, want defaults on read error", idle, absolute)
	}
}

func TestResolveSessionTimeoutsClampsIdleToAbsolute(t *testing.T) {
	t.Parallel()

	policy := &fakeSignInPolicyReadStore{policy: SignInPolicy{
		IdleTimeoutSeconds:     7200, // 2h idle
		AbsoluteTimeoutSeconds: 3600, // 1h absolute — shorter than idle
	}}
	idle, absolute := resolveSessionTimeouts(context.Background(), policy, "tenant_a", 30*time.Minute, 12*time.Hour)
	if absolute != time.Hour {
		t.Fatalf("absolute = %v, want 1h", absolute)
	}
	if idle != absolute {
		t.Fatalf("idle = %v, want clamped to absolute (%v)", idle, absolute)
	}
}

func TestResolveSessionTimeoutsNilPolicyStoreUsesDefault(t *testing.T) {
	t.Parallel()

	idle, absolute := resolveSessionTimeouts(context.Background(), nil, "tenant_a", 30*time.Minute, 12*time.Hour)
	if idle != 30*time.Minute || absolute != 12*time.Hour {
		t.Fatalf("idle=%v absolute=%v, want defaults with no policy store wired", idle, absolute)
	}
}

// TestLocalLoginIssuesSessionWithPerTenantTimeoutOverride is an end-to-end
// proof (through LocalIdentityHandler.handleLogin, not just the unit-level
// resolveSessionTimeouts) that a tenant's stored idle_timeout_seconds/
// absolute_timeout_seconds actually reach the issued session record — the
// concern item 7 (epic #4962, issue #4968) exists to close: the policy
// fields were persisted end-to-end before this, but session issuance still
// used the static process-wide default regardless of what was stored.
func TestLocalLoginIssuesSessionWithPerTenantTimeoutOverride(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	store := &fakeLocalIdentityStore{
		authResult: LocalIdentityAuthenticationResult{
			Status:        LocalIdentityAuthAuthenticated,
			Authenticated: true,
			Auth: LocalIdentityAuthContext{
				TenantID:      "tenant_local",
				SubjectIDHash: "sha256:dev-subject",
				AllScopes:     false,
			},
		},
	}
	sessions := &fakeBrowserSessionStore{}
	policy := &fakeSignInPolicyReadStore{policy: SignInPolicy{
		IdleTimeoutSeconds:     600,  // 10m, overrides the 30m process default
		AbsoluteTimeoutSeconds: 7200, // 2h, overrides the 12h process default
	}}
	handler := &LocalIdentityHandler{
		Store:           store,
		Sessions:        sessions,
		SignInPolicy:    policy,
		Now:             func() time.Time { return now },
		IdleTimeout:     30 * time.Minute,
		AbsoluteTimeout: 12 * time.Hour,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/login",
		bytes.NewBufferString(`{"login_id":"dev@example.test","password":"plaintext-password"}`),
	)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
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
}
