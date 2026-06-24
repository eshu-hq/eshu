// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestSourceEmitsIAMPolicyFactsFromFixturePage(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("gcp-runtime-iam-test")
	metrics, err := gcpcloud.NewMetrics(meter)
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}

	scopeCfg := testScope()
	scopeCfg.ContentFamily = "iam_policy"
	resolved := scopeCfg.withDefaults()
	provider := NewFixturePageProvider(map[string][]gcpcloud.AssetsListPage{
		resolved.ScopeID: {readFixturePage(t, "assets_list_iam_policy.json")},
	})
	src := newSource(t, testConfig(scopeCfg), provider, nil)
	src.Metrics = metrics

	collected, ok, err := src.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("Next: ok=%v err=%v", ok, err)
	}
	envs := drainFacts(t, collected)
	if got := countKind(envs, facts.GCPCloudResourceFactKind); got != 1 {
		t.Fatalf("resource fact count = %d, want 1", got)
	}
	if got := countKind(envs, facts.GCPIAMPolicyObservationFactKind); got != 2 {
		t.Fatalf("iam policy fact count = %d, want 2", got)
	}
	iam := firstEnvelopeKind(t, envs, facts.GCPIAMPolicyObservationFactKind)
	if iam.Payload["read_time"] == nil {
		t.Fatal("IAM observation read_time missing")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	if got := factsEmittedCount(rm, facts.GCPIAMPolicyObservationFactKind); got != 2 {
		t.Fatalf("iam facts_emitted count = %d, want 2", got)
	}
}
