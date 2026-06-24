// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"golang.org/x/crypto/bcrypt"
)

func TestLocalIdentityBootstrapRequiresSharedOperatorAndStoresHashes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 14, 0, 0, 0, time.UTC)
	store := &fakeLocalIdentityStore{}
	audit := &fakeGovernanceAuditAppender{}
	handler := &LocalIdentityHandler{
		Store:        store,
		Audit:        audit,
		Now:          func() time.Time { return now },
		NewSecret:    sequenceSecrets("user-id", "mfa-id"),
		PasswordCost: bcrypt.MinCost,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	body := bytes.NewBufferString(`{
		"tenant_id":"tenant_local",
		"workspace_id":"workspace_local",
		"login_id":"owner@example.test",
		"profile_handle":"owner",
		"password":"plaintext-password",
		"mfa_factor_kind":"recovery_code",
		"recovery_codes":["one-time-a","one-time-b"]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/bootstrap", body)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{Mode: AuthModeShared, AllScopes: true}))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	record := store.bootstrap
	if record.UserID != "user-id" || record.MFAFactorID != "mfa-id" {
		t.Fatalf("bootstrap ids = %q/%q, want generated ids", record.UserID, record.MFAFactorID)
	}
	if record.SubjectIDHash == "owner@example.test" || record.ProfileHandleHash == "owner" {
		t.Fatalf("bootstrap record leaked raw identifiers: %#v", record)
	}
	if record.PasswordHash == "plaintext-password" ||
		bcrypt.CompareHashAndPassword([]byte(record.PasswordHash), []byte("plaintext-password")) != nil {
		t.Fatalf("bootstrap password hash did not store bcrypt proof: %q", record.PasswordHash)
	}
	if len(record.RecoveryCodeHashes) != 2 || record.RecoveryCodeHashes[0] == "one-time-a" {
		t.Fatalf("bootstrap recovery hashes = %#v, want hash-only codes", record.RecoveryCodeHashes)
	}
	if strings.Contains(rec.Body.String(), "plaintext-password") || strings.Contains(rec.Body.String(), "one-time-a") {
		t.Fatalf("bootstrap response leaked setup secrets: %s", rec.Body.String())
	}
	if len(audit.events) != 1 || audit.events[0].Type != governanceaudit.EventTypeBootstrap {
		t.Fatalf("audit events = %#v, want bootstrap event", audit.events)
	}
}

func TestLocalIdentityBootstrapPreservesPasswordWhitespace(t *testing.T) {
	t.Parallel()

	store := &fakeLocalIdentityStore{}
	handler := &LocalIdentityHandler{
		Store:        store,
		NewSecret:    sequenceSecrets("user-id", "mfa-id"),
		PasswordCost: bcrypt.MinCost,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/bootstrap", bytes.NewBufferString(`{
		"tenant_id":"tenant_local",
		"workspace_id":"workspace_local",
		"login_id":"owner@example.test",
		"password":"  plaintext-password  ",
		"recovery_codes":["one-time-a"]
	}`))
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{Mode: AuthModeShared, AllScopes: true}))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if bcrypt.CompareHashAndPassword([]byte(store.bootstrap.PasswordHash), []byte("  plaintext-password  ")) != nil {
		t.Fatal("bootstrap password hash did not preserve submitted whitespace")
	}
}

func TestLocalIdentityBootstrapRejectsBrowserSessionOperator(t *testing.T) {
	t.Parallel()

	handler := &LocalIdentityHandler{Store: &fakeLocalIdentityStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/bootstrap", bytes.NewBufferString(`{}`))
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{Mode: AuthModeBrowserSession, AllScopes: true}))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestLocalIdentityLoginIssuesBrowserSessionAfterMFARecoveryProof(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 14, 10, 0, 0, time.UTC)
	store := &fakeLocalIdentityStore{
		authResult: LocalIdentityAuthenticationResult{
			Status:        LocalIdentityAuthAuthenticated,
			Authenticated: true,
			Auth: LocalIdentityAuthContext{
				TenantID:           "tenant_local",
				WorkspaceID:        "workspace_local",
				SubjectIDHash:      "sha256:owner-subject",
				SubjectClass:       "local_user",
				PolicyRevisionHash: "sha256:policy",
				AllScopes:          true,
			},
		},
	}
	sessions := &fakeBrowserSessionStore{}
	handler := &LocalIdentityHandler{
		Store:           store,
		Sessions:        sessions,
		NewSecret:       sequenceSecrets("session-secret", "csrf-secret"),
		Now:             func() time.Time { return now },
		IdleTimeout:     30 * time.Minute,
		AbsoluteTimeout: 12 * time.Hour,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	body := bytes.NewBufferString(`{
		"login_id":"owner@example.test",
		"password":"plaintext-password",
		"recovery_code":"one-time-a"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/login", body)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if store.attempt.SubjectIDHash == "owner@example.test" ||
		store.attempt.MFARecoveryCodeHash == "one-time-a" {
		t.Fatalf("login attempt leaked raw identifiers/codes: %#v", store.attempt)
	}
	if len(sessions.created) != 1 {
		t.Fatalf("created sessions = %d, want 1", len(sessions.created))
	}
	created := sessions.created[0]
	if got, want := created.SessionHash, BrowserSessionSecretHash("session-secret"); got != want {
		t.Fatalf("session hash = %q, want %q", got, want)
	}
	if strings.Contains(rec.Body.String(), "session-secret") || strings.Contains(rec.Body.String(), "plaintext-password") {
		t.Fatalf("login response leaked secrets: %s", rec.Body.String())
	}
	if requireCookie(t, rec.Result(), BrowserSessionCookieName).Value != "session-secret" {
		t.Fatal("session cookie missing generated secret")
	}
	var response LocalIdentitySessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}
	if response.Status != string(LocalIdentityAuthAuthenticated) || response.CSRFToken != "csrf-secret" {
		t.Fatalf("login response = %#v, want authenticated csrf response", response)
	}
}

func TestLocalIdentityLoginMFARequiredDoesNotIssueSession(t *testing.T) {
	t.Parallel()

	store := &fakeLocalIdentityStore{
		authResult: LocalIdentityAuthenticationResult{Status: LocalIdentityAuthMFARequired},
	}
	sessions := &fakeBrowserSessionStore{}
	handler := &LocalIdentityHandler{Store: store, Sessions: sessions}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/login",
		bytes.NewBufferString(`{"login_id":"owner@example.test","password":"plaintext-password"}`),
	)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if len(sessions.created) != 0 {
		t.Fatalf("created sessions = %d, want 0", len(sessions.created))
	}
}

