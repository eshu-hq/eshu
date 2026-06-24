// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package grafana

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPClientCollectObservedMetadataUsesBoundedGrafanaEndpoints(t *testing.T) {
	t.Parallel()

	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer grafana-token" {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		requested = append(requested, r.URL.String())
		switch r.URL.Path {
		case "/api/search":
			if got := r.URL.Query().Get("limit"); got != "2" {
				t.Fatalf("search limit = %q, want 2", got)
			}
			if r.URL.Query().Get("page") == "2" {
				writeJSON(t, w, []map[string]any{{
					"uid":       "dash-checkout",
					"type":      "dash-db",
					"title":     "Checkout Latency",
					"folderUid": "folder-prod",
					"url":       "/d/dash-checkout?secret=hidden",
				}})
				return
			}
			writeJSON(t, w, []map[string]any{
				{
					"uid":       "folder-prod",
					"type":      "dash-folder",
					"title":     "Production",
					"url":       "/dashboards/f/folder-prod?token=secret",
					"isStarred": true,
				},
				{
					"uid":       "dash-checkout",
					"type":      "dash-db",
					"title":     "Checkout Latency",
					"folderUid": "folder-prod",
					"url":       "/d/dash-checkout?secret=hidden",
				},
			})
		case "/api/datasources":
			writeJSON(t, w, []map[string]any{{
				"uid":              "prometheus-prod",
				"name":             "prod-prometheus",
				"type":             "prometheus",
				"url":              "https://prometheus.example.internal",
				"basicAuth":        true,
				"secureJsonFields": map[string]any{"password": true},
				"jsonData":         map[string]any{"httpHeaderName1": "X-Scope-OrgID"},
			}})
		case "/api/v1/provisioning/alert-rules":
			writeJSON(t, w, []map[string]any{{
				"uid":           "rule-checkout-latency",
				"title":         "Checkout Latency",
				"ruleGroup":     "checkout.rules",
				"folderUID":     "folder-prod",
				"condition":     "B",
				"data":          []map[string]any{{"model": map[string]any{"expr": "up"}}},
				"updated":       "2026-06-01T12:05:00Z",
				"for":           "5m",
				"noDataState":   "OK",
				"execErrState":  "Error",
				"contactPoint":  "oncall@example.com",
				"notification":  "https://hooks.example.internal/secret",
				"datasourceUid": "prometheus-prod",
			}})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{
		BaseURL: server.URL,
		Token:   "grafana-token",
		Client:  server.Client(),
	})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	result, err := client.CollectObservedMetadata(context.Background(), TargetConfig{
		Provider:         ProviderGrafana,
		ScopeID:          "grafana:instance:prod",
		InstanceID:       "grafana-prod",
		BaseURL:          server.URL,
		Token:            "grafana-token",
		ResourceLimit:    2,
		DeclaredUIDs:     map[string]struct{}{"dash-checkout": {}},
		StaleAfter:       time.Hour,
		ObservedOnlyHint: true,
	})
	if err != nil {
		t.Fatalf("CollectObservedMetadata() error = %v, want nil", err)
	}
	if got, want := len(result.Resources), 3; got != want {
		t.Fatalf("len(Resources) = %d, want %d", got, want)
	}
	if got, want := len(result.Rules), 1; got != want {
		t.Fatalf("len(Rules) = %d, want %d", got, want)
	}
	if got, want := result.Stats.PagesFetched, 4; got != want {
		t.Fatalf("PagesFetched = %d, want %d", got, want)
	}
	if got := result.Resources[1].DeclaredMatchState; got != MatchStateMatchedDeclared {
		t.Fatalf("dashboard DeclaredMatchState = %q, want %q", got, MatchStateMatchedDeclared)
	}
	if got := result.Resources[2].URL; got != "" {
		t.Fatalf("datasource URL = %q, want omitted", got)
	}
	if !result.Resources[2].URLRedacted {
		t.Fatal("datasource URLRedacted = false, want true")
	}
	if !result.Rules[0].QueryModelRedacted {
		t.Fatal("rule QueryModelRedacted = false, want true")
	}
	if !result.Rules[0].ContactPointRedacted || !result.Rules[0].NotificationURLRedacted {
		t.Fatal("rule contact/notification redaction flags = false, want true")
	}
	rendered := strings.ToLower(anyJSON(t, result))
	for _, forbidden := range []string{"prometheus.example.internal", "oncall@example.com", "hooks.example.internal", "\"expr\":\"up\"", "secret=hidden"} {
		if strings.Contains(rendered, strings.ToLower(forbidden)) {
			t.Fatalf("result leaked forbidden value %q: %s", forbidden, rendered)
		}
	}
	wantPaths := []string{
		"/api/search?limit=2&page=1",
		"/api/search?limit=2&page=2",
		"/api/datasources",
		"/api/v1/provisioning/alert-rules",
	}
	for i, want := range wantPaths {
		if requested[i] != want {
			t.Fatalf("requested[%d] = %q, want %q; all=%v", i, requested[i], want, requested)
		}
	}
}

