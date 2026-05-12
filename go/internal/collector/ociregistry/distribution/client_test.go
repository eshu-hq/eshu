package distribution

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
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
