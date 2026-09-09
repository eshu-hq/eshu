// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestRevokeInvitationScopesToAuthTenantAndAudits(t *testing.T) {
	t.Parallel()

	store := &fakeAdminMutationStore{inviteResult: InvitationRevokeResult{Found: true, Revoked: true, Status: "revoked"}}
	audit := &querytestutil.FakeGovernanceAuditAppender{}
	mux := newMutationMux(store, audit)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, mutationRequest(http.MethodPost, "/api/v0/auth/local/invitations/inv_1/revoke", "", allScopeAdminAuth("tenant_a", "workspace_a")))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if store.gotInviteRevoke.TenantID != "tenant_a" || store.gotInviteRevoke.WorkspaceID != "workspace_a" {
		t.Fatalf("store scoped to %q/%q, want tenant_a/workspace_a", store.gotInviteRevoke.TenantID, store.gotInviteRevoke.WorkspaceID)
	}
	if store.gotInviteRevoke.InviteID != "inv_1" {
		t.Fatalf("invite id = %q, want inv_1", store.gotInviteRevoke.InviteID)
	}
	if !hasAuditReason(audit, "invitation_revoked") {
		t.Fatalf("revoke did not audit invitation_revoked: %#v", audit.Events)
	}
}

func TestRevokeInvitationNotFound(t *testing.T) {
	t.Parallel()

	store := &fakeAdminMutationStore{inviteResult: InvitationRevokeResult{Found: false}}
	audit := &querytestutil.FakeGovernanceAuditAppender{}
	mux := newMutationMux(store, audit)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, mutationRequest(http.MethodPost, "/api/v0/auth/local/invitations/missing/revoke", "", allScopeAdminAuth("tenant_a", "workspace_a")))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if !hasAuditReason(audit, "invitation_not_found") {
		t.Fatalf("not-found did not audit invitation_not_found")
	}
}

func TestRevokeInvitationIdempotentNoop(t *testing.T) {
	t.Parallel()

	// Already revoked: Found true, Revoked false. Must be 200 with status, no error.
	store := &fakeAdminMutationStore{inviteResult: InvitationRevokeResult{Found: true, Revoked: false, Status: "revoked"}}
	audit := &querytestutil.FakeGovernanceAuditAppender{}
	mux := newMutationMux(store, audit)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, mutationRequest(http.MethodPost, "/api/v0/auth/local/invitations/inv_1/revoke", "", allScopeAdminAuth("tenant_a", "workspace_a")))
	if rec.Code != http.StatusOK {
		t.Fatalf("idempotent re-revoke status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var decoded struct {
		Status  string `json:"status"`
		Revoked bool   `json:"revoked"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if decoded.Revoked {
		t.Fatalf("revoked = true, want false for an already-revoked invitation")
	}
	if !hasAuditReason(audit, "invitation_revoke_noop") {
		t.Fatalf("idempotent no-op did not audit invitation_revoke_noop")
	}
}

func TestRevokeInvitationNeverEchoesInviteCode(t *testing.T) {
	t.Parallel()

	store := &fakeAdminMutationStore{inviteResult: InvitationRevokeResult{Found: true, Revoked: true, Status: "revoked"}}
	mux := newMutationMux(store, &querytestutil.FakeGovernanceAuditAppender{})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, mutationRequest(http.MethodPost, "/api/v0/auth/local/invitations/inv_1/revoke", "", allScopeAdminAuth("tenant_a", "workspace_a")))
	body := rec.Body.String()
	for _, forbidden := range []string{"invite_code", "_hash", "secret"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("revoke response leaked %q: %s", forbidden, body)
		}
	}
}

// --- Endpoint 2: grant role assignment ---
