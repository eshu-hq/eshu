// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// fakeAdminMutationStore records the scoped arguments it was asked for and
// returns canned results, modeling the Postgres tenant/workspace filter so
// handler-level isolation and idempotency can be proven without a database.
type fakeAdminMutationStore struct {
	gotInviteRevoke  AdminInvitationRevokeRequest
	gotGrant         AdminRoleAssignmentGrantRequest
	gotRoleRevoke    AdminRoleAssignmentRevokeRequest
	gotMappingCreate AdminIdPGroupMappingCreateRequest
	gotMappingDelete AdminIdPGroupMappingDeleteRequest

	inviteResult        AdminInvitationRevokeResult
	grantResult         AdminRoleAssignmentMutationResult
	roleRevokeResult    AdminRoleAssignmentMutationResult
	mappingCreateResult AdminIdPGroupMappingCreateResult
	mappingDeleteResult AdminIdPGroupMappingDeleteResult

	forceErr error
}

func (f *fakeAdminMutationStore) RevokeAdminInvitation(_ context.Context, req AdminInvitationRevokeRequest) (AdminInvitationRevokeResult, error) {
	f.gotInviteRevoke = req
	if f.forceErr != nil {
		return AdminInvitationRevokeResult{}, f.forceErr
	}
	return f.inviteResult, nil
}

func (f *fakeAdminMutationStore) GrantAdminRoleAssignment(_ context.Context, req AdminRoleAssignmentGrantRequest) (AdminRoleAssignmentMutationResult, error) {
	f.gotGrant = req
	if f.forceErr != nil {
		return AdminRoleAssignmentMutationResult{}, f.forceErr
	}
	return f.grantResult, nil
}

func (f *fakeAdminMutationStore) RevokeAdminRoleAssignment(_ context.Context, req AdminRoleAssignmentRevokeRequest) (AdminRoleAssignmentMutationResult, error) {
	f.gotRoleRevoke = req
	if f.forceErr != nil {
		return AdminRoleAssignmentMutationResult{}, f.forceErr
	}
	return f.roleRevokeResult, nil
}

func (f *fakeAdminMutationStore) CreateAdminIdPGroupMapping(_ context.Context, req AdminIdPGroupMappingCreateRequest) (AdminIdPGroupMappingCreateResult, error) {
	f.gotMappingCreate = req
	if f.forceErr != nil {
		return AdminIdPGroupMappingCreateResult{}, f.forceErr
	}
	return f.mappingCreateResult, nil
}

func (f *fakeAdminMutationStore) DeleteAdminIdPGroupMapping(_ context.Context, req AdminIdPGroupMappingDeleteRequest) (AdminIdPGroupMappingDeleteResult, error) {
	f.gotMappingDelete = req
	if f.forceErr != nil {
		return AdminIdPGroupMappingDeleteResult{}, f.forceErr
	}
	return f.mappingDeleteResult, nil
}

// recordingAuditAppender captures every event emitted so tests can assert that
// allowed and denied mutations both audit.
type recordingAuditAppender struct {
	events []governanceaudit.Event
}

func (a *recordingAuditAppender) Append(_ context.Context, events []governanceaudit.Event) error {
	a.events = append(a.events, events...)
	return nil
}

func (a *recordingAuditAppender) hasReason(reason string) bool {
	for _, e := range a.events {
		if e.ReasonCode == reason {
			return true
		}
	}
	return false
}

func mutationRequest(method, target, body string, auth AuthContext) *http.Request {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	return req.WithContext(ContextWithAuthContext(req.Context(), auth))
}

func newMutationMux(store AdminIdentityMutationStore, audit GovernanceAuditAppender) *http.ServeMux {
	handler := &AdminIdentityMutationHandler{Store: store, Audit: audit}
	mux := http.NewServeMux()
	handler.Mount(mux)
	return mux
}

