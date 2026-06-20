package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddlewareWithScopedTokensAllowsFactSchemaVersionRoutes(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/api/v0/fact-schema-versions",
		"/api/v0/fact-schema-versions/repository",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if !scopedHTTPRouteSupportsTenantFilter(req) {
			t.Fatalf("scopedHTTPRouteSupportsTenantFilter(GET %s) = false, want true so scoped tokens are not permission_denied", path)
		}
	}
}
