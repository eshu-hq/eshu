// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts/alertruntime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestLoadClaimedRuntimeConfigSelectsSecurityAlertInstanceAndLoadsTokenEnv(t *testing.T) {
	t.Parallel()

	config, err := loadClaimedRuntimeConfig(func(key string) string {
		switch key {
		case envCollectorInstances:
			return `[{
				"instance_id": "security-alert-primary",
				"collector_kind": "security_alert",
				"mode": "continuous",
				"enabled": true,
				"claims_enabled": true,
				"configuration": {
					"targets": [{
						"provider": "github_dependabot",
						"scope_id": "security-alert:github:example-org/example-repo",
						"repository": "example-org/example-repo",
						"token_env": "GITHUB_TOKEN",
						"allowed_repositories": ["example-org/example-repo"],
						"repository_alert_limit": 25,
						"max_pages": 2
					}]
				}
			}]`
		case envCollectorInstanceID:
			return "security-alert-primary"
		case envOwnerID:
			return "pod-security-alerts"
		case "GITHUB_TOKEN":
			return "token-value"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig() error = %v, want nil", err)
	}
	if got, want := config.Instance.CollectorKind, scope.CollectorSecurityAlert; got != want {
		t.Fatalf("CollectorKind = %q, want %q", got, want)
	}
	if got, want := config.Source.Targets[0].Token, "token-value"; got != want {
		t.Fatalf("Target token = %q, want %q", got, want)
	}
}

func TestBuildClaimedServiceWiresGenerationDeadLetters(t *testing.T) {
	t.Parallel()

	service, err := buildClaimedService(nil, testSecurityAlertGetenv, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildClaimedService() error = %v, want nil", err)
	}
	if service.DeadLetters == nil {
		t.Fatal("DeadLetters = nil, want shared collector generation dead-letter sink")
	}
	if _, ok := service.DeadLetters.(collector.GenerationDeadLetterReplayCompleter); !ok {
		t.Fatalf("DeadLetters type %T does not complete replay state", service.DeadLetters)
	}
	if got, want := service.MaxAttempts, workflow.DefaultClaimMaxAttempts(); got != want {
		t.Fatalf("MaxAttempts = %d, want %d", got, want)
	}
}

func TestLoadClaimedRuntimeConfigRejectsMissingGitHubToken(t *testing.T) {
	t.Parallel()

	_, err := loadClaimedRuntimeConfig(func(key string) string {
		if key != envCollectorInstances {
			return ""
		}
		return `[{
			"instance_id": "security-alert-primary",
			"collector_kind": "security_alert",
			"mode": "continuous",
			"enabled": true,
			"claims_enabled": true,
			"configuration": {
				"targets": [{
					"provider": "github_dependabot",
					"scope_id": "security-alert:github:example-org/example-repo",
					"repository": "example-org/example-repo",
					"token_env": "GITHUB_TOKEN",
					"allowed_repositories": ["example-org/example-repo"]
				}]
			}
		}]`
	})
	if err == nil {
		t.Fatal("loadClaimedRuntimeConfig() error = nil, want missing credential error")
	}
	if strings.Contains(err.Error(), "token-value") || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("loadClaimedRuntimeConfig() error = %q, want credential env reference without value", err)
	}
}

func TestRunProviderAccessPreflightReportsSanitizedAuthDenied(t *testing.T) {
	t.Parallel()

	client := &preflightAlertClient{
		err: securityalerts.GitHubDependabotError{
			StatusCode: 403,
			Message:    "raw upstream error mentions token-value and example-org/example-repo",
		},
	}
	err := runProviderAccessPreflightWithSignals(
		t.Context(),
		testSecurityAlertGetenv,
		func(alertruntime.TargetConfig) (alertruntime.RepositoryAlertClient, error) {
			return client, nil
		},
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("runProviderAccessPreflightWithSignals() error = nil, want auth-denied failure")
	}
	if got, want := client.maxPages, 1; got != want {
		t.Fatalf("preflight maxPages = %d, want %d", got, want)
	}
	if strings.Contains(err.Error(), "token-value") || strings.Contains(err.Error(), "example-org/example-repo") {
		t.Fatalf("runProviderAccessPreflightWithSignals() error = %q, want sanitized failure", err)
	}
	if !strings.Contains(err.Error(), "auth_denied") {
		t.Fatalf("runProviderAccessPreflightWithSignals() error = %q, want auth_denied", err)
	}
}

