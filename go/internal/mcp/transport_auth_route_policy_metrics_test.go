// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// allScopeBearerResolver resolves every credential to the admin-equivalent
// bearer shape: AuthModeScoped, bound to one tenant and workspace, AllScopes
// set, no repository or scope ids. An ESHU_SCOPED_TOKENS_FILE entry with
// "all_scopes": true and an OIDC provider's all-scopes grant both produce it.
type allScopeBearerResolver struct{}

func (allScopeBearerResolver) ResolveScopedToken(context.Context, string) (query.AuthContext, bool, error) {
	return query.AuthContext{
		Mode:          query.AuthModeScoped,
		TenantID:      "tenant-a",
		WorkspaceID:   "workspace-a",
		SubjectIDHash: "sub-a",
		AllScopes:     true,
	}, true, nil
}

// routePolicyTestServer mirrors authedTestServer but states the route policy
// explicitly, which is what cmd/mcp-server's buildTransportAuthMiddleware
// threads from ESHU_GOVERNANCE_MODE. The zero-value policy is the
// hosted_multi_tenant posture: an all-scope bearer is refused on a grant-bound
// route, and GET /sse and POST /mcp/message are grant-bound by default because
// neither carries a scoped-route class.
func routePolicyTestServer(
	t *testing.T,
	resolver query.ScopedTokenResolver,
	policy query.BrowserSessionRoutePolicy,
) *Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/repositories", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"repos": []string{"test/repo"}})
	})
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	// See authedTestServer for why a non-empty shared key is required even
	// though these tests authenticate with a bearer: an empty shared token
	// opens the headerless dev bypass.
	const unusedSharedAPIKey = "test-shared-key-never-sent-by-these-tests"
	transportAuth := func(next http.Handler) http.Handler {
		return query.AuthMiddlewareWithScopedTokensAndRoutePolicy(unusedSharedAPIKey, resolver, next, policy)
	}
	return NewServer(mux, logger, WithTransportAuth(transportAuth))
}

// installTestMeterProvider points the global meter provider at a fresh
// Prometheus registry for one test and rebinds the lazily registered
// transport-auth-denied counter to it, returning the registry to scrape.
func installTestMeterProvider(t *testing.T) *prometheus.Registry {
	t.Helper()
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
	return registry
}

// TestMCPTransportRoutePolicyDenialIsNotCountedAsUnauthenticated is the PR
// #6497 review regression. Holding all-scope bearers to the browser-session
// route policy made hosted_multi_tenant refuse a VALID credential at the MCP
// handshake with a 403. authenticatedTransportHandler classified every unmarked
// 401/403 as reason="unauthenticated", so the refusal landed in the series an
// operator watches for credential-stuffing and catalog enumeration -- a
// governance mode working exactly as configured would have paged someone.
//
// Both transport endpoints are asserted because both are refused: GET /sse
// never establishes the session, and POST /mcp/message never reaches
// initialize or tools/list.
func TestMCPTransportRoutePolicyDenialIsNotCountedAsUnauthenticated(t *testing.T) {
	for _, tc := range []struct {
		name       string
		request    func() *http.Request
		wantMethod string
	}{
		{
			name: "sse handshake",
			request: func() *http.Request {
				req := httptest.NewRequest(http.MethodGet, "/sse", nil)
				req.Header.Set("Authorization", "Bearer all-scope-token")
				return req
			},
			wantMethod: "sse",
		},
		{
			name: "tools/list message",
			request: func() *http.Request {
				req := httptest.NewRequest(http.MethodPost, "/mcp/message",
					strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
				req.Header.Set("Authorization", "Bearer all-scope-token")
				return req
			},
			wantMethod: "tools/list",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Not parallel: installs a process-global meter provider.
			registry := installTestMeterProvider(t)

			// The hosted_multi_tenant posture, derived the way wireAPI derives
			// it rather than hand-built, so a change to that mapping breaks
			// this test instead of silently leaving it asserting nothing.
			policy := query.ScopedRoutePolicyForGovernanceMode(
				query.GovernanceStatusConfig{Mode: "hosted_multi_tenant"},
			)
			if policy.AllowTenantBoundAllScopes {
				t.Fatal("hosted_multi_tenant policy admits tenant-bound all-scopes callers; this test asserts the refusal path")
			}
			mux := fullHTTPMux(routePolicyTestServer(t, allScopeBearerResolver{}, policy))

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, tc.request())
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403; body = %s", rec.Code, rec.Body.String())
			}

			scrape := scrapeMCPMetrics(t, registry)
			if got := mcpDeniedSeriesValue(t, scrape, tc.wantMethod, "route_policy"); got != 1 {
				t.Fatalf("route_policy denial count for mcp_method=%q = %v, want 1\n--- scrape ---\n%s",
					tc.wantMethod, got, scrape)
			}
			// The credential resolved; nothing about this request failed
			// authentication. Any unauthenticated series at all is the defect.
			if total := totalUnauthenticatedDenials(scrape); total != 0 {
				t.Fatalf("unauthenticated denials for a policy refusal = %v, want 0\n--- scrape ---\n%s", total, scrape)
			}
		})
	}
}

// TestMCPTransportUnauthenticatedStillCountsAsUnauthenticated pins the other
// side of the split. Classifying the policy refusal separately must not
// reclassify a genuine authentication failure: a headerless tools/list under
// the SAME fail-closed policy still reports reason="unauthenticated" and
// records no route_policy series.
func TestMCPTransportUnauthenticatedStillCountsAsUnauthenticated(t *testing.T) {
	// Not parallel: installs a process-global meter provider.
	registry := installTestMeterProvider(t)

	policy := query.ScopedRoutePolicyForGovernanceMode(
		query.GovernanceStatusConfig{Mode: "hosted_multi_tenant"},
	)
	mux := fullHTTPMux(routePolicyTestServer(t, allScopeBearerResolver{}, policy))

	req := httptest.NewRequest(http.MethodPost, "/mcp/message",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("headerless tools/list status = %d, want 401; body = %s", rec.Code, rec.Body.String())
	}

	scrape := scrapeMCPMetrics(t, registry)
	if got := mcpDeniedSeriesValue(t, scrape, "tools/list", "unauthenticated"); got != 1 {
		t.Fatalf("unauthenticated denial count = %v, want 1\n--- scrape ---\n%s", got, scrape)
	}
	if got := mcpDeniedSeriesValue(t, scrape, "tools/list", "route_policy"); got != 0 {
		t.Fatalf("route_policy denial count for a headerless request = %v, want 0\n--- scrape ---\n%s", got, scrape)
	}
}
