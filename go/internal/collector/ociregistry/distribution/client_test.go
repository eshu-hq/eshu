// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package distribution

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

const (
	testDigest   = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testReferrer = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestClientPingAcceptsDistributionAuthChallenge(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/" {
			t.Fatalf("path = %q, want /v2/", r.URL.Path)
		}
		w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")
		w.Header().Set("WWW-Authenticate", `Bearer realm="https://registry.example/token"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}

func TestClientListTagsAndManifestUsesEscapedRepositoryPath(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/team/api/tags/list":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"team/api","tags":["latest","release"]}`))
		case "/v2/team/api/manifests/latest":
			if got := r.Header.Get("Accept"); got == "" {
				t.Fatal("manifest request missing Accept header")
			}
			w.Header().Set("Docker-Content-Digest", testDigest)
			w.Header().Set("Content-Type", ociregistry.MediaTypeOCIImageManifest)
			_, _ = w.Write([]byte(`{"schemaVersion":2}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{BaseURL: server.URL, Username: "u", Password: "p"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	tags, err := client.ListTags(context.Background(), "team/api")
	if err != nil {
		t.Fatalf("ListTags() error = %v", err)
	}
	if len(tags) != 2 || tags[0] != "latest" || tags[1] != "release" {
		t.Fatalf("tags = %#v", tags)
	}
	manifest, err := client.GetManifest(context.Background(), "team/api", "latest")
	if err != nil {
		t.Fatalf("GetManifest() error = %v", err)
	}
	if manifest.Digest != testDigest || manifest.MediaType != ociregistry.MediaTypeOCIImageManifest {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestClientGetBlobCapsResponseBodyRead(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/team/api/blobs/"+testDigest {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/vnd.oci.image.config.v1+json")
		_, _ = w.Write([]byte(strings.Repeat("x", int(maxBlobReadBytes)+4096)))
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	blob, err := client.GetBlob(context.Background(), "team/api", testDigest)
	if err != nil {
		t.Fatalf("GetBlob() error = %v", err)
	}
	if got, want := len(blob.Body), int(maxBlobReadBytes)+1; got != want {
		t.Fatalf("len(blob.Body) = %d, want capped %d", got, want)
	}
}

func TestClientListReferrersParsesDescriptors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/team/api/referrers/"+testDigest {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", ociregistry.MediaTypeOCIImageIndex)
		_, _ = w.Write([]byte(`{
			"schemaVersion": 2,
			"manifests": [{
				"mediaType": "application/vnd.in-toto+json",
				"digest": "` + testReferrer + `",
				"size": 333,
				"artifactType": "application/vnd.in-toto+json",
				"annotations": {"org.opencontainers.image.source":"https://example.com/repo"}
			}]
		}`))
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	referrers, err := client.ListReferrers(context.Background(), "team/api", testDigest)
	if err != nil {
		t.Fatalf("ListReferrers() error = %v", err)
	}
	if len(referrers.Referrers) != 1 {
		t.Fatalf("referrers = %#v", referrers)
	}
	if referrers.Referrers[0].Digest != testReferrer {
		t.Fatalf("referrer digest = %q", referrers.Referrers[0].Digest)
	}
}

func TestClientClassifiesStatusFailureWithoutLeakingRepositoryPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		status      int
		wantClass   string
		wantDetails string
	}{
		{
			name:        "auth denied",
			status:      http.StatusUnauthorized,
			wantClass:   "registry_auth_denied",
			wantDetails: "provider=oci operation=list_tags status_code=401",
		},
		{
			name:        "not found",
			status:      http.StatusNotFound,
			wantClass:   "registry_not_found",
			wantDetails: "provider=oci operation=list_tags status_code=404",
		},
		{
			name:        "rate limited",
			status:      http.StatusTooManyRequests,
			wantClass:   "registry_rate_limited",
			wantDetails: "provider=oci operation=list_tags status_code=429",
		},
		{
			name:        "retryable",
			status:      http.StatusInternalServerError,
			wantClass:   "registry_retryable_failure",
			wantDetails: "provider=oci operation=list_tags status_code=500",
		},
		{
			name:        "terminal",
			status:      http.StatusBadRequest,
			wantClass:   "registry_terminal_failure",
			wantDetails: "provider=oci operation=list_tags status_code=400",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "private repo team/api", tt.status)
			}))
			defer server.Close()

			client, err := NewClient(ClientConfig{BaseURL: server.URL})
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			_, err = client.ListTags(context.Background(), "private/team-api")
			if err == nil {
				t.Fatal("ListTags() error = nil, want classified failure")
			}
			if got := failureClass(err); got != tt.wantClass {
				t.Fatalf("FailureClass() = %q, want %q; error = %v", got, tt.wantClass, err)
			}
			if got := failureDetails(err); got != tt.wantDetails {
				t.Fatalf("FailureDetails() = %q, want %q", got, tt.wantDetails)
			}
			var httpErr sdk.HTTPError
			if !errors.As(err, &httpErr) {
				t.Fatalf("ListTags() error = %T %[1]v, want SDK HTTPError cause", err)
			}
			if got := httpErr.StatusCode; got != tt.status {
				t.Fatalf("SDK HTTPError StatusCode = %d, want %d", got, tt.status)
			}
			for _, leaked := range []string{"private/team-api", "/v2/private", "team/api"} {
				if strings.Contains(err.Error(), leaked) || strings.Contains(failureDetails(err), leaked) {
					t.Fatalf("OCI failure leaked %q: error=%q details=%q", leaked, err.Error(), failureDetails(err))
				}
			}
		})
	}
}

func TestClientTransportFailureWrapsSDKHTTPErrorWithoutLeakingRequestDetails(t *testing.T) {
	t.Parallel()

	transportErr := errors.New("dial denied for registry.example.test/team/api")
	client, err := NewClient(ClientConfig{
		BaseURL: "https://registry.example.test",
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, transportErr
		})},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.GetManifest(context.Background(), "private/team-api", testDigest)
	if err == nil {
		t.Fatal("GetManifest() error = nil, want transport failure")
	}
	if got := failureClass(err); got != "registry_retryable_failure" {
		t.Fatalf("FailureClass() = %q, want registry_retryable_failure", got)
	}
	var httpErr sdk.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("GetManifest() error = %T %[1]v, want SDK HTTPError cause", err)
	}
	if httpErr.StatusCode != 0 {
		t.Fatalf("SDK HTTPError StatusCode = %d, want 0 for transport failure", httpErr.StatusCode)
	}
	if !errors.Is(err, transportErr) {
		t.Fatalf("GetManifest() error = %v, want transport cause", err)
	}
	for _, leaked := range []string{"private/team-api", "registry.example.test", testDigest} {
		if strings.Contains(err.Error(), leaked) || strings.Contains(failureDetails(err), leaked) {
			t.Fatalf("transport failure leaked %q: error=%q details=%q", leaked, err.Error(), failureDetails(err))
		}
	}
}

func TestNewClientRejectsCredentialBearingAndNonHTTPBaseURL(t *testing.T) {
	t.Parallel()

	for _, rawURL := range []string{
		"https://user:secret@registry.example.test",
		"ftp://registry.example.test",
	} {
		_, err := NewClient(ClientConfig{BaseURL: rawURL})
		if err == nil {
			t.Fatalf("NewClient(%q) error = nil, want validation failure", rawURL)
		}
		for _, leaked := range []string{"user", "secret"} {
			if strings.Contains(err.Error(), leaked) {
				t.Fatalf("NewClient(%q) leaked %q in error: %v", rawURL, leaked, err)
			}
		}
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