func TestNewProviderAccessPreflightTelemetryBuildsSignals(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	signals, err := newProviderAccessPreflightTelemetry(t.Context())
	if err != nil {
		t.Fatalf("newProviderAccessPreflightTelemetry() error = %v, want nil", err)
	}
	if signals.tracer == nil {
		t.Fatal("tracer = nil, want telemetry tracer")
	}
	if signals.instruments == nil {
		t.Fatal("instruments = nil, want telemetry instruments")
	}
	if signals.shutdown == nil {
		t.Fatal("shutdown = nil, want provider shutdown function")
	}
	defer func() {
		_ = signals.shutdown(context.Background())
	}()
}

func TestRunProviderAccessPreflightPassesWithBoundedProviderRead(t *testing.T) {
	t.Parallel()

	client := &preflightAlertClient{}
	if err := runProviderAccessPreflightWithSignals(
		t.Context(),
		testSecurityAlertGetenv,
		func(alertruntime.TargetConfig) (alertruntime.RepositoryAlertClient, error) {
			return client, nil
		},
		nil,
		nil,
	); err != nil {
		t.Fatalf("runProviderAccessPreflightWithSignals() error = %v, want nil", err)
	}
	if got, want := client.maxPages, 1; got != want {
		t.Fatalf("preflight maxPages = %d, want %d", got, want)
	}
}

