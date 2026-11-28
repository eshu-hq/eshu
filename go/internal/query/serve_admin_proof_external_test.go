// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// externalProofReadRoutes are the two apilib-owned proof reads the core mux
// leaves unmounted when CoreServeDeps.AdminProofExternal is set, so the
// external apilib satellite host serves them without a duplicate mount.
var externalProofReadRoutes = []struct {
	method string
	path   string
}{
	{http.MethodGet, "/api/v0/admin/shared-projection/tuning-report"},
	{http.MethodPost, "/api/v0/admin/work-items/query"},
}

// probeRouteStatus issues a wrong-method request: a mounted pattern answers
// 405, an unmounted path answers 404 — without executing any handler.
func probeRouteStatus(mux *http.ServeMux, method, path string) int {
	wrong := http.MethodPost
	if method == http.MethodPost {
		wrong = http.MethodGet
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(wrong, path, nil))
	return rec.Code
}

func mountAdminRouter(t *testing.T, external bool) *http.ServeMux {
	t.Helper()
	router, err := query.BuildCoreRouter(query.CoreServeDeps{
		Profile:            query.ProfileProduction,
		AdminProofExternal: external,
	})
	if err != nil {
		t.Fatalf("BuildCoreRouter(AdminProofExternal=%v): %v", external, err)
	}
	if router.Admin == nil {
		t.Fatalf("BuildCoreRouter(AdminProofExternal=%v): Admin handler is nil", external)
	}
	if router.Admin.OmitProofReads != external {
		t.Fatalf("Admin.OmitProofReads = %v, want %v", router.Admin.OmitProofReads, external)
	}
	mux := http.NewServeMux()
	router.Admin.Mount(mux)
	return mux
}

func TestBuildCoreRouterAdminProofExternalUnmountsProofReads(t *testing.T) {
	t.Parallel()
	mux := mountAdminRouter(t, true)
	for _, route := range externalProofReadRoutes {
		if got := probeRouteStatus(mux, route.method, route.path); got != http.StatusNotFound {
			t.Errorf("external %s %s: status = %d, want 404 (unmounted)", route.method, route.path, got)
		}
	}
}

func TestBuildCoreRouterAdminProofExternalDefaultMountsProofReads(t *testing.T) {
	t.Parallel()
	mux := mountAdminRouter(t, false)
	for _, route := range externalProofReadRoutes {
		if got := probeRouteStatus(mux, route.method, route.path); got != http.StatusMethodNotAllowed {
			t.Errorf("default %s %s: status = %d, want 405 (mounted)", route.method, route.path, got)
		}
	}
}
