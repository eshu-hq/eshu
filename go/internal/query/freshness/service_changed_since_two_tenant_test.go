// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// serveServiceLineageTwoTenant drives the production handler against the
// grant-mirroring lineage fixture.
func serveServiceLineageTwoTenant(
	t *testing.T, authCtx auth.AuthContext, query url.Values,
) *httptest.ResponseRecorder {
	t.Helper()

	handler := &Handler{
		ServiceChangedSince: &testutil.GrantMirroringServiceChangedSince{Rows: testutil.TwoTenantServiceLineageRows()},
		Profile:             querycontract.ProfileLocalAuthoritative,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/freshness/services/changed-since?"+query.Encode(), nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	req = req.WithContext(auth.ContextWithAuthContext(req.Context(), authCtx))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	return rec
}

func serviceLineageQuery(serviceID, scopeID, since string) url.Values {
	values := url.Values{"service_id": {serviceID}, "since_generation_id": {since}}
	if scopeID != "" {
		values.Set("scope_id", scopeID)
	}
	return values
}

// scopedServiceLineageGrant is a scoped token over the given repositories and
// scopes, in its own tenant and workspace.
func scopedServiceLineageGrant(repositories, scopes []string) auth.AuthContext {
	return auth.AuthContext{
		Mode: auth.AuthModeScoped, TenantID: "tenant-x", WorkspaceID: "workspace-x",
		AllowedRepositoryIDs: repositories, AllowedScopeIDs: scopes,
	}
}

// assertServiceLineageSameNotFound requires two refusals to be the same
// response once each caller's own echoed selectors are normalized: a
// difference would let a caller tell another tenant's lineage apart from one
// that does not exist.
func assertServiceLineageSameNotFound(
	t *testing.T, got, want *httptest.ResponseRecorder, gotEcho, wantEcho []string,
) {
	t.Helper()
	normalize := func(body string, echo []string) string {
		for _, value := range echo {
			body = strings.ReplaceAll(body, value, "SELECTOR")
		}
		return body
	}
	if got.Code != http.StatusNotFound || want.Code != http.StatusNotFound {
		t.Fatalf("status = %d and %d, want 404 for both; bodies:\n %s\n %s",
			got.Code, want.Code, got.Body.String(), want.Body.String())
	}
	if g, w := normalize(got.Body.String(), gotEcho), normalize(want.Body.String(), wantEcho); g != w {
		t.Fatalf("refusal is distinguishable from absence:\n got:  %s\n want: %s", g, w)
	}
	for _, leak := range []string{"scope-b", "gen-b-current", "gen-a-current", "gen-legacy-current", "gen-api-legacy"} {
		if strings.Contains(got.Body.String(), leak) {
			t.Fatalf("refusal body leaks %q: %s", leak, got.Body.String())
		}
	}
}

// TestServiceChangedSinceTwoTenantLineageBoundary is the #6475 part B proof
// that lets GET /api/v0/freshness/services/changed-since leave the pending
// row-filtering ledger. Two tenants both declare component:default/api, so
// the id holds one lineage per tenant scope plus an unattributed legacy one.
func TestServiceChangedSinceTwoTenantLineageBoundary(t *testing.T) {
	t.Parallel()

	tenantA := testutil.ScopedChangedSinceTenantA()

	t.Run("tenant A reads only its own lineage of the shared id", func(t *testing.T) {
		t.Parallel()
		rec := serveServiceLineageTwoTenant(t, tenantA, serviceLineageQuery(testutil.ServiceLineageSharedID, "", "gen-a-prior"))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
		data, _ := testutil.DecodeChangedSinceEnvelope(t, rec)
		if data["scope_id"] != "scope-a" || data["current_active_generation_id"] != "gen-a-current" ||
			data["since_generation_id"] != "gen-a-prior" || data["unattributed"] != false {
			t.Fatalf("data = %v; want tenant A's scope-a lineage", data)
		}
		for _, leak := range []string{"scope-b", "gen-b-"} {
			if strings.Contains(rec.Body.String(), leak) {
				t.Fatalf("tenant A's answer leaks tenant B's %q: %s", leak, rec.Body.String())
			}
		}
	})

	t.Run("tenant B's prior generation id is answered like an unknown id", func(t *testing.T) {
		t.Parallel()
		foreign := serveServiceLineageTwoTenant(t, tenantA, serviceLineageQuery(testutil.ServiceLineageSharedID, "", "gen-b-prior"))
		unknown := serveServiceLineageTwoTenant(t, tenantA, serviceLineageQuery(testutil.ServiceLineageSharedID, "", "gen-nowhere"))
		assertServiceLineageSameNotFound(t, foreign, unknown, []string{"gen-b-prior"}, []string{"gen-nowhere"})
	})

	t.Run("unattributed lineage is invisible to every scoped token", func(t *testing.T) {
		t.Parallel()
		absent := serveServiceLineageTwoTenant(t, tenantA, serviceLineageQuery("component:default/nowhere", "", "gen-legacy-prior"))
		for _, grant := range []auth.AuthContext{
			tenantA,
			scopedServiceLineageGrant([]string{"repo-a", "repo-b"}, []string{"scope-a", "scope-b"}),
		} {
			rec := serveServiceLineageTwoTenant(t, grant, serviceLineageQuery(testutil.ServiceLineageLegacyID, "", "gen-legacy-prior"))
			assertServiceLineageSameNotFound(t, rec, absent,
				[]string{testutil.ServiceLineageLegacyID}, []string{"component:default/nowhere"})
		}
	})

	t.Run("unattributed lineage is visible to the shared key", func(t *testing.T) {
		t.Parallel()
		rec := serveServiceLineageTwoTenant(t, auth.AuthContext{Mode: auth.AuthModeShared},
			serviceLineageQuery(testutil.ServiceLineageLegacyID, "", "gen-legacy-prior"))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
		data, _ := testutil.DecodeChangedSinceEnvelope(t, rec)
		if data["unattributed"] != true || data["scope_id"] != "" || data["current_active_generation_id"] != "gen-legacy-current" {
			t.Fatalf("data = %v; want the unattributed legacy lineage", data)
		}
	})

	t.Run("legacy beside one attributed lineage serves the attributed one to every caller", func(t *testing.T) {
		t.Parallel()
		for name, grant := range map[string]auth.AuthContext{
			"shared key": {Mode: auth.AuthModeShared},
			"tenant A":   tenantA,
		} {
			rec := serveServiceLineageTwoTenant(t, grant,
				serviceLineageQuery(testutil.ServiceLineageMigratedID, "", "gen-migrated-a-prior"))
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: status = %d, want 200 (one attributed lineage, no conflict); body = %s", name, rec.Code, rec.Body.String())
			}
			data, _ := testutil.DecodeChangedSinceEnvelope(t, rec)
			if data["scope_id"] != "scope-a" || data["unattributed"] != false ||
				data["current_active_generation_id"] != "gen-migrated-a-current" {
				t.Fatalf("%s: data = %v; want the attributed scope-a lineage, never the legacy one", name, data)
			}
		}
	})

	t.Run("grant spanning both scopes with no selector is a 409 listing them", func(t *testing.T) {
		t.Parallel()
		for name, grant := range map[string]auth.AuthContext{
			"scope grant":      scopedServiceLineageGrant(nil, []string{"scope-b", "scope-a"}),
			"repository grant": scopedServiceLineageGrant([]string{"repo-a", "repo-b"}, nil),
			"shared key":       {Mode: auth.AuthModeShared},
		} {
			rec := serveServiceLineageTwoTenant(t, grant, serviceLineageQuery(testutil.ServiceLineageSharedID, "", "gen-a-prior"))
			if rec.Code != http.StatusConflict {
				t.Fatalf("%s: status = %d, want 409; body = %s", name, rec.Code, rec.Body.String())
			}
			_, errEnvelope := testutil.DecodeChangedSinceEnvelope(t, rec)
			if errEnvelope["code"] != string(querycontract.ErrorCodeAmbiguous) {
				t.Fatalf("%s: error.code = %v, want ambiguous", name, errEnvelope["code"])
			}
			details, _ := errEnvelope["details"].(map[string]any)
			if got, want := details["scope_ids"], []any{"scope-a", "scope-b"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("%s: details.scope_ids = %#v, want %#v", name, got, want)
			}
			if details["truncated"] != false || details["service_id"] != testutil.ServiceLineageSharedID {
				t.Fatalf("%s: details = %v", name, details)
			}
			for _, leak := range []string{"gen-a-current", "gen-b-current", "gen-api-legacy"} {
				if strings.Contains(rec.Body.String(), leak) {
					t.Fatalf("%s: ambiguity answer leaks lineage %q: %s", name, leak, rec.Body.String())
				}
			}
		}
	})

	t.Run("a grant covering one scope gets no ambiguity for the shared id", func(t *testing.T) {
		t.Parallel()
		rec := serveServiceLineageTwoTenant(t, scopedServiceLineageGrant(nil, []string{"scope-a"}),
			serviceLineageQuery(testutil.ServiceLineageSharedID, "", "gen-a-prior"))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (tenant A's scope is the only admitted one); body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("explicit selector inside the grant serves that lineage", func(t *testing.T) {
		t.Parallel()
		rec := serveServiceLineageTwoTenant(t, scopedServiceLineageGrant(nil, []string{"scope-a", "scope-b"}),
			serviceLineageQuery(testutil.ServiceLineageSharedID, "scope-b", "gen-b-prior"))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
		data, _ := testutil.DecodeChangedSinceEnvelope(t, rec)
		if data["scope_id"] != "scope-b" || data["current_active_generation_id"] != "gen-b-current" {
			t.Fatalf("data = %v; want the selected scope-b lineage", data)
		}
	})

	t.Run("explicit selector outside the grant is not found", func(t *testing.T) {
		t.Parallel()
		rec := serveServiceLineageTwoTenant(t, tenantA, serviceLineageQuery(testutil.ServiceLineageSharedID, "scope-b", "gen-b-prior"))
		absent := serveServiceLineageTwoTenant(t, tenantA, serviceLineageQuery("component:default/nowhere", "scope-b", "gen-b-prior"))
		assertServiceLineageSameNotFound(t, rec, absent,
			[]string{testutil.ServiceLineageSharedID}, []string{"component:default/nowhere"})
		if got, _ := testutil.DecodeChangedSinceEnvelope(t, rec); got != nil {
			t.Fatalf("not-found carried data: %v", got)
		}
		_, errEnvelope := testutil.DecodeChangedSinceEnvelope(t, rec)
		if errEnvelope["code"] != string(querycontract.ErrorCodeServiceNotFound) {
			t.Fatalf("error.code = %v, want service_not_found", errEnvelope["code"])
		}
	})

	t.Run("repository grant reads its own scope's lineage", func(t *testing.T) {
		t.Parallel()
		rec := serveServiceLineageTwoTenant(t, scopedServiceLineageGrant([]string{"repo-b"}, nil),
			serviceLineageQuery(testutil.ServiceLineageSharedID, "", "gen-b-prior"))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
		data, _ := testutil.DecodeChangedSinceEnvelope(t, rec)
		if data["scope_id"] != "scope-b" || data["since_generation_id"] != "gen-b-prior" {
			t.Fatalf("data = %v; want scope-b through the repository grant", data)
		}
	})

	t.Run("non-envelope 409 carries the same scope list", func(t *testing.T) {
		t.Parallel()
		handler := &Handler{
			ServiceChangedSince: &testutil.GrantMirroringServiceChangedSince{Rows: testutil.TwoTenantServiceLineageRows()},
			Profile:             querycontract.ProfileLocalAuthoritative,
		}
		mux := http.NewServeMux()
		handler.Mount(mux)
		req := httptest.NewRequest(http.MethodGet, "/api/v0/freshness/services/changed-since?"+
			serviceLineageQuery(testutil.ServiceLineageSharedID, "", "gen-a-prior").Encode(), nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409; body = %s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got, want := body["scope_ids"], []any{"scope-a", "scope-b"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("scope_ids = %#v, want %#v; body = %s", got, want, rec.Body.String())
		}
	})
}

// TestServiceChangedSinceServesScopedCallerWithoutOwnershipStore pins that the
// route no longer depends on the service-catalog correlation store (#6475 part
// B). The grant binds in the lineage SQL, so a deployment that wires no such
// store must answer a scoped caller by the ordinary rules -- its own lineage,
// or not-found for another tenant's -- never with a refusal about a dependency
// the route does not call.
func TestServiceChangedSinceServesScopedCallerWithoutOwnershipStore(t *testing.T) {
	t.Parallel()

	serve := func(query url.Values) *httptest.ResponseRecorder {
		handler := &Handler{
			ServiceChangedSince: &testutil.GrantMirroringServiceChangedSince{Rows: testutil.TwoTenantServiceLineageRows()},
			Profile:             querycontract.ProfileLocalAuthoritative,
		}
		mux := http.NewServeMux()
		handler.Mount(mux)
		req := httptest.NewRequest(http.MethodGet, "/api/v0/freshness/services/changed-since?"+query.Encode(), nil)
		req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
		req = req.WithContext(auth.ContextWithAuthContext(req.Context(), testutil.ScopedChangedSinceTenantA()))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	own := serve(serviceLineageQuery(testutil.ServiceLineageSharedID, "", "gen-a-prior"))
	if own.Code != http.StatusOK {
		t.Fatalf("own lineage: status = %d, want 200; body = %s", own.Code, own.Body.String())
	}
	data, _ := testutil.DecodeChangedSinceEnvelope(t, own)
	if data["scope_id"] != "scope-a" {
		t.Fatalf("own lineage: data[scope_id] = %v, want scope-a", data["scope_id"])
	}

	foreign := serve(serviceLineageQuery(testutil.ServiceLineageSharedID, "scope-b", "gen-b-prior"))
	if foreign.Code != http.StatusNotFound {
		t.Fatalf("foreign scope: status = %d, want 404; body = %s", foreign.Code, foreign.Body.String())
	}
}
