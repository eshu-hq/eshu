// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// TestServiceChangedSinceBearerTwoTenantBoundary drives GET
// /api/v0/freshness/services/changed-since through the REAL scoped-token
// middleware into the real FreshnessHandler, now that #6475 part B promoted
// the route off pendingRowFilteringRoutes. The sibling
// TestServiceChangedSinceTwoTenantLineageBoundary (package freshness) proves
// the grant binds with an AuthContext already in the request; this one proves
// the admission decision in front of it: a restricted bearer is admitted and
// bounded by its grant in every governance mode, and an all-scope bearer --
// whose grant the lineage SQL cannot bind -- never reaches the read under
// hosted_multi_tenant.
func TestServiceChangedSinceBearerTwoTenantBoundary(t *testing.T) {
	t.Parallel()

	allScopeBearer := AuthContext{
		Mode: AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a", AllScopes: true,
	}

	t.Run("restricted bearer reads only its own lineage in every mode", func(t *testing.T) {
		t.Parallel()
		for _, mode := range []string{"hosted_multi_tenant", "local_no_policy"} {
			rec, reader := serveServiceChangedSinceThroughBearerMiddleware(
				t, testutil.ScopedChangedSinceTenantA(), mode, testutil.ServiceLineageSharedID, "", "gen-a-prior",
			)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: status = %d, want 200; body = %s", mode, rec.Code, rec.Body.String())
			}
			if !reader.LastFilter.Scoped {
				t.Fatalf("%s: filter.Scoped = false for a restricted bearer; the grant never reached the query", mode)
			}
			data, _ := testutil.DecodeChangedSinceEnvelope(t, rec)
			if data["scope_id"] != "scope-a" {
				t.Fatalf("%s: data[scope_id] = %v, want scope-a", mode, data["scope_id"])
			}
		}
	})

	t.Run("restricted bearer selecting another tenant's scope is not found", func(t *testing.T) {
		t.Parallel()
		rec, _ := serveServiceChangedSinceThroughBearerMiddleware(
			t, testutil.ScopedChangedSinceTenantA(), "hosted_multi_tenant", testutil.ServiceLineageSharedID, "scope-b", "gen-b-prior",
		)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
		}
		for _, leak := range []string{"gen-b-current", "gen-a-current"} {
			if strings.Contains(rec.Body.String(), leak) {
				t.Fatalf("refusal leaks %q: %s", leak, rec.Body.String())
			}
		}
	})

	t.Run("all-scope bearer never runs the read under hosted_multi_tenant", func(t *testing.T) {
		t.Parallel()
		rec, reader := serveServiceChangedSinceThroughBearerMiddleware(
			t, allScopeBearer, "hosted_multi_tenant", testutil.ServiceLineageSharedID, "", "gen-a-prior",
		)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403; body = %s", rec.Code, rec.Body.String())
		}
		if reader.Called {
			t.Fatalf("the lineage reader ran for a refused all-scope bearer; filter = %#v", reader.LastFilter)
		}
	})

	t.Run("all-scope bearer under local_no_policy reads every lineage and gets the conflict", func(t *testing.T) {
		t.Parallel()
		rec, reader := serveServiceChangedSinceThroughBearerMiddleware(
			t, allScopeBearer, "local_no_policy", testutil.ServiceLineageSharedID, "", "gen-a-prior",
		)
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409; body = %s", rec.Code, rec.Body.String())
		}
		if reader.LastFilter.Scoped {
			t.Fatal("filter.Scoped = true for an all-scope bearer; its grant is inert, which is why hosted_multi_tenant refuses it")
		}
		_, errEnvelope := testutil.DecodeChangedSinceEnvelope(t, rec)
		details, _ := errEnvelope["details"].(map[string]any)
		if got, want := details["scope_ids"], []any{"scope-a", "scope-b"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("details.scope_ids = %#v, want %#v", got, want)
		}
	})
}

// serveServiceChangedSinceThroughBearerMiddleware is the service-route twin of
// serveChangedSinceThroughBearerMiddleware: one bearer request through the
// route-policy middleware for the named ESHU_GOVERNANCE_MODE, into the real
// FreshnessHandler over the #6475 grant-mirroring lineage fixture.
func serveServiceChangedSinceThroughBearerMiddleware(
	t *testing.T,
	auth AuthContext,
	mode, serviceID, scopeID, since string,
) (*httptest.ResponseRecorder, *testutil.GrantMirroringServiceChangedSince) {
	t.Helper()

	reader := &testutil.GrantMirroringServiceChangedSince{Rows: testutil.TwoTenantServiceLineageRows()}
	mux := http.NewServeMux()
	(&FreshnessHandler{
		ServiceChangedSince: reader,
		ServiceOwnership:    &PostgresServiceCatalogCorrelationStore{},
		Profile:             ProfileLocalAuthoritative,
	}).Mount(mux)

	values := url.Values{"service_id": {serviceID}, "since_generation_id": {since}}
	if scopeID != "" {
		values.Set("scope_id", scopeID)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/freshness/services/changed-since?"+values.Encode(), nil)
	policy := ScopedRoutePolicyForGovernanceMode(GovernanceStatusConfig{Mode: mode})
	return serveThroughBearerMiddleware(t, req, auth, policy, mux), reader
}
