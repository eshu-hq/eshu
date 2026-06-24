// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestScopedCollectorExtractionReadinessRoute(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		method string
		path   string
		want   bool
	}{
		{"list", http.MethodGet, "/api/v0/collector-extraction-readiness", true},
		{"family", http.MethodGet, "/api/v0/collector-extraction-readiness/pagerduty", true},
		{"empty family", http.MethodGet, "/api/v0/collector-extraction-readiness/", false},
		{"nested family", http.MethodGet, "/api/v0/collector-extraction-readiness/a/b", false},
		{"wrong method", http.MethodPost, "/api/v0/collector-extraction-readiness", false},
		{"other route", http.MethodGet, "/api/v0/component-extensions", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if got := scopedCollectorExtractionReadinessRoute(req); got != tc.want {
				t.Fatalf("scopedCollectorExtractionReadinessRoute(%s %s) = %v, want %v", tc.method, tc.path, got, tc.want)
			}
		})
	}
}

func TestScopedTenantFilterAllowsCollectorExtractionReadiness(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/api/v0/collector-extraction-readiness",
		"/api/v0/collector-extraction-readiness/git",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if !scopedHTTPRouteSupportsTenantFilter(req) {
			t.Fatalf("scopedHTTPRouteSupportsTenantFilter(GET %s) = false, want true so scoped tokens are not permission_denied", path)
		}
	}
}
