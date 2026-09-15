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
	if !crossRepoMetricHasPoint(
		resources,
		"eshu_dp_cross_repo_edges_dropped_total",
		string(relationships.RelDependsOn),
		"reason",
		"foreign_owned",
		1,
	) {
		t.Fatal("dropped foreign-owned edge left no dedicated metric point")
	}
	if !crossRepoMetricHasPoint(
		resources,
		"eshu_dp_cross_repo_edges_resolved_total",
		string(relationships.RelDependsOn),
		"outcome",
		"",
		1,
	) {
		t.Fatal("resolved counter did not preserve its routed-only, outcome-free point")
	}
}

func crossRepoMetricHasPoint(
	resources metricdata.ResourceMetrics,
	metricName string,
	relationshipType string,
	optionalAttribute string,
	attributeValue string,
	want int64,
) bool {
	for _, scope := range resources.ScopeMetrics {
		for _, candidate := range scope.Metrics {
			if candidate.Name != metricName {
				continue
			}
			sum, ok := candidate.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				gotType, typeOK := point.Attributes.Value(attribute.Key("relationship_type"))
				gotAttribute, attributeOK := point.Attributes.Value(attribute.Key(optionalAttribute))
				attributeMatches := attributeValue == "" && !attributeOK
				attributeMatches = attributeMatches ||
					(attributeOK && gotAttribute.AsString() == attributeValue)
				if typeOK && gotType.AsString() == relationshipType &&
					attributeMatches && point.Value == want {
					return true
				}
			}
		}
	}
	return false
}
