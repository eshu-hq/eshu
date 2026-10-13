// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package loki

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

func TestHTTPClientCollectsBoundedLokiMetadata(t *testing.T) {
	t.Parallel()

	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.String())
		if got := r.Header.Get("X-Scope-OrgID"); got != "tenant-secret-prod" {
			t.Fatalf("X-Scope-OrgID = %q, want tenant header", got)
		}
		switch r.URL.Path {
		case "/loki/api/v1/labels":
			writeJSON(t, w, map[string]any{
				"status": "success",
				"data":   []string{"app", "namespace", "trace_id"},
			})
		case "/loki/api/v1/label/app/values":
			writeJSON(t, w, map[string]any{
				"status": "success",
				"data":   []string{"checkout-prod", "billing-prod"},
			})
		case "/loki/api/v1/label/trace_id/values":
			writeJSON(t, w, map[string]any{
				"status": "success",
				"data":   []string{"trace-1", "trace-2", "trace-3"},
			})
		case "/loki/api/v1/series":
			writeJSON(t, w, map[string]any{
				"status": "success",
				"data": []map[string]string{{
					"app":       "checkout-prod",
					"namespace": "payments",
					"trace_id":  "trace-123",
				}},
			})
		case "/loki/api/v1/rules":
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write([]byte(`prod:
- name: checkout.rules
  rules:
  - alert: HighLogErrors
    expr: sum(rate({app="checkout-prod"} |= "payment failed" [5m])) > 0
    labels:
      severity: page
    annotations:
      summary: Checkout errors
`))
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
		ScopeID:                "loki:tenant:prod",
		InstanceID:             "loki-prod",
		BaseURL:                server.URL,
		TenantID:               "tenant-secret-prod",
		ResourceLimit:          50,
		LabelValueNames:        []string{"app", "trace_id"},
		MaxLabelValuesPerLabel: 2,
		SeriesMatchers:         []string{`{app=~".+"}`},
		DeclaredIDs:            map[string]struct{}{"prod/checkout.rules:HighLogErrors": {}},
		ObservedOnlyHint:       true,
	})
	if err != nil {
		t.Fatalf("CollectObservedMetadata() error = %v, want nil", err)
	}
	if got, want := len(result.Signals), 2; got != want {
		t.Fatalf("len(Signals) = %d, want %d", got, want)
	}
	if got, want := len(result.Rules), 1; got != want {
		t.Fatalf("len(Rules) = %d, want %d", got, want)
	}
	if got, want := len(result.Warnings), 1; got != want {
		t.Fatalf("len(Warnings) = %d, want %d", got, want)
	}
	if got, want := result.Warnings[0].Reason, WarningHighCardinality; got != want {
		t.Fatalf("warning reason = %q, want %q", got, want)
	}
	if got, want := result.Rules[0].DeclaredMatchState, MatchStateMatchedDeclared; got != want {
		t.Fatalf("DeclaredMatchState = %q, want %q", got, want)
	}
	if !result.Source.TenantRedacted {
		t.Fatal("TenantRedacted = false, want tenant redaction")
	}
	rendered := strings.ToLower(anyJSON(t, result))
	for _, forbidden := range []string{
		"tenant-secret-prod",
		"checkout-prod",
		"billing-prod",
		"trace-123",
		"sum(rate",
		"payment failed",
		"checkout errors",
	} {
		if strings.Contains(rendered, strings.ToLower(forbidden)) {
			t.Fatalf("result leaked forbidden value %q: %s", forbidden, rendered)
		}
	}
	wantPaths := []string{
		"/loki/api/v1/labels",
		"/loki/api/v1/label/app/values",
		"/loki/api/v1/label/trace_id/values",
		"/loki/api/v1/series",
		"/loki/api/v1/rules",
	}
	for i, want := range wantPaths {
		if !strings.HasPrefix(requested[i], want) {
			t.Fatalf("requested[%d] = %q, want prefix %q; all=%v", i, requested[i], want, requested)
		}
	}
}

