package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel"
)

func TestNewMCPQueryRouterLeavesLocalSemanticHybridDisabledByDefault(t *testing.T) {
	t.Parallel()

	router := newMCPQueryRouter(
		nil,
		nil,
		nil,
		staticStatusReader{},
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
	if router.SemanticSearch == nil {
		t.Fatal("newMCPQueryRouter().SemanticSearch = nil")
	}
	if router.SemanticSearch.LocalHybrid != nil {
		t.Fatal("newMCPQueryRouter().SemanticSearch.LocalHybrid != nil, want disabled by default")
	}
}

func TestNewMCPQueryRouterWiresLocalSemanticHybridWhenExplicitlyConfigured(t *testing.T) {
	t.Parallel()

	router := newMCPQueryRouter(
		nil,
		nil,
		nil,
		staticStatusReader{},
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
	if router.SemanticSearch == nil {
		t.Fatal("newMCPQueryRouter().SemanticSearch = nil")
	}
	if router.SemanticSearch.LocalHybrid == nil {
		t.Fatal("newMCPQueryRouter().SemanticSearch.LocalHybrid = nil, want configured local hybrid backend")
	}
	if _, ok := router.SemanticSearch.LocalHybrid.(*query.PersistedLocalSemanticSearchHybrid); !ok {
		t.Fatalf("newMCPQueryRouter().SemanticSearch.LocalHybrid = %T, want persisted vector backend", router.SemanticSearch.LocalHybrid)
	}
}

func TestNewMCPQueryRouterWiresLocalSemanticHybridVectorStoresWithPostgresInstrumentation(t *testing.T) {
	t.Parallel()

	instruments, err := telemetry.NewInstruments(otel.Meter("eshu-mcp-server-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	router := newMCPQueryRouter(
		nil,
		nil,
		nil,
		staticStatusReader{},
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
	hybrid, ok := router.SemanticSearch.LocalHybrid.(*query.PersistedLocalSemanticSearchHybrid)
	if !ok {
		t.Fatalf("newMCPQueryRouter().SemanticSearch.LocalHybrid = %T, want persisted vector backend", router.SemanticSearch.LocalHybrid)
	}
	metadata, ok := hybrid.Metadata.(instrumentedSemanticSearchVectorMetadataStore)
	if !ok {
		t.Fatalf("hybrid.Metadata = %T, want instrumented vector metadata store", hybrid.Metadata)
	}
	if metadata.db.StoreName != semanticSearchVectorMetadataStoreName {
		t.Fatalf("metadata store name = %q, want %q", metadata.db.StoreName, semanticSearchVectorMetadataStoreName)
	}
	if metadata.db.Instruments != instruments {
		t.Fatal("metadata store instruments do not match MCP instruments")
	}
	values, ok := hybrid.Values.(instrumentedSemanticSearchVectorValueStore)
	if !ok {
		t.Fatalf("hybrid.Values = %T, want instrumented vector value store", hybrid.Values)
	}
	if values.db.StoreName != semanticSearchVectorValueStoreName {
		t.Fatalf("value store name = %q, want %q", values.db.StoreName, semanticSearchVectorValueStoreName)
	}
	if values.db.Instruments != instruments {
		t.Fatal("value store instruments do not match MCP instruments")
	}
}

func TestWireAPIRejectsUnknownSemanticSearchLocalEmbedderBeforeDatastore(t *testing.T) {
	_, _, _, err := wireAPI(context.Background(), func(key string) string {
		if key == envSemanticSearchLocalEmbedder {
			return "hosted"
		}
		return ""
	}, nil, nil)
	if err == nil {
		t.Fatal("wireAPI() error = nil, want invalid local embedder error")
	}
}
