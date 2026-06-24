// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ociruntime

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/distribution"
	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// tlsRegistryServer starts an httptest TLS server that speaks the minimum OCI
// Distribution surface the collector exercises (ping, tags, manifest), backed
// by an ephemeral self-signed certificate minted at test runtime. The CA PEM is
// returned for the collector to trust; no key material touches the repository.
func tlsRegistryServer(t *testing.T, repository, reference, digest string) (*httptest.Server, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "eshu-local-registry"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/":
			w.WriteHeader(http.StatusOK)
		case "/v2/" + repository + "/tags/list":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"` + repository + `","tags":["` + reference + `"]}`))
		case "/v2/" + repository + "/manifests/" + reference:
			w.Header().Set("Docker-Content-Digest", digest)
			w.Header().Set("Content-Type", ociregistry.MediaTypeOCIImageManifest)
			_, _ = w.Write(testManifestBody(t))
		case "/v2/" + repository + "/referrers/" + digest:
			w.Header().Set("Content-Type", ociregistry.MediaTypeOCIImageIndex)
			_, _ = w.Write([]byte(`{"manifests":[]}`))
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server, caPEM
}

// localTLSClientFactory wires a TargetConfig to the real distribution client
// using the target's resolved HTTP client. It mirrors what the production
// provider factory does for a generic Distribution registry, so the test proves
// the real transport path rather than a stub.
type localTLSClientFactory struct {
	baseURL string
}

func (f localTLSClientFactory) Client(_ context.Context, target TargetConfig) (RegistryClient, error) {
	httpClient, _, err := target.HTTPClient()
	if err != nil {
		return nil, err
	}
	return distribution.NewClient(distribution.ClientConfig{
		BaseURL: f.baseURL,
		Client:  httpClient,
	})
}

// TestSourceScansLocalTLSRegistryWithCustomCA proves the issue #3080 acceptance:
// a localhost TLS registry can be scanned and emits an image-identity manifest
// fact with the registry-reported digest when the collector trusts a custom CA,
// while the default system-roots path is correctly rejected.
func TestSourceScansLocalTLSRegistryWithCustomCA(t *testing.T) {
	const (
		repository = "library/demo"
		reference  = "1.0.0"
	)
	server, caPEM := tlsRegistryServer(t, repository, reference, testManifestDigest)

	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, caPEM, 0o600); err != nil {
		t.Fatalf("write CA bundle: %v", err)
	}

	target := TargetConfig{
		Provider:   ociregistry.ProviderHarbor,
		Registry:   server.URL,
		Repository: repository,
		References: []string{reference},
		TagLimit:   1,
		Visibility: ociregistry.VisibilityPrivate,
		AuthMode:   ociregistry.AuthModeAnonymous,
		SourceURI:  server.URL + "/v2/" + repository,
		TLS:        TLSConfig{CACertPath: caPath},
	}

	source := Source{
		Config: Config{
			CollectorInstanceID: "oci-localtls-test",
			Targets:             []TargetConfig{target},
		},
		ClientFactory: localTLSClientFactory{baseURL: server.URL},
		Clock:         func() time.Time { return time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC) },
	}

	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() against local TLS registry error = %v", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want a collected generation")
	}

	envelopes := drainFacts(t, collected)
	identityDigest := imageIdentityDigest(t, envelopes)
	if identityDigest != testManifestDigest {
		t.Fatalf("image-identity digest = %q, want %q", identityDigest, testManifestDigest)
	}
}

// TestSourceRejectsLocalTLSRegistryWithoutCustomCA proves the negative path: the
// same registry fails the scan when no custom CA is configured, confirming the
// custom-CA knob is load-bearing and not a no-op.
func TestSourceRejectsLocalTLSRegistryWithoutCustomCA(t *testing.T) {
	const (
		repository = "library/demo"
		reference  = "1.0.0"
	)
	server, _ := tlsRegistryServer(t, repository, reference, testManifestDigest)

	target := TargetConfig{
		Provider:   ociregistry.ProviderHarbor,
		Registry:   server.URL,
		Repository: repository,
		References: []string{reference},
		TagLimit:   1,
		SourceURI:  server.URL + "/v2/" + repository,
	}
	source := Source{
		Config: Config{
			CollectorInstanceID: "oci-localtls-negative",
			Targets:             []TargetConfig{target},
		},
		ClientFactory: localTLSClientFactory{baseURL: server.URL},
		Clock:         func() time.Time { return time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC) },
	}

	if _, _, err := source.Next(context.Background()); err == nil {
		t.Fatal("Next() error = nil, want certificate verification failure without a trusted CA")
	}
}

func TestTargetTLSModeResolution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		target TargetConfig
		want   sdk.TLSMode
	}{
		{name: "default", target: TargetConfig{}, want: sdk.TLSModeSystem},
		{name: "custom ca", target: TargetConfig{TLS: TLSConfig{CACertPath: "/tmp/ca.pem"}}, want: sdk.TLSModeCustomCA},
		{name: "insecure", target: TargetConfig{TLS: TLSConfig{InsecureSkipVerify: true}}, want: sdk.TLSModeInsecureSkipVerify},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.target.TLSMode(); got != tt.want {
				t.Fatalf("TLSMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

// imageIdentityDigest extracts the registry-reported digest from the emitted
// image-identity fact. A scanned reference resolves to either a single-arch
// image manifest or a multi-arch image index; both carry the image digest that
// the supply-chain image-identity hop depends on, so either fact satisfies the
// proof.
func imageIdentityDigest(t *testing.T, envelopes []facts.Envelope) string {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.OCIImageManifestFactKind &&
			envelope.FactKind != facts.OCIImageIndexFactKind {
			continue
		}
		digest, ok := envelope.Payload["digest"].(string)
		if !ok {
			t.Fatalf("%q fact payload digest type = %T, want string", envelope.FactKind, envelope.Payload["digest"])
		}
		return digest
	}
	t.Fatalf("no image-identity (%q or %q) fact found in %d envelopes",
		facts.OCIImageManifestFactKind, facts.OCIImageIndexFactKind, len(envelopes))
	return ""
}
