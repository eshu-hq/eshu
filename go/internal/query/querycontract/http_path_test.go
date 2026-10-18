// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// TestPathParamKeepsQualifiedServiceSelectorWhole proves a workload: selector
// forwarded whole as an HTTP path segment reaches the services handler
// intact: the ServeMux unescapes the segment and PathParam returns it whole,
// so the lane-1 handle ladder resolves it server-side (#5385 lane 2).
// url.PathEscape leaves ':' unescaped, so the wire form usually carries
// literal colons; an explicitly %3A-escaped form must decode the same way.
func TestPathParamKeepsQualifiedServiceSelectorWhole(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		target string
		want   string
	}{
		{"handle literal colons", "/api/v0/services/" + url.PathEscape("workload:checkout") + "/context", "workload:checkout"},
		{"handle explicit escape", "/api/v0/services/workload%3Acheckout/context", "workload:checkout"},
		{"full scoped id", "/api/v0/services/" + url.PathEscape("workload:repository:r_8477a002:checkout") + "/context", "workload:repository:r_8477a002:checkout"},
		{"bare name", "/api/v0/services/checkout/context", "checkout"},
	} {
		var got string
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/v0/services/{service_name}/context", func(w http.ResponseWriter, r *http.Request) {
			got = PathParam(r, "service_name")
		})
		req := httptest.NewRequest(http.MethodGet, tc.target, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want %d for target %q", tc.name, w.Code, http.StatusOK, tc.target)
		}
		if got != tc.want {
			t.Errorf("%s: PathParam(service_name) = %q, want %q", tc.name, got, tc.want)
		}
	}
}
