// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// This file holds the package_registry.package_artifact-specific canonical
// extraction tests and fixture, split out of package_registry_canonical_test.go
// to stay under the package's 500-line-per-file convention (mirrors
// package_registry_canonical_artifact.go's split from
// package_registry_canonical.go). The shared package/version/dependency
// fixtures and scope/generation helpers (packageRegistryScope,
// packageRegistryGeneration, packageRegistryFacts, packageRegistryPackageID,
// packageRegistryVersionID, packageRegistryPublishedAt) stay in
// package_registry_canonical_test.go and are used here unqualified since both
// files are in package projector.

func TestBuildCanonicalMaterializationExtractsPackageRegistryArtifacts(t *testing.T) {
	t.Parallel()

	result, quarantined := buildCanonicalMaterialization(
		packageRegistryScope(),
		packageRegistryGeneration(),
		append(packageRegistryFacts(), packageRegistryArtifactFact()),
	)

	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %+v, want none", quarantined)
	}
	if got, want := len(result.PackageRegistryArtifacts), 1; got != want {
		t.Fatalf("len(PackageRegistryArtifacts) = %d, want %d", got, want)
	}
	artifact := result.PackageRegistryArtifacts[0]
	if got, want := artifact.UID, "package-registry-artifact-1"; got != want {
		t.Fatalf("artifact UID = %q, want %q", got, want)
	}
	if got, want := artifact.PackageID, packageRegistryPackageID(); got != want {
		t.Fatalf("artifact PackageID = %q, want %q", got, want)
	}
	if got, want := artifact.VersionID, packageRegistryVersionID(); got != want {
		t.Fatalf("artifact VersionID = %q, want %q", got, want)
	}
	if got, want := artifact.ArtifactKey, "pkg-1.2.3.tgz"; got != want {
		t.Fatalf("artifact ArtifactKey = %q, want %q", got, want)
	}
	if got, want := artifact.ArtifactType, "tarball"; got != want {
		t.Fatalf("artifact ArtifactType = %q, want %q", got, want)
	}
	if got, want := artifact.SizeBytes, int64(4096); got != want {
		t.Fatalf("artifact SizeBytes = %d, want %d", got, want)
	}
	if got, want := artifact.Hashes["sha256"], "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"; got != want {
		t.Fatalf("artifact Hashes[sha256] = %q, want %q", got, want)
	}
	if got, want := artifact.Hashes["sha512"], "cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3"; got != want {
		t.Fatalf("artifact Hashes[sha512] = %q, want %q", got, want)
	}
}

func TestBuildCanonicalMaterializationQuarantinesPackageRegistryArtifactMissingIdentity(t *testing.T) {
	t.Parallel()

	artifactFact := packageRegistryArtifactFact()
	delete(artifactFact.Payload, "artifact_key")
	result, quarantined := buildCanonicalMaterialization(
		packageRegistryScope(),
		packageRegistryGeneration(),
		append(packageRegistryFacts(), artifactFact),
	)

	if got := len(result.PackageRegistryArtifacts); got != 0 {
		t.Fatalf("len(PackageRegistryArtifacts) = %d, want 0 for missing artifact_key", got)
	}
	if got, want := len(quarantined), 1; got != want {
		t.Fatalf("len(quarantined) = %d, want %d", got, want)
	}
	if got, want := quarantined[0].factID, "package-registry-artifact-1"; got != want {
		t.Fatalf("quarantined[0].factID = %q, want %q", got, want)
	}
	if got, want := quarantined[0].field, "artifact_key"; got != want {
		t.Fatalf("quarantined[0].field = %q, want %q", got, want)
	}
}

// TestBuildCanonicalMaterializationExtractsPackageRegistryArtifactColonBearingHashAlgorithm
// is the end-to-end regression for the #5820 P2 review finding: an earlier
// version of DecodePackageRegistryPackageArtifact rejected a colon-bearing
// hashes algorithm name as input_invalid, silently narrowing the public v1
// contract (package_registry.package_artifact.v1.schema.json's
// hashes.additionalProperties accepts any string key). The fix moves
// unambiguity from rejection to a lossless escaping scheme in the canonical
// graph writer (packageRegistryHashPairs in
// go/internal/storage/cypher/package_registry_artifact_writer.go), so a
// colon-bearing algorithm name now projects cleanly all the way through the
// row builder instead of quarantining.
func TestBuildCanonicalMaterializationExtractsPackageRegistryArtifactColonBearingHashAlgorithm(t *testing.T) {
	t.Parallel()

	artifactFact := packageRegistryArtifactFact()
	artifactFact.Payload["hashes"] = map[string]any{
		"sha256:extra": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}
	result, quarantined := buildCanonicalMaterialization(
		packageRegistryScope(),
		packageRegistryGeneration(),
		append(packageRegistryFacts(), artifactFact),
	)

	if got, want := len(quarantined), 0; got != want {
		t.Fatalf("quarantined = %+v, want none for a colon-bearing hash algorithm (v1 schema allows any string key)", quarantined)
	}
	if got, want := len(result.PackageRegistryArtifacts), 1; got != want {
		t.Fatalf("len(PackageRegistryArtifacts) = %d, want %d", got, want)
	}
	if got, want := result.PackageRegistryArtifacts[0].Hashes["sha256:extra"], "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"; got != want {
		t.Fatalf("Hashes[%q] = %q, want %q", "sha256:extra", got, want)
	}
}