// adminMutationCases enumerates each mutation route with a valid body so the
// auth gates can be asserted uniformly.
func adminMutationCases() []struct {
	method string
	target string
	body   string
} {
	return []struct {
		method string
		target string
		body   string
	}{
		{http.MethodPost, "/api/v0/auth/local/invitations/inv_1/revoke", ""},
		{http.MethodPost, "/api/v0/auth/admin/role-assignments", `{"user_id":"u1","role_id":"developer"}`},
		{http.MethodPost, "/api/v0/auth/admin/role-assignments/revoke", `{"user_id":"u1","role_id":"developer"}`},
		{http.MethodPost, "/api/v0/auth/admin/idp-group-mappings", `{"provider_config_id":"prov_1","external_group":"Eshu Developers","role_id":"developer"}`},
		{http.MethodDelete, "/api/v0/auth/admin/idp-group-mappings/ref_1", ""},
	}
}

// TestAdminMutationsRequireAllScope verifies every mutation route returns 403
// for a non-all-scope caller, even one carrying a tenant, and audits the denial.
func TestAdminMutationsRequireAllScope(t *testing.T) {
	t.Parallel()

	scoped := AuthContext{Mode: AuthModeScoped, TenantID: "tenant_a", WorkspaceID: "workspace_a", AllScopes: false}
	for _, tc := range adminMutationCases() {
		audit := &recordingAuditAppender{}
		mux := newMutationMux(&fakeAdminMutationStore{}, audit)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, mutationRequest(tc.method, tc.target, tc.body, scoped))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as non-admin = %d, want 403: %s", tc.method, tc.target, rec.Code, rec.Body.String())
		}
		if !audit.hasReason("admin_scope_required") {
			t.Errorf("%s %s: denied mutation did not audit admin_scope_required: %#v", tc.method, tc.target, audit.events)
		}
	}
}

// TestAdminMutationsRequireTenant verifies an all-scope caller with no tenant is
// rejected so a blank tenant can never mutate across tenants.
func TestAdminMutationsRequireTenant(t *testing.T) {
	t.Parallel()

	tenantless := AuthContext{Mode: AuthModeShared, AllScopes: true}
	for _, tc := range adminMutationCases() {
		audit := &recordingAuditAppender{}
		mux := newMutationMux(&fakeAdminMutationStore{}, audit)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, mutationRequest(tc.method, tc.target, tc.body, tenantless))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s with no tenant = %d, want 403", tc.method, tc.target, rec.Code)
		}
		if !audit.hasReason("admin_tenant_required") {
			t.Errorf("%s %s: tenantless mutation did not audit admin_tenant_required", tc.method, tc.target)
		}
	}
}

// TestAdminMutationsSharedTokenDenialAuditsValidEvent verifies that a denial
// from a shared bearer-token caller (AuthModeShared, no SubjectIDHash, no
// tenant) produces a governance audit event that passes NormalizeEvent. Without
// the sharedAdminActorIDHash fallback the event has ActorClass=shared_token and
// empty ActorIDHash, which NormalizeEvent rejects, silently dropping the record.
func TestAdminMutationsSharedTokenDenialAuditsValidEvent(t *testing.T) {
	t.Parallel()

	// Shared bearer token: no SubjectIDHash, no tenant — triggers admin_tenant_required.
	sharedNoTenant := AuthContext{Mode: AuthModeShared, AllScopes: true, SubjectIDHash: ""}
	for _, tc := range adminMutationCases() {
		audit := &recordingAuditAppender{}
		mux := newMutationMux(&fakeAdminMutationStore{}, audit)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, mutationRequest(tc.method, tc.target, tc.body, sharedNoTenant))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: status = %d, want 403", tc.method, tc.target, rec.Code)
		}
		if len(audit.events) == 0 {
			t.Errorf("%s %s: no audit event recorded for shared-token denial", tc.method, tc.target)
			continue
		}
		// Every emitted event must normalizable — a silently dropped event would
		// mean the denial is unrecorded in the governance trail.
		for i, ev := range audit.events {
			if _, err := governanceaudit.NormalizeEvent(ev); err != nil {
				t.Errorf("%s %s: event[%d] failed NormalizeEvent: %v (ActorIDHash=%q ActorClass=%q)",
					tc.method, tc.target, i, err, ev.ActorIDHash, ev.ActorClass)
			}
		}
	}
}

