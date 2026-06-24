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

func TestSourceEmitsRelationshipFactsFromFixturePage(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("gcp-runtime-relationship-test")
	metrics, err := gcpcloud.NewMetrics(meter)
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}

	scopeCfg := testScope()
	scopeCfg.ContentFamily = "relationship"
	resolved := scopeCfg.withDefaults()
	provider := NewFixturePageProvider(map[string][]gcpcloud.AssetsListPage{
		resolved.ScopeID: {readFixturePage(t, "assets_list_relationship.json")},
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
	if got := countKind(envs, facts.GCPCloudRelationshipFactKind); got != 1 {
		t.Fatalf("relationship fact count = %d, want 1", got)
	}
	rel := firstEnvelopeKind(t, envs, facts.GCPCloudRelationshipFactKind)
	if rel.Payload["read_time"] == nil {
		t.Fatal("relationship observation read_time missing")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	if got := factsEmittedCount(rm, facts.GCPCloudRelationshipFactKind); got != 1 {
		t.Fatalf("relationship facts_emitted count = %d, want 1", got)
	}
}
