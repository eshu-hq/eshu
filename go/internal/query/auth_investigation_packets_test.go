package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddlewareWithScopedTokensAllowsInvestigationPacketRoutes(t *testing.T) {
	t.Parallel()

	allowed := []string{
		"/api/v0/investigations/supply-chain/impact/packet?finding_id=finding-1&repository_id=repo-team-a",
		"/api/v0/investigations/deployable-unit/packet?scope_id=repo-team-a&generation_id=generation-1",
	}
	for _, target := range allowed {
		target := target
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, target, nil)
			if !scopedHTTPRouteSupportsTenantFilter(req) {
				t.Fatalf("scopedHTTPRouteSupportsTenantFilter(GET %s) = false, want true", target)
			}
		})
	}

	drift := httptest.NewRequest(http.MethodGet, "/api/v0/investigations/drift/packet?scope_id=account-1", nil)
	if scopedHTTPRouteSupportsTenantFilter(drift) {
		t.Fatal("scopedHTTPRouteSupportsTenantFilter(GET drift packet) = true, want false until drift rows have repository-grant proof")
	}
}