func TestHTTPClientConvertsPermissionAndRateLimitToWarnings(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search":
			http.Error(w, "hidden", http.StatusForbidden)
		case "/api/datasources":
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message":"rate limited"}`))
		case "/api/v1/provisioning/alert-rules":
			http.Error(w, "unsupported", http.StatusNotFound)
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "grafana-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	result, err := client.CollectObservedMetadata(context.Background(), TargetConfig{
		Provider:      ProviderGrafana,
		ScopeID:       "grafana:instance:prod",
		InstanceID:    "grafana-prod",
		BaseURL:       server.URL,
		Token:         "grafana-token",
		ResourceLimit: 50,
	})
	if err != nil {
		t.Fatalf("CollectObservedMetadata() error = %v, want nil partial result", err)
	}
	if !result.Stats.Partial {
		t.Fatal("Partial = false, want true")
	}
	assertWarningReason(t, result.Warnings, WarningPermissionHidden)
	assertWarningReason(t, result.Warnings, WarningRateLimited)
	assertWarningReason(t, result.Warnings, WarningUnsupported)
	if got, want := result.Stats.RateLimits, 3; got != want {
		t.Fatalf("RateLimits = %d, want %d", got, want)
	}
	if got, want := result.Stats.Retries, 2; got != want {
		t.Fatalf("Retries = %d, want %d", got, want)
	}
}

func TestHTTPClientRetriesRateLimitedSearchBeforeWarning(t *testing.T) {
	t.Parallel()

	searchRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search":
			searchRequests++
			if searchRequests == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			writeJSON(t, w, []map[string]any{})
		case "/api/datasources", "/api/v1/provisioning/alert-rules":
			writeJSON(t, w, []map[string]any{})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "grafana-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	result, err := client.CollectObservedMetadata(context.Background(), TargetConfig{
		Provider:      ProviderGrafana,
		ScopeID:       "grafana:instance:prod",
		InstanceID:    "grafana-prod",
		BaseURL:       server.URL,
		Token:         "grafana-token",
		ResourceLimit: 50,
	})
	if err != nil {
		t.Fatalf("CollectObservedMetadata() error = %v, want nil", err)
	}
	if got, want := searchRequests, 2; got != want {
		t.Fatalf("search requests = %d, want %d", got, want)
	}
	if got, want := result.Stats.Retries, 1; got != want {
		t.Fatalf("Retries = %d, want %d", got, want)
	}
	if got, want := result.Stats.RateLimits, 1; got != want {
		t.Fatalf("RateLimits = %d, want %d", got, want)
	}
	if result.Stats.Partial {
		t.Fatal("Partial = true, want successful retry to avoid partial warning")
	}
}

func TestHTTPClientMarksStaleAlertRules(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search", "/api/datasources":
			writeJSON(t, w, []map[string]any{})
		case "/api/v1/provisioning/alert-rules":
			writeJSON(t, w, []map[string]any{{
				"uid":       "rule-stale",
				"title":     "Stale Alert",
				"ruleGroup": "checkout.rules",
				"updated":   "2000-01-01T00:00:00Z",
			}})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "grafana-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	result, err := client.CollectObservedMetadata(context.Background(), TargetConfig{
		Provider:      ProviderGrafana,
		ScopeID:       "grafana:instance:prod",
		InstanceID:    "grafana-prod",
		BaseURL:       server.URL,
		Token:         "grafana-token",
		ResourceLimit: 50,
		StaleAfter:    time.Minute,
	})
	if err != nil {
		t.Fatalf("CollectObservedMetadata() error = %v, want nil", err)
	}
	if got, want := len(result.Rules), 1; got != want {
		t.Fatalf("len(Rules) = %d, want %d", got, want)
	}
	if got := result.Rules[0].FreshnessState; got != FreshnessStale {
		t.Fatalf("FreshnessState = %q, want %q", got, FreshnessStale)
	}
	if got := result.Rules[0].Outcome; got != OutcomeStale {
		t.Fatalf("Outcome = %q, want %q", got, OutcomeStale)
	}
	assertWarningReason(t, result.Warnings, WarningStale)
}

func TestNewHTTPClientRejectsUnsafeBaseURL(t *testing.T) {
	t.Parallel()

	_, err := NewHTTPClient(HTTPClientConfig{BaseURL: "https://user:pass@grafana.example.internal", Token: "token"})
	if err == nil {
		t.Fatal("NewHTTPClient() error = nil, want credentials rejected")
	}
}

func TestClassifiedProviderFailureMarksRateLimitRetryable(t *testing.T) {
	t.Parallel()

	err := GrafanaError{StatusCode: http.StatusTooManyRequests, Message: "slow down"}
	failure := classifiedProviderFailure(err)
	if failure.FailureClass() != FailureRateLimited {
		t.Fatalf("FailureClass() = %q, want %q", failure.FailureClass(), FailureRateLimited)
	}
	if !errors.Is(failure, err) {
		t.Fatalf("classifiedProviderFailure() does not wrap original error")
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

func assertWarningReason(t *testing.T, warnings []Warning, reason string) {
	t.Helper()
	for _, warning := range warnings {
		if warning.Reason == reason {
			return
		}
	}
	t.Fatalf("missing warning reason %q in %#v", reason, warnings)
}
