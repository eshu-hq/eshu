package ociruntime

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/distribution"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestSourceNextEmitsCollectedGenerationForRegistryTarget(t *testing.T) {
	observedAt := time.Date(2026, 5, 12, 14, 0, 0, 0, time.UTC)
	client := &stubRegistryClient{
		tags: []string{"latest"},
		manifest: distribution.ManifestResponse{
			Digest:    testManifestDigest,
			MediaType: ociregistry.MediaTypeOCIImageManifest,
			Body:      testManifestBody(t),
			SizeBytes: 512,
		},
		referrers: distribution.ReferrersResponse{Referrers: []ociregistry.Descriptor{{
			Digest:       testReferrerDigest,
			MediaType:    ociregistry.MediaTypeOCIImageManifest,
			SizeBytes:    128,
			ArtifactType: "application/vnd.in-toto+json",
		}}},
	}

	source := Source{
		Config: Config{
			CollectorInstanceID: "oci-runtime-test",
			Targets: []TargetConfig{{
				Provider:     ociregistry.ProviderDockerHub,
				Registry:     "registry-1.docker.io",
				Repository:   "library/busybox",
				TagLimit:     1,
				Visibility:   ociregistry.VisibilityPublic,
				AuthMode:     ociregistry.AuthModeAnonymous,
				SourceURI:    "https://registry-1.docker.io/v2/library/busybox",
				FencingToken: 7,
			}},
		},
		ClientFactory: ClientFactoryFunc(func(context.Context, TargetConfig) (RegistryClient, error) {
			return client, nil
		}),
		Clock: func() time.Time { return observedAt },
	}

	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want true")
	}
	if got, want := collected.Scope.ScopeKind, scope.KindContainerRegistryRepository; got != want {
		t.Fatalf("ScopeKind = %q, want %q", got, want)
	}
	if got, want := collected.Scope.CollectorKind, scope.CollectorOCIRegistry; got != want {
		t.Fatalf("CollectorKind = %q, want %q", got, want)
	}
	if got, want := collected.Generation.GenerationID[:13], "oci-registry:"; got != want {
		t.Fatalf("GenerationID prefix = %q, want %q", got, want)
	}

	envelopes := drainFacts(t, collected)
	if got, want := collected.FactCount, len(envelopes); got != want {
		t.Fatalf("FactCount = %d, want %d", got, want)
	}
	kinds := make([]string, 0, len(envelopes))
	for _, envelope := range envelopes {
		kinds = append(kinds, envelope.FactKind)
		if envelope.GenerationID != collected.Generation.GenerationID {
			t.Fatalf("fact %q GenerationID = %q, want %q", envelope.FactKind, envelope.GenerationID, collected.Generation.GenerationID)
		}
	}
	for _, want := range []string{
		facts.OCIRegistryRepositoryFactKind,
		facts.OCIImageTagObservationFactKind,
		facts.OCIImageManifestFactKind,
		facts.OCIImageDescriptorFactKind,
		facts.OCIImageReferrerFactKind,
	} {
		if !slices.Contains(kinds, want) {
			t.Fatalf("fact kinds = %v, want %q", kinds, want)
		}
	}
}

func TestSourceNextComputesDigestAndWarnsWhenManifestDigestHeaderIsMissing(t *testing.T) {
	client := &stubRegistryClient{
		tags: []string{"latest"},
		manifest: distribution.ManifestResponse{
			MediaType: ociregistry.MediaTypeOCIImageManifest,
			Body:      testManifestBody(t),
		},
	}
	source := Source{
		Config: Config{
			CollectorInstanceID: "oci-runtime-test",
			Targets: []TargetConfig{{
				Provider:   ociregistry.ProviderGHCR,
				Registry:   "ghcr.io",
				Repository: "stargz-containers/busybox",
				TagLimit:   1,
			}},
		},
		ClientFactory: ClientFactoryFunc(func(context.Context, TargetConfig) (RegistryClient, error) {
			return client, nil
		}),
		Clock: func() time.Time { return time.Date(2026, 5, 12, 14, 0, 0, 0, time.UTC) },
	}

	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want true")
	}
	envelopes := drainFacts(t, collected)
	kinds := factKinds(envelopes)
	if !slices.Contains(kinds, facts.OCIImageManifestFactKind) {
		t.Fatalf("fact kinds = %v, want manifest fact", kinds)
	}
	if !slices.Contains(kinds, facts.OCIImageTagObservationFactKind) {
		t.Fatalf("fact kinds = %v, want tag observation fact", kinds)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.OCIRegistryWarningFactKind {
			return
		}
	}
	t.Fatalf("fact kinds = %v, want warning fact", kinds)
}

const (
	testManifestDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	testConfigDigest   = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	testLayerDigest    = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	testReferrerDigest = "sha256:4444444444444444444444444444444444444444444444444444444444444444"
)

type stubRegistryClient struct {
	tags      []string
	manifest  distribution.ManifestResponse
	referrers distribution.ReferrersResponse
}

func (s *stubRegistryClient) Ping(context.Context) error { return nil }

func (s *stubRegistryClient) ListTags(context.Context, string) ([]string, error) {
	return append([]string(nil), s.tags...), nil
}

func (s *stubRegistryClient) GetManifest(context.Context, string, string) (distribution.ManifestResponse, error) {
	return s.manifest, nil
}

func (s *stubRegistryClient) ListReferrers(context.Context, string, string) (distribution.ReferrersResponse, error) {
	return s.referrers, nil
}

func testManifestBody(t *testing.T) []byte {
	t.Helper()
	body := map[string]any{
		"schemaVersion": 2,
		"mediaType":     ociregistry.MediaTypeOCIImageManifest,
		"config": map[string]any{
			"mediaType": "application/vnd.oci.image.config.v1+json",
			"digest":    testConfigDigest,
			"size":      256,
		},
		"layers": []map[string]any{{
			"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
			"digest":    testLayerDigest,
			"size":      1024,
		}},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal test manifest: %v", err)
	}
	return encoded
}

func drainFacts(t *testing.T, collected collector.CollectedGeneration) []facts.Envelope {
	t.Helper()
	envelopes := make([]facts.Envelope, 0, collected.FactCount)
	for envelope := range collected.Facts {
		envelopes = append(envelopes, envelope)
	}
	return envelopes
}

func factKinds(envelopes []facts.Envelope) []string {
	kinds := make([]string, 0, len(envelopes))
	for _, envelope := range envelopes {
		kinds = append(kinds, envelope.FactKind)
	}
	return kinds
}
