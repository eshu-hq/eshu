// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	apilibadmin "github.com/eshu-hq/eshu/go/apilib/admin"
)

// proofReadOmittedRoutes are the two routes the apilib extraction owns: with
// OmitProofReads they stay unmounted for the external satellite host.
var proofReadOmittedRoutes = []struct {
	method string
	path   string
}{
	{"GET", "/api/v0/admin/shared-projection/tuning-report"},
	{"POST", "/api/v0/admin/work-items/query"},
}

// stayingAdminRoutes are the admin routes that mount regardless of
// OmitProofReads.
var stayingAdminRoutes = []struct {
	method string
	path   string
}{
	{"POST", "/api/v0/admin/refinalize"},
	{"POST", "/api/v0/admin/reindex"},
	{"GET", "/api/v0/admin/delta-union/completeness"},
	{"GET", "/api/v0/admin/reindex/progress"},
	{"POST", "/api/v0/admin/recover-generations"},
	{"POST", "/api/v0/admin/dead-letters/query"},
	{"POST", "/api/v0/admin/input-invalid-facts/query"},
	{"POST", "/api/v0/admin/changed-since/poisoned-links/query"},
	{"POST", "/api/v0/admin/decisions/query"},
	{"POST", "/api/v0/admin/dead-letter"},
	{"POST", "/api/v0/admin/skip"},
	{"POST", "/api/v0/admin/workflow-retire-unclaimed"},
	{"POST", "/api/v0/admin/replay"},
	{"POST", "/api/v0/admin/reopen"},
	{"POST", "/api/v0/admin/backfill"},
	{"POST", "/api/v0/admin/replay-events/query"},
}

// oppositeMethod probes route registration without executing a handler: a
// registered pattern with the wrong method answers 405, an unmounted one
// 404.
func oppositeMethod(method string) string {
	if method == http.MethodGet {
		return http.MethodPost
	}
	return http.MethodGet
}

// mountedStatus returns the mux status for a wrong-method probe of path.
func mountedStatus(t *testing.T, mux *http.ServeMux, method, path string) int {
	t.Helper()
	req := httptest.NewRequest(oppositeMethod(method), path, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code
}

func TestMountOmitProofReadsLeavesOnlyProofReadsUnmounted(t *testing.T) {
	mux := http.NewServeMux()
	(&Handler{OmitProofReads: true}).Mount(mux)
	for _, route := range proofReadOmittedRoutes {
		if got := mountedStatus(t, mux, route.method, route.path); got != http.StatusNotFound {
			t.Errorf("OmitProofReads %s %s: want 404, got %d", route.method, route.path, got)
		}
	}
	for _, route := range stayingAdminRoutes {
		if got := mountedStatus(t, mux, route.method, route.path); got != http.StatusMethodNotAllowed {
			t.Errorf("OmitProofReads %s %s: want 405 (still mounted), got %d", route.method, route.path, got)
		}
	}
}

func TestMountDefaultKeepsProofReadsMounted(t *testing.T) {
	mux := http.NewServeMux()
	(&Handler{}).Mount(mux)
	for _, route := range proofReadOmittedRoutes {
		if got := mountedStatus(t, mux, route.method, route.path); got != http.StatusMethodNotAllowed {
			t.Errorf("default Mount %s %s: want 405 (mounted), got %d", route.method, route.path, got)
		}
	}
}

func TestTuningReportMatchesSatelliteBytes(t *testing.T) {
	core := httptest.NewRecorder()
	(&Handler{}).tuningReport(core, httptest.NewRequest(http.MethodGet, "/api/v0/admin/shared-projection/tuning-report", nil))
	satellite := httptest.NewRecorder()
	apilibadmin.WriteTuningReport(satellite, httptest.NewRequest(http.MethodGet, "/api/v0/admin/shared-projection/tuning-report", nil))
	if core.Code != http.StatusOK || satellite.Code != http.StatusOK {
		t.Fatalf("tuning report: want both 200, got core %d satellite %d", core.Code, satellite.Code)
	}
	if core.Body.String() != satellite.Body.String() {
		t.Fatalf("tuning report drift:\n core %q\n satellite %q", core.Body.String(), satellite.Body.String())
	}
}
