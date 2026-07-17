// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/searchembedruntime"
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
		false,
		query.CookieSecureAuto,
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
	if router.SemanticSearch.ScopeResolver == nil {
		t.Fatal("newRouter().SemanticSearch.ScopeResolver = nil, want repository-to-scope resolver")
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
		false,
		query.CookieSecureAuto,
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

func TestNewRouterWiresProviderSemanticHybridWhenProfileConfigured(t *testing.T) {
	t.Parallel()

	embeddingConfig := providerSemanticSearchConfig(t)
	router, err := newRouterWithSemanticEmbedding(
		nil,
		nil,
		nil,
		staticStatusReader{},
		nil,
		query.ProfileLocalFullStack,
		query.GraphBackendNornicDB,
		nil,
		nil,
		embeddingConfig,
		"",
		component.Policy{},
		query.GovernanceStatusConfig{},
		nil,
		false,
		query.CookieSecureAuto,
	)
	if err != nil {
		t.Fatalf("newRouterWithSemanticEmbedding() error = %v, want nil", err)
	}
	hybrid, ok := router.SemanticSearch.LocalHybrid.(*query.PersistedLocalSemanticSearchHybrid)
	if !ok {
		t.Fatalf("newRouterWithSemanticEmbedding().SemanticSearch.LocalHybrid = %T, want persisted vector backend", router.SemanticSearch.LocalHybrid)
	}
	if got, want := hybrid.Config.ProviderProfileID, "semantic-search-default"; got != want {
		t.Fatalf("ProviderProfileID = %q, want %q", got, want)
	}
	if got, want := hybrid.Config.EmbeddingModelID, "search-embed-v1"; got != want {
		t.Fatalf("EmbeddingModelID = %q, want %q", got, want)
	}
	if got, want := hybrid.Embedder.Dimensions(), 3; got != want {
		t.Fatalf("Embedder dimensions = %d, want %d", got, want)
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
		false,
		query.CookieSecureAuto,
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
	snapshots, ok := hybrid.Snapshots.(instrumentedSemanticSearchSnapshotStore)
	if !ok {
		t.Fatalf("hybrid.Snapshots = %T, want instrumented snapshot store", hybrid.Snapshots)
	}
	if snapshots.db.StoreName != semanticSearchSnapshotStoreName {
		t.Fatalf("snapshot store name = %q, want %q", snapshots.db.StoreName, semanticSearchSnapshotStoreName)
	}
	if snapshots.db.Instruments != instruments {
		t.Fatal("snapshot store instruments do not match API instruments")
	}
	if snapshots.db.Tracer == nil {
		t.Fatal("snapshot store tracer = nil, want Postgres child spans")
	}
}

func TestNewRouterWiresSemanticSearchScopeResolverWithPostgresInstrumentation(t *testing.T) {
	t.Parallel()

	instruments, err := telemetry.NewInstruments(otel.Meter("eshu-api-scope-test"))
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
		"",
		"",
		component.Policy{},
		query.GovernanceStatusConfig{},
		nil,
		false,
		query.CookieSecureAuto,
	)
	if err != nil {
		t.Fatalf("newRouter() error = %v, want nil", err)
	}
	resolver, ok := router.SemanticSearch.ScopeResolver.(instrumentedSemanticSearchScopeResolver)
	if !ok {
		t.Fatalf("ScopeResolver = %T, want instrumented resolver", router.SemanticSearch.ScopeResolver)
	}
	if got, want := resolver.db.StoreName, semanticSearchScopeStoreName; got != want {
		t.Fatalf("scope resolver store name = %q, want %q", got, want)
	}
	if resolver.db.Instruments != instruments {
		t.Fatal("scope resolver instruments do not match API instruments")
	}
	if resolver.db.Tracer == nil {
		t.Fatal("scope resolver tracer = nil, want Postgres child spans")
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

func providerSemanticSearchConfig(t *testing.T) searchembedruntime.Config {
	t.Helper()
	raw := `{"profiles":[{"profile_id":"semantic-search-default","provider_kind":"openai_compatible","credential_source":{"kind":"cloud_workload_identity"},"model_id":"search-embed-v1","embedding_dimensions":3,"endpoint_profile_id":"https://provider.example","source_classes":["search_documents"],"source_policy_configured":true}]}`
	config, err := searchembedruntime.ConfigFromEnv(func(key string) string {
		if key == searchembedruntime.EnvProviderProfilesJSON {
			return raw
		}
		if key == "ESHU_SEMANTIC_EXTRACTION_POLICY_JSON" {
			return semanticSearchPolicyJSON("semantic-search-default")
		}
		return ""
	}, nil)
	if err != nil {
		t.Fatalf("ConfigFromEnv() error = %v, want nil", err)
	}
	return config
}

func semanticSearchPolicyJSON(profileID string) string {
	return `{"policy_id":"semantic-search","enabled":true,"egress":{"mode":"restricted","semantic_providers":[{"provider_profile_id":"` + profileID + `","source_classes":["search_documents"],"decision":"allow"}]},"rules":[{"rule_id":"search-default","provider_profile_id":"` + profileID + `","source_classes":["search_documents"],"scopes":[{"kind":"repository","id":"local"}],"source_allowlist":[{"kind":"all","value":"*"}],"settings":{"limits":{"max_chunk_bytes":8192,"max_tokens_per_chunk":2048,"max_daily_tokens":100000},"redaction":{"mode":"strict","policy_ref":"search-redaction-v1"},"retention":{"posture":"metadata_only","prompt":"none","response":"hash_only"}}}]}`
}
