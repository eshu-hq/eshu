package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/serviceintelhttp"
)

// TestMCPServerMountsServiceIntelligenceReportRoute proves the report route is
// reachable when mounted the way wireAPI mounts it (router + ReportHandler on the
// same mux), so the get_service_intelligence_report MCP tool does not dispatch to
// a missing route. The lightweight profile makes the handler return 501
// (unsupported capability), which is distinct from a mux 404 for an unmounted
// route — so a 501 here proves the handler was reached.
func TestMCPServerMountsServiceIntelligenceReportRoute(t *testing.T) {
	t.Parallel()

	router := newMCPQueryRouter(
		nil,
		nil,
		nil,
		staticStatusReader{},
		query.ProfileLocalLightweight,
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
	mux := http.NewServeMux()
	router.Mount(mux)
	(&serviceintelhttp.ReportHandler{Entities: router.Entities}).Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/checkout/intelligence-report", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Fatalf("report route returned 404 — handler not mounted on the MCP mux")
	}
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501 (unsupported capability proves the handler was reached)", rec.Code)
	}
}
