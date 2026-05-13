package packageruntime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/packageregistry"
)

func TestHTTPMetadataProviderFetchesMetadataWithBearerAuth(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Bearer token-123"; got != want {
			t.Fatalf("Authorization = %q, want %q", got, want)
		}
		_, _ = w.Write([]byte(`{"name":"team-api","version":"1.2.3"}`))
	}))
	defer server.Close()

	document, err := HTTPMetadataProvider{Client: server.Client()}.FetchMetadata(context.Background(), TargetConfig{
		Base: packageregistry.TargetConfig{
			Provider:  "jfrog",
			Ecosystem: packageregistry.EcosystemGeneric,
			Registry:  "https://artifactory.example.com",
			ScopeID:   "package-registry://jfrog/generic/team-api",
		},
		MetadataURL: server.URL,
		BearerToken: "token-123",
	})
	if err != nil {
		t.Fatalf("FetchMetadata() error = %v, want nil", err)
	}
	if got, want := string(document.Body), `{"name":"team-api","version":"1.2.3"}`; got != want {
		t.Fatalf("Body = %q, want %q", got, want)
	}
}

func TestHTTPMetadataProviderDefaultClientHasTimeout(t *testing.T) {
	t.Parallel()

	client := (HTTPMetadataProvider{}).httpClient()

	if client.Timeout <= 0 {
		t.Fatal("default HTTP client Timeout <= 0, want bounded metadata request timeout")
	}
	if client == http.DefaultClient {
		t.Fatal("default HTTP client is http.DefaultClient, want bounded runtime client")
	}
}

func TestHTTPMetadataProviderUsesXMLAcceptForXMLFeeds(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); !strings.Contains(got, "application/xml") {
			t.Fatalf("Accept = %q, want XML-capable metadata accept header", got)
		}
		_, _ = w.Write([]byte(`<metadata><versioning></versioning></metadata>`))
	}))
	defer server.Close()

	_, err := HTTPMetadataProvider{Client: server.Client()}.FetchMetadata(context.Background(), TargetConfig{
		Base: packageregistry.TargetConfig{
			Provider:  "jfrog",
			Ecosystem: packageregistry.EcosystemMaven,
			Registry:  "https://artifactory.example.com",
			ScopeID:   "package-registry://jfrog/maven/org.example:team-api",
		},
		MetadataURL: server.URL,
	})
	if err != nil {
		t.Fatalf("FetchMetadata() error = %v, want nil", err)
	}
}

func TestHTTPMetadataProviderReturnsRateLimited(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer server.Close()

	_, err := HTTPMetadataProvider{Client: server.Client()}.FetchMetadata(context.Background(), TargetConfig{
		Base: packageregistry.TargetConfig{
			Provider:  "jfrog",
			Ecosystem: packageregistry.EcosystemGeneric,
			Registry:  "https://artifactory.example.com",
			ScopeID:   "package-registry://jfrog/generic/team-api",
		},
		MetadataURL: server.URL,
	})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("FetchMetadata() error = %v, want ErrRateLimited", err)
	}
}

func TestHTTPMetadataProviderSanitizesReturnedSourceURI(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Query().Get("access_token"), "secret"; got != want {
			t.Fatalf("request access_token = %q, want %q", got, want)
		}
		_, _ = w.Write([]byte(`{"name":"team-api","version":"1.2.3"}`))
	}))
	defer server.Close()

	document, err := HTTPMetadataProvider{Client: server.Client()}.FetchMetadata(context.Background(), TargetConfig{
		Base: packageregistry.TargetConfig{
			Provider:  "jfrog",
			Ecosystem: packageregistry.EcosystemGeneric,
			Registry:  "https://artifactory.example.com",
			ScopeID:   "package-registry://jfrog/generic/team-api",
		},
		MetadataURL: server.URL + "?access_token=secret&package=team-api#metadata",
	})
	if err != nil {
		t.Fatalf("FetchMetadata() error = %v, want nil", err)
	}
	if strings.Contains(document.SourceURI, "secret") || strings.Contains(document.SourceURI, "?") ||
		strings.Contains(document.SourceURI, "#") {
		t.Fatalf("SourceURI = %q, want sanitized URL without query, fragment, or secret", document.SourceURI)
	}
}
