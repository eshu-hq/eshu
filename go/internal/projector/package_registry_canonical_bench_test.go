// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// BenchmarkExtractPackageRegistryRows measures the typed-decode canonical
// extractor on a representative multi-package corpus so the migration from raw
// payloadString/payloadBoolPtr/payloadStringSlice reads to the factschema seam
// carries a before/after no-regression number on the touched projection path.
// Each iteration extracts one package, one version, and one dependency row for
// benchPackageRegistryCount packages.
func BenchmarkExtractPackageRegistryRows(b *testing.B) {
	envelopes := benchPackageRegistryFacts(benchPackageRegistryCount)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mat := &CanonicalMaterialization{}
		quarantined := extractPackageRegistryRows(mat, envelopes)
		if len(quarantined) != 0 {
			b.Fatalf("quarantined = %d, want 0 for an all-valid corpus", len(quarantined))
		}
		if len(mat.PackageRegistryPackages) != benchPackageRegistryCount {
			b.Fatalf("packages = %d, want %d", len(mat.PackageRegistryPackages), benchPackageRegistryCount)
		}
	}
}

// benchPackageRegistryCount is the number of synthetic packages the benchmark
// corpus spans; each contributes three package_registry facts (package,
// package_version, package_dependency), matching benchOCIRepoCount's 1,000
// scale so the two families' before/after numbers are comparable.
const benchPackageRegistryCount = 1000

// benchPackageRegistryFacts builds a fully-valid package_registry fact corpus
// for the benchmark: packageCount packages, each with one package fact, one
// version fact, and one dependency fact, so the extractor exercises every row
// builder and the shared decode seam on realistic input.
func benchPackageRegistryFacts(packageCount int) []facts.Envelope {
	observedAt := time.Date(2026, time.May, 12, 15, 0, 0, 0, time.UTC)
	envelopes := make([]facts.Envelope, 0, packageCount*3)
	for i := 0; i < packageCount; i++ {
		packageID := fmt.Sprintf("package://npm/registry.npmjs.org/pkg-%05d", i)
		versionID := packageID + "@1.0.0"
		dependencyPackageID := fmt.Sprintf("package://npm/registry.npmjs.org/dep-%05d", i)
		base := func(kind, schemaVersion string, payload map[string]any) facts.Envelope {
			return facts.Envelope{
				FactID:           fmt.Sprintf("%s-%05d", kind, i),
				ScopeID:          "package-registry-bench-scope-1",
				GenerationID:     "package-registry-bench-generation-1",
				FactKind:         kind,
				StableFactKey:    fmt.Sprintf("%s-%05d", kind, i),
				SchemaVersion:    schemaVersion,
				CollectorKind:    "package_registry",
				SourceConfidence: facts.SourceConfidenceReported,
				ObservedAt:       observedAt,
				Payload:          payload,
			}
		}
		envelopes = append(envelopes,
			base(facts.PackageRegistryPackageFactKind, facts.PackageRegistryPackageSchemaVersion, map[string]any{
				"package_id":      packageID,
				"ecosystem":       "npm",
				"registry":        "https://registry.npmjs.org",
				"raw_name":        fmt.Sprintf("pkg-%05d", i),
				"normalized_name": fmt.Sprintf("pkg-%05d", i),
				"purl":            fmt.Sprintf("pkg:npm/pkg-%05d", i),
				"package_manager": "npm",
				"visibility":      "public",
			}),
			base(facts.PackageRegistryPackageVersionFactKind, facts.PackageRegistryPackageVersionSchemaVersion, map[string]any{
				"package_id":    packageID,
				"version_id":    versionID,
				"version":       "1.0.0",
				"ecosystem":     "npm",
				"registry":      "https://registry.npmjs.org",
				"purl":          fmt.Sprintf("pkg:npm/pkg-%05d@1.0.0", i),
				"is_yanked":     false,
				"is_unlisted":   false,
				"is_deprecated": false,
				"is_retracted":  false,
				"artifact_urls": []any{
					fmt.Sprintf("https://registry.npmjs.org/pkg-%05d/-/pkg-%05d-1.0.0.tgz", i, i),
				},
				"checksums": map[string]any{"sha512": "sha512-bench"},
			}),
			base(facts.PackageRegistryPackageDependencyFactKind, facts.PackageRegistryPackageDependencySchemaVersion, map[string]any{
				"package_id":            packageID,
				"version_id":            versionID,
				"version":               "1.0.0",
				"dependency_package_id": dependencyPackageID,
				"dependency_ecosystem":  "npm",
				"dependency_registry":   "https://registry.npmjs.org",
				"dependency_normalized": fmt.Sprintf("dep-%05d", i),
				"dependency_purl":       fmt.Sprintf("pkg:npm/dep-%05d", i),
				"dependency_type":       "runtime",
				"optional":              false,
				"excluded":              false,
			}),
		)
	}
	return envelopes
}