// TestBuildCanonicalMaterializationQuarantinesPackageRegistryArtifactConflictingWhitespaceHashKeys
// is the end-to-end regression for the #5820 P2 review finding: two hashes
// keys that normalize to the same trimmed algorithm name via whitespace (here
// "sha256" and " sha256 ") but carry DIFFERENT digests must dead-letter
// deterministically rather than silently keeping whichever value Go's
// randomized map iteration order visited last -- the worst class of defect in
// this repo, a projected graph value that can differ between otherwise
// identical runs of the same fact.
func TestBuildCanonicalMaterializationQuarantinesPackageRegistryArtifactConflictingWhitespaceHashKeys(t *testing.T) {
	t.Parallel()

	artifactFact := packageRegistryArtifactFact()
	artifactFact.Payload["hashes"] = map[string]any{
		"sha256":   "aaa",
		" sha256 ": "bbb",
	}
	result, quarantined := buildCanonicalMaterialization(
		packageRegistryScope(),
		packageRegistryGeneration(),
		append(packageRegistryFacts(), artifactFact),
	)

	if got := len(result.PackageRegistryArtifacts); got != 0 {
		t.Fatalf("len(PackageRegistryArtifacts) = %d, want 0 for a whitespace-collision hash-key conflict", got)
	}
	if got, want := len(quarantined), 1; got != want {
		t.Fatalf("len(quarantined) = %d, want %d", got, want)
	}
	if got, want := quarantined[0].factID, "package-registry-artifact-1"; got != want {
		t.Fatalf("quarantined[0].factID = %q, want %q", got, want)
	}
	if got, want := quarantined[0].classification, "input_invalid"; got != want {
		t.Fatalf("quarantined[0].classification = %q, want %q", got, want)
	}
}

// TestBuildCanonicalMaterializationMergesPackageRegistryArtifactIdenticalWhitespaceHashKeys
// is the positive counterpart: two hashes keys that normalize to the same
// trimmed algorithm name AND agree on the value are not a conflict, so they
// collapse to one hashes entry instead of dead-lettering.
func TestBuildCanonicalMaterializationMergesPackageRegistryArtifactIdenticalWhitespaceHashKeys(t *testing.T) {
	t.Parallel()

	artifactFact := packageRegistryArtifactFact()
	artifactFact.Payload["hashes"] = map[string]any{
		"sha256":   "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		" sha256 ": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}
	result, quarantined := buildCanonicalMaterialization(
		packageRegistryScope(),
		packageRegistryGeneration(),
		append(packageRegistryFacts(), artifactFact),
	)

	if got, want := len(quarantined), 0; got != want {
		t.Fatalf("quarantined = %+v, want none for an identical-value whitespace collision", quarantined)
	}
	if got, want := len(result.PackageRegistryArtifacts), 1; got != want {
		t.Fatalf("len(PackageRegistryArtifacts) = %d, want %d", got, want)
	}
	if got, want := result.PackageRegistryArtifacts[0].Hashes, (map[string]string{
		"sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}); !reflect.DeepEqual(got, want) {
		t.Fatalf("Hashes = %#v, want %#v", got, want)
	}
}

func TestBuildCanonicalMaterializationSkipsUnstablePackageRegistryArtifact(t *testing.T) {
	t.Parallel()

	artifactFact := packageRegistryArtifactFact()
	artifactFact.StableFactKey = ""
	artifactFact.FactID = "ephemeral-package-registry-artifact-1"
	result, _ := buildCanonicalMaterialization(
		packageRegistryScope(),
		packageRegistryGeneration(),
		append(packageRegistryFacts(), artifactFact),
	)

	if got := len(result.PackageRegistryArtifacts); got != 0 {
		t.Fatalf("len(PackageRegistryArtifacts) = %d, want 0 for missing stable fact key", got)
	}
}

func packageRegistryArtifactFact() facts.Envelope {
	return facts.Envelope{
		FactID:           "package-registry-artifact-1",
		ScopeID:          "package-registry-scope-1",
		GenerationID:     "package-registry-generation-1",
		FactKind:         facts.PackageRegistryPackageArtifactFactKind,
		StableFactKey:    "package-registry-artifact-1",
		SchemaVersion:    facts.PackageRegistryPackageArtifactSchemaVersion,
		CollectorKind:    "package_registry",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.May, 13, 14, 0, 0, 0, time.UTC),
		Payload: map[string]any{
			"collector_instance_id": "package-registry-collector-1",
			"ecosystem":             "npm",
			"registry":              "https://registry.npmjs.org",
			"package_id":            packageRegistryPackageID(),
			"version_id":            packageRegistryVersionID(),
			"version":               "1.2.3",
			"artifact_key":          "pkg-1.2.3.tgz",
			"artifact_type":         "tarball",
			"artifact_url":          "https://registry.npmjs.org/@scope/pkg/-/pkg-1.2.3.tgz",
			"size_bytes":            int64(4096),
			"hashes": map[string]any{
				"sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
				"sha512": "cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3",
			},
			"correlation_anchors": []any{
				packageRegistryPackageID(),
				packageRegistryVersionID(),
				"pkg-1.2.3.tgz",
			},
		},
		SourceRef: facts.Ref{
			SourceSystem:   "package_registry",
			ScopeID:        "package-registry-scope-1",
			GenerationID:   "package-registry-generation-1",
			SourceRecordID: packageRegistryVersionID() + "#pkg-1.2.3.tgz",
		},
	}
}