// TestAdminMutationsNilStoreReturns503 verifies a nil store yields 503 not panic.
func TestAdminMutationsNilStoreReturns503(t *testing.T) {
	t.Parallel()

	handler := &AdminIdentityMutationHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)
	for _, tc := range adminMutationCases() {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, mutationRequest(tc.method, tc.target, tc.body, allScopeAdminAuth("tenant_a", "workspace_a")))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s nil store = %d, want 503", tc.method, tc.target, rec.Code)
		}
	}
}

// --- Endpoint 1: revoke invitation ---

func TestRevokeInvitationScopesToAuthTenantAndAudits(t *testing.T) {
	t.Parallel()

	store := &fakeAdminMutationStore{inviteResult: AdminInvitationRevokeResult{Found: true, Revoked: true, Status: "revoked"}}
	audit := &recordingAuditAppender{}
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
	if !audit.hasReason("invitation_revoked") {
		t.Fatalf("revoke did not audit invitation_revoked: %#v", audit.events)
	}
}

func TestRevokeInvitationNotFound(t *testing.T) {
	t.Parallel()

	store := &fakeAdminMutationStore{inviteResult: AdminInvitationRevokeResult{Found: false}}
	audit := &recordingAuditAppender{}
	mux := newMutationMux(store, audit)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, mutationRequest(http.MethodPost, "/api/v0/auth/local/invitations/missing/revoke", "", allScopeAdminAuth("tenant_a", "workspace_a")))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if !audit.hasReason("invitation_not_found") {
		t.Fatalf("not-found did not audit invitation_not_found")
	}
}

func TestRevokeInvitationIdempotentNoop(t *testing.T) {
	t.Parallel()

	// Already revoked: Found true, Revoked false. Must be 200 with status, no error.
	store := &fakeAdminMutationStore{inviteResult: AdminInvitationRevokeResult{Found: true, Revoked: false, Status: "revoked"}}
	audit := &recordingAuditAppender{}
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
	if !audit.hasReason("invitation_revoke_noop") {
		t.Fatalf("idempotent no-op did not audit invitation_revoke_noop")
	}
}

func TestRevokeInvitationNeverEchoesInviteCode(t *testing.T) {
	t.Parallel()

	store := &fakeAdminMutationStore{inviteResult: AdminInvitationRevokeResult{Found: true, Revoked: true, Status: "revoked"}}
	mux := newMutationMux(store, &recordingAuditAppender{})

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

func TestGrantRoleAssignmentValidatesRole(t *testing.T) {
	t.Parallel()

	store := &fakeAdminMutationStore{grantResult: AdminRoleAssignmentMutationResult{RoleValid: false}}
	audit := &recordingAuditAppender{}
	mux := newMutationMux(store, audit)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, mutationRequest(http.MethodPost, "/api/v0/auth/admin/role-assignments", `{"user_id":"u1","role_id":"ghost"}`, allScopeAdminAuth("tenant_a", "workspace_a")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown role status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !audit.hasReason("role_assignment_unknown_role") {
		t.Fatalf("unknown role did not audit role_assignment_unknown_role")
	}
}

func TestGrantRoleAssignmentScopesAndAudits(t *testing.T) {
	t.Parallel()

	store := &fakeAdminMutationStore{grantResult: AdminRoleAssignmentMutationResult{RoleValid: true, UserValid: true, Changed: true, Status: "active"}}
	audit := &recordingAuditAppender{}
	mux := newMutationMux(store, audit)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, mutationRequest(http.MethodPost, "/api/v0/auth/admin/role-assignments", `{"user_id":"u1","role_id":"developer"}`, allScopeAdminAuth("tenant_a", "workspace_a")))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if store.gotGrant.TenantID != "tenant_a" || store.gotGrant.WorkspaceID != "workspace_a" {
		t.Fatalf("grant scoped to %q/%q, want tenant_a/workspace_a", store.gotGrant.TenantID, store.gotGrant.WorkspaceID)
	}
	if !audit.hasReason("role_assignment_granted") {
		t.Fatalf("grant did not audit role_assignment_granted")
	}
}

// TestGrantRoleAssignmentRejectsForeignWorkspace proves an admin cannot grant in
// another workspace by passing a different workspace_id.
func TestGrantRoleAssignmentRejectsForeignWorkspace(t *testing.T) {
	t.Parallel()

	store := &fakeAdminMutationStore{grantResult: AdminRoleAssignmentMutationResult{RoleValid: true}}
	audit := &recordingAuditAppender{}
	mux := newMutationMux(store, audit)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, mutationRequest(http.MethodPost, "/api/v0/auth/admin/role-assignments", `{"user_id":"u1","role_id":"developer","workspace_id":"workspace_b"}`, allScopeAdminAuth("tenant_a", "workspace_a")))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign workspace status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if store.gotGrant.TenantID != "" {
		t.Fatalf("store was called despite foreign workspace: %#v", store.gotGrant)
	}
	if !audit.hasReason("role_assignment_workspace_mismatch") {
		t.Fatalf("foreign workspace did not audit role_assignment_workspace_mismatch")
	}
}

