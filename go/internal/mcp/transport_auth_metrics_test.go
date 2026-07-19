// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// resetMCPAuthDeniedMetricsForTest rebinds the lazily registered
// transport-auth-denied counter so a test can register it against its own
// meter provider regardless of test ordering (mirrors
// query.resetAPIRequestMetricsForTest).
func resetMCPAuthDeniedMetricsForTest() {
	mcpAuthDeniedOnce = sync.Once{}
	mcpAuthDeniedCounter = nil
}

// TestMCPTransportAuthDeniedCounterEmitsLabeledSeries proves
// eshu_dp_mcp_transport_auth_denied_total actually increments with the
// correct {mcp_method,reason} labels on both denial paths (issue #5168 review
// P2): an unauthenticated POST /mcp/message and a cross-principal SSE
// session-hijack. verify-telemetry-coverage only proves the metric is
// registered; this proves it is emitted.
func TestMCPTransportAuthDeniedCounterEmitsLabeledSeries(t *testing.T) {
	// Not parallel: installs a process-global meter provider.
	registry := prometheus.NewRegistry()
	exporter, err := otelprom.New(otelprom.WithRegisterer(registry))
	if err != nil {
		t.Fatalf("otelprom.New() error = %v", err)
	}
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })

	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() { otel.SetMeterProvider(previous) })
	resetMCPAuthDeniedMetricsForTest()
	t.Cleanup(resetMCPAuthDeniedMetricsForTest)

	resolver := &fakeScopedTokenResolver{byCredential: map[string]query.AuthContext{
		"token-a": {Mode: query.AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "ws-a", SubjectIDHash: "sub-a"},
		"token-b": {Mode: query.AuthModeScoped, TenantID: "tenant-b", WorkspaceID: "ws-b", SubjectIDHash: "sub-b"},
	}}
	s := authedTestServer(t, resolver)
	mux := fullHTTPMux(s)

	// Deny path 1: unauthenticated tools/list -> {mcp_method="tools/list", reason="unauthenticated"}.
	unauthReq := httptest.NewRequest(http.MethodPost, "/mcp/message", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	unauthRec := httptest.NewRecorder()
	mux.ServeHTTP(unauthRec, unauthReq)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated tools/list status = %d, want 401", unauthRec.Code)
	}

	// Deny path 2: credential B posts to credential A's session ->
	// {mcp_method="mcp_message", reason="session_principal_mismatch"}.
	sessionID := openSSESession(t, mux, "token-a")
	hijackReq := httptest.NewRequest(http.MethodPost, "/mcp/message?sessionId="+sessionID, strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"ping"}`))
	hijackReq.Header.Set("Authorization", "Bearer token-b")
	hijackRec := httptest.NewRecorder()
	mux.ServeHTTP(hijackRec, hijackReq)
	if hijackRec.Code != http.StatusForbidden {
		t.Fatalf("session-hijack status = %d, want 403", hijackRec.Code)
	}

	scrape := scrapeMCPMetrics(t, registry)
	for _, want := range []string{
		`eshu_dp_mcp_transport_auth_denied_total{`,
		`mcp_method="tools/list"`,
		`reason="unauthenticated"`,
		`mcp_method="mcp_message"`,
		`reason="session_principal_mismatch"`,
	} {
		if !strings.Contains(scrape, want) {
			t.Fatalf("scrape missing %q\n--- scrape ---\n%s", want, scrape)
		}
	}
}

func scrapeMCPMetrics(t *testing.T, registry *prometheus.Registry) string {
	t.Helper()
	rec := httptest.NewRecorder()
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("scrape status = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}