func TestHTTPClientRetriesRateLimitedLabels(t *testing.T) {
	t.Parallel()

	labelRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loki/api/v1/labels":
			labelRequests++
			if labelRequests == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			writeJSON(t, w, map[string]any{"status": "success", "data": []string{"app"}})
		case "/loki/api/v1/series":
			writeJSON(t, w, map[string]any{"status": "success", "data": []map[string]string{}})
		case "/loki/api/v1/rules":
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write([]byte("{}\n"))
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
		ScopeID:       "loki:tenant:prod",
		InstanceID:    "loki-prod",
		BaseURL:       server.URL,
		ResourceLimit: 50,
	})
	if err != nil {
		t.Fatalf("CollectObservedMetadata() error = %v, want nil", err)
	}
	if got, want := labelRequests, 2; got != want {
		t.Fatalf("label requests = %d, want %d", got, want)
	}
	if got, want := result.Stats.Retries, 1; got != want {
		t.Fatalf("Retries = %d, want %d", got, want)
	}
	if got, want := result.Stats.RateLimits, 1; got != want {
		t.Fatalf("RateLimits = %d, want %d", got, want)
	}
}

func TestHTTPClientDeduplicatesRepeatedSeriesAndRules(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loki/api/v1/labels":
			writeJSON(t, w, map[string]any{"status": "success", "data": []string{}})
		case "/loki/api/v1/series":
			writeJSON(t, w, map[string]any{
				"status": "success",
				"data": []map[string]string{
					{"app": "checkout-prod", "namespace": "payments"},
					{"namespace": "payments", "app": "checkout-prod"},
				},
			})
		case "/loki/api/v1/rules":
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write([]byte(`prod:
- name: checkout.rules
  rules:
  - alert: HighLogErrors
    expr: sum(rate({app="checkout-prod"} |= "payment failed" [5m])) > 0
  - alert: HighLogErrors
    expr: sum(rate({app="checkout-prod"} |= "payment failed" [5m])) > 0
`))
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
		ScopeID:       "loki:tenant:prod",
		InstanceID:    "loki-prod",
		BaseURL:       server.URL,
		ResourceLimit: 50,
		SeriesMatchers: []string{
			`{app=~".+"}`,
		},
	})
	if err != nil {
		t.Fatalf("CollectObservedMetadata() error = %v, want nil", err)
	}
	if got, want := len(result.Signals), 1; got != want {
		t.Fatalf("len(Signals) = %d, want duplicate series collapsed to %d", got, want)
	}
	if got, want := len(result.Rules), 1; got != want {
		t.Fatalf("len(Rules) = %d, want duplicate rules collapsed to %d", got, want)
	}
}

func TestHTTPClientRejectsProviderErrorStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loki/api/v1/labels":
			writeJSON(t, w, map[string]any{
				"status":    "error",
				"errorType": "bad_data",
				"error":     "{app=\"checkout-prod\"} |= \"payment failed\"",
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
		ScopeID:       "loki:tenant:prod",
		InstanceID:    "loki-prod",
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
	if strings.Contains(err.Error(), "checkout-prod") || strings.Contains(err.Error(), "payment failed") {
		t.Fatalf("provider error leaked response body: %v", err)
	}
}

func TestHTTPClientReturnsSDKHTTPErrorForServerFailure(t *testing.T) {
	t.Parallel()

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if r.URL.Path != "/loki/api/v1/labels" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("loki response body token-secret"))
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	result, err := client.CollectObservedMetadata(context.Background(), TargetConfig{
		ScopeID:       "loki:tenant:prod",
		InstanceID:    "loki-prod",
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

	_, err := NewHTTPClient(HTTPClientConfig{BaseURL: "https://user:pass@loki.example.internal"})
	if err == nil {
		t.Fatal("NewHTTPClient() error = nil, want credentials rejected")
	}
}

func TestTimeQueryUsesNanosecondEpoch(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 12, 0, 0, 123, time.UTC)
	query := timeQuery(observedAt)
	if got, want := query.Get("end"), fmt.Sprintf("%d", observedAt.UnixNano()); got != want {
		t.Fatalf("end = %q, want nanosecond epoch %q", got, want)
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
