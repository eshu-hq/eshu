// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package loki

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestHTTPClientMarksTruncatedSeriesWhenLimitExceeded(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loki/api/v1/labels":
			writeJSON(t, w, map[string]any{"status": "success", "data": []string{}})
		case "/loki/api/v1/series":
			writeJSON(t, w, map[string]any{
				"status": "success",
				"data": []map[string]string{
					{"app": "checkout-prod"},
					{"app": "billing-prod"},
					{"app": "search-prod"},
				},
			})
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
		ResourceLimit: 2,
	})
	if err != nil {
		t.Fatalf("CollectObservedMetadata() error = %v, want nil", err)
	}
	if got, want := len(result.Signals), 2; got != want {
		t.Fatalf("len(Signals) = %d, want %d", got, want)
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
		case "/loki/api/v1/labels":
			writeJSON(t, w, map[string]any{"status": "success", "data": []string{}})
		case "/loki/api/v1/series":
			writeJSON(t, w, map[string]any{"status": "success", "data": []map[string]string{}})
		case "/loki/api/v1/rules":
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write([]byte(`prod:
- name: checkout.rules
  rules:
  - alert: RuleOne
    expr: up == 0
  - alert: RuleTwo
    expr: up == 0
  - alert: RuleThree
    expr: up == 0
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
		seriesCount   int
		resourceLimit int
	}{
		{name: "under limit", seriesCount: 1, resourceLimit: 2},
		{name: "exactly at limit", seriesCount: 2, resourceLimit: 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var seriesPayload []map[string]string
			for i := 0; i < tc.seriesCount; i++ {
				seriesPayload = append(seriesPayload, map[string]string{"app": fmt.Sprintf("app-%d", i)})
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/loki/api/v1/labels":
					writeJSON(t, w, map[string]any{"status": "success", "data": []string{}})
				case "/loki/api/v1/series":
					writeJSON(t, w, map[string]any{"status": "success", "data": seriesPayload})
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
				ResourceLimit: tc.resourceLimit,
			})
			if err != nil {
				t.Fatalf("CollectObservedMetadata() error = %v, want nil", err)
			}
			if got, want := len(result.Signals), tc.seriesCount; got != want {
				t.Fatalf("len(Signals) = %d, want %d", got, want)
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

func TestSeriesQueryUsesExplicitSeriesLookback(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	target := TargetConfig{SeriesLookback: 10 * time.Minute}

	query := seriesQuery(target, observedAt)

	assertBoundedStart(t, query, observedAt, 10*time.Minute)
}

func TestSeriesQueryDoesNotInheritStaleAfterForLookback(t *testing.T) {
	t.Parallel()

	// StaleAfter is a rule-staleness knob and was previously inert for the
	// series window. It must stay inert: series lookback resolves to its own
	// default when SeriesLookback is unset, regardless of StaleAfter, so a
	// deployment that set stale_after does not silently gain a tighter (or
	// looser) series-fetch window than the documented default.
	observedAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	target := TargetConfig{StaleAfter: time.Hour}

	query := seriesQuery(target, observedAt)

	assertBoundedStart(t, query, observedAt, defaultSeriesLookback)
}

func TestSeriesQueryFallsBackToDefaultLookbackWhenUnconfigured(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	target := TargetConfig{}

	query := seriesQuery(target, observedAt)

	assertBoundedStart(t, query, observedAt, defaultSeriesLookback)
}

func TestSeriesQueryOmitsStartWhenObservedAtIsZero(t *testing.T) {
	t.Parallel()

	query := seriesQuery(TargetConfig{StaleAfter: time.Hour}, time.Time{})

	if got := query.Get("start"); got != "" {
		t.Fatalf("start = %q, want omitted for zero observedAt", got)
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

func assertBoundedStart(t *testing.T, query url.Values, observedAt time.Time, wantLookback time.Duration) {
	t.Helper()

	start := query.Get("start")
	end := query.Get("end")
	if start == "" {
		t.Fatal("start query param missing, want bounded lookback")
	}
	if end == "" {
		t.Fatal("end query param missing")
	}
	wantStart := fmt.Sprintf("%d", observedAt.Add(-wantLookback).UnixNano())
	if start != wantStart {
		t.Fatalf("start = %q, want %q", start, wantStart)
	}
	wantEnd := fmt.Sprintf("%d", observedAt.UnixNano())
	if end != wantEnd {
		t.Fatalf("end = %q, want %q", end, wantEnd)
	}
}
