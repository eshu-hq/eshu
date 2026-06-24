package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// fakeAdminIdentityReadStore records the tenant/workspace it was asked for and
// returns only rows whose tenant matches, modeling the Postgres tenant filter so
// handler-level isolation can be proven without a database.
type fakeAdminIdentityReadStore struct {
	gotTenantID    string
	gotWorkspaceID string
	gotUserID      string

	invitations    map[string][]AdminInvitationListItem
	assignments    map[string][]AdminRoleAssignmentListItem
	roles          map[string][]AdminRoleListItem
	providers      map[string][]AdminIdPProviderListItem
	groupMappings  map[string][]AdminIdPGroupMappingListItem
	apiTokens      map[string][]AdminAPITokenListItem
	forceListError error
}

func (f *fakeAdminIdentityReadStore) ListAdminInvitations(_ context.Context, tenantID, workspaceID string) ([]AdminInvitationListItem, error) {
	f.gotTenantID, f.gotWorkspaceID = tenantID, workspaceID
	if f.forceListError != nil {
		return nil, f.forceListError
	}
	return f.invitations[tenantID], nil
}

func (f *fakeAdminIdentityReadStore) ListAdminRoleAssignments(_ context.Context, tenantID, workspaceID, userID string) ([]AdminRoleAssignmentListItem, error) {
	f.gotTenantID, f.gotWorkspaceID, f.gotUserID = tenantID, workspaceID, userID
	return f.assignments[tenantID], nil
}

func (f *fakeAdminIdentityReadStore) ListAdminRoles(_ context.Context, tenantID string) ([]AdminRoleListItem, error) {
	f.gotTenantID = tenantID
	return f.roles[tenantID], nil
}

func (f *fakeAdminIdentityReadStore) ListAdminIdPProviders(_ context.Context, tenantID string) ([]AdminIdPProviderListItem, error) {
	f.gotTenantID = tenantID
	return f.providers[tenantID], nil
}

func (f *fakeAdminIdentityReadStore) ListAdminIdPGroupMappings(_ context.Context, tenantID, workspaceID string) ([]AdminIdPGroupMappingListItem, error) {
	f.gotTenantID, f.gotWorkspaceID = tenantID, workspaceID
	return f.groupMappings[tenantID], nil
}

func (f *fakeAdminIdentityReadStore) ListAdminAPITokens(_ context.Context, tenantID, workspaceID string) ([]AdminAPITokenListItem, error) {
	f.gotTenantID, f.gotWorkspaceID = tenantID, workspaceID
	return f.apiTokens[tenantID], nil
}

// fakeAdminAuditReader records the query it received and returns canned events.
type fakeAdminAuditReader struct {
	gotQuery       AdminAuditQuery
	events         []governanceaudit.Event
	summary        governanceaudit.Summary
	forceListError error
}

func (f *fakeAdminAuditReader) ListAuditEvents(_ context.Context, q AdminAuditQuery) ([]governanceaudit.Event, error) {
	f.gotQuery = q
	if f.forceListError != nil {
		return nil, f.forceListError
	}
	return f.events, nil
}

func (f *fakeAdminAuditReader) SummarizeAuditEvents(_ context.Context) (governanceaudit.Summary, error) {
	return f.summary, nil
}

func adminRequest(t *testing.T, method, target string, auth AuthContext) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	return req.WithContext(ContextWithAuthContext(req.Context(), auth))
}

func allScopeAdminAuth(tenantID, workspaceID string) AuthContext {
	return AuthContext{
		Mode:        AuthModeBrowserSession,
		TenantID:    tenantID,
		WorkspaceID: workspaceID,
		AllScopes:   true,
	}
}

// TestAdminIdentityReadsRequireAllScope verifies every admin read route returns
// 403 for a non-all-scope caller, even one carrying a tenant.
func TestAdminIdentityReadsRequireAllScope(t *testing.T) {
	t.Parallel()

	handler := &AdminIdentityReadHandler{
		Store: &fakeAdminIdentityReadStore{},
		Audit: &fakeAdminAuditReader{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	scopedAuth := AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant_a",
		WorkspaceID: "workspace_a",
		AllScopes:   false,
	}
	for _, path := range adminReadPaths() {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, adminRequest(t, http.MethodGet, path, scopedAuth))
		if rec.Code != http.StatusForbidden {
			t.Errorf("GET %s as non-admin = %d, want 403: %s", path, rec.Code, rec.Body.String())
		}
	}
}

