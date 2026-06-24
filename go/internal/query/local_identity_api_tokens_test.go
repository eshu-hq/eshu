// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

func TestLocalIdentityCreatePersonalAPITokenReturnsSecretOnceAndStoresHashOnly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 9, 0, 0, 0, time.UTC)
	store := &fakeLocalIdentityStore{}
	audit := &fakeGovernanceAuditAppender{}
	handler := &LocalIdentityHandler{
		Store:     store,
		Audit:     audit,
		NewSecret: sequenceSecrets("token-id", "raw-generated-token"),
		Now:       func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/api-tokens",
		bytes.NewBufferString(`{
			"token_class":"personal",
			"user_id":"user_owner",
			"display_label":"owner laptop",
			"expires_at":"2026-06-30T09:00:00Z"
		}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:               AuthModeBrowserSession,
		TenantID:           "tenant_local",
		WorkspaceID:        "workspace_local",
		SubjectIDHash:      "sha256:operator-subject",
		PolicyRevisionHash: "sha256:policy",
		AllScopes:          true,
	}))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var response localIdentityAPITokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if response.APIToken != "raw-generated-token" || response.TokenID != "token-id" {
		t.Fatalf("response = %#v, want one-time generated token and id", response)
	}
	if strings.Contains(rec.Body.String(), "owner laptop") {
		t.Fatalf("response leaked display label: %s", rec.Body.String())
	}
	created := store.createdAPIToken
	if got, want := created.TokenHash, localIdentityHash("raw-generated-token"); got != want {
		t.Fatalf("created token hash = %q, want %q", got, want)
	}
	if created.TokenHash == "raw-generated-token" || created.DisplayHandleHash == "owner laptop" {
		t.Fatalf("created token leaked raw material: %#v", created)
	}
	if got, want := created.TokenClass, "personal"; got != want {
		t.Fatalf("token class = %q, want %q", got, want)
	}
	if got, want := created.UserID, "user_owner"; got != want {
		t.Fatalf("user_id = %q, want %q", got, want)
	}
	if created.TenantID != "tenant_local" || created.WorkspaceID != "workspace_local" {
		t.Fatalf("created tenant/workspace = %q/%q", created.TenantID, created.WorkspaceID)
	}
	if len(audit.events) != 1 || audit.events[0].Type != governanceaudit.EventTypeTokenLifecycle {
		t.Fatalf("audit events = %#v, want token lifecycle event", audit.events)
	}
}

func TestLocalIdentityRevokeAndRotateAPITokenAuditLifecycle(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 9, 10, 0, 0, time.UTC)
	store := &fakeLocalIdentityStore{}
	audit := &fakeGovernanceAuditAppender{}
	handler := &LocalIdentityHandler{
		Store:     store,
		Audit:     audit,
		NewSecret: sequenceSecrets("new-token-id", "new-raw-token"),
		Now:       func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	auth := AuthContext{
		Mode:               AuthModeBrowserSession,
		TenantID:           "tenant_local",
		WorkspaceID:        "workspace_local",
		SubjectIDHash:      "sha256:operator-subject",
		PolicyRevisionHash: "sha256:policy",
		AllScopes:          true,
	}

	revokeReq := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/api-tokens/token-old/revoke",
		bytes.NewBufferString(`{"reason_code":"operator_revoke"}`),
	)
	revokeReq = revokeReq.WithContext(ContextWithAuthContext(revokeReq.Context(), auth))
	revokeRec := httptest.NewRecorder()
	mux.ServeHTTP(revokeRec, revokeReq)

	if revokeRec.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want %d: %s", revokeRec.Code, http.StatusNoContent, revokeRec.Body.String())
	}
	if got, want := store.revokedAPIToken.TokenID, "token-old"; got != want {
		t.Fatalf("revoked token id = %q, want %q", got, want)
	}

	rotateReq := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/api-tokens/token-old/rotate",
		bytes.NewBufferString(`{"expires_at":"2026-07-01T09:10:00Z"}`),
	)
	rotateReq = rotateReq.WithContext(ContextWithAuthContext(rotateReq.Context(), auth))
	rotateRec := httptest.NewRecorder()
	mux.ServeHTTP(rotateRec, rotateReq)

	if rotateRec.Code != http.StatusCreated {
		t.Fatalf("rotate status = %d, want %d: %s", rotateRec.Code, http.StatusCreated, rotateRec.Body.String())
	}
	var response localIdentityAPITokenResponse
	if err := json.Unmarshal(rotateRec.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if response.APIToken != "new-raw-token" || response.TokenID != "new-token-id" {
		t.Fatalf("rotate response = %#v, want one-time replacement token", response)
	}
	if store.rotatedAPIToken.NewTokenHash == "new-raw-token" ||
		store.rotatedAPIToken.NewTokenHash != localIdentityHash("new-raw-token") {
		t.Fatalf("rotated token hash = %q, want hash-only replacement", store.rotatedAPIToken.NewTokenHash)
	}
	if got, want := len(audit.events), 2; got != want {
		t.Fatalf("audit event count = %d, want %d", got, want)
	}
	for _, event := range audit.events {
		if event.Type != governanceaudit.EventTypeTokenLifecycle {
			t.Fatalf("audit event type = %q, want token_lifecycle", event.Type)
		}
	}
}

func TestLocalIdentitySharedOperatorRevokeAndRotateAPITokenUsesRequestTenantWorkspace(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 9, 20, 0, 0, time.UTC)
	store := &fakeLocalIdentityStore{}
	handler := &LocalIdentityHandler{
		Store:     store,
		NewSecret: sequenceSecrets("rotated-token-id", "rotated-raw-token"),
		Now:       func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	auth := AuthContext{Mode: AuthModeShared, AllScopes: true}

	revokeReq := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/api-tokens/token-old/revoke",
		bytes.NewBufferString(`{
			"tenant_id":"tenant_local",
			"workspace_id":"workspace_local"
		}`),
	)
	revokeReq = revokeReq.WithContext(ContextWithAuthContext(revokeReq.Context(), auth))
	revokeRec := httptest.NewRecorder()
	mux.ServeHTTP(revokeRec, revokeReq)

	if revokeRec.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want %d: %s", revokeRec.Code, http.StatusNoContent, revokeRec.Body.String())
	}
	if store.revokedAPIToken.TenantID != "tenant_local" ||
		store.revokedAPIToken.WorkspaceID != "workspace_local" {
		t.Fatalf("revoke scope = %q/%q, want request tenant/workspace",
			store.revokedAPIToken.TenantID, store.revokedAPIToken.WorkspaceID)
	}

	rotateReq := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/api-tokens/token-old/rotate",
		bytes.NewBufferString(`{
			"tenant_id":"tenant_local",
			"workspace_id":"workspace_local"
		}`),
	)
	rotateReq = rotateReq.WithContext(ContextWithAuthContext(rotateReq.Context(), auth))
	rotateRec := httptest.NewRecorder()
	mux.ServeHTTP(rotateRec, rotateReq)

	if rotateRec.Code != http.StatusCreated {
		t.Fatalf("rotate status = %d, want %d: %s", rotateRec.Code, http.StatusCreated, rotateRec.Body.String())
	}
	if store.rotatedAPIToken.TenantID != "tenant_local" ||
		store.rotatedAPIToken.WorkspaceID != "workspace_local" {
		t.Fatalf("rotate scope = %q/%q, want request tenant/workspace",
			store.rotatedAPIToken.TenantID, store.rotatedAPIToken.WorkspaceID)
	}
}

func TestLocalIdentityAPITokenLifecycleUsesAuthenticatedTenantWorkspaceFirst(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 9, 25, 0, 0, time.UTC)
	store := &fakeLocalIdentityStore{}
	handler := &LocalIdentityHandler{
		Store:     store,
		NewSecret: sequenceSecrets("created-token-id", "created-token", "rotated-token-id", "rotated-token"),
		Now:       func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	auth := AuthContext{
		Mode:        AuthModeBrowserSession,
		TenantID:    "tenant_auth",
		WorkspaceID: "workspace_auth",
		AllScopes:   true,
	}

	createReq := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/api-tokens",
		bytes.NewBufferString(`{
			"token_class":"personal",
			"tenant_id":"tenant_request",
			"workspace_id":"workspace_request",
			"user_id":"user_owner"
		}`),
	)
	createReq = createReq.WithContext(ContextWithAuthContext(createReq.Context(), auth))
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d: %s", createRec.Code, http.StatusCreated, createRec.Body.String())
	}
	if store.createdAPIToken.TenantID != "tenant_auth" ||
		store.createdAPIToken.WorkspaceID != "workspace_auth" {
		t.Fatalf("create scope = %q/%q, want authenticated tenant/workspace",
			store.createdAPIToken.TenantID, store.createdAPIToken.WorkspaceID)
	}

	revokeReq := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/api-tokens/token-old/revoke",
		bytes.NewBufferString(`{
			"tenant_id":"tenant_request",
			"workspace_id":"workspace_request"
		}`),
	)
	revokeReq = revokeReq.WithContext(ContextWithAuthContext(revokeReq.Context(), auth))
	revokeRec := httptest.NewRecorder()
	mux.ServeHTTP(revokeRec, revokeReq)

	if revokeRec.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want %d: %s", revokeRec.Code, http.StatusNoContent, revokeRec.Body.String())
	}
	if store.revokedAPIToken.TenantID != "tenant_auth" ||
		store.revokedAPIToken.WorkspaceID != "workspace_auth" {
		t.Fatalf("revoke scope = %q/%q, want authenticated tenant/workspace",
			store.revokedAPIToken.TenantID, store.revokedAPIToken.WorkspaceID)
	}

	rotateReq := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/api-tokens/token-old/rotate",
		bytes.NewBufferString(`{
			"tenant_id":"tenant_request",
			"workspace_id":"workspace_request"
		}`),
	)
	rotateReq = rotateReq.WithContext(ContextWithAuthContext(rotateReq.Context(), auth))
	rotateRec := httptest.NewRecorder()
	mux.ServeHTTP(rotateRec, rotateReq)

	if rotateRec.Code != http.StatusCreated {
		t.Fatalf("rotate status = %d, want %d: %s", rotateRec.Code, http.StatusCreated, rotateRec.Body.String())
	}
	if store.rotatedAPIToken.TenantID != "tenant_auth" ||
		store.rotatedAPIToken.WorkspaceID != "workspace_auth" {
		t.Fatalf("rotate scope = %q/%q, want authenticated tenant/workspace",
			store.rotatedAPIToken.TenantID, store.rotatedAPIToken.WorkspaceID)
	}
}
