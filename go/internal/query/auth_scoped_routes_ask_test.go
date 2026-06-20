package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestScopedHTTPRoute_Ask verifies the scoped-token allowlist for the Ask Eshu
// endpoint: POST /api/v0/ask is permitted (its tenant scoping is enforced
// transitively by re-dispatching inner tool calls through this same gate), while
// a non-orchestration whole-graph route such as ecosystem/overview is not.
func TestScopedHTTPRoute_Ask(t *testing.T) {
	t.Parallel()

	cases := []struct {
		method string
		path   string
		want   bool
	}{
		{http.MethodPost, "/api/v0/ask", true},
		{http.MethodGet, "/api/v0/ask", false},                 // only POST is the ask endpoint
		{http.MethodGet, "/api/v0/ecosystem/overview", false},  // whole-graph, not allowlisted
		{http.MethodPost, "/api/v0/ecosystem/overview", false}, // not allowlisted under any method
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, nil)
		if got := scopedHTTPRouteSupportsTenantFilter(req); got != c.want {
			t.Errorf("%s %s: got %v, want %v", c.method, c.path, got, c.want)
		}
	}
}
