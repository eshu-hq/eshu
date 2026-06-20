package searchembedprovider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/semanticprofile"
)

func TestNewEmbedderUsesSearchDocumentProfile(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotAuth string
	var gotModel string
	var gotInput string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotModel = req.Model
		gotInput = req.Input
		_ = json.NewEncoder(w).Encode(embeddingResponse{
			Data: []embeddingData{{Embedding: []float64{0.25, -0.5, 1}}},
		})
	}))
	defer server.Close()

	embedder, err := New(semanticprofile.ProviderProfile{
		ProfileID:              "semantic-search-default",
		ProviderKind:           semanticprofile.ProviderOpenAICompatible,
		CredentialSource:       semanticprofile.CredentialSource{Kind: semanticprofile.CredentialSourceEnvironmentVariable, Handle: "SEARCH_EMBEDDINGS_API_KEY"},
		ModelID:                "search-embed-v1",
		EndpointProfileID:      server.URL,
		SourceClasses:          []string{semanticprofile.SourceSearchDocuments},
		SourcePolicyConfigured: true,
		EmbeddingDimensions:    3,
	}, func(key string) string {
		if key == "SEARCH_EMBEDDINGS_API_KEY" {
			return "test-secret"
		}
		return ""
	}, server.Client())
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if got, want := embedder.Dimensions(), 3; got != want {
		t.Fatalf("Dimensions() = %d, want %d", got, want)
	}

	vector, err := embedder.Embed(context.Background(), "refund checkout")
	if err != nil {
		t.Fatalf("Embed() error = %v, want nil", err)
	}
	if got, want := gotPath, "/v1/embeddings"; got != want {
		t.Fatalf("request path = %q, want %q", got, want)
	}
	if got, want := gotAuth, "Bearer test-secret"; got != want {
		t.Fatalf("Authorization = %q, want %q", got, want)
	}
	if got, want := gotModel, "search-embed-v1"; got != want {
		t.Fatalf("model = %q, want %q", got, want)
	}
	if got, want := gotInput, "refund checkout"; got != want {
		t.Fatalf("input = %q, want %q", got, want)
	}
	if got, want := vector, []float64{0.25, -0.5, 1}; !sameVector(got, want) {
		t.Fatalf("vector = %#v, want %#v", got, want)
	}
}

func TestNewRejectsProfileWithoutSearchDocumentPolicy(t *testing.T) {
	t.Parallel()

	_, err := New(semanticprofile.ProviderProfile{
		ProfileID:              "docs-profile",
		ProviderKind:           semanticprofile.ProviderOpenAICompatible,
		CredentialSource:       semanticprofile.CredentialSource{Kind: semanticprofile.CredentialSourceCloudWorkloadIdentity},
		ModelID:                "search-embed-v1",
		EndpointProfileID:      "https://provider.example",
		SourceClasses:          []string{semanticprofile.SourceDocumentation},
		SourcePolicyConfigured: true,
		EmbeddingDimensions:    3,
	}, func(string) string { return "" }, nil)
	if err == nil {
		t.Fatal("New() error = nil, want source class rejection")
	}
	if !strings.Contains(err.Error(), semanticprofile.SourceSearchDocuments) {
		t.Fatalf("New() error = %q, want search document context", err)
	}

	_, err = New(semanticprofile.ProviderProfile{
		ProfileID:              "search-profile",
		ProviderKind:           semanticprofile.ProviderOpenAICompatible,
		CredentialSource:       semanticprofile.CredentialSource{Kind: semanticprofile.CredentialSourceCloudWorkloadIdentity},
		ModelID:                "search-embed-v1",
		EndpointProfileID:      "https://provider.example",
		SourceClasses:          []string{semanticprofile.SourceSearchDocuments},
		SourcePolicyConfigured: false,
		EmbeddingDimensions:    3,
	}, func(string) string { return "" }, nil)
	if err == nil {
		t.Fatal("New() error = nil, want source policy rejection")
	}
	if !strings.Contains(err.Error(), "source policy") {
		t.Fatalf("New() error = %q, want source policy context", err)
	}
}

func TestNewRejectsProviderKindWithoutEmbeddingsTransport(t *testing.T) {
	t.Parallel()

	_, err := New(semanticprofile.ProviderProfile{
		ProfileID:              "anthropic-search",
		ProviderKind:           semanticprofile.ProviderAnthropic,
		CredentialSource:       semanticprofile.CredentialSource{Kind: semanticprofile.CredentialSourceCloudWorkloadIdentity},
		ModelID:                "claude-embed",
		EndpointProfileID:      "https://provider.example",
		SourceClasses:          []string{semanticprofile.SourceSearchDocuments},
		SourcePolicyConfigured: true,
		EmbeddingDimensions:    3,
	}, func(string) string { return "" }, nil)
	if err == nil {
		t.Fatal("New() error = nil, want provider kind rejection")
	}
	if !strings.Contains(err.Error(), "provider_kind") || !strings.Contains(err.Error(), "/v1/embeddings") {
		t.Fatalf("New() error = %q, want provider kind and transport context", err)
	}
}

