package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
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
