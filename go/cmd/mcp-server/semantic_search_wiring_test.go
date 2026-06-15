package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
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
