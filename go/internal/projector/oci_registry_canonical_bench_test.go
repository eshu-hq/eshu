// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// BenchmarkExtractOCIRegistryRows measures the typed-decode canonical extractor
// on a representative multi-repository OCI corpus so the migration from raw
// payloadString reads to the factschema seam carries a before/after
// no-regression number on the touched projection path. Each iteration extracts
// one repository plus manifest/index/descriptor/tag/referrer rows for
// benchOCIRepoCount repositories.
func BenchmarkExtractOCIRegistryRows(b *testing.B) {
	envelopes := benchOCIRegistryFacts(benchOCIRepoCount)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mat := &CanonicalMaterialization{}
		quarantined := extractOCIRegistryRows(mat, envelopes)
		if len(quarantined) != 0 {
			b.Fatalf("quarantined = %d, want 0 for an all-valid corpus", len(quarantined))
		}
		if len(mat.OCIImageManifests) != benchOCIRepoCount {
			b.Fatalf("manifests = %d, want %d", len(mat.OCIImageManifests), benchOCIRepoCount)
		}
	}
}

// benchOCIRepoCount is the number of synthetic repositories the benchmark
// corpus spans; each contributes six OCI facts (repository, manifest, index,
// descriptor, tag, referrer).
const benchOCIRepoCount = 1000

// benchOCIRegistryFacts builds a fully-valid OCI fact corpus for the benchmark:
// repoCount repositories, each with one repository fact and one of each
// digest-addressed and observation kind, so the extractor exercises every row
// builder and the shared decode seam on realistic input.
func benchOCIRegistryFacts(repoCount int) []facts.Envelope {
	observedAt := time.Date(2026, time.May, 12, 15, 0, 0, 0, time.UTC)
	envelopes := make([]facts.Envelope, 0, repoCount*6)
	for i := 0; i < repoCount; i++ {
		repoID := fmt.Sprintf("oci-registry://registry.example.com/team/api-%05d", i)
		manifestDigest := fmt.Sprintf("sha256:%064x", i*4+1)
		indexDigest := fmt.Sprintf("sha256:%064x", i*4+2)
		subjectDigest := fmt.Sprintf("sha256:%064x", i*4+3)
		referrerDigest := fmt.Sprintf("sha256:%064x", i*4+4)
		base := func(kind, schemaVersion string, payload map[string]any) facts.Envelope {
			return facts.Envelope{
				FactID:           fmt.Sprintf("%s-%05d", kind, i),
				ScopeID:          "oci-scope-1",
				GenerationID:     "oci-generation-1",
				FactKind:         kind,
				SchemaVersion:    schemaVersion,
				CollectorKind:    "oci_registry",
				SourceConfidence: facts.SourceConfidenceReported,
				ObservedAt:       observedAt,
				Payload:          payload,
			}
		}
		envelopes = append(envelopes,
			base(facts.OCIRegistryRepositoryFactKind, facts.OCIRegistryRepositorySchemaVersion, map[string]any{
				"repository_id": repoID,
				"provider":      "ghcr",
				"registry":      "registry.example.com",
				"repository":    fmt.Sprintf("team/api-%05d", i),
				"visibility":    "private",
			}),
			base(facts.OCIImageManifestFactKind, facts.OCIImageManifestSchemaVersion, map[string]any{
				"repository_id": repoID,
				"digest":        manifestDigest,
				"media_type":    "application/vnd.oci.image.manifest.v1+json",
				"size_bytes":    int64(1024),
				"config":        map[string]any{"digest": fmt.Sprintf("sha256:%064x", i*7+9)},
				"layers": []any{
					map[string]any{"digest": fmt.Sprintf("sha256:%064x", i*7+10)},
					map[string]any{"digest": fmt.Sprintf("sha256:%064x", i*7+11)},
				},
			}),
			base(facts.OCIImageIndexFactKind, facts.OCIImageIndexSchemaVersion, map[string]any{
				"repository_id": repoID,
				"digest":        indexDigest,
				"media_type":    "application/vnd.oci.image.index.v1+json",
				"manifests":     []any{map[string]any{"digest": manifestDigest}},
			}),
			base(facts.OCIImageDescriptorFactKind, facts.OCIImageDescriptorSchemaVersion, map[string]any{
				"repository_id": repoID,
				"digest":        manifestDigest,
				"media_type":    "application/vnd.oci.image.manifest.v1+json",
			}),
			base(facts.OCIImageTagObservationFactKind, facts.OCIImageTagObservationSchemaVersion, map[string]any{
				"repository_id":   repoID,
				"tag":             "prod",
				"resolved_digest": manifestDigest,
				"media_type":      "application/vnd.oci.image.manifest.v1+json",
			}),
			base(facts.OCIImageReferrerFactKind, facts.OCIImageReferrerSchemaVersion, map[string]any{
				"repository_id":   repoID,
				"subject_digest":  subjectDigest,
				"referrer_digest": referrerDigest,
				"artifact_type":   "application/vnd.example.sbom",
			}),
		)
	}
	return envelopes
}