func TestNewRejectsInvalidEndpointWithoutLeakingCredentialHandle(t *testing.T) {
	t.Parallel()

	_, err := New(semanticprofile.ProviderProfile{
		ProfileID:              "semantic-search-default",
		ProviderKind:           semanticprofile.ProviderOpenAICompatible,
		CredentialSource:       semanticprofile.CredentialSource{Kind: semanticprofile.CredentialSourceEnvironmentVariable, Handle: "SEARCH_EMBEDDINGS_API_KEY"},
		ModelID:                "search-embed-v1",
		EndpointProfileID:      "semantic-search-gateway",
		SourceClasses:          []string{semanticprofile.SourceSearchDocuments},
		SourcePolicyConfigured: true,
		EmbeddingDimensions:    3,
	}, func(string) string { return "" }, nil)
	if err == nil {
		t.Fatal("New() error = nil, want invalid endpoint error")
	}
	if strings.Contains(err.Error(), "SEARCH_EMBEDDINGS_API_KEY") {
		t.Fatalf("New() error leaked credential handle: %q", err)
	}
	if !strings.Contains(err.Error(), "endpoint_profile_id") {
		t.Fatalf("New() error = %q, want endpoint context", err)
	}
}

func TestEmbedRedactsProviderErrorBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "provider body with raw source and secret", http.StatusBadRequest)
	}))
	defer server.Close()

	embedder, err := New(semanticprofile.ProviderProfile{
		ProfileID:              "semantic-search-default",
		ProviderKind:           semanticprofile.ProviderOpenAICompatible,
		CredentialSource:       semanticprofile.CredentialSource{Kind: semanticprofile.CredentialSourceCloudWorkloadIdentity},
		ModelID:                "search-embed-v1",
		EndpointProfileID:      server.URL,
		SourceClasses:          []string{semanticprofile.SourceSearchDocuments},
		SourcePolicyConfigured: true,
		EmbeddingDimensions:    3,
	}, func(string) string { return "" }, server.Client())
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	_, err = embedder.Embed(context.Background(), "sensitive source")
	if err == nil {
		t.Fatal("Embed() error = nil, want provider error")
	}
	if strings.Contains(err.Error(), "raw source") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("Embed() error leaked provider body: %q", err)
	}
	if !strings.Contains(err.Error(), "HTTP 400") {
		t.Fatalf("Embed() error = %q, want status code", err)
	}
}

func TestEmbedRedactsTransportEndpoint(t *testing.T) {
	t.Parallel()

	const endpoint = "https://semantic-search-gateway.internal.example"
	embedder, err := New(semanticprofile.ProviderProfile{
		ProfileID:              "semantic-search-default",
		ProviderKind:           semanticprofile.ProviderOpenAICompatible,
		CredentialSource:       semanticprofile.CredentialSource{Kind: semanticprofile.CredentialSourceCloudWorkloadIdentity},
		ModelID:                "search-embed-v1",
		EndpointProfileID:      endpoint,
		SourceClasses:          []string{semanticprofile.SourceSearchDocuments},
		SourcePolicyConfigured: true,
		EmbeddingDimensions:    3,
	}, func(string) string { return "" }, &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial semantic-search-gateway.internal.example failed")
		}),
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	_, err = embedder.Embed(context.Background(), "sensitive source")
	if err == nil {
		t.Fatal("Embed() error = nil, want transport error")
	}
	if strings.Contains(err.Error(), "semantic-search-gateway") || strings.Contains(err.Error(), endpoint) {
		t.Fatalf("Embed() error leaked endpoint: %q", err)
	}
	if got, want := err.Error(), "execute embedding request failed"; got != want {
		t.Fatalf("Embed() error = %q, want %q", got, want)
	}
}

func TestEmbedUsesCallerContext(t *testing.T) {
	t.Parallel()

	embedder, err := New(semanticprofile.ProviderProfile{
		ProfileID:              "semantic-search-default",
		ProviderKind:           semanticprofile.ProviderOpenAICompatible,
		CredentialSource:       semanticprofile.CredentialSource{Kind: semanticprofile.CredentialSourceCloudWorkloadIdentity},
		ModelID:                "search-embed-v1",
		EndpointProfileID:      "https://provider.example",
		SourceClasses:          []string{semanticprofile.SourceSearchDocuments},
		SourcePolicyConfigured: true,
		EmbeddingDimensions:    3,
	}, func(string) string { return "" }, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if err := req.Context().Err(); err != nil {
				return nil, err
			}
			return nil, errors.New("request was not canceled")
		}),
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = embedder.Embed(ctx, "sensitive source")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Embed() error = %v, want context.Canceled", err)
	}
}

func sameVector(got []float64, want []float64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
