// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestScopedTokenReachesFreshnessDeltaPairOnly pins which freshness delta
// reads a scoped token may reach after #5167's freshness workstream. The pair
// keyed on repository/scope rows is promoted; the service lineage read is not,
// because service_materialization_generations carries no column that names the
// tenant its rows belong to (#6475). Promoting it while that is true would let
// one tenant read another's lineage whenever the correlation the handler fence
// probes has aged out of the correlating scope's active generation, which is
// exactly the "a promoted route never turns a 403 into a cross-tenant read"
// contract in pendingRowFilteringRoutes' header.
//
// The service route's handler fence (serviceChangedSinceGrantAdmits) is in the
// tree and tested, so this assertion is about the middleware ledger, not about
// the fence: the fence is the first half of the promotion and #6475 is the
// second.
func TestScopedTokenReachesFreshnessDeltaPairOnly(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "changed_since_promoted",
			path:       "/api/v0/freshness/changed-since",
			wantStatus: http.StatusOK,
		},
		{
			name:       "generations_promoted",
			path:       "/api/v0/freshness/generations",
			wantStatus: http.StatusOK,
		},
		{
			name:       "service_changed_since_still_pending",
			path:       "/api/v0/freshness/services/changed-since",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resolver := &fakeScopedTokenResolver{
				context: AuthContext{
					Mode:                 AuthModeScoped,
					TenantID:             "tenant_a",
					WorkspaceID:          "workspace_a",
					AllowedRepositoryIDs: []string{"repo_a"},
				},
				ok: true,
			}
			called := false
			handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			req.Header.Set("Authorization", "Bearer scoped-token")
			req.Header.Set("X-Correlation-ID", "corr-freshness-delta")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if got := rec.Code; got != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", got, tc.wantStatus, rec.Body.String())
			}
			if want := tc.wantStatus == http.StatusOK; called != want {
				t.Fatalf("next handler called = %t, want %t", called, want)
			}
			if tc.wantStatus != http.StatusForbidden {
				return
			}
			var envelope ResponseEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("json.Unmarshal() error = %v, want nil", err)
			}
			if envelope.Error == nil {
				t.Fatalf("envelope.Error = nil, want scoped-route denial; body = %s", rec.Body.String())
			}
			if got, want := envelope.Error.Code, ErrorCodePermissionDenied; got != want {
				t.Fatalf("error code = %q, want %q", got, want)
			}
		})
	}
}

// TestServiceChangedSinceStaysOnPendingLedger asserts the ledger side of the
// same withdrawal directly, so a future contributor who allowlists the route
// without the #6475 schema change fails here as well as in the middleware
// table above.
func TestServiceChangedSinceStaysOnPendingLedger(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/api/v0/freshness/services/changed-since", nil)
	if !IsPendingRowFilteringRoute(req) {
		t.Fatal("IsPendingRowFilteringRoute() = false, want true until #6475 names the tenant on lineage rows")
	}
	if ScopedHTTPRouteSupportsTenantFilter(req) {
		t.Fatal("ScopedHTTPRouteSupportsTenantFilter() = true, want false while the route is pending")
	}
	if _, ok := scopedTokenAdvertisedRoutes["GET /api/v0/freshness/services/changed-since"]; ok {
		t.Fatal("service changed-since is advertised in scopedTokenAdvertisedRoutes, want absent while pending")
	}
	for _, promoted := range []string{"/api/v0/freshness/changed-since", "/api/v0/freshness/generations"} {
		promotedReq := httptest.NewRequest(http.MethodGet, promoted, nil)
		if !ScopedHTTPRouteSupportsTenantFilter(promotedReq) {
			t.Fatalf("ScopedHTTPRouteSupportsTenantFilter(%s) = false, want true", promoted)
		}
		if IsPendingRowFilteringRoute(promotedReq) {
			t.Fatalf("IsPendingRowFilteringRoute(%s) = true, want false", promoted)
		}
	}
}
