package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestNewMCPQueryRouterServesCollectorExtractionReadiness guards against the
// regression where the MCP server router advertised the
// list_/get_collector_extraction_readiness tools but never mounted their API
// routes, so MCP deployments returned 404. The handler is static policy data and
// needs no datastore, so nil deps are sufficient.
func TestNewMCPQueryRouterServesCollectorExtractionReadiness(t *testing.T) {
	t.Parallel()

	router := newMCPQueryRouter(
		nil,
		nil,
		nil,
		staticStatusReader{},
		query.ProfileProduction,
		query.GraphBackendNornicDB,
		nil,
		nil,
		"",
		"",
		component.Policy{},
		query.GovernanceStatusConfig{},
		nil,
		false,
	)
	if router.ExtractionReadiness == nil {
		t.Fatal("newMCPQueryRouter().ExtractionReadiness = nil, want mounted handler")
	}

	mux := http.NewServeMux()
	router.Mount(mux)

	for _, path := range []string{
		"/api/v0/collector-extraction-readiness",
		"/api/v0/collector-extraction-readiness/pagerduty",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s through MCP router = %d, want 200 (route must be mounted)", path, rec.Code)
		}
	}
}
