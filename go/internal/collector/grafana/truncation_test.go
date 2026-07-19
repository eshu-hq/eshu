// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package grafana

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPClientMarksTruncatedSearchResourcesWhenLimitExceeded(t *testing.T) {
	t.Parallel()

	// resource_limit=2, so perPage=2. Page 1 returns 2 dashboards (== perPage,
	// so the walk continues); page 2 returns a distinct 3rd dashboard that the
	// client-side cap drops. That drop must set Truncated + WarningTruncated on
	// the search/dashboard path, which previously only accounted the cap on
	// datasources and alert rules.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/datasources", "/api/v1/provisioning/alert-rules":
			writeJSON(t, w, []map[string]any{})
		case "/api/search":
			if r.URL.Query().Get("page") == "2" {
				writeJSON(t, w, []map[string]any{
					{"uid": "dash-3", "type": "dash-db", "title": "Three"},
				})
				return
			}
			writeJSON(t, w, []map[string]any{
				{"uid": "dash-1", "type": "dash-db", "title": "One"},
				{"uid": "dash-2", "type": "dash-db", "title": "Two"},
			})
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
		ResourceLimit: 2,
	})
	if err != nil {
		t.Fatalf("CollectObservedMetadata() error = %v, want nil", err)
	}
	if got, want := len(result.Resources), 2; got != want {
		t.Fatalf("len(Resources) = %d, want %d", got, want)
	}
	if !result.Stats.Truncated {
		t.Fatal("Stats.Truncated = false, want true (search dropped a distinct dashboard past the cap)")
	}
	assertWarningReason(t, result.Warnings, WarningTruncated)
	for _, warning := range result.Warnings {
		if warning.Reason == WarningTruncated && warning.ResourceClass != ResourceClassDashboard {
			t.Fatalf("truncated warning resource class = %q, want %q", warning.ResourceClass, ResourceClassDashboard)
		}
	}
}

func TestHTTPClientNoTruncationWarningForSearchAtExactLimit(t *testing.T) {
	t.Parallel()

	// Exactly resource_limit distinct dashboards on a single terminal page
	// (len(response) < perPage, so the walk stops). No record is dropped, so
	// no truncation warning must fire.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/datasources", "/api/v1/provisioning/alert-rules":
			writeJSON(t, w, []map[string]any{})
		case "/api/search":
			writeJSON(t, w, []map[string]any{
				{"uid": "dash-1", "type": "dash-db", "title": "One"},
				{"uid": "dash-2", "type": "dash-db", "title": "Two"},
			})
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
		ResourceLimit: 2,
	})
	if err != nil {
		t.Fatalf("CollectObservedMetadata() error = %v, want nil", err)
	}
	if got, want := len(result.Resources), 2; got != want {
		t.Fatalf("len(Resources) = %d, want %d", got, want)
	}
	if result.Stats.Truncated {
		t.Fatal("Stats.Truncated = true, want false at exactly the limit")
	}
	for _, warning := range result.Warnings {
		if warning.Reason == WarningTruncated {
			t.Fatalf("unexpected truncated warning: %#v", warning)
		}
	}
}

func TestHTTPClientMarksTruncatedDatasourcesWhenLimitExceeded(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search", "/api/v1/provisioning/alert-rules":
			writeJSON(t, w, []map[string]any{})
		case "/api/datasources":
			writeJSON(t, w, []map[string]any{
				{"uid": "ds-1", "name": "ds-1", "type": "prometheus"},
				{"uid": "ds-2", "name": "ds-2", "type": "prometheus"},
				{"uid": "ds-3", "name": "ds-3", "type": "prometheus"},
			})
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
		ResourceLimit: 2,
	})
	if err != nil {
		t.Fatalf("CollectObservedMetadata() error = %v, want nil", err)
	}
	if got, want := len(result.Resources), 2; got != want {
		t.Fatalf("len(Resources) = %d, want %d", got, want)
	}
	if !result.Stats.Truncated {
		t.Fatal("Stats.Truncated = false, want true")
	}
	assertWarningReason(t, result.Warnings, WarningTruncated)
}

func TestHTTPClientMarksTruncatedAlertRulesWhenLimitExceeded(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search", "/api/datasources":
			writeJSON(t, w, []map[string]any{})
		case "/api/v1/provisioning/alert-rules":
			writeJSON(t, w, []map[string]any{
				{"uid": "rule-1", "title": "rule-1"},
				{"uid": "rule-2", "title": "rule-2"},
				{"uid": "rule-3", "title": "rule-3"},
			})
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
		ResourceLimit: 2,
	})
	if err != nil {
		t.Fatalf("CollectObservedMetadata() error = %v, want nil", err)
	}
	if got, want := len(result.Rules), 2; got != want {
		t.Fatalf("len(Rules) = %d, want %d", got, want)
	}
	if !result.Stats.Truncated {
		t.Fatal("Stats.Truncated = false, want true")
	}
	assertWarningReason(t, result.Warnings, WarningTruncated)
}

func TestHTTPClientNoTruncationWarningAtOrUnderLimit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		datasources   int
		resourceLimit int
	}{
		{name: "under limit", datasources: 1, resourceLimit: 2},
		{name: "exactly at limit", datasources: 2, resourceLimit: 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var dsPayload []map[string]any
			for i := 0; i < tc.datasources; i++ {
				dsPayload = append(dsPayload, map[string]any{
					"uid": fmt.Sprintf("ds-%d", i), "name": fmt.Sprintf("ds-%d", i), "type": "prometheus",
				})
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/search", "/api/v1/provisioning/alert-rules":
					writeJSON(t, w, []map[string]any{})
				case "/api/datasources":
					writeJSON(t, w, dsPayload)
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
				ResourceLimit: tc.resourceLimit,
			})
			if err != nil {
				t.Fatalf("CollectObservedMetadata() error = %v, want nil", err)
			}
			if got, want := len(result.Resources), tc.datasources; got != want {
				t.Fatalf("len(Resources) = %d, want %d", got, want)
			}
			if result.Stats.Truncated {
				t.Fatal("Stats.Truncated = true, want false")
			}
			for _, warning := range result.Warnings {
				if warning.Reason == WarningTruncated {
					t.Fatalf("unexpected truncated warning: %#v", warning)
				}
			}
		})
	}
}
