// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestBuildProjectionQueuesSingleServiceCatalogCorrelationIntent(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "service-catalog-manifest://repo-checkout/catalog-info.yaml",
		SourceSystem: "service_catalog",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "generation-service-catalog",
	}
	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{
		{
			FactID:        "service-catalog-entity",
			ScopeID:       scopeValue.ScopeID,
			GenerationID:  generation.GenerationID,
			FactKind:      facts.ServiceCatalogEntityFactKind,
			SchemaVersion: facts.ServiceCatalogSchemaVersionV1,
			Payload: map[string]any{
				"entity_ref": "component:default/checkout",
			},
		},
		{
			FactID:        "service-catalog-repository-link",
			ScopeID:       scopeValue.ScopeID,
			GenerationID:  generation.GenerationID,
			FactKind:      facts.ServiceCatalogRepositoryLinkFactKind,
			SchemaVersion: facts.ServiceCatalogSchemaVersionV1,
			Payload: map[string]any{
				"entity_ref":     "component:default/checkout",
				"repository_url": "https://github.com/acme/checkout.git",
			},
		},
	})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	if got, want := len(projection.reducerIntents), 1; got != want {
		t.Fatalf("len(reducerIntents) = %d, want %d", got, want)
	}
	intent := projection.reducerIntents[0]
	if got, want := intent.Domain, reducer.DomainServiceCatalogCorrelation; got != want {
		t.Fatalf("intent.Domain = %q, want %q", got, want)
	}
	if got, want := intent.EntityKey, "service_catalog_correlation:"+scopeValue.ScopeID; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
}

func TestBuildProjectionRejectsUnsupportedServiceCatalogSchemaVersion(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "service-catalog-manifest://repo-checkout/catalog-info.yaml",
		SourceSystem: "service_catalog",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "generation-service-catalog",
	}
	_, err := buildProjection(scopeValue, generation, []facts.Envelope{
		{
			FactID:        "service-catalog-entity",
			ScopeID:       scopeValue.ScopeID,
			GenerationID:  generation.GenerationID,
			FactKind:      facts.ServiceCatalogEntityFactKind,
			SchemaVersion: "2099-01-01",
			Payload: map[string]any{
				"entity_ref": "component:default/checkout",
			},
		},
	})
	if err == nil {
		t.Fatal("buildProjection() error = nil, want unsupported schema_version error")
	}
}
