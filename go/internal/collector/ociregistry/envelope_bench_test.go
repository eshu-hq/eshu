// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ociregistry

import (
	"testing"
	"time"
)

var benchmarkEnvelopeSink any

func BenchmarkNewManifestEnvelope(b *testing.B) {
	observation := ManifestObservation{
		Repository: RepositoryIdentity{
			Provider:   ProviderGHCR,
			Registry:   "ghcr.io",
			Repository: "eshu-hq/eshu",
		},
		Descriptor: Descriptor{
			Digest:       "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			MediaType:    MediaTypeOCIImageManifest,
			SizeBytes:    2048,
			ArtifactType: "application/vnd.example.scan",
		},
		Config: Descriptor{
			Digest:    "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			MediaType: "application/vnd.oci.image.config.v1+json",
			SizeBytes: 512,
		},
		ConfigLabels: map[string]string{
			"org.opencontainers.image.source": "https://github.com/eshu-hq/eshu",
		},
		Layers: []Descriptor{{
			Digest:    "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			MediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
			SizeBytes: 4096,
		}},
		SourceTag:           "latest",
		GenerationID:        "generation:oci:001",
		CollectorInstanceID: "ghcr-prod",
		FencingToken:        7,
		ObservedAt:          time.Date(2026, 7, 8, 2, 0, 0, 0, time.UTC),
		SourceURI:           "https://ghcr.io/v2/eshu-hq/eshu/manifests/latest",
	}

	b.ReportAllocs()
	for b.Loop() {
		envelope, err := NewManifestEnvelope(observation)
		if err != nil {
			b.Fatalf("NewManifestEnvelope() error = %v", err)
		}
		benchmarkEnvelopeSink = envelope
	}
}
