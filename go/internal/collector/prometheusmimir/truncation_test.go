// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package prometheusmimir

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPClientMarksTruncatedTargetsWhenLimitExceeded(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/targets":
			writeJSON(t, w, map[string]any{
				"status": "success",
				"data": map[string]any{
					"activeTargets": []map[string]any{
						{"scrapePool": "pool-1", "health": "up"},
						{"scrapePool": "pool-2", "health": "up"},
						{"scrapePool": "pool-3", "health": "up"},
					},
				},
			})
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
		ResourceLimit: 2,
	})
	if err != nil {
		t.Fatalf("CollectObservedMetadata() error = %v, want nil", err)
	}
	if got, want := len(result.Targets), 2; got != want {
		t.Fatalf("len(Targets) = %d, want %d", got, want)
	}
	if !result.Stats.Truncated {
		t.Fatal("Stats.Truncated = false, want true")
	}
	assertWarningReason(t, result.Warnings, WarningTruncated)
}

func TestHTTPClientMarksTruncatedRulesWhenLimitExceeded(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/targets":
			writeJSON(t, w, map[string]any{"status": "success", "data": map[string]any{"activeTargets": []map[string]any{}}})
		case "/api/v1/rules":
			writeJSON(t, w, map[string]any{
				"status": "success",
				"data": map[string]any{
					"groups": []map[string]any{{
						"name": "checkout.rules",
						"rules": []map[string]any{
							{"name": "RuleOne", "type": "alerting", "health": "ok"},
							{"name": "RuleTwo", "type": "alerting", "health": "ok"},
							{"name": "RuleThree", "type": "alerting", "health": "ok"},
						},
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
		Provider:      ProviderPrometheus,
		ScopeID:       "prometheus:instance:prod",
		InstanceID:    "prometheus-prod",
		BaseURL:       server.URL,
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
		targetCount   int
		resourceLimit int
	}{
		{name: "under limit", targetCount: 1, resourceLimit: 2},
		{name: "exactly at limit", targetCount: 2, resourceLimit: 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var targetsPayload []map[string]any
			for i := 0; i < tc.targetCount; i++ {
				targetsPayload = append(targetsPayload, map[string]any{
					"scrapePool": fmt.Sprintf("pool-%d", i), "health": "up",
				})
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/targets":
					writeJSON(t, w, map[string]any{
						"status": "success",
						"data":   map[string]any{"activeTargets": targetsPayload},
					})
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
				ResourceLimit: tc.resourceLimit,
			})
			if err != nil {
				t.Fatalf("CollectObservedMetadata() error = %v, want nil", err)
			}
			if got, want := len(result.Targets), tc.targetCount; got != want {
				t.Fatalf("len(Targets) = %d, want %d", got, want)
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

func assertWarningReason(t *testing.T, warnings []Warning, reason string) {
	t.Helper()
	for _, warning := range warnings {
		if warning.Reason == reason {
			return
		}
	}
	t.Fatalf("missing warning reason %q in %#v", reason, warnings)
}
