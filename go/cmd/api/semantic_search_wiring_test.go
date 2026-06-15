package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel"
)

func TestNewRouterLeavesLocalSemanticHybridDisabledByDefault(t *testing.T) {
	t.Parallel()

	router, err := newRouter(
		nil,
		nil,
		nil,
		staticStatusReader{},
		nil,
		query.ProfileLocalFullStack,
		query.GraphBackendNornicDB,
		nil,
		nil,
		"",
		"",
		component.Policy{},
		query.GovernanceStatusConfig{},
		nil,
	)
	if err != nil {
		t.Fatalf("newRouter() error = %v, want nil", err)
	}
	if router.SemanticSearch == nil {
		t.Fatal("newRouter().SemanticSearch = nil")
	}
	if router.SemanticSearch.LocalHybrid != nil {
		t.Fatal("newRouter().SemanticSearch.LocalHybrid != nil, want disabled by default")
	}
}

func TestNewRouterWiresLocalSemanticHybridWhenExplicitlyConfigured(t *testing.T) {
	t.Parallel()

	router, err := newRouter(
		nil,
		nil,
		nil,
		staticStatusReader{},
		nil,
		query.ProfileLocalFullStack,
		query.GraphBackendNornicDB,
		nil,
		nil,
		"hash",
		"",
		component.Policy{},
		query.GovernanceStatusConfig{},
		nil,
	)
	if err != nil {
		t.Fatalf("newRouter() error = %v, want nil", err)
	}
	if router.SemanticSearch == nil {
		t.Fatal("newRouter().SemanticSearch = nil")
	}
	if router.SemanticSearch.LocalHybrid == nil {
		t.Fatal("newRouter().SemanticSearch.LocalHybrid = nil, want configured local hybrid backend")
	}
	if _, ok := router.SemanticSearch.LocalHybrid.(*query.PersistedLocalSemanticSearchHybrid); !ok {
		t.Fatalf("newRouter().SemanticSearch.LocalHybrid = %T, want persisted vector backend", router.SemanticSearch.LocalHybrid)
	}
}

func TestNewRouterWiresLocalSemanticHybridVectorStoresWithPostgresInstrumentation(t *testing.T) {
	t.Parallel()

	instruments, err := telemetry.NewInstruments(otel.Meter("eshu-api-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	router, err := newRouter(
		nil,
		nil,
		nil,
		staticStatusReader{},
		nil,
		query.ProfileLocalFullStack,
		query.GraphBackendNornicDB,
		nil,
		instruments,
		"hash",
		"",
		component.Policy{},
		query.GovernanceStatusConfig{},
		nil,
	)
	if err != nil {
		t.Fatalf("newRouter() error = %v, want nil", err)
	}
	hybrid, ok := router.SemanticSearch.LocalHybrid.(*query.PersistedLocalSemanticSearchHybrid)
	if !ok {
		t.Fatalf("newRouter().SemanticSearch.LocalHybrid = %T, want persisted vector backend", router.SemanticSearch.LocalHybrid)
	}
	metadata, ok := hybrid.Metadata.(instrumentedSemanticSearchVectorMetadataStore)
	if !ok {
		t.Fatalf("hybrid.Metadata = %T, want instrumented vector metadata store", hybrid.Metadata)
	}
	if metadata.db.StoreName != semanticSearchVectorMetadataStoreName {
		t.Fatalf("metadata store name = %q, want %q", metadata.db.StoreName, semanticSearchVectorMetadataStoreName)
	}
	if metadata.db.Instruments != instruments {
		t.Fatal("metadata store instruments do not match API instruments")
	}
	values, ok := hybrid.Values.(instrumentedSemanticSearchVectorValueStore)
	if !ok {
		t.Fatalf("hybrid.Values = %T, want instrumented vector value store", hybrid.Values)
	}
	if values.db.StoreName != semanticSearchVectorValueStoreName {
		t.Fatalf("value store name = %q, want %q", values.db.StoreName, semanticSearchVectorValueStoreName)
	}
	if values.db.Instruments != instruments {
		t.Fatal("value store instruments do not match API instruments")
	}
}

func TestWireAPIRejectsUnknownSemanticSearchLocalEmbedderBeforeDatastore(t *testing.T) {
	_, _, err := wireAPI(context.Background(), func(key string) string {
		if key == envSemanticSearchLocalEmbedder {
			return "hosted"
		}
		return ""
	}, nil, nil)
	if err == nil {
		t.Fatal("wireAPI() error = nil, want invalid local embedder error")
	}
}
