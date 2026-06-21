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
// the MCP server mux via mountAskAndNarration so the "ask" MCP tool dispatch
// never 404s. The handler is default-off (nil Asker → 503 state:"unavailable")
// when ESHU_ASK_ENABLED is unset, so a 503 response proves the handler was
// reached rather than the mux returning a generic 404 for an unmounted route.
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
		false,
	)
	mux := http.NewServeMux()
	router.Mount(mux)
	// Wire the engine through mountAskAndNarration — the real production path.
	// With no env vars set, ESHU_ASK_ENABLED defaults to false so the handler
	// is default-off (nil Asker).
	mountAskAndNarration(func(string) string { return "" }, mux, "", router.Status, nil)

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

// TestMCPServerAskDefaultOffNoProfileConfigured proves that even when
// ESHU_ASK_ENABLED=true but no agent_reasoning profile is configured, the MCP
// server ask route remains default-off (503, not a crash or 404).
func TestMCPServerAskDefaultOffNoProfileConfigured(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mountAskAndNarration(
		func(key string) string {
			if key == "ESHU_ASK_ENABLED" {
				return "true"
			}
			return ""
		},
		mux,
		"",
		nil,
		nil,
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/ask",
		strings.NewReader(`{"question":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Fatal("POST /api/v0/ask returned 404; route not registered")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (default-off: no agent_reasoning profile)", rec.Code)
	}
}

// TestMCPServerAskResponseBodyContainsUnavailableState proves the default-off
// 503 response body carries state:"unavailable", matching the MCP tool
// contract that the tool reads to report capability.
func TestMCPServerAskResponseBodyContainsUnavailableState(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mountAskAndNarration(func(string) string { return "" }, mux, "", nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/ask",
		strings.NewReader(`{"question":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "unavailable") {
		t.Fatalf("response body %q does not contain %q; MCP tool cannot determine availability", body, "unavailable")
	}
}