func TestGrantRoleAssignmentRequiresFields(t *testing.T) {
	t.Parallel()

	store := &fakeAdminMutationStore{}
	mux := newMutationMux(store, &recordingAuditAppender{})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, mutationRequest(http.MethodPost, "/api/v0/auth/admin/role-assignments", `{"user_id":"u1"}`, allScopeAdminAuth("tenant_a", "workspace_a")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing role_id status = %d, want 400", rec.Code)
	}
	if store.gotGrant.TenantID != "" {
		t.Fatalf("store called with missing fields")
	}
}

// TestGrantRoleAssignmentRejectsUnknownUser verifies that a grant to a user with
// no active tenant membership returns 400 role_assignment_unknown_user rather
// than propagating a database FK violation as a 500. The store signals this via
// UserValid=false; the handler must never write a row for a non-member.
func TestGrantRoleAssignmentRejectsUnknownUser(t *testing.T) {
	t.Parallel()

	store := &fakeAdminMutationStore{grantResult: AdminRoleAssignmentMutationResult{
		RoleValid: true,
		UserValid: false,
	}}
	audit := &recordingAuditAppender{}
	mux := newMutationMux(store, audit)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, mutationRequest(http.MethodPost, "/api/v0/auth/admin/role-assignments", `{"user_id":"u_no_membership","role_id":"developer"}`, allScopeAdminAuth("tenant_a", "workspace_a")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("non-member grant status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !audit.hasReason("role_assignment_unknown_user") {
		t.Fatalf("non-member grant did not audit role_assignment_unknown_user: %#v", audit.events)
	}
	// Store was called (role is valid), but result was never written.
	if store.gotGrant.TenantID != "tenant_a" {
		t.Fatalf("grant not scoped to tenant_a: %q", store.gotGrant.TenantID)
	}
}

// --- Endpoint 3: revoke role assignment ---

func TestRevokeRoleAssignmentIdempotentNoop(t *testing.T) {
	t.Parallel()

	store := &fakeAdminMutationStore{roleRevokeResult: AdminRoleAssignmentMutationResult{Changed: false, Status: "revoked"}}
	audit := &recordingAuditAppender{}
	mux := newMutationMux(store, audit)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, mutationRequest(http.MethodPost, "/api/v0/auth/admin/role-assignments/revoke", `{"user_id":"u1","role_id":"developer"}`, allScopeAdminAuth("tenant_a", "workspace_a")))
	if rec.Code != http.StatusOK {
		t.Fatalf("idempotent re-revoke status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !audit.hasReason("role_assignment_revoke_noop") {
		t.Fatalf("idempotent no-op did not audit role_assignment_revoke_noop")
	}
	if store.gotRoleRevoke.TenantID != "tenant_a" {
		t.Fatalf("revoke scoped to %q, want tenant_a", store.gotRoleRevoke.TenantID)
	}
}

// --- Endpoint 4: create IdP group mapping ---

func TestCreateIdPGroupMappingHashesGroupAndNeverLeaksRaw(t *testing.T) {
	t.Parallel()

	const rawGroup = "Eshu Developers"
	store := &fakeAdminMutationStore{mappingCreateResult: AdminIdPGroupMappingCreateResult{
		ProviderValid: true, RoleValid: true, Created: true, MappingRef: "ref_abc", Status: "active",
	}}
	audit := &recordingAuditAppender{}
	mux := newMutationMux(store, audit)

	body := `{"provider_config_id":"prov_1","external_group":"` + rawGroup + `","role_id":"developer"}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, mutationRequest(http.MethodPost, "/api/v0/auth/admin/idp-group-mappings", body, allScopeAdminAuth("tenant_a", "workspace_a")))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	// The store must receive the SAME hash the OIDC login path computes for a
	// group claim: "sha256:"+hex(sha256(TrimSpace(group))). This is the canonical
	// form oidclogin.SHA256Hash produces; asserting it independently here (rather
	// than calling oidclogin, which would create an import cycle) locks the
	// write-side hash to the read-side lookup contract.
	sum := sha256.Sum256([]byte(rawGroup))
	want := "sha256:" + hex.EncodeToString(sum[:])
	if store.gotMappingCreate.ExternalGroupHash != want {
		t.Fatalf("external group hash = %q, want %q (must match OIDC login group hash)", store.gotMappingCreate.ExternalGroupHash, want)
	}
	// The raw group name must never reach the response.
	if strings.Contains(rec.Body.String(), rawGroup) || strings.Contains(rec.Body.String(), "external_group") {
		t.Fatalf("response leaked raw external group: %s", rec.Body.String())
	}
	var decoded struct {
		MappingRef string `json:"mapping_ref"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if decoded.MappingRef != "ref_abc" {
		t.Fatalf("mapping_ref = %q, want ref_abc", decoded.MappingRef)
	}
	if !audit.hasReason("idp_group_mapping_created") {
		t.Fatalf("create did not audit idp_group_mapping_created")
	}
}

func TestCreateIdPGroupMappingValidatesProviderAndRole(t *testing.T) {
	t.Parallel()

	body := `{"provider_config_id":"prov_x","external_group":"g","role_id":"r"}`
	cases := []struct {
		name   string
		result AdminIdPGroupMappingCreateResult
		reason string
	}{
		{"unknown provider", AdminIdPGroupMappingCreateResult{ProviderValid: false}, "idp_group_mapping_unknown_provider"},
		{"unknown role", AdminIdPGroupMappingCreateResult{ProviderValid: true, RoleValid: false}, "idp_group_mapping_unknown_role"},
	}
	for _, tc := range cases {
		store := &fakeAdminMutationStore{mappingCreateResult: tc.result}
		audit := &recordingAuditAppender{}
		mux := newMutationMux(store, audit)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, mutationRequest(http.MethodPost, "/api/v0/auth/admin/idp-group-mappings", body, allScopeAdminAuth("tenant_a", "workspace_a")))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d, want 400", tc.name, rec.Code)
		}
		if !audit.hasReason(tc.reason) {
			t.Errorf("%s did not audit %s", tc.name, tc.reason)
		}
	}
}