func TestLocalIdentityLoginAuditsPublicFailureAsAnonymous(t *testing.T) {
	t.Parallel()

	store := &fakeLocalIdentityStore{
		authResult: LocalIdentityAuthenticationResult{Status: LocalIdentityAuthInvalid},
	}
	audit := &fakeGovernanceAuditAppender{}
	handler := &LocalIdentityHandler{Store: store, Sessions: &fakeBrowserSessionStore{}, Audit: audit}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/login",
		bytes.NewBufferString(`{"login_id":"owner@example.test","password":"wrong-password"}`),
	)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(audit.events))
	}
	if audit.events[0].ActorClass != governanceaudit.ActorClassAnonymous {
		t.Fatalf("audit actor class = %q, want anonymous", audit.events[0].ActorClass)
	}
}

func TestLocalIdentityInvitationUsesAuthScopeDefaults(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 14, 30, 0, 0, time.UTC)
	store := &fakeLocalIdentityStore{}
	handler := &LocalIdentityHandler{
		Store:     store,
		NewSecret: sequenceSecrets("invite-id"),
		Now:       func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/invitations",
		bytes.NewBufferString(`{"invite_code":"invite-secret","role_id":"developer"}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:          AuthModeBrowserSession,
		TenantID:      "tenant_from_auth",
		WorkspaceID:   "workspace_from_auth",
		SubjectIDHash: "sha256:admin",
		AllScopes:     true,
	}))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if store.invitation.TenantID != "tenant_from_auth" ||
		store.invitation.WorkspaceID != "workspace_from_auth" {
		t.Fatalf("invitation scope = %q/%q, want auth scope", store.invitation.TenantID, store.invitation.WorkspaceID)
	}
	wantPolicy := localIdentityPolicyRevision("tenant_from_auth", "workspace_from_auth")
	if store.invitation.PolicyRevisionHash != wantPolicy {
		t.Fatalf("policy hash = %q, want %q", store.invitation.PolicyRevisionHash, wantPolicy)
	}
}

func TestLocalIdentityPublicPathsAreNarrow(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/api/v0/auth/local/login",
		"/api/v0/auth/local/invitations/accept",
		"/api/v0/auth/local/break-glass/session",
	} {
		if !publicHTTPPaths[path] {
			t.Fatalf("publicHTTPPaths[%q] = false, want true", path)
		}
	}
	if publicHTTPPaths["/api/v0/auth/local/bootstrap"] ||
		publicHTTPPaths["/api/v0/auth/local/break-glass"] {
		t.Fatal("operator local identity routes must not bypass authentication")
	}
}

type fakeLocalIdentityStore struct {
	bootstrap       LocalIdentityBootstrapRecord
	attempt         LocalIdentityAuthenticationAttempt
	authResult      LocalIdentityAuthenticationResult
	invitation      LocalIdentityInvitationRecord
	acceptance      LocalIdentityInvitationAcceptance
	passwordReset   LocalIdentityPasswordReset
	mfaReset        LocalIdentityMFAReset
	disable         LocalIdentityDisableUser
	breakGlass      LocalIdentityBreakGlassWindow
	breakGlassAuth  LocalIdentityAuthContext
	breakGlassError error
	createdAPIToken LocalIdentityAPITokenCreate
	revokedAPIToken LocalIdentityAPITokenRevoke
	rotatedAPIToken LocalIdentityAPITokenRotate
}

func (s *fakeLocalIdentityStore) BootstrapLocalIdentity(
	_ context.Context,
	record LocalIdentityBootstrapRecord,
) error {
	s.bootstrap = record
	return nil
}

func (s *fakeLocalIdentityStore) AuthenticateLocalIdentity(
	_ context.Context,
	attempt LocalIdentityAuthenticationAttempt,
) (LocalIdentityAuthenticationResult, error) {
	s.attempt = attempt
	return s.authResult, nil
}

func (s *fakeLocalIdentityStore) CreateLocalIdentityInvitation(
	_ context.Context,
	record LocalIdentityInvitationRecord,
) error {
	s.invitation = record
	return nil
}

func (s *fakeLocalIdentityStore) AcceptLocalIdentityInvitation(
	_ context.Context,
	acceptance LocalIdentityInvitationAcceptance,
) error {
	s.acceptance = acceptance
	return nil
}

func (s *fakeLocalIdentityStore) ResetLocalIdentityPassword(
	_ context.Context,
	reset LocalIdentityPasswordReset,
) error {
	s.passwordReset = reset
	return nil
}

func (s *fakeLocalIdentityStore) ResetLocalIdentityMFA(_ context.Context, reset LocalIdentityMFAReset) error {
	s.mfaReset = reset
	return nil
}

func (s *fakeLocalIdentityStore) DisableLocalIdentityUser(_ context.Context, disable LocalIdentityDisableUser) error {
	s.disable = disable
	return nil
}

func (s *fakeLocalIdentityStore) EnableLocalIdentityBreakGlass(
	_ context.Context,
	window LocalIdentityBreakGlassWindow,
) error {
	s.breakGlass = window
	return nil
}

func (s *fakeLocalIdentityStore) ResolveLocalIdentityBreakGlass(
	_ context.Context,
	_ LocalIdentityBreakGlassAttempt,
) (LocalIdentityAuthContext, error) {
	return s.breakGlassAuth, s.breakGlassError
}

func (s *fakeLocalIdentityStore) CreateLocalIdentityAPIToken(
	_ context.Context,
	token LocalIdentityAPITokenCreate,
) error {
	s.createdAPIToken = token
	return nil
}

func (s *fakeLocalIdentityStore) RevokeLocalIdentityAPIToken(
	_ context.Context,
	revoke LocalIdentityAPITokenRevoke,
) error {
	s.revokedAPIToken = revoke
	return nil
}

func (s *fakeLocalIdentityStore) RotateLocalIdentityAPIToken(
	_ context.Context,
	rotate LocalIdentityAPITokenRotate,
) error {
	s.rotatedAPIToken = rotate
	return nil
}

func (s *fakeLocalIdentityStore) ListAPITokensBySubject(
	_ context.Context,
	_ string,
	_ time.Time,
) ([]LocalIdentityAPITokenListItem, error) {
	return nil, nil
}

func (s *fakeLocalIdentityStore) GetLocalIdentityMFAStatus(
	_ context.Context,
	_ string,
	_ time.Time,
) (LocalIdentityMFAStatus, error) {
	return LocalIdentityMFAStatus{}, nil
}
