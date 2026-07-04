// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchembedruntime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestBuildReducerServiceWiresSearchVectorBuildRunnerWhenLocalHashConfigured(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	service, err := buildReducerService(
		db,
		stubGraphExecutor{},
		stubCypherExecutor{},
		postgres.NewSharedIntentStore(db),
		stubCypherReader{},
		stubCypherReader{},
		func(key string) string {
			if key == searchembedruntime.EnvLocalEmbedder {
				return "hash"
			}
			return ""
		},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}
	runner := service.SearchVectorBuildRunner
	if runner == nil {
		t.Fatal("buildReducerService() search vector build runner = nil, want local hash vector builder")
	}
	if got, want := runner.Config.ProviderProfileID, searchembedruntime.LocalProviderProfileID; got != want {
		t.Fatalf("ProviderProfileID = %q, want %q", got, want)
	}
	if got, want := runner.Config.EmbeddingModelID, searchembedruntime.LocalEmbeddingModelID; got != want {
		t.Fatalf("EmbeddingModelID = %q, want %q", got, want)
	}
	if runner.ReadyPublisher == nil {
		t.Fatal("buildReducerService() search vector build runner ReadyPublisher = nil, want the search_vector_ready watermark publisher (#4673)")
	}
}

func TestBuildReducerServiceWiresSearchVectorBuildRunnerWhenProviderProfileConfigured(t *testing.T) {
	t.Parallel()

	raw := `{"profiles":[{"profile_id":"semantic-search-default","provider_kind":"openai_compatible","credential_source":{"kind":"cloud_workload_identity"},"model_id":"search-embed-v1","embedding_dimensions":3,"endpoint_profile_id":"https://provider.example","source_classes":["search_documents"],"source_policy_configured":true}]}`
	db := &fakeReducerDB{}
	service, err := buildReducerService(
		db,
		stubGraphExecutor{},
		stubCypherExecutor{},
		postgres.NewSharedIntentStore(db),
		stubCypherReader{},
		stubCypherReader{},
		func(key string) string {
			switch key {
			case "ESHU_SEMANTIC_PROVIDER_PROFILES_JSON":
				return raw
			case "ESHU_SEMANTIC_EXTRACTION_POLICY_JSON":
				return `{"policy_id":"semantic-search","enabled":true,"egress":{"mode":"restricted","semantic_providers":[{"provider_profile_id":"semantic-search-default","source_classes":["search_documents"],"decision":"allow"}]},"rules":[{"rule_id":"search-default","provider_profile_id":"semantic-search-default","source_classes":["search_documents"],"scopes":[{"kind":"repository","id":"local"}],"source_allowlist":[{"kind":"all","value":"*"}],"settings":{"limits":{"max_chunk_bytes":8192,"max_tokens_per_chunk":2048,"max_daily_tokens":100000},"redaction":{"mode":"strict","policy_ref":"search-redaction-v1"},"retention":{"posture":"metadata_only","prompt":"none","response":"hash_only"}}}]}`
			default:
				return ""
			}
		},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}
	runner := service.SearchVectorBuildRunner
	if runner == nil {
		t.Fatal("buildReducerService() search vector build runner = nil, want provider vector builder")
	}
	if got, want := runner.Config.ProviderProfileID, "semantic-search-default"; got != want {
		t.Fatalf("ProviderProfileID = %q, want %q", got, want)
	}
	if got, want := runner.Config.EmbeddingModelID, "search-embed-v1"; got != want {
		t.Fatalf("EmbeddingModelID = %q, want %q", got, want)
	}
}