func TestRunProviderAccessPreflightRecordsProviderTelemetry(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("collector-security-alerts-preflight-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	client := &preflightAlertClient{}

	if err := runProviderAccessPreflightWithSignals(
		t.Context(),
		testSecurityAlertGetenv,
		func(alertruntime.TargetConfig) (alertruntime.RepositoryAlertClient, error) {
			return client, nil
		},
		tracerProvider.Tracer(telemetry.DefaultSignalName),
		instruments,
	); err != nil {
		t.Fatalf("runProviderAccessPreflightWithSignals() error = %v, want nil", err)
	}

	rm := collectPreflightMetrics(t, reader)
	assertPreflightCounterPoint(t, rm, "eshu_dp_security_alert_provider_requests_total", map[string]string{
		telemetry.MetricDimensionProvider:    alertruntime.ProviderGitHubDependabot,
		telemetry.MetricDimensionStatusClass: "success",
	})
	assertPreflightHistogramPoint(t, rm, "eshu_dp_security_alert_fetch_duration_seconds", map[string]string{
		telemetry.MetricDimensionProvider:    alertruntime.ProviderGitHubDependabot,
		telemetry.MetricDimensionStatusClass: "success",
	})
	if !preflightSpanRecorded(spanRecorder, telemetry.SpanSecurityAlertObserve) {
		t.Fatalf("span %q was not recorded", telemetry.SpanSecurityAlertObserve)
	}
	if !preflightSpanRecorded(spanRecorder, telemetry.SpanSecurityAlertFetch) {
		t.Fatalf("span %q was not recorded", telemetry.SpanSecurityAlertFetch)
	}
}

func testSecurityAlertGetenv(key string) string {
	switch key {
	case envCollectorInstances:
		return `[{
			"instance_id": "security-alert-primary",
			"collector_kind": "security_alert",
			"mode": "continuous",
			"enabled": true,
			"claims_enabled": true,
			"configuration": {
				"targets": [{
					"provider": "github_dependabot",
					"scope_id": "security-alert:github:example-org/example-repo",
					"repository": "example-org/example-repo",
					"token_env": "GITHUB_TOKEN",
					"allowed_repositories": ["example-org/example-repo"],
					"repository_alert_limit": 25,
					"max_pages": 2
				}]
			}
		}]`
	case envCollectorInstanceID:
		return "security-alert-primary"
	case "GITHUB_TOKEN":
		return "token-value"
	default:
		return ""
	}
}

type preflightAlertClient struct {
	err      error
	maxPages int
}

func (c *preflightAlertClient) ListRepositoryAlertsPages(
	_ context.Context,
	_ string,
	maxPages int,
) (securityalerts.GitHubDependabotAlertResult, error) {
	c.maxPages = maxPages
	return securityalerts.GitHubDependabotAlertResult{}, c.err
}

func (c *preflightAlertClient) ListOrganizationAlertsPages(
	_ context.Context,
	_ string,
	maxPages int,
) (securityalerts.GitHubDependabotAlertResult, error) {
	c.maxPages = maxPages
	return securityalerts.GitHubDependabotAlertResult{}, c.err
}

func TestLoadClaimedRuntimeConfigMapsOrganizationScopeTarget(t *testing.T) {
	t.Parallel()

	config, err := loadClaimedRuntimeConfig(func(key string) string {
		switch key {
		case envCollectorInstances:
			return `[{
				"instance_id": "security-alert-primary",
				"collector_kind": "security_alert",
				"mode": "continuous",
				"enabled": true,
				"claims_enabled": true,
				"configuration": {
					"targets": [{
						"provider": "github_dependabot",
						"scope": "org",
						"scope_id": "security-alert:github-org:example-org",
						"organization": "example-org",
						"token_env": "GITHUB_TOKEN",
						"max_pages": 5,
						"allowed_repositories": ["example-org/alpha-repo", "example-org/beta-repo"]
					}]
				}
			}]`
		case envCollectorInstanceID:
			return "security-alert-primary"
		case "GITHUB_TOKEN":
			return "token-value"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig() error = %v, want nil", err)
	}
	target := config.Source.Targets[0]
	if got, want := target.Scope, alertruntime.TargetScopeOrganization; got != want {
		t.Fatalf("Scope = %q, want %q", got, want)
	}
	if got, want := target.Organization, "example-org"; got != want {
		t.Fatalf("Organization = %q, want %q", got, want)
	}
	if got, want := target.MaxPages, 5; got != want {
		t.Fatalf("MaxPages = %d, want %d", got, want)
	}
	if target.Repository != "" {
		t.Fatalf("Repository = %q, want empty for org scope", target.Repository)
	}
	if got, want := len(target.AllowedRepositories), 2; got != want {
		t.Fatalf("len(AllowedRepositories) = %d, want %d", got, want)
	}
}

func collectPreflightMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	return rm
}

func assertPreflightCounterPoint(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	name string,
	attrs map[string]string,
) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s has type %T, want Sum[int64]", name, m.Data)
			}
			for _, point := range sum.DataPoints {
				if preflightAttributeSetContains(point.Attributes, attrs) && point.Value > 0 {
					return
				}
			}
		}
	}
	t.Fatalf("metric %s with attrs %v was not recorded", name, attrs)
}

func assertPreflightHistogramPoint(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	name string,
	attrs map[string]string,
) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("metric %s has type %T, want Histogram[float64]", name, m.Data)
			}
			for _, point := range histogram.DataPoints {
				if preflightAttributeSetContains(point.Attributes, attrs) && point.Count > 0 {
					return
				}
			}
		}
	}
	t.Fatalf("histogram %s with attrs %v was not recorded", name, attrs)
}

func preflightAttributeSetContains(attrs attribute.Set, want map[string]string) bool {
	for key, wantValue := range want {
		var matched bool
		for _, kv := range attrs.ToSlice() {
			if string(kv.Key) == key && kv.Value.AsString() == wantValue {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func preflightSpanRecorded(recorder *tracetest.SpanRecorder, name string) bool {
	for _, span := range recorder.Ended() {
		if span.Name() == name {
			return true
		}
	}
	return false
}
