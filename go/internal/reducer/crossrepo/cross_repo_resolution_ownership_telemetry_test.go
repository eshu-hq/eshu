// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossrepo

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestCrossRepoResolutionRecordsDroppedForeignOwnedEdges(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: ownershipTestFacts()},
		IntentWriter:   &recordingRepoDependencyIntentWriter{},
		ScopeRepos:     &fakeScopeRepositoryReader{repos: []string{"repo-own"}},
		Instruments:    instruments,
	}
	if _, err := handler.Resolve(context.Background(), "scope-own", "gen-own"); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	var resources metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &resources); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if !crossRepoEdgeOutcomeHasPoint(
		resources,
		string(relationships.RelDependsOn),
		"foreign_owned_dropped",
		1,
	) {
		t.Fatal("dropped foreign-owned edge left no recorded metric outcome")
	}
	if !crossRepoEdgeOutcomeHasPoint(
		resources,
		string(relationships.RelDependsOn),
		"owned_routed",
		1,
	) {
		t.Fatal("owned routed edge left no recorded metric outcome")
	}
}

func crossRepoEdgeOutcomeHasPoint(
	resources metricdata.ResourceMetrics,
	relationshipType string,
	outcome string,
	want int64,
) bool {
	for _, scope := range resources.ScopeMetrics {
		for _, candidate := range scope.Metrics {
			if candidate.Name != "eshu_dp_cross_repo_edges_resolved_total" {
				continue
			}
			sum, ok := candidate.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				gotType, typeOK := point.Attributes.Value(attribute.Key("relationship_type"))
				gotOutcome, outcomeOK := point.Attributes.Value(attribute.Key("outcome"))
				if typeOK && outcomeOK && gotType.AsString() == relationshipType &&
					gotOutcome.AsString() == outcome && point.Value == want {
					return true
				}
			}
		}
	}
	return false
}
