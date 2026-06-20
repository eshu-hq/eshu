package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestMCPServerMountsAskRoute proves that POST /api/v0/ask is registered on
// the MCP server mux so the "ask" MCP tool dispatch never 404s. The
// AskHandler is default-off (nil Asker → 503 state:"unavailable"), so a
// 503 response here proves the handler was reached rather than the mux
// returning a generic 404 for an unmounted route.
func TestMCPServerMountsAskRoute(t *testing.T) {
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
	)
	mux := http.NewServeMux()
	router.Mount(mux)
	(&query.AskHandler{}).Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/ask",
		strings.NewReader(`{"question":"which repos are indexed?"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Fatal("POST /api/v0/ask returned 404 — AskHandler not mounted on the MCP mux")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (default-off handler proves the route was reached)", rec.Code)
	}
}
