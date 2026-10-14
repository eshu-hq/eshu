// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/rest"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestGitHubAPIRetrySignalsReachTheOperator drives a real selection cycle
// through a flaky listing and asserts the three operator signals of #8084:
// the attempts and retries counters, the github.api_retry span event, and the
// per-retry WARN log.
func TestGitHubAPIRetrySignalsReachTheOperator(t *testing.T) {
	t.Parallel()

	server, _ := listRetryFlakyServer(t, 1, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "{}", http.StatusBadGateway)
	})
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(
		sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("github-api-retry-test"),
	)
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	spans := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)).Tracer("github-api-retry-test")
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	rec := newListRetryRecorder()
	selector := &NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:      t.TempDir(),
			SourceMode:    "githubOrg",
			GithubOrg:     "acme",
			RepoLimit:     100,
			GitAuthMethod: "token",
			GitToken:      "token-1",
			GitHubAPI:     rest.Settings{BaseURL: server.URL, Retry: rec.config(4)},
		},
		SyncGit: func(context.Context, RepoSyncConfig, []string) (GitSyncSelection, error) {
			return GitSyncSelection{}, nil
		},
		Instruments: instruments,
		Logger:      logger,
	}
	ctx, span := tracer.Start(context.Background(), "scope.assign")
	if _, err := selector.SelectRepositories(ctx); err != nil {
		t.Fatalf("SelectRepositories() error = %v", err)
	}
	span.End()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	checks := []struct {
		metric string
		attrs  map[string]string
		want   int64
	}{
		{"eshu_dp_github_api_attempts_total", map[string]string{"operation": "list", "failure_class": "server_error", "outcome": "retried"}, 1},
		{"eshu_dp_github_api_attempts_total", map[string]string{"operation": "list", "failure_class": "none", "outcome": "success"}, 2},
		{"eshu_dp_github_api_retries_total", map[string]string{"operation": "list", "failure_class": "server_error"}, 1},
	}
	for _, check := range checks {
		if got := collectorCounterValue(t, rm, check.metric, check.attrs); got != check.want {
			t.Fatalf("%s %v = %d, want %d", check.metric, check.attrs, got, check.want)
		}
	}

	events := 0
	for _, ended := range spans.Ended() {
		for _, event := range ended.Events() {
			if event.Name == "github.api_retry" {
				events++
			}
		}
	}
	if events != 1 {
		t.Fatalf("%s span events = %d, want 1", "github.api_retry", events)
	}
	for _, fragment := range []string{`"msg":"GitHub API call retrying"`, `"failure_class":"server_error"`, `"operation":"list"`, `"attempt":1`} {
		if !strings.Contains(logs.String(), fragment) {
			t.Fatalf("retry log %q missing %s", logs.String(), fragment)
		}
	}
}

// TestGitHubAPIFinalFailureIsCountedFailed pins the failed outcome and that a
// final 401 starts no retry.
func TestGitHubAPIFinalFailureIsCountedFailed(t *testing.T) {
	t.Parallel()

	server, _ := listRetryFlakyServer(t, 10, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "{}", http.StatusUnauthorized)
	})
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(
		sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("github-api-final-test"),
	)
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	ctx := rest.WithObserver(context.Background(), instruments, nil)
	rec := newListRetryRecorder()
	if _, _, err := listGitHubOrgRepositoriesFromWithRetry(
		ctx, server.Client(), server.URL, "acme", 100, "token-1", rec.config(4),
	); err == nil {
		t.Fatal("listing error = nil, want a final 401")
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	attrs := map[string]string{"operation": "list", "failure_class": "client_error", "outcome": "failed"}
	if got := collectorCounterValue(t, rm, "eshu_dp_github_api_attempts_total", attrs); got != 1 {
		t.Fatalf("failed attempts = %d, want 1", got)
	}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == "eshu_dp_github_api_retries_total" {
				t.Fatalf("retries metric recorded for a final failure: %+v", m.Data)
			}
		}
	}
}

// TestDiscoveryConsecutiveFailuresGaugeFollowsTheSelector pins the gauge of
// #8084: it rises with each retryable cycle, returns to 0 on the first good
// selection, and a webhook batch (which never reaches the selector) cannot
// move it.
func TestDiscoveryConsecutiveFailuresGaugeFollowsTheSelector(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(
		sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("consecutive-gauge-test"),
	)
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	selector, _ := discoveryRetrySelector(t, []error{
		retryableListingFailure(), retryableListingFailure(), retryableListingFailure(), nil,
	})
	selector.Instruments = instruments

	read := func() int64 {
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &rm); err != nil {
			t.Fatalf("Collect() error = %v", err)
		}
		for _, scopeMetrics := range rm.ScopeMetrics {
			for _, m := range scopeMetrics.Metrics {
				if m.Name != "eshu_dp_collector_discovery_consecutive_failures" {
					continue
				}
				for _, dp := range m.Data.(metricdata.Gauge[int64]).DataPoints {
					if collectorHasAttrs(dp.Attributes.ToSlice(), map[string]string{"collector_kind": "git", "operation": "list"}) {
						return dp.Value
					}
				}
			}
		}
		t.Fatal("gauge eshu_dp_collector_discovery_consecutive_failures not recorded")
		return -1
	}
	for want := int64(1); want <= 3; want++ {
		if _, err := selector.SelectRepositories(context.Background()); err == nil {
			t.Fatal("SelectRepositories() error = nil, want a retryable failure")
		}
		if got := read(); got != want {
			t.Fatalf("gauge = %d, want %d", got, want)
		}
	}
	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v, want success", err)
	}
	if got := read(); got != 0 {
		t.Fatalf("gauge after a good selection = %d, want 0", got)
	}
}