// TestAdminIdentityReadsRequireTenant verifies an all-scope caller with no
// tenant is rejected, so a blank tenant can never list across tenants.
func TestAdminIdentityReadsRequireTenant(t *testing.T) {
	t.Parallel()

	handler := &AdminIdentityReadHandler{
		Store: &fakeAdminIdentityReadStore{},
		Audit: &fakeAdminAuditReader{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	tenantlessAdmin := AuthContext{Mode: AuthModeShared, AllScopes: true}
	for _, path := range adminReadPaths() {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, adminRequest(t, http.MethodGet, path, tenantlessAdmin))
		if rec.Code != http.StatusForbidden {
			t.Errorf("GET %s with no tenant = %d, want 403: %s", path, rec.Code, rec.Body.String())
		}
	}
}

// TestAdminIdentityReadsTenantIsolation proves an admin can only ever read their
// own tenant's data: the handler passes the AuthContext tenant to the store, and
// rows belonging to another tenant are never returned even when present.
func TestAdminIdentityReadsTenantIsolation(t *testing.T) {
	t.Parallel()

	store := &fakeAdminIdentityReadStore{
		invitations: map[string][]AdminInvitationListItem{
			"tenant_a": {{InviteID: "inv_a", RoleID: "developer", Status: "active", TenantID: "tenant_a", WorkspaceID: "workspace_a"}},
			"tenant_b": {{InviteID: "inv_b", RoleID: "admin", Status: "active", TenantID: "tenant_b", WorkspaceID: "workspace_b"}},
		},
	}
	handler := &AdminIdentityReadHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminRequest(t, http.MethodGet, "/api/v0/auth/local/invitations", allScopeAdminAuth("tenant_a", "workspace_a")))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if store.gotTenantID != "tenant_a" || store.gotWorkspaceID != "workspace_a" {
		t.Fatalf("store scoped to %q/%q, want tenant_a/workspace_a", store.gotTenantID, store.gotWorkspaceID)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "inv_a") {
		t.Fatalf("response missing own-tenant invitation: %s", body)
	}
	if strings.Contains(body, "inv_b") || strings.Contains(body, "tenant_b") {
		t.Fatalf("response leaked another tenant's data: %s", body)
	}
}

// TestAdminIdentityReadsNeverLeakSecrets renders every read with rows that would
// carry a secret if the projection were wrong, then asserts no secret-shaped
// token appears in any response body. The fake item structs contain only safe
// fields by construction, so this guards the JSON projection.
func TestAdminIdentityReadsNeverLeakSecrets(t *testing.T) {
	t.Parallel()

	store := &fakeAdminIdentityReadStore{
		invitations: map[string][]AdminInvitationListItem{
			"tenant_a": {{InviteID: "inv_a", RoleID: "developer", Status: "active", TenantID: "tenant_a", WorkspaceID: "workspace_a"}},
		},
		assignments: map[string][]AdminRoleAssignmentListItem{
			"tenant_a": {{UserID: "user_1", RoleID: "developer", AssignmentSource: "invitation", Status: "active", TenantID: "tenant_a", WorkspaceID: "workspace_a"}},
		},
		roles: map[string][]AdminRoleListItem{
			"tenant_a": {{RoleID: "developer", Status: "active", BuiltIn: true, Grants: []AdminRoleGrantListItem{{GrantID: "g1", Action: "read", Feature: "code", DataClass: "metadata", ScopeClass: "tenant", Status: "active"}}}},
		},
		providers: map[string][]AdminIdPProviderListItem{
			"tenant_a": {{ProviderConfigID: "prov_1", ProviderKind: "oidc", Status: "active"}},
		},
		groupMappings: map[string][]AdminIdPGroupMappingListItem{
			"tenant_a": {{MappingRef: "ref_1", ProviderConfigID: "prov_1", RoleID: "developer", Status: "active", TenantID: "tenant_a", WorkspaceID: "workspace_a"}},
		},
		apiTokens: map[string][]AdminAPITokenListItem{
			"tenant_a": {{TokenID: "tok_1", TokenClass: "personal", UserID: "user_1", Status: "active", TenantID: "tenant_a", WorkspaceID: "workspace_a"}},
		},
	}
	handler := &AdminIdentityReadHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	forbidden := []string{
		"_hash", "invite_code", "credential_handle", "external_group_hash",
		"token_hash", "password", "secret", "issuer_hash",
	}
	for _, path := range []string{
		"/api/v0/auth/local/invitations",
		"/api/v0/auth/admin/role-assignments",
		"/api/v0/auth/admin/roles",
		"/api/v0/auth/admin/idp-providers",
		"/api/v0/auth/admin/idp-group-mappings",
		"/api/v0/auth/admin/api-tokens",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, adminRequest(t, http.MethodGet, path, allScopeAdminAuth("tenant_a", "workspace_a")))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200: %s", path, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		for _, secret := range forbidden {
			if strings.Contains(body, secret) {
				t.Errorf("GET %s leaked secret-shaped token %q: %s", path, secret, body)
			}
		}
	}
}