// --- Endpoint 5: delete IdP group mapping ---

func TestDeleteIdPGroupMappingScopesAndAudits(t *testing.T) {
	t.Parallel()

	store := &fakeAdminMutationStore{mappingDeleteResult: AdminIdPGroupMappingDeleteResult{Found: true, Deleted: true}}
	audit := &recordingAuditAppender{}
	mux := newMutationMux(store, audit)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, mutationRequest(http.MethodDelete, "/api/v0/auth/admin/idp-group-mappings/ref_abc", "", allScopeAdminAuth("tenant_a", "workspace_a")))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if store.gotMappingDelete.TenantID != "tenant_a" || store.gotMappingDelete.WorkspaceID != "workspace_a" {
		t.Fatalf("delete scoped to %q/%q, want tenant_a/workspace_a", store.gotMappingDelete.TenantID, store.gotMappingDelete.WorkspaceID)
	}
	if store.gotMappingDelete.MappingRef != "ref_abc" {
		t.Fatalf("mapping_ref = %q, want ref_abc", store.gotMappingDelete.MappingRef)
	}
	if !audit.hasReason("idp_group_mapping_deleted") {
		t.Fatalf("delete did not audit idp_group_mapping_deleted")
	}
}

func TestDeleteIdPGroupMappingIdempotentNoop(t *testing.T) {
	t.Parallel()

	store := &fakeAdminMutationStore{mappingDeleteResult: AdminIdPGroupMappingDeleteResult{Found: false}}
	audit := &recordingAuditAppender{}
	mux := newMutationMux(store, audit)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, mutationRequest(http.MethodDelete, "/api/v0/auth/admin/idp-group-mappings/missing", "", allScopeAdminAuth("tenant_a", "workspace_a")))
	if rec.Code != http.StatusOK {
		t.Fatalf("idempotent delete status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !audit.hasReason("idp_group_mapping_delete_noop") {
		t.Fatalf("idempotent no-op did not audit idp_group_mapping_delete_noop")
	}
}

