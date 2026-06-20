package searchembedruntime

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/semanticpolicy"
)

func TestConfigFromEnvUsesExplicitLocalHash(t *testing.T) {
	t.Parallel()

	config, err := ConfigFromEnv(func(key string) string {
		if key == EnvLocalEmbedder {
			return "hash"
		}
		return ""
	}, nil)
	if err != nil {
		t.Fatalf("ConfigFromEnv() error = %v, want nil", err)
	}
	if !config.Enabled {
		t.Fatal("Enabled = false, want true")
	}
	if config.ProviderProfileID != LocalProviderProfileID {
		t.Fatalf("ProviderProfileID = %q, want %q", config.ProviderProfileID, LocalProviderProfileID)
	}
	if config.EmbeddingModelID != LocalEmbeddingModelID {
		t.Fatalf("EmbeddingModelID = %q, want %q", config.EmbeddingModelID, LocalEmbeddingModelID)
	}
	if config.Embedder == nil {
		t.Fatal("Embedder = nil, want local hash embedder")
	}
	if config.VectorRetrieval != searchhybrid.VectorRetrievalAuto {
		t.Fatalf("VectorRetrieval = %q, want auto", config.VectorRetrieval)
	}
}

func TestConfigFromEnvUsesSingleConfiguredSearchDocumentProvider(t *testing.T) {
	t.Parallel()

	raw := `{
		"profiles": [{
			"profile_id": "semantic-search-default",
			"provider_kind": "openai_compatible",
			"credential_source": {"kind": "cloud_workload_identity"},
			"model_id": "search-embed-v1",
			"embedding_dimensions": 3,
			"endpoint_profile_id": "https://provider.example",
			"source_classes": ["search_documents"],
			"source_policy_configured": true
		}]
	}`

	config, err := ConfigFromEnv(func(key string) string {
		if key == EnvProviderProfilesJSON {
			return raw
		}
		if key == semanticpolicy.EnvPolicyJSON {
			return searchPolicyJSON("semantic-search-default")
		}
		return ""
	}, nil)
	if err != nil {
		t.Fatalf("ConfigFromEnv() error = %v, want nil", err)
	}
	if !config.Enabled {
		t.Fatal("Enabled = false, want true")
	}
	if config.ProviderProfileID != "semantic-search-default" {
		t.Fatalf("ProviderProfileID = %q, want semantic-search-default", config.ProviderProfileID)
	}
	if config.EmbeddingModelID != "search-embed-v1" {
		t.Fatalf("EmbeddingModelID = %q, want search-embed-v1", config.EmbeddingModelID)
	}
	if got, want := config.Embedder.Dimensions(), 3; got != want {
		t.Fatalf("Embedder dimensions = %d, want %d", got, want)
	}
}

func TestConfigFromEnvRequiresSearchDocumentPolicy(t *testing.T) {
	t.Parallel()

	raw := `{
		"profiles": [{
			"profile_id": "semantic-search-default",
			"provider_kind": "openai_compatible",
			"credential_source": {"kind": "cloud_workload_identity"},
			"model_id": "search-embed-v1",
			"embedding_dimensions": 3,
			"endpoint_profile_id": "https://provider.example",
			"source_classes": ["search_documents"],
			"source_policy_configured": true
		}]
	}`

	config, err := ConfigFromEnv(func(key string) string {
		if key == EnvProviderProfilesJSON {
			return raw
		}
		return ""
	}, nil)
	if err != nil {
		t.Fatalf("ConfigFromEnv() error = %v, want nil disabled config", err)
	}
	if config.Enabled {
		t.Fatal("Enabled = true, want false without explicit source policy")
	}
}

