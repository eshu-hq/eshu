package cypher

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

func TestCanonicalNodeWriterBuildsPackageRegistryStatements(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 2, nil)
	mat := projector.CanonicalMaterialization{
		ScopeID:      "package-registry-scope-1",
		GenerationID: "package-registry-generation-1",
		PackageRegistryPackages: []projector.PackageRegistryPackageRow{{
			UID:                 "package://npm/registry.npmjs.org/@scope/pkg",
			Ecosystem:           "npm",
			Registry:            "https://registry.npmjs.org",
			RawName:             "@scope/pkg",
			NormalizedName:      "@scope/pkg",
			Namespace:           "scope",
			Classifier:          "library",
			Visibility:          "public",
			SourceFactID:        "package-registry-package-1",
			StableFactKey:       "package://npm/registry.npmjs.org/@scope/pkg",
			SourceSystem:        "package_registry",
			SourceRecordID:      "package://npm/registry.npmjs.org/@scope/pkg",
			SourceConfidence:    facts.SourceConfidenceReported,
			CollectorKind:       "package_registry",
			CorrelationAnchors:  []string{"package://npm/registry.npmjs.org/@scope/pkg"},
			CollectorInstanceID: "package-registry-collector-1",
		}},
		PackageRegistryVersions: []projector.PackageRegistryVersionRow{{
			UID:                 "package://npm/registry.npmjs.org/@scope/pkg@1.2.3",
			PackageID:           "package://npm/registry.npmjs.org/@scope/pkg",
			Ecosystem:           "npm",
			Registry:            "https://registry.npmjs.org",
			Version:             "1.2.3",
			PublishedAt:         time.Date(2026, time.May, 13, 13, 0, 0, 0, time.UTC),
			ArtifactURLs:        []string{"https://registry.npmjs.org/@scope/pkg/-/pkg-1.2.3.tgz"},
			Checksums:           map[string]string{"sha512": "sha512-test"},
			SourceFactID:        "package-registry-version-1",
			StableFactKey:       "package://npm/registry.npmjs.org/@scope/pkg@1.2.3",
			SourceSystem:        "package_registry",
			SourceRecordID:      "package://npm/registry.npmjs.org/@scope/pkg@1.2.3",
			SourceConfidence:    facts.SourceConfidenceReported,
			CollectorKind:       "package_registry",
			CorrelationAnchors:  []string{"package://npm/registry.npmjs.org/@scope/pkg"},
			CollectorInstanceID: "package-registry-collector-1",
		}},
	}

	statements := writer.buildPackageRegistryStatements(mat)
	if got, want := len(statements), 2; got != want {
		t.Fatalf("buildPackageRegistryStatements() count = %d, want %d", got, want)
	}

	pkg := statements[0]
	if !strings.Contains(pkg.Cypher, "MERGE (p:Package:PackageRegistryPackage {uid: row.uid})") {
		t.Fatalf("package Cypher = %q, want Package uid merge", pkg.Cypher)
	}
	if strings.Contains(pkg.Cypher, "Repository") {
		t.Fatalf("package Cypher = %q, must not infer repository ownership", pkg.Cypher)
	}
	if got, want := pkg.Parameters[StatementMetadataPhaseKey], canonicalPhasePackageRegistry; got != want {
		t.Fatalf("package phase = %#v, want %#v", got, want)
	}

	version := statements[1]
	if !strings.Contains(version.Cypher, "MATCH (p:Package {uid: row.package_id})") {
		t.Fatalf("version Cypher = %q, want package uid anchor", version.Cypher)
	}
	if !strings.Contains(version.Cypher, "MERGE (v:PackageVersion:PackageRegistryPackageVersion {uid: row.uid})") {
		t.Fatalf("version Cypher = %q, want PackageVersion uid merge", version.Cypher)
	}
	if !strings.Contains(version.Cypher, "MERGE (p)-[rel:HAS_VERSION]->(v)") {
		t.Fatalf("version Cypher = %q, want package version edge", version.Cypher)
	}
	if strings.Contains(version.Cypher, "SourceHint") || strings.Contains(version.Cypher, "Repository") {
		t.Fatalf("version Cypher = %q, must not promote source hints to ownership", version.Cypher)
	}
}
