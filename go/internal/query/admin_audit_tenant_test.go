// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// Tenant-scoped audit tests for issue #3717.
//
// These tests cover:
// 1. A tenant admin (AllScopes + TenantID) can read their OWN tenant's audit.
// 2. A tenant admin never sees another tenant's events (isolation).
// 3. The shared-operator (AuthModeShared, no tenant) sees everything.
// 4. The handler passes TenantID from the AuthContext to the audit reader query.
// 5. The handler passes TenantID to the summary reader.
//
// All tests FAIL before the handler gating is updated (currently sharedOperatorScope
// rejects any non-shared caller, so a tenant admin gets 403 always).

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

// ---------------------------------------------------------------------------
// Fake audit reader that records per-tenant isolation
// ---------------------------------------------------------------------------

// tenantIsolatingAuditReader models the real store: it stores events keyed by
// tenant and returns only the requested tenant's events (or all when TenantID
// is blank — shared operator).
type tenantIsolatingAuditReader struct {
	// eventsByTenant maps tenantID → events. Key "" = global events.
	eventsByTenant map[string][]governanceaudit.Event
	// gotListQuery and gotSummaryTenantID are set on every call for assertions.
	gotListQuery       AdminAuditQuery
	gotSummaryTenantID string
	// summaryByTenant maps tenantID → summary.
	summaryByTenant map[string]governanceaudit.Summary
}

func (r *tenantIsolatingAuditReader) ListAuditEvents(_ context.Context, q AdminAuditQuery) ([]governanceaudit.Event, error) {
	r.gotListQuery = q
	// Shared operator (no TenantID) sees everything.
	if q.TenantID == "" {
		var all []governanceaudit.Event
		for _, evs := range r.eventsByTenant {
			all = append(all, evs...)
		}
		return all, nil
	}
	return r.eventsByTenant[q.TenantID], nil
}

func (r *tenantIsolatingAuditReader) SummarizeAuditEvents(_ context.Context) (governanceaudit.Summary, error) {
	r.gotSummaryTenantID = ""
	if s, ok := r.summaryByTenant[""]; ok {
		return s, nil
	}
	return governanceaudit.Summary{}, nil
}

func (r *tenantIsolatingAuditReader) SummarizeAuditEventsForTenant(_ context.Context, tenantID string) (governanceaudit.Summary, error) {
	r.gotSummaryTenantID = tenantID
	if s, ok := r.summaryByTenant[tenantID]; ok {
		return s, nil
	}
	return governanceaudit.Summary{}, nil
}

// ---------------------------------------------------------------------------
// Test: tenant admin can read their own tenant's audit
// ---------------------------------------------------------------------------

