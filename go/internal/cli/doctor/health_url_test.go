// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package doctor

import "testing"

// TestHealthURLAppendsToThePathNotTheQuery is the regression screen for a bug
// that plain concatenation hides: `baseURL + "/health"` appends to whatever the
// string ends with, and for a query-bearing base URL that is the query VALUE.
//
// The package explicitly supports an operator-configured base URL carrying a
// token in its query string -- doctor_redaction_test.go asserts that shape is
// redacted -- so probing it correctly is part of the same contract. Getting it
// wrong is silent: the probe 404s or fails to connect and doctor reports the
// API as unreachable when it is fine.
func TestHealthURLAppendsToThePathNotTheQuery(t *testing.T) {
	for _, tc := range []struct {
		name string
		base string
		want string
	}{
		{
			name: "plain base url is unchanged in behaviour",
			base: "http://localhost:8080",
			want: "http://localhost:8080/health",
		},
		{
			name: "trailing slash does not double up",
			base: "http://localhost:8080/",
			want: "http://localhost:8080/health",
		},
		{
			name: "existing path is extended, not replaced",
			base: "http://localhost:8080/api",
			want: "http://localhost:8080/api/health",
		},
		{
			// The case concatenation gets wrong: it would produce
			// "http://host/x?api_key=t/health", putting the segment inside the
			// query value.
			name: "query survives and the segment lands on the path",
			base: "http://host/x?api_key=t",
			want: "http://host/x/health?api_key=t",
		},
		{
			name: "credential in userinfo is not disturbed by path handling",
			base: "http://user:pass@host:9000",
			want: "http://user:pass@host:9000/health",
		},
		{
			// No host means url.Parse gives us nothing useful to build on, so
			// the fallback keeps the old behaviour rather than inventing a URL.
			name: "unparseable base falls back to concatenation",
			base: "not a url",
			want: "not a url/health",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := healthURL(tc.base); got != tc.want {
				t.Fatalf("healthURL(%q) = %q, want %q", tc.base, got, tc.want)
			}
		})
	}
}
