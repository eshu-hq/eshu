package ociruntime

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/distribution"
)

// TestLiveLocalTLSRegistryImageIdentity scans a real localhost TLS OCI registry
// through the full Source path and asserts an image-identity manifest fact with
// a non-empty digest. It is the in-process half of the demo proof script
// examples/supply-chain-demo/scripts/run-oci-localtls-identity-proof.sh, which
// stands up a registry:2 over TLS and exports these variables before invoking
// this test. It skips unless ESHU_OCI_LOCALTLS_LIVE=1 so CI never depends on a
// running registry.
func TestLiveLocalTLSRegistryImageIdentity(t *testing.T) {
	if os.Getenv("ESHU_OCI_LOCALTLS_LIVE") != "1" {
		t.Skip("set ESHU_OCI_LOCALTLS_LIVE=1 to run the local TLS registry image-identity proof")
	}
	baseURL := strings.TrimSpace(os.Getenv("ESHU_OCI_LOCALTLS_URL"))
	repository := strings.TrimSpace(os.Getenv("ESHU_OCI_LOCALTLS_REPOSITORY"))
	reference := strings.TrimSpace(os.Getenv("ESHU_OCI_LOCALTLS_REFERENCE"))
	caPath := strings.TrimSpace(os.Getenv("ESHU_OCI_LOCALTLS_CA_CERT_PATH"))
	if baseURL == "" || repository == "" || reference == "" || caPath == "" {
		t.Fatal("ESHU_OCI_LOCALTLS_URL, _REPOSITORY, _REFERENCE, and _CA_CERT_PATH are required")
	}

	target := TargetConfig{
		Provider:   ociregistry.ProviderHarbor,
		Registry:   baseURL,
		Repository: repository,
		References: []string{reference},
		TagLimit:   1,
		Visibility: ociregistry.VisibilityPrivate,
		AuthMode:   ociregistry.AuthModeAnonymous,
		SourceURI:  baseURL + "/v2/" + repository,
		TLS:        TLSConfig{CACertPath: caPath},
	}

	source := Source{
		Config: Config{
			CollectorInstanceID: "oci-localtls-live",
			Targets:             []TargetConfig{target},
		},
		ClientFactory: ClientFactoryFunc(func(_ context.Context, tc TargetConfig) (RegistryClient, error) {
			httpClient, mode, err := tc.HTTPClient()
			if err != nil {
				return nil, err
			}
			t.Logf("resolved tls_mode=%s for %s", mode, baseURL)
			return distribution.NewClient(distribution.ClientConfig{BaseURL: baseURL, Client: httpClient})
		}),
		Clock: func() time.Time { return time.Now().UTC() },
	}

	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() against live local TLS registry error = %v", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want a collected generation")
	}

	envelopes := drainFacts(t, collected)
	digest := imageIdentityDigest(t, envelopes)
	if !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("image-identity digest = %q, want a sha256 digest", digest)
	}
	t.Logf("PROVEN: local TLS registry %s/%s:%s emitted an image-identity fact with digest %s",
		baseURL, repository, reference, digest)
}