// TestAuditEventsHandlerTenantAdminSeesOwnTenant verifies that a caller with
// AllScopes + TenantID gets HTTP 200 on the audit events endpoint and the
// handler passes TenantID to the reader.
// FAILS before the handler gating is updated (currently returns 403).
func TestAuditEventsHandlerTenantAdminSeesOwnTenant(t *testing.T) {
	t.Parallel()

	auditTime := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	reader := &tenantIsolatingAuditReader{
		eventsByTenant: map[string][]governanceaudit.Event{
			"tenant_a": {
				{
					Type:       governanceaudit.EventTypeTokenLifecycle,
					ActorClass: governanceaudit.ActorClassScopedToken,
					ScopeClass: governanceaudit.ScopeClassTenant,
					Decision:   governanceaudit.DecisionAllowed,
					ReasonCode: "api_token_issued",
					TenantID:   "tenant_a",
					OccurredAt: auditTime,
				},
			},
			"tenant_b": {
				{
					Type:       governanceaudit.EventTypeTokenLifecycle,
					ActorClass: governanceaudit.ActorClassScopedToken,
					ScopeClass: governanceaudit.ScopeClassTenant,
					Decision:   governanceaudit.DecisionDenied,
					ReasonCode: "policy_denied",
					TenantID:   "tenant_b",
					OccurredAt: auditTime.Add(time.Minute),
				},
			},
		},
	}

	handler := &AdminIdentityReadHandler{
		Store: &fakeAdminIdentityReadStore{},
		Audit: reader,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	// Tenant admin auth: AllScopes=true + TenantID set (not shared-operator).
	tenantAuth := AuthContext{
		Mode:        AuthModeBrowserSession,
		TenantID:    "tenant_a",
		WorkspaceID: "workspace_a",
		AllScopes:   true,
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminRequest(t, http.MethodGet, "/api/v0/auth/admin/audit/events", tenantAuth))

	if rec.Code != http.StatusOK {
		t.Fatalf("tenant admin audit/events status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	// Handler must pass TenantID to the reader so it can apply the filter.
	if reader.gotListQuery.TenantID != "tenant_a" {
		t.Fatalf("handler passed TenantID=%q to reader, want %q", reader.gotListQuery.TenantID, "tenant_a")
	}
}

// ---------------------------------------------------------------------------
// Test: tenant admin never sees another tenant's events
// ---------------------------------------------------------------------------

// TestAuditEventsHandlerTenantAdminCrossIsolation verifies that the response
// body contains only the caller's tenant's events — no cross-tenant leakage.
func TestAuditEventsHandlerTenantAdminCrossIsolation(t *testing.T) {
	t.Parallel()

	auditTime := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	reader := &tenantIsolatingAuditReader{
		eventsByTenant: map[string][]governanceaudit.Event{
			"tenant_a": {
				{
					Type:          governanceaudit.EventTypeTokenLifecycle,
					ActorClass:    governanceaudit.ActorClassScopedToken,
					ScopeClass:    governanceaudit.ScopeClassTenant,
					Decision:      governanceaudit.DecisionAllowed,
					ReasonCode:    "api_token_issued",
					CorrelationID: "corr-tenant-a-1",
					TenantID:      "tenant_a",
					OccurredAt:    auditTime,
				},
			},
			"tenant_b": {
				{
					Type:          governanceaudit.EventTypeTokenLifecycle,
					ActorClass:    governanceaudit.ActorClassScopedToken,
					ScopeClass:    governanceaudit.ScopeClassTenant,
					Decision:      governanceaudit.DecisionDenied,
					ReasonCode:    "policy_denied",
					CorrelationID: "corr-tenant-b-secret",
					TenantID:      "tenant_b",
					OccurredAt:    auditTime.Add(time.Minute),
				},
			},
		},
	}

	handler := &AdminIdentityReadHandler{
		Store: &fakeAdminIdentityReadStore{},
		Audit: reader,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	tenantAuth := AuthContext{
		Mode:        AuthModeBrowserSession,
		TenantID:    "tenant_a",
		WorkspaceID: "workspace_a",
		AllScopes:   true,
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminRequest(t, http.MethodGet, "/api/v0/auth/admin/audit/events", tenantAuth))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "corr-tenant-b-secret") || strings.Contains(body, "tenant_b") {
		t.Fatalf("tenant_a admin sees tenant_b audit data — cross-tenant leak:\n%s", body)
	}
	if !strings.Contains(body, "corr-tenant-a-1") {
		t.Fatalf("tenant_a admin missing own events:\n%s", body)
	}
}

// ---------------------------------------------------------------------------
// Test: shared-operator sees all tenants
// ---------------------------------------------------------------------------

// TestAuditEventsHandlerSharedOperatorSeesAllTenants verifies the existing
// shared-operator path continues to work and returns events from all tenants
// (no TenantID filter applied).
func TestAuditEventsHandlerSharedOperatorSeesAllTenants(t *testing.T) {
	t.Parallel()

	auditTime := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	reader := &tenantIsolatingAuditReader{
		eventsByTenant: map[string][]governanceaudit.Event{
			"tenant_a": {
				{
					Type:          governanceaudit.EventTypeTokenLifecycle,
					ActorClass:    governanceaudit.ActorClassScopedToken,
					ScopeClass:    governanceaudit.ScopeClassTenant,
					Decision:      governanceaudit.DecisionAllowed,
					ReasonCode:    "api_token_issued",
					CorrelationID: "corr-ta",
					TenantID:      "tenant_a",
					OccurredAt:    auditTime,
				},
			},
			"tenant_b": {
				{
					Type:          governanceaudit.EventTypeTokenLifecycle,
					ActorClass:    governanceaudit.ActorClassScopedToken,
					ScopeClass:    governanceaudit.ScopeClassTenant,
					Decision:      governanceaudit.DecisionAllowed,
					ReasonCode:    "api_token_issued",
					CorrelationID: "corr-tb",
					TenantID:      "tenant_b",
					OccurredAt:    auditTime.Add(time.Minute),
				},
			},
		},
	}

	handler := &AdminIdentityReadHandler{
		Store: &fakeAdminIdentityReadStore{},
		Audit: reader,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	sharedAuth := AuthContext{Mode: AuthModeShared}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminRequest(t, http.MethodGet, "/api/v0/auth/admin/audit/events", sharedAuth))

	if rec.Code != http.StatusOK {
		t.Fatalf("shared-op audit/events status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	// Shared operator must not have a TenantID filter applied.
	if reader.gotListQuery.TenantID != "" {
		t.Fatalf("shared-op List TenantID = %q, want empty (no filter)", reader.gotListQuery.TenantID)
	}
}

// ---------------------------------------------------------------------------
// Test: tenant admin can read their own summary
// ---------------------------------------------------------------------------

// TestAuditSummaryHandlerTenantAdminSeesOwnSummary verifies that an AllScopes
// + TenantID caller gets 200 on /audit/summary and the handler calls the
// tenant-scoped summary path.
// FAILS before the handler gating is updated.
func TestAuditSummaryHandlerTenantAdminSeesOwnSummary(t *testing.T) {
	t.Parallel()

	reader := &tenantIsolatingAuditReader{
		summaryByTenant: map[string]governanceaudit.Summary{
			"tenant_a": {Total: 5, Denied: 2},
		},
	}

	handler := &AdminIdentityReadHandler{
		Store: &fakeAdminIdentityReadStore{},
		Audit: reader,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	tenantAuth := AuthContext{
		Mode:        AuthModeBrowserSession,
		TenantID:    "tenant_a",
		WorkspaceID: "workspace_a",
		AllScopes:   true,
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminRequest(t, http.MethodGet, "/api/v0/auth/admin/audit/summary", tenantAuth))

	if rec.Code != http.StatusOK {
		t.Fatalf("tenant admin audit/summary status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if reader.gotSummaryTenantID != "tenant_a" {
		t.Fatalf("handler called SummaryForTenant with tenant=%q, want %q", reader.gotSummaryTenantID, "tenant_a")
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if got, want := body["total"], float64(5); got != want {
		t.Fatalf("summary total = %v, want %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// Test: unauthenticated caller still gets 403
// ---------------------------------------------------------------------------

// TestAuditHandlersRejectUnauthenticated verifies neither audit endpoint
// accepts an unauthenticated call after the gating change.
func TestAuditHandlersRejectUnauthenticated(t *testing.T) {
	t.Parallel()

	handler := &AdminIdentityReadHandler{
		Store: &fakeAdminIdentityReadStore{},
		Audit: &tenantIsolatingAuditReader{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, path := range []string{
		"/api/v0/auth/admin/audit/events",
		"/api/v0/auth/admin/audit/summary",
	} {
		rec := httptest.NewRecorder()
		// No auth context injected — bare request.
		req := httptest.NewRequest(http.MethodGet, path, nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("unauthenticated GET %s = %d, want 403", path, rec.Code)
		}
	}
}

// TestAuditHandlersRejectNonAdminScoped verifies a scoped token without
// AllScopes cannot reach either audit endpoint.
func TestAuditHandlersRejectNonAdminScoped(t *testing.T) {
	t.Parallel()

	handler := &AdminIdentityReadHandler{
		Store: &fakeAdminIdentityReadStore{},
		Audit: &tenantIsolatingAuditReader{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	nonAdminAuth := AuthContext{
		Mode:      AuthModeScoped,
		TenantID:  "tenant_a",
		AllScopes: false,
	}
	for _, path := range []string{
		"/api/v0/auth/admin/audit/events",
		"/api/v0/auth/admin/audit/summary",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, adminRequest(t, http.MethodGet, path, nonAdminAuth))
		if rec.Code != http.StatusForbidden {
			t.Errorf("non-admin GET %s = %d, want 403", path, rec.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers used above (adminReadPaths is defined in admin_identity_reads_test.go)
// ---------------------------------------------------------------------------

func adminAuditReadPaths() []string {
	return []string{
		"/api/v0/auth/admin/audit/events",
		"/api/v0/auth/admin/audit/summary",
	}
}
