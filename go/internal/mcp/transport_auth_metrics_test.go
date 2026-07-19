// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"net/http"
	"net/http/httptest"
	"strconv"
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
	// Exact-count assertions (not substring Contains): the earlier Contains
	// form let a double-count hide, because it only checked a series was
	// present, not its value (issue #5168 review P2). The unauthenticated
	// tools/list denial and the session-hijack mismatch must each be exactly 1.
	if got := mcpDeniedSeriesValue(t, scrape, "tools/list", "unauthenticated"); got != 1 {
		t.Fatalf("unauthenticated tools/list denial count = %v, want 1\n--- scrape ---\n%s", got, scrape)
	}
	if got := mcpDeniedSeriesValue(t, scrape, "mcp_message", "session_principal_mismatch"); got != 1 {
		t.Fatalf("session-hijack mismatch count = %v, want 1\n--- scrape ---\n%s", got, scrape)
	}
}

// TestMCPTransportAuthHijackCountsOnlyMismatchNotUnauthenticated is the issue
// #5168 review P2 regression: a session hijack (valid credential B posting to
// credential A's session) must increment ONLY {reason="session_principal_mismatch"}
// exactly once, and MUST NOT also inflate a {reason="unauthenticated"} series.
// Before the fix, handleHTTPMessage recorded the mismatch AND
// authenticatedTransportHandler's status recorder observed the same 403 and
// recorded a second, misleading "unauthenticated" denial (labeled by the peeked
// method "ping"). This test does a PURE hijack (no unauthenticated request) so
// any unauthenticated series is proof of the double-count.
func TestMCPTransportAuthHijackCountsOnlyMismatchNotUnauthenticated(t *testing.T) {
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

	sessionID := openSSESession(t, mux, "token-a")
	hijackReq := httptest.NewRequest(http.MethodPost, "/mcp/message?sessionId="+sessionID, strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"ping"}`))
	hijackReq.Header.Set("Authorization", "Bearer token-b")
	hijackRec := httptest.NewRecorder()
	mux.ServeHTTP(hijackRec, hijackReq)
	if hijackRec.Code != http.StatusForbidden {
		t.Fatalf("session-hijack status = %d, want 403", hijackRec.Code)
	}

	scrape := scrapeMCPMetrics(t, registry)
	if got := mcpDeniedSeriesValue(t, scrape, "mcp_message", "session_principal_mismatch"); got != 1 {
		t.Fatalf("session_principal_mismatch count = %v, want exactly 1\n--- scrape ---\n%s", got, scrape)
	}
	// The credential is valid, so it passes transport auth; the only denial is
	// the principal mismatch. No unauthenticated series may exist for any
	// method. "ping" is the method the wrapper peeks from the hijack body, so
	// it is the specific false series the old double-count produced.
	if got := mcpDeniedSeriesValue(t, scrape, "ping", "unauthenticated"); got != 0 {
		t.Fatalf("unauthenticated denial count for a pure hijack = %v, want 0 (double-count regression)\n--- scrape ---\n%s", got, scrape)
	}
	if total := totalUnauthenticatedDenials(scrape); total != 0 {
		t.Fatalf("total unauthenticated denials for a pure hijack = %v, want 0\n--- scrape ---\n%s", total, scrape)
	}
}

// mcpDeniedSeriesValue returns the value of the
// eshu_dp_mcp_transport_auth_denied_total series whose labels include the given
// mcp_method and reason, or 0 when no such series is present in the scrape.
func mcpDeniedSeriesValue(t *testing.T, scrape, method, reason string) float64 {
	t.Helper()
	for _, line := range strings.Split(scrape, "\n") {
		if !strings.HasPrefix(line, "eshu_dp_mcp_transport_auth_denied_total{") {
			continue
		}
		if !strings.Contains(line, `mcp_method="`+method+`"`) || !strings.Contains(line, `reason="`+reason+`"`) {
			continue
		}
		return parsePromLineValue(t, line)
	}
	return 0
}

// totalUnauthenticatedDenials sums every eshu_dp_mcp_transport_auth_denied_total
// series with reason="unauthenticated" across all method labels.
func totalUnauthenticatedDenials(scrape string) float64 {
	var total float64
	for _, line := range strings.Split(scrape, "\n") {
		if !strings.HasPrefix(line, "eshu_dp_mcp_transport_auth_denied_total{") {
			continue
		}
		if !strings.Contains(line, `reason="unauthenticated"`) {
			continue
		}
		if v, err := strconv.ParseFloat(strings.TrimSpace(line[strings.LastIndex(line, "}")+1:]), 64); err == nil {
			total += v
		}
	}
	return total
}

func parsePromLineValue(t *testing.T, line string) float64 {
	t.Helper()
	value := strings.TrimSpace(line[strings.LastIndex(line, "}")+1:])
	v, err := strconv.ParseFloat(value, 64)
	if err != nil {
		t.Fatalf("parse prometheus value from %q: %v", line, err)
	}
	return v
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