func TestConfigAllowsOnlyPolicyAdmittedSearchDocuments(t *testing.T) {
	t.Parallel()

	raw := `{
		"profiles": [{
			"profile_id": "semantic-search-default",
			"provider_kind": "openai_compatible",
			"credential_source": {"kind": "cloud_workload_identity"},
			"model_id": "search-embed-v1",
			"embedding_dimensions": 3,
			"endpoint_profile_id": "https://provider.example",
			"source_classes": ["search_documents"],
			"source_policy_configured": true
		}]
	}`

	config, err := ConfigFromEnv(func(key string) string {
		if key == EnvProviderProfilesJSON {
			return raw
		}
		if key == semanticpolicy.EnvPolicyJSON {
			return searchPolicyJSON("semantic-search-default")
		}
		return ""
	}, nil)
	if err != nil {
		t.Fatalf("ConfigFromEnv() error = %v, want nil", err)
	}
	if !config.AllowsSearchDocument("local", "doc-1", "cmd/api/main.go") {
		t.Fatal("AllowsSearchDocument(local) = false, want true")
	}
	if config.AllowsSearchDocument("other-repo", "doc-1", "cmd/api/main.go") {
		t.Fatal("AllowsSearchDocument(other-repo) = true, want false")
	}
	if config.AllowsSearchDocument("local", "doc-1", "private/model.go") {
		t.Fatal("AllowsSearchDocument(private path) = true, want false")
	}
}

func TestConfigFromEnvRequiresSelectorForMultipleProviders(t *testing.T) {
	t.Parallel()

	raw := `{"profiles": [
		{
			"profile_id": "search-a",
			"provider_kind": "openai_compatible",
			"credential_source": {"kind": "cloud_workload_identity"},
			"model_id": "embed-a",
			"embedding_dimensions": 3,
			"endpoint_profile_id": "https://a.example",
			"source_classes": ["search_documents"],
			"source_policy_configured": true
		},
		{
			"profile_id": "search-b",
			"provider_kind": "openai_compatible",
			"credential_source": {"kind": "cloud_workload_identity"},
			"model_id": "embed-b",
			"embedding_dimensions": 3,
			"endpoint_profile_id": "https://b.example",
			"source_classes": ["search_documents"],
			"source_policy_configured": true
		}
	]}`

	_, err := ConfigFromEnv(func(key string) string {
		if key == EnvProviderProfilesJSON {
			return raw
		}
		if key == semanticpolicy.EnvPolicyJSON {
			return searchPolicyJSON("search-a", "search-b")
		}
		return ""
	}, nil)
	if err == nil {
		t.Fatal("ConfigFromEnv() error = nil, want selector requirement")
	}

	config, err := ConfigFromEnv(func(key string) string {
		switch key {
		case EnvProviderProfilesJSON:
			return raw
		case semanticpolicy.EnvPolicyJSON:
			return searchPolicyJSON("search-a", "search-b")
		case EnvProviderProfileID:
			return "search-b"
		default:
			return ""
		}
	}, nil)
	if err != nil {
		t.Fatalf("ConfigFromEnv() with selector error = %v, want nil", err)
	}
	if got, want := config.ProviderProfileID, "search-b"; got != want {
		t.Fatalf("ProviderProfileID = %q, want %q", got, want)
	}
}

func searchPolicyJSON(profileIDs ...string) string {
	rules := ""
	egressRules := ""
	for i, profileID := range profileIDs {
		if i > 0 {
			rules += ","
			egressRules += ","
		}
		rules += `{"rule_id":"search-` + profileID + `","provider_profile_id":"` + profileID + `","source_classes":["search_documents"],"scopes":[{"kind":"repository","id":"local"}],"source_allowlist":[{"kind":"path_prefix","value":"cmd/"}],"settings":{"limits":{"max_chunk_bytes":8192,"max_tokens_per_chunk":2048,"max_daily_tokens":100000},"redaction":{"mode":"strict","policy_ref":"search-redaction-v1"},"retention":{"posture":"metadata_only","prompt":"none","response":"hash_only"}}}`
		egressRules += `{"provider_profile_id":"` + profileID + `","source_classes":["search_documents"],"decision":"allow"}`
	}
	return `{"policy_id":"semantic-search","enabled":true,"egress":{"mode":"restricted","semantic_providers":[` + egressRules + `]},"rules":[` + rules + `]}`
}
