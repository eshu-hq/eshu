// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestScopedServiceIntelligenceReportRoute(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"report", "/api/v0/services/checkout/intelligence-report", true},
		{"empty service", "/api/v0/services//intelligence-report", false},
		{"nested service", "/api/v0/services/a/b/intelligence-report", false},
		{"story not report", "/api/v0/services/checkout/story", false},
		{"prefix only", "/api/v0/services/intelligence-report", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := scopedServiceIntelligenceReportRoute(tc.path); got != tc.want {
				t.Fatalf("scopedServiceIntelligenceReportRoute(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestScopedTenantFilterAllowsServiceIntelligenceReport(t *testing.T) {
	t.Parallel()
	// A scoped token must reach the report route (and be tenant-filtered by the
	// seam), not be rejected with permission_denied at the middleware gate.
	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/checkout/intelligence-report", nil)
	if !scopedHTTPRouteSupportsTenantFilter(req) {
		t.Fatal("scopedHTTPRouteSupportsTenantFilter(GET intelligence-report) = false, want true")
	}
	// A non-GET must not be tenant-filter eligible.
	post := httptest.NewRequest(http.MethodPost, "/api/v0/services/checkout/intelligence-report", nil)
	if scopedHTTPRouteSupportsTenantFilter(post) {
		t.Fatal("scopedHTTPRouteSupportsTenantFilter(POST intelligence-report) = true, want false")
	}
}
