package ociruntime

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/distribution"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
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

func TestClaimedSourceNextClaimedScansMatchingTargetWithClaimGeneration(t *testing.T) {
	observedAt := time.Date(2026, 5, 13, 15, 30, 0, 0, time.UTC)
	client := &stubRegistryClient{
		tags: []string{"latest"},
		manifest: distribution.ManifestResponse{
			Digest:    testManifestDigest,
			MediaType: ociregistry.MediaTypeOCIImageManifest,
			Body:      testManifestBody(t),
			SizeBytes: 512,
		},
	}
	source := ClaimedSource{
		Source: Source{
			Config: Config{
				CollectorInstanceID: "oci-runtime-test",
				Targets: []TargetConfig{{
					Provider:   ociregistry.ProviderDockerHub,
					Registry:   "registry-1.docker.io",
					Repository: "library/busybox",
					References: []string{"latest"},
					TagLimit:   1,
				}},
			},
			ClientFactory: ClientFactoryFunc(func(context.Context, TargetConfig) (RegistryClient, error) {
				return client, nil
			}),
			Clock: func() time.Time { return observedAt },
		},
	}
	item := workflow.WorkItem{
		WorkItemID:          "oci-item-1",
		RunID:               "oci-run-1",
		CollectorKind:       scope.CollectorOCIRegistry,
		CollectorInstanceID: "oci-runtime-test",
		SourceSystem:        string(scope.CollectorOCIRegistry),
		ScopeID:             "oci-registry://registry-1.docker.io/library/busybox",
		AcceptanceUnitID:    "oci-registry://registry-1.docker.io/library/busybox",
		SourceRunID:         "oci-generation-1",
		GenerationID:        "oci-generation-1",
		CurrentFencingToken: 9,
		Status:              workflow.WorkItemStatusClaimed,
		CreatedAt:           observedAt,
		UpdatedAt:           observedAt,
	}

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if got, want := collected.Generation.GenerationID, item.GenerationID; got != want {
		t.Fatalf("GenerationID = %q, want %q", got, want)
	}
	envelopes := drainFacts(t, collected)
	if got, want := envelopes[0].FencingToken, int64(9); got != want {
		t.Fatalf("first envelope FencingToken = %d, want %d", got, want)
	}
}

func TestSourceNextDoesNotLeakReferenceWhenManifestFetchFails(t *testing.T) {
	t.Parallel()

	client := &stubRegistryClient{
		tags:        []string{"private-prod"},
		manifestErr: collector.RegistryHTTPFailure("oci", "", "get_manifest", 404, nil),
	}
	source := Source{
		Config: Config{
			CollectorInstanceID: "oci-runtime-test",
			Targets: []TargetConfig{{
				Provider:   ociregistry.ProviderGHCR,
				Registry:   "ghcr.io",
				Repository: "secret/team-api",
				TagLimit:   1,
			}},
		},
		ClientFactory: ClientFactoryFunc(func(context.Context, TargetConfig) (RegistryClient, error) {
			return client, nil
		}),
	}

	_, _, err := source.Next(context.Background())
	if err == nil {
		t.Fatal("Next() error = nil, want manifest fetch failure")
	}
	if got := failureClass(err); got != collector.RegistryFailureNotFound {
		t.Fatalf("FailureClass() = %q, want %q; error = %v", got, collector.RegistryFailureNotFound, err)
	}
	for _, leaked := range []string{"private-prod", "secret/team-api", "ghcr.io"} {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("Next() error leaked %q: %q", leaked, err.Error())
		}
	}
}

func TestClaimedSourceNextClaimedRejectsDuplicateMatchingTargets(t *testing.T) {
	observedAt := time.Date(2026, 5, 13, 15, 30, 0, 0, time.UTC)
	source := ClaimedSource{
		Source: Source{
			Config: Config{
				CollectorInstanceID: "oci-runtime-test",
				Targets: []TargetConfig{
					{
						Provider:   ociregistry.ProviderDockerHub,
						Registry:   "https://docker.io",
						Repository: "library/busybox",
						References: []string{"latest"},
					},
					{
						Provider:   ociregistry.ProviderDockerHub,
						Registry:   "docker.io",
						Repository: "library/busybox",
						References: []string{"stable"},
					},
				},
			},
			ClientFactory: ClientFactoryFunc(func(context.Context, TargetConfig) (RegistryClient, error) {
				t.Fatal("ClientFactory should not be called for duplicate claimed targets")
				return nil, nil
			}),
			Clock: func() time.Time { return observedAt },
		},
	}
	item := workflow.WorkItem{
		WorkItemID:          "oci-item-1",
		RunID:               "oci-run-1",
		CollectorKind:       scope.CollectorOCIRegistry,
		CollectorInstanceID: "oci-runtime-test",
		SourceSystem:        string(scope.CollectorOCIRegistry),
		ScopeID:             "oci-registry://docker.io/library/busybox",
		AcceptanceUnitID:    "oci-registry://docker.io/library/busybox",
		SourceRunID:         "oci-generation-1",
		GenerationID:        "oci-generation-1",
		CurrentFencingToken: 9,
		Status:              workflow.WorkItemStatusClaimed,
		CreatedAt:           observedAt,
		UpdatedAt:           observedAt,
	}

	if _, _, err := source.NextClaimed(context.Background(), item); err == nil {
		t.Fatal("NextClaimed() error = nil, want duplicate target rejection")
	}
}

const (
	testManifestDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	testConfigDigest   = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	testLayerDigest    = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	testReferrerDigest = "sha256:4444444444444444444444444444444444444444444444444444444444444444"
)

type stubRegistryClient struct {
	tags        []string
	manifest    distribution.ManifestResponse
	manifestErr error
	referrers   distribution.ReferrersResponse
}

func (s *stubRegistryClient) Ping(context.Context) error { return nil }

func (s *stubRegistryClient) ListTags(context.Context, string) ([]string, error) {
	return append([]string(nil), s.tags...), nil
}

func (s *stubRegistryClient) GetManifest(context.Context, string, string) (distribution.ManifestResponse, error) {
	if s.manifestErr != nil {
		return distribution.ManifestResponse{}, s.manifestErr
	}
	return s.manifest, nil
}

func (s *stubRegistryClient) ListReferrers(context.Context, string, string) (distribution.ReferrersResponse, error) {
	return s.referrers, nil
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
