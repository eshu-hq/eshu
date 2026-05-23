package packageruntime

import (
	"context"
	"errors"
	"io"
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

func TestHTTPMetadataProviderRequestsAbbreviatedNPMPackument(t *testing.T) {
	t.Parallel()

	accepts := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept := r.Header.Get("Accept")
		accepts <- accept
		if !strings.Contains(accept, "application/vnd.npm.install-v1+json") {
			_, _ = io.CopyN(w, repeatedByteReader('x'), int64(maxMetadataDocumentBytes+1))
			return
		}
		_, _ = w.Write([]byte(`{
			"name": "vite",
			"versions": {
				"5.4.21": {
					"dist": {
						"tarball": "https://registry.npmjs.org/vite/-/vite-5.4.21.tgz",
						"shasum": "abc123"
					}
				}
			}
		}`))
	}))
	defer server.Close()

	document, err := HTTPMetadataProvider{Client: server.Client()}.FetchMetadata(context.Background(), TargetConfig{
		Base: packageregistry.TargetConfig{
			Provider:  "npmjs",
			Ecosystem: packageregistry.EcosystemNPM,
			Registry:  "https://registry.npmjs.org",
			ScopeID:   "package-registry://npmjs/npm/vite",
		},
		MetadataURL: server.URL,
	})
	if err != nil {
		t.Fatalf("FetchMetadata() error = %v, want abbreviated npm metadata success", err)
	}
	if got := <-accepts; !strings.Contains(got, "application/vnd.npm.install-v1+json") {
		t.Fatalf("Accept = %q, want abbreviated npm packument media type", got)
	}
	if len(document.Body) > maxMetadataDocumentBytes {
		t.Fatalf("Body length = %d, want bounded abbreviated metadata", len(document.Body))
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

func TestHTTPMetadataProviderClassifiesFailureWithoutLeakingMetadataURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		status      int
		wantClass   string
		wantDetails string
	}{
		{
			name:        "auth denied",
			status:      http.StatusForbidden,
			wantClass:   "registry_auth_denied",
			wantDetails: "provider=jfrog ecosystem=generic operation=fetch_metadata status_code=403",
		},
		{
			name:        "not found",
			status:      http.StatusNotFound,
			wantClass:   "registry_not_found",
			wantDetails: "provider=jfrog ecosystem=generic operation=fetch_metadata status_code=404",
		},
		{
			name:        "rate limited",
			status:      http.StatusTooManyRequests,
			wantClass:   "registry_rate_limited",
			wantDetails: "provider=jfrog ecosystem=generic operation=fetch_metadata status_code=429",
		},
		{
			name:        "retryable",
			status:      http.StatusBadGateway,
			wantClass:   "registry_retryable_failure",
			wantDetails: "provider=jfrog ecosystem=generic operation=fetch_metadata status_code=502",
		},
		{
			name:        "terminal",
			status:      http.StatusBadRequest,
			wantClass:   "registry_terminal_failure",
			wantDetails: "provider=jfrog ecosystem=generic operation=fetch_metadata status_code=400",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "private package team-api", tt.status)
			}))
			defer server.Close()

			_, err := HTTPMetadataProvider{Client: server.Client()}.FetchMetadata(context.Background(), TargetConfig{
				Base: packageregistry.TargetConfig{
					Provider:  "jfrog",
					Ecosystem: packageregistry.EcosystemGeneric,
					Registry:  "https://artifactory.example.com/private",
					ScopeID:   "package-registry://jfrog/generic/private/team-api",
				},
				MetadataURL: server.URL + "/private/team-api?token=secret",
			})
			if err == nil {
				t.Fatal("FetchMetadata() error = nil, want classified failure")
			}
			if got := failureClass(err); got != tt.wantClass {
				t.Fatalf("FailureClass() = %q, want %q; error = %v", got, tt.wantClass, err)
			}
			if got := failureDetails(err); got != tt.wantDetails {
				t.Fatalf("FailureDetails() = %q, want %q", got, tt.wantDetails)
			}
			for _, leaked := range []string{"private/team-api", "token=secret", "artifactory.example.com"} {
				if strings.Contains(err.Error(), leaked) || strings.Contains(failureDetails(err), leaked) {
					t.Fatalf("registry failure leaked %q: error=%q details=%q", leaked, err.Error(), failureDetails(err))
				}
			}
		})
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

func failureClass(err error) string {
	var classified interface {
		FailureClass() string
	}
	if errors.As(err, &classified) {
		return classified.FailureClass()
	}
	return ""
}

func failureDetails(err error) string {
	var detailed interface {
		FailureDetails() string
	}
	if errors.As(err, &detailed) {
		return detailed.FailureDetails()
	}
	return ""
}

type repeatedByteReader byte

func (r repeatedByteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(r)
	}
	return len(p), nil
}