// TestAdminMutationAuditEventCarriesTenantID verifies that a successful
// mutation from a tenant admin (AllScopes + TenantID) produces an audit event
// with TenantID and WorkspaceID set to the caller's tenant/workspace (#3717).
// This ensures the tenant admin can read their own mutation events via the
// tenant-scoped audit read endpoint. A bare shared-operator (no TenantID)
// produces a global/NULL event.
func TestAdminMutationAuditEventCarriesTenantID(t *testing.T) {
	t.Parallel()

	store := &fakeAdminMutationStore{
		grantResult: AdminRoleAssignmentMutationResult{
			RoleValid: true, UserValid: true, Changed: true, Status: "active",
		},
	}
	audit := &recordingAuditAppender{}
	mux := newMutationMux(store, audit)

	tenantAdminAuth := allScopeAdminAuth("tenant_a", "workspace_a")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, mutationRequest(
		http.MethodPost, "/api/v0/auth/admin/role-assignments",
		`{"user_id":"user_1","role_id":"developer"}`,
		tenantAdminAuth,
	))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(audit.events) == 0 {
		t.Fatal("no audit events recorded for tenant-admin mutation")
	}
	for _, ev := range audit.events {
		if ev.TenantID != "tenant_a" {
			t.Fatalf("mutation audit event TenantID = %q, want %q: "+
				"tenant admin mutation events must carry TenantID so tenant-scoped audit reads return them",
				ev.TenantID, "tenant_a")
		}
	}

	// A bare shared-operator with no TenantID must produce a global/NULL event.
	sharedAudit := &recordingAuditAppender{}
	sharedMux := newMutationMux(store, sharedAudit)
	// AuthModeShared + no TenantID → rejected by adminScope (admin_tenant_required),
	// but a denial event is still emitted — and it must carry empty TenantID.
	sharedAuth := AuthContext{Mode: AuthModeShared, AllScopes: true, TenantID: ""}
	sharedRec := httptest.NewRecorder()
	sharedMux.ServeHTTP(sharedRec, mutationRequest(
		http.MethodPost, "/api/v0/auth/admin/role-assignments",
		`{"user_id":"user_1","role_id":"developer"}`,
		sharedAuth,
	))
	for _, ev := range sharedAudit.events {
		if ev.TenantID != "" {
			t.Fatalf("shared-operator (no tenant) mutation audit event TenantID = %q, want empty (global event)",
				ev.TenantID)
		}
	}
}