// TestAdminRoleAssignmentsUserFilterThreaded verifies the user_id query
// parameter is passed through to the store.
func TestAdminRoleAssignmentsUserFilterThreaded(t *testing.T) {
	t.Parallel()

	store := &fakeAdminIdentityReadStore{}
	handler := &AdminIdentityReadHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminRequest(t, http.MethodGet, "/api/v0/auth/admin/role-assignments?user_id=user_42", allScopeAdminAuth("tenant_a", "workspace_a")))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if store.gotUserID != "user_42" {
		t.Fatalf("store user filter = %q, want user_42", store.gotUserID)
	}
}

// TestAdminAuditEventsSetsOperatorAuthorizedAndFilters verifies the audit events
// handler sets OperatorAuthorized only after the admin gate passes and threads
// the bounded filter parameters.
func TestAdminAuditEventsSetsOperatorAuthorizedAndFilters(t *testing.T) {
	t.Parallel()

	reader := &fakeAdminAuditReader{
		events: []governanceaudit.Event{{
			Type:        governanceaudit.EventTypeTokenLifecycle,
			ActorClass:  governanceaudit.ActorClassOperator,
			ScopeClass:  governanceaudit.ScopeClassAdmin,
			Decision:    governanceaudit.DecisionAllowed,
			ReasonCode:  "api_token_issued",
			ActorIDHash: "sha256:should-not-render",
			ScopeIDHash: "sha256:should-not-render",
			OccurredAt:  time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC),
		}},
	}
	handler := &AdminIdentityReadHandler{Audit: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	target := "/api/v0/auth/admin/audit/events?event_type=token_lifecycle&decision=allowed&reason_code=api_token_issued&limit=10&occurred_after=2026-06-01T00:00:00Z"
	rec := httptest.NewRecorder()
	// Audit reads require the global shared-operator scope (governance_audit_events
	// has no tenant column; per-tenant audit is tracked in #3717).
	mux.ServeHTTP(rec, adminRequest(t, http.MethodGet, target, AuthContext{Mode: AuthModeShared, AllScopes: true}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !reader.gotQuery.OperatorAuthorized {
		t.Fatal("audit query OperatorAuthorized = false, want true after shared-operator gate")
	}
	if reader.gotQuery.EventType != "token_lifecycle" || reader.gotQuery.Decision != "allowed" ||
		reader.gotQuery.ReasonCode != "api_token_issued" || reader.gotQuery.Limit != 10 {
		t.Fatalf("audit filter not threaded: %#v", reader.gotQuery)
	}
	if reader.gotQuery.OccurredAfter.IsZero() {
		t.Fatal("occurred_after not parsed")
	}
	body := rec.Body.String()
	if strings.Contains(body, "should-not-render") || strings.Contains(body, "actor_id_hash") || strings.Contains(body, "scope_id_hash") {
		t.Fatalf("audit events leaked hashed identifiers: %s", body)
	}

	var decoded struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(decoded.Events) != 1 || decoded.Events[0]["event_type"] != "token_lifecycle" {
		t.Fatalf("events = %#v, want one token_lifecycle event", decoded.Events)
	}
}

// TestAdminAuditEventsRequireSharedOperator verifies the audit endpoints require
// AuthModeShared. Both a non-admin scoped caller and a tenant admin (AllScopes +
// tenant, the browser-session pattern) must be rejected with 403.
//
// Background: governance_audit_events has no tenant_id column, so the data is
// GLOBAL. A tenant admin in tenant_a would see tenant_b's audit volumes and
// decisions if allowed. Only a shared operator (AuthModeShared bearer token)
// holds the authority to read the full cross-tenant audit stream.
// Per-tenant audit is tracked in #3717.
func TestAdminAuditEventsRequireSharedOperator(t *testing.T) {
	t.Parallel()

	reader := &fakeAdminAuditReader{}
	handler := &AdminIdentityReadHandler{Audit: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	auditPaths := []string{"/api/v0/auth/admin/audit/events", "/api/v0/auth/admin/audit/summary"}

	// Scoped (non-admin) caller: must be denied.
	scopedAuth := AuthContext{Mode: AuthModeScoped, TenantID: "tenant_a", AllScopes: false}
	for _, path := range auditPaths {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, adminRequest(t, http.MethodGet, path, scopedAuth))
		if rec.Code != http.StatusForbidden {
			t.Errorf("GET %s as scoped non-admin = %d, want 403", path, rec.Code)
		}
	}

	// Tenant admin (AllScopes + tenant, browser-session mode): must also be denied.
	// This is the key cross-tenant data-leak scenario.
	tenantAdmin := AuthContext{Mode: AuthModeBrowserSession, TenantID: "tenant_a", WorkspaceID: "workspace_a", AllScopes: true}
	for _, path := range auditPaths {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, adminRequest(t, http.MethodGet, path, tenantAdmin))
		if rec.Code != http.StatusForbidden {
			t.Errorf("GET %s as tenant-admin (AllScopes+tenant) = %d, want 403: "+
				"tenant admin must not access global audit data", path, rec.Code)
		}
	}

	// No denied caller may reach the audit store. Assert this BEFORE the allowed
	// shared-operator call below, which legitimately queries the store.
	if reader.gotQuery.OperatorAuthorized {
		t.Fatal("audit store was queried with OperatorAuthorized despite denied callers")
	}

	// Shared operator: must be allowed.
	sharedOp := AuthContext{Mode: AuthModeShared, AllScopes: true}
	for _, path := range auditPaths {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, adminRequest(t, http.MethodGet, path, sharedOp))
		// 200 or 503 (nil audit store case is separate test); must NOT be 403.
		if rec.Code == http.StatusForbidden {
			t.Errorf("GET %s as shared operator = 403, want non-403: shared operator must reach audit endpoints", path)
		}
	}
}

// TestAdminIdentityReadsNilStoreReturns503 verifies a nil store yields 503 not a
// panic.
func TestAdminIdentityReadsNilStoreReturns503(t *testing.T) {
	t.Parallel()

	handler := &AdminIdentityReadHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminRequest(t, http.MethodGet, "/api/v0/auth/local/invitations", allScopeAdminAuth("tenant_a", "workspace_a")))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil store status = %d, want 503", rec.Code)
	}
}

// adminReadPaths returns the tenant-admin identity read paths (all-scope +
// tenant gate). The audit paths are excluded because they use the
// shared-operator gate (AuthModeShared), not the tenant-admin gate; they are
// tested separately in TestAdminAuditEventsRequireSharedOperator.
func adminReadPaths() []string {
	return []string{
		"/api/v0/auth/local/invitations",
		"/api/v0/auth/admin/role-assignments",
		"/api/v0/auth/admin/roles",
		"/api/v0/auth/admin/idp-providers",
		"/api/v0/auth/admin/idp-group-mappings",
		"/api/v0/auth/admin/api-tokens",
	}
}
