// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

const (
	testScopeID      = "service-catalog-manifest://repo-checkout/catalog-info.yaml"
	testGenerationID = "generation-service-catalog"
)

func testScope(sourceSystem string) scope.IngestionScope {
	return scope.IngestionScope{ScopeID: testScopeID, SourceSystem: sourceSystem}
}

func testGeneration() scope.ScopeGeneration {
	return scope.ScopeGeneration{ScopeID: testScopeID, GenerationID: testGenerationID}
}

func catalogEnvelope(factID, factKind, sourceSystem, collectorKind string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       testScopeID,
		GenerationID:  testGenerationID,
		FactKind:      factKind,
		SchemaVersion: facts.ServiceCatalogSchemaVersionV1,
		CollectorKind: collectorKind,
		SourceRef:     facts.Ref{SourceSystem: sourceSystem},
		Payload:       map[string]any{"entity_ref": "component:default/checkout"},
	}
}

// TestBuildServiceCatalogCorrelationReducerIntent proves the builder anchors
// to the earliest service-catalog fact in original input order regardless of
// which catalog kind it is, that every registry-recognized catalog kind is a
// trigger on its own, that the source-system label falls back from
// SourceRef.SourceSystem to CollectorKind to the ingestion scope's own
// SourceSystem in that order, and that a generation with no catalog fact
// enqueues nothing.
func TestBuildServiceCatalogCorrelationReducerIntent(t *testing.T) {
	t.Parallel()

	t.Run("queues once from the earliest catalog fact across kinds", func(t *testing.T) {
		t.Parallel()
		// The repository link is placed before the entity on purpose: the
		// trigger is any recognized catalog kind, so input order picks the
		// anchor, not a per-kind priority.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			{FactID: "decoy-1", FactKind: "code_symbol_reference"},
			catalogEnvelope("service-catalog-repository-link", facts.ServiceCatalogRepositoryLinkFactKind, "service_catalog", ""),
			catalogEnvelope("service-catalog-entity", facts.ServiceCatalogEntityFactKind, "service_catalog", ""),
		})
		got, ok := BuildServiceCatalogCorrelationReducerIntent(testScope("service_catalog"), testGeneration(), lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := projectorintent.ReducerIntent{
			ScopeID: testScopeID, GenerationID: testGenerationID,
			Domain:    reducer.DomainServiceCatalogCorrelation,
			EntityKey: "service_catalog_correlation:" + testScopeID,
			Reason:    "service catalog facts observed",
			FactID:    "service-catalog-repository-link", SourceSystem: "service_catalog",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("intent = %#v, want %#v", got, want)
		}
	})

	t.Run("every registry-recognized catalog kind triggers on its own", func(t *testing.T) {
		t.Parallel()
		for _, kind := range facts.ServiceCatalogFactKinds() {
			lookup := projectorintent.NewFactLookup([]facts.Envelope{
				{FactID: "decoy-1", FactKind: "code_symbol_reference"},
				catalogEnvelope("catalog-"+kind, kind, "service_catalog", ""),
			})
			got, ok := BuildServiceCatalogCorrelationReducerIntent(testScope("service_catalog"), testGeneration(), lookup)
			if !ok {
				t.Fatalf("kind %q: ok = false, want true", kind)
			}
			if got.FactID != "catalog-"+kind {
				t.Fatalf("kind %q: FactID = %q, want %q", kind, got.FactID, "catalog-"+kind)
			}
		}
	})

	t.Run("falls back to CollectorKind when SourceRef is blank", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			catalogEnvelope("service-catalog-entity", facts.ServiceCatalogEntityFactKind, "  ", "service_catalog_collector"),
		})
		got, ok := BuildServiceCatalogCorrelationReducerIntent(testScope("service_catalog"), testGeneration(), lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.SourceSystem != "service_catalog_collector" {
			t.Fatalf("SourceSystem = %q, want %q", got.SourceSystem, "service_catalog_collector")
		}
	})

	t.Run("falls back to the scope SourceSystem when the envelope carries none", func(t *testing.T) {
		t.Parallel()
		// This third tier is the one projectorintent.SourceSystem lacks; it
		// is why the builder keeps its own helper and takes the scope value.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			catalogEnvelope("service-catalog-entity", facts.ServiceCatalogEntityFactKind, "", "  "),
		})
		got, ok := BuildServiceCatalogCorrelationReducerIntent(testScope("  scope_catalog  "), testGeneration(), lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.SourceSystem != "scope_catalog" {
			t.Fatalf("SourceSystem = %q, want %q", got.SourceSystem, "scope_catalog")
		}
	})

	t.Run("does not queue without a service-catalog fact", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			{FactID: "decoy-1", FactKind: "code_symbol_reference"},
			{FactID: "decoy-2", FactKind: facts.PackageRegistryPackageFactKind},
		})
		got, ok := BuildServiceCatalogCorrelationReducerIntent(testScope("service_catalog"), testGeneration(), lookup)
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("returned (%#v, %t) without a catalog fact, want zero intent and false", got, ok)
		}
	})
}
