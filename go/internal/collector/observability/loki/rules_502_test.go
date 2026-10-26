// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package loki

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

// TestCollectRules502DegradesToUnsupportedWarning proves eshu-hq/eshu#8860:
// the ruler-backed GET /loki/api/v1/rules answers 502 Bad Gateway on
// deployments without a ruler (the gateway has no ruler backend to proxy),
// so a 502 from that endpoint alone must degrade to an unsupported-rule
// warning -- identical to the 404/405 path -- instead of failing the whole
// collection. Labels/series behavior is unchanged.
func TestCollectRules502DegradesToUnsupportedWarning(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loki/api/v1/labels":
			writeJSON(t, w, map[string]any{"status": "success", "data": []string{"app"}})
		case "/loki/api/v1/series":
			writeJSON(t, w, map[string]any{
				"status": "success",
				"data": []map[string]string{{
					"app":       "checkout",
					"namespace": "payments",
				}},
			})
		case "/loki/api/v1/rules":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("Bad Gateway: no ruler backend"))
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	result, err := client.CollectObservedMetadata(t.Context(), TargetConfig{
		ScopeID:       "loki:tenant:prod",
		InstanceID:    "loki-prod",
		BaseURL:       server.URL,
		ResourceLimit: 50,
	})
	if err != nil {
		t.Fatalf("CollectObservedMetadata() error = %v, want nil (rules 502 must degrade, not fail)", err)
	}
	var httpErr sdk.HTTPError
	if errors.As(err, &httpErr) {
		t.Fatalf("CollectObservedMetadata() error = %v, want no ProviderHTTPError", err)
	}
	if got := len(result.Rules); got != 0 {
		t.Fatalf("len(Rules) = %d, want 0 (502 carries no rule payload)", got)
	}
	if got := len(result.Warnings); got != 1 {
		t.Fatalf("len(Warnings) = %d, want exactly 1 unsupported-rule warning, got %+v", got, result.Warnings)
	}
	if got := result.Warnings[0].ResourceClass; got != ResourceClassRule {
		t.Fatalf("warning ResourceClass = %q, want %q", got, ResourceClassRule)
	}
	if got := result.Warnings[0].Reason; got != WarningUnsupported {
		t.Fatalf("warning Reason = %q, want %q", got, WarningUnsupported)
	}
	if !result.Stats.Partial {
		t.Fatal("Stats.Partial = false, want true (rules coverage is partial)")
	}
	if got := len(result.Signals); got != 2 {
		t.Fatalf("len(Signals) = %d, want 2 retained label-set and series signals", got)
	}
	if got, want := result.Stats.PagesFetched, 2; got != want {
		t.Fatalf("Stats.PagesFetched = %d, want %d (labels + series only; rules 502 takes the 404/405 path with no increment)", got, want)
	}
}

// TestCollectRules500StillFails pins the ruling scope: only the
// ruler-absent 502 degrades. Any other rules failure (here 500) still fails
// hard with a bounded sdk.HTTPError.
func TestCollectRules500StillFails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loki/api/v1/labels":
			writeJSON(t, w, map[string]any{"status": "success", "data": []string{"app"}})
		case "/loki/api/v1/series":
			writeJSON(t, w, map[string]any{"status": "success", "data": []map[string]string{}})
		case "/loki/api/v1/rules":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("ruler exploded"))
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	_, err = client.CollectObservedMetadata(t.Context(), TargetConfig{
		ScopeID:       "loki:tenant:prod",
		InstanceID:    "loki-prod",
		BaseURL:       server.URL,
		ResourceLimit: 50,
	})
	if err == nil {
		t.Fatal("CollectObservedMetadata() error = nil, want hard failure for rules 500")
	}
	var httpErr sdk.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("CollectObservedMetadata() error = %T, want sdk.HTTPError", err)
	}
	if got, want := httpErr.StatusCode, http.StatusInternalServerError; got != want {
		t.Fatalf("StatusCode = %d, want %d", got, want)
	}
}

