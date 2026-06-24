// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package prometheusmimir

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

func TestHTTPClientCollectsBoundedPrometheusTargetsAndRules(t *testing.T) {
	t.Parallel()

	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.String())
		switch r.URL.Path {
		case "/api/v1/targets":
			writeJSON(t, w, map[string]any{
				"status": "success",
				"data": map[string]any{
					"activeTargets": []map[string]any{{
						"discoveredLabels": map[string]any{
							"__address__":                "https://user:pass@10.0.0.1:9100/metrics",
							"__meta_kubernetes_pod_name": "checkout-abc",
						},
						"labels": map[string]any{
							"instance": "10.0.0.1:9100",
							"job":      "checkout",
							"pod":      "checkout-abc",
						},
						"scrapePool": "kubernetes-pods",
						"scrapeUrl":  "https://user:pass@10.0.0.1:9100/metrics",
						"health":     "up",
						"lastScrape": "2026-06-01T12:00:00Z",
					}},
				},
			})
		case "/api/v1/rules":
			writeJSON(t, w, map[string]any{
				"status": "success",
				"data": map[string]any{
					"groups": []map[string]any{{
						"name": "checkout.rules",
						"file": "rules/checkout.yaml",
						"rules": []map[string]any{{
							"name":           "HighLatency",
							"type":           "alerting",
							"health":         "ok",
							"query":          "histogram_quantile(0.95, rate(secret_bucket[5m]))",
							"lastEvaluation": "2026-06-01T12:00:05Z",
							"labels":         map[string]any{"service": "checkout", "severity": "page"},
							"annotations":    map[string]any{"summary": "Checkout latency is high"},
						}},
					}},
				},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	result, err := client.CollectObservedMetadata(context.Background(), TargetConfig{
		Provider:         ProviderPrometheus,
		ScopeID:          "prometheus:instance:prod",
		InstanceID:       "prometheus-prod",
		BaseURL:          server.URL,
		ResourceLimit:    50,
		StaleAfter:       time.Hour,
		DeclaredIDs:      map[string]struct{}{"checkout.rules:HighLatency": {}},
		ObservedOnlyHint: true,
	})
	if err != nil {
		t.Fatalf("CollectObservedMetadata() error = %v, want nil", err)
	}
	if got, want := len(result.Targets), 1; got != want {
		t.Fatalf("len(Targets) = %d, want %d", got, want)
	}
	if got, want := len(result.Rules), 1; got != want {
		t.Fatalf("len(Rules) = %d, want %d", got, want)
	}
	if got := result.Targets[0].ScrapeURL; got != "" {
		t.Fatalf("ScrapeURL = %q, want omitted", got)
	}
	if !result.Targets[0].ScrapeURLRedacted {
		t.Fatal("ScrapeURLRedacted = false, want true")
	}
	if got := result.Rules[0].DeclaredMatchState; got != MatchStateMatchedDeclared {
		t.Fatalf("DeclaredMatchState = %q, want %q", got, MatchStateMatchedDeclared)
	}
	if !result.Rules[0].QueryRedacted {
		t.Fatal("QueryRedacted = false, want true")
	}
	rendered := strings.ToLower(anyJSON(t, result))
	for _, forbidden := range []string{"10.0.0.1", "user:pass", "checkout-abc", "histogram_quantile", "secret_bucket", "checkout latency is high"} {
		if strings.Contains(rendered, strings.ToLower(forbidden)) {
			t.Fatalf("result leaked forbidden value %q: %s", forbidden, rendered)
		}
	}
	wantPaths := []string{"/api/v1/targets?state=active", "/api/v1/rules"}
	for i, want := range wantPaths {
		if requested[i] != want {
			t.Fatalf("requested[%d] = %q, want %q; all=%v", i, requested[i], want, requested)
		}
	}
}

func TestHTTPClientSendsMimirTenantHeaderWithoutPersistingTenant(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Scope-OrgID"); got != "tenant-secret-prod" {
			t.Fatalf("X-Scope-OrgID = %q, want tenant header", got)
		}
		switch r.URL.Path {
		case "/prometheus/api/v1/rules":
			writeJSON(t, w, map[string]any{
				"status": "success",
				"data": map[string]any{
					"groups": []map[string]any{{
						"name": "tenant.rules",
						"rules": []map[string]any{{
							"name":  "TenantRule",
							"type":  "recording",
							"query": "sum by (tenant) (sensitive_metric)",
						}},
					}},
				},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	result, err := client.CollectObservedMetadata(context.Background(), TargetConfig{
		Provider:      ProviderMimir,
		ScopeID:       "mimir:tenant:prod",
		InstanceID:    "mimir-prod",
		BaseURL:       server.URL,
		PathPrefix:    "/prometheus",
		TenantID:      "tenant-secret-prod",
		ResourceLimit: 50,
	})
	if err != nil {
		t.Fatalf("CollectObservedMetadata() error = %v, want nil", err)
	}
	if got, want := len(result.Targets), 0; got != want {
		t.Fatalf("len(Targets) = %d, want %d for Mimir", got, want)
	}
	if got, want := len(result.Rules), 1; got != want {
		t.Fatalf("len(Rules) = %d, want %d", got, want)
	}
	if !result.Source.TenantRedacted {
		t.Fatal("TenantRedacted = false, want tenant redaction")
	}
	rendered := strings.ToLower(anyJSON(t, result))
	for _, forbidden := range []string{"tenant-secret-prod", "sum by", "sensitive_metric"} {
		if strings.Contains(rendered, strings.ToLower(forbidden)) {
			t.Fatalf("result leaked forbidden value %q: %s", forbidden, rendered)
		}
	}
}

func TestHTTPClientRetriesRateLimitedTargets(t *testing.T) {
	t.Parallel()

	targetRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/targets":
			targetRequests++
			if targetRequests == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			writeJSON(t, w, map[string]any{"status": "success", "data": map[string]any{"activeTargets": []map[string]any{}}})
		case "/api/v1/rules":
			writeJSON(t, w, map[string]any{"status": "success", "data": map[string]any{"groups": []map[string]any{}}})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	result, err := client.CollectObservedMetadata(context.Background(), TargetConfig{
		Provider:      ProviderPrometheus,
		ScopeID:       "prometheus:instance:prod",
		InstanceID:    "prometheus-prod",
		BaseURL:       server.URL,
		ResourceLimit: 50,
	})
	if err != nil {
		t.Fatalf("CollectObservedMetadata() error = %v, want nil", err)
	}
	if got, want := targetRequests, 2; got != want {
		t.Fatalf("target requests = %d, want %d", got, want)
	}
	if got, want := result.Stats.Retries, 1; got != want {
		t.Fatalf("Retries = %d, want %d", got, want)
	}
	if got, want := result.Stats.RateLimits, 1; got != want {
		t.Fatalf("RateLimits = %d, want %d", got, want)
	}
}

func TestHTTPClientRejectsProviderErrorStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/targets":
			writeJSON(t, w, map[string]any{
				"status":    "error",
				"errorType": "bad_data",
				"error":     "secret_metric{token=\"do-not-persist\"}",
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	_, err = client.CollectObservedMetadata(context.Background(), TargetConfig{
		Provider:      ProviderPrometheus,
		ScopeID:       "prometheus:instance:prod",
		InstanceID:    "prometheus-prod",
		BaseURL:       server.URL,
		ResourceLimit: 50,
	})
	if err == nil {
		t.Fatal("CollectObservedMetadata() error = nil, want provider API error")
	}
	var providerErr ProviderAPIError
	if !errors.As(err, &providerErr) {
		t.Fatalf("CollectObservedMetadata() error = %T, want ProviderAPIError", err)
	}
	if got, want := providerErr.ErrorType, "bad_data"; got != want {
		t.Fatalf("ProviderAPIError.ErrorType = %q, want %q", got, want)
	}
	if strings.Contains(err.Error(), "do-not-persist") || strings.Contains(err.Error(), "secret_metric") {
		t.Fatalf("provider error leaked response body: %v", err)
	}
}

func TestHTTPClientReturnsSDKHTTPErrorForServerFailure(t *testing.T) {
	t.Parallel()

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if r.URL.Path != "/api/v1/targets" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("metric response body token-secret"))
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	result, err := client.CollectObservedMetadata(context.Background(), TargetConfig{
		Provider:      ProviderPrometheus,
		ScopeID:       "prometheus:instance:prod",
		InstanceID:    "prometheus-prod",
		BaseURL:       server.URL,
		ResourceLimit: 50,
	})
	if err == nil {
		t.Fatal("CollectObservedMetadata() error = nil, want server failure")
	}
	var httpErr sdk.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("CollectObservedMetadata() error = %T, want sdk.HTTPError", err)
	}
	if got, want := httpErr.StatusCode, http.StatusInternalServerError; got != want {
		t.Fatalf("StatusCode = %d, want %d", got, want)
	}
	if got, want := attempts, maxHTTPRetries+1; got != want {
		t.Fatalf("attempts = %d, want %d", got, want)
	}
	if got, want := result.Stats.Retries, maxHTTPRetries; got != want {
		t.Fatalf("Retries = %d, want %d", got, want)
	}
	if strings.Contains(err.Error(), "token-secret") {
		t.Fatalf("server failure leaked response body: %v", err)
	}
}

func TestNewHTTPClientRejectsUnsafeBaseURL(t *testing.T) {
	t.Parallel()

	_, err := NewHTTPClient(HTTPClientConfig{BaseURL: "https://user:pass@prometheus.example.internal"})
	if err == nil {
		t.Fatal("NewHTTPClient() error = nil, want credentials rejected")
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode json: %v", err)
	}
}

func anyJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return string(data)
}