// TestCollectLabels502StillFails pins the endpoint gate: the 502 degrade
// applies ONLY to the rules endpoint. A 502 from labels still fails hard,
// so moving the degrade into the shared status handler ungated must break
// this test.
func TestCollectLabels502StillFails(t *testing.T) {
	t.Parallel()

	rulesHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loki/api/v1/labels":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("gateway exploded"))
		case "/loki/api/v1/rules":
			rulesHits++
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

	_, err = client.CollectObservedMetadata(t.Context(), TargetConfig{
		ScopeID:       "loki:tenant:prod",
		InstanceID:    "loki-prod",
		BaseURL:       server.URL,
		ResourceLimit: 50,
	})
	if err == nil {
		t.Fatal("CollectObservedMetadata() error = nil, want hard failure for labels 502")
	}
	var httpErr sdk.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("CollectObservedMetadata() error = %T, want sdk.HTTPError", err)
	}
	if got, want := httpErr.StatusCode, http.StatusBadGateway; got != want {
		t.Fatalf("StatusCode = %d, want %d", got, want)
	}
	if rulesHits != 0 {
		t.Fatalf("rules requests = %d, want 0 (labels failure aborts before rules)", rulesHits)
	}
}

// TestCollectSeries502StillFails mirrors the labels pin for the series
// phase: a 502 from series still fails hard, proving the degrade gate admits
// exactly the rules endpoint and nothing else on the data path.
func TestCollectSeries502StillFails(t *testing.T) {
	t.Parallel()

	rulesHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loki/api/v1/labels":
			writeJSON(t, w, map[string]any{"status": "success", "data": []string{"app"}})
		case "/loki/api/v1/series":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("gateway exploded"))
		case "/loki/api/v1/rules":
			rulesHits++
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

	_, err = client.CollectObservedMetadata(t.Context(), TargetConfig{
		ScopeID:       "loki:tenant:prod",
		InstanceID:    "loki-prod",
		BaseURL:       server.URL,
		ResourceLimit: 50,
	})
	if err == nil {
		t.Fatal("CollectObservedMetadata() error = nil, want hard failure for series 502")
	}
	var httpErr sdk.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("CollectObservedMetadata() error = %T, want sdk.HTTPError", err)
	}
	if got, want := httpErr.StatusCode, http.StatusBadGateway; got != want {
		t.Fatalf("StatusCode = %d, want %d", got, want)
	}
	if rulesHits != 0 {
		t.Fatalf("rules requests = %d, want 0 (series failure aborts before rules)", rulesHits)
	}
}

// TestCollectLabelValues502StillFails closes the last 502 gap: label-values
// endpoints carry dynamic paths that can never equal the rules constant, so
// a 502 there still fails hard instead of degrading.
func TestCollectLabelValues502StillFails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loki/api/v1/labels":
			writeJSON(t, w, map[string]any{"status": "success", "data": []string{"app"}})
		case "/loki/api/v1/label/app/values":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("gateway exploded"))
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	_, err = client.CollectObservedMetadata(t.Context(), TargetConfig{
		ScopeID:                "loki:tenant:prod",
		InstanceID:             "loki-prod",
		BaseURL:                server.URL,
		ResourceLimit:          50,
		LabelValueNames:        []string{"app"},
		MaxLabelValuesPerLabel: 20,
	})
	if err == nil {
		t.Fatal("CollectObservedMetadata() error = nil, want hard failure for label-values 502")
	}
	var httpErr sdk.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("CollectObservedMetadata() error = %T, want sdk.HTTPError", err)
	}
	if got, want := httpErr.StatusCode, http.StatusBadGateway; got != want {
		t.Fatalf("StatusCode = %d, want %d", got, want)
	}
}

// TestCollectRules502DoesNotRetry proves the wasted-retry suppression:
// a ruler-absent 502 is deterministic, so the rules call must go out exactly
// once with no retry accounting.
func TestCollectRules502DoesNotRetry(t *testing.T) {
	t.Parallel()

	rulesHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loki/api/v1/labels":
			writeJSON(t, w, map[string]any{"status": "success", "data": []string{"app"}})
		case "/loki/api/v1/series":
			writeJSON(t, w, map[string]any{"status": "success", "data": []map[string]string{}})
		case "/loki/api/v1/rules":
			rulesHits++
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("Bad Gateway: no ruler backend"))
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	result, err := client.CollectObservedMetadata(t.Context(), TargetConfig{
		ScopeID:       "loki:tenant:prod",
		InstanceID:    "loki-prod",
		BaseURL:       server.URL,
		ResourceLimit: 50,
	})
	if err != nil {
		t.Fatalf("CollectObservedMetadata() error = %v, want nil", err)
	}
	if got, want := rulesHits, 1; got != want {
		t.Fatalf("rules requests = %d, want %d (no wasted retries on deterministic 502)", got, want)
	}
	if got := result.Stats.Retries; got != 0 {
		t.Fatalf("Stats.Retries = %d, want 0", got)
	}
}
