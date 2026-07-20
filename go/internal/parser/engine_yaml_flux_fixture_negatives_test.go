// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Fixture-intent coverage for the negative and OCI/Bucket sourceRef scenarios
// the RECONCILES_FROM correlation edge (issue #5360 PR B) must handle
// honestly. These prove the PARSER side of the contract: sourceRef fields are
// captured verbatim regardless of whether the name resolves to anything
// (dangling), the kind is one of the three known source kinds (OCI/Bucket),
// or the kind is entirely unrecognized (ExternalArtifact) -- the parser never
// validates or discriminates on sourceRef.kind/name, that judgment belongs
// entirely to the Go-side edge resolution
// (go/internal/storage/cypher/canonical_flux_edges_test.go covers the T1-T4
// tiers and every skip case against these same field shapes).
//
// Wires tests/fixtures/ecosystems/flux_comprehensive/ -- previously referenced
// only from docs/public/languages/flux.md's "Fixture repo" line with no
// automated consumer -- into an executable proof, mirroring
// TestDefaultEngineParsePathYAMLCloudFormationVpcFixtureRealLines's
// fixtureDir/ParsePath pattern.
package parser

import (
	"path/filepath"
	"testing"
)

func fluxComprehensiveFixtureDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "tests", "fixtures", "ecosystems", "flux_comprehensive"))
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v, want nil", err)
	}
	return dir
}

func parseFluxFixtureFile(t *testing.T, fileName string) map[string]any {
	t.Helper()
	fixtureDir := fluxComprehensiveFixtureDir(t)
	filePath := filepath.Join(fixtureDir, fileName)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(fixtureDir, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", fileName, err)
	}
	return got
}

// TestFluxComprehensiveFixtureDanglingSourceRefCapturedVerbatim proves a
// Kustomization whose sourceRef names a GitRepository absent from the entire
// fixture repo still captures source_ref_kind/name/namespace honestly -- the
// parser makes no existence judgment; that is resolved later, and correctly
// skipped, by the edge builder (TestFluxReconcilesFromDanglingRefSkips).
func TestFluxComprehensiveFixtureDanglingSourceRefCapturedVerbatim(t *testing.T) {
	t.Parallel()

	got := parseFluxFixtureFile(t, "dangling-sourceref.yaml")
	assertNamedBucketContains(t, got, "flux_kustomizations", "orphaned")
	assertBucketContainsFieldValue(t, got, "flux_kustomizations", "source_ref_kind", "GitRepository")
	assertBucketContainsFieldValue(t, got, "flux_kustomizations", "source_ref_name", "does-not-exist")
}

// TestFluxComprehensiveFixtureUnknownKindSourceRefCapturedVerbatim proves a
// sourceRef.kind outside the closed {GitRepository, OCIRepository, Bucket} set
// (e.g. the feature-gated ExternalArtifact) is still captured as the literal
// string the manifest declares -- the parser never validates or rejects an
// unrecognized kind; the edge builder treats it as an honest non-link
// (TestFluxReconcilesFromUnknownSourceRefKindSkips).
func TestFluxComprehensiveFixtureUnknownKindSourceRefCapturedVerbatim(t *testing.T) {
	t.Parallel()

	got := parseFluxFixtureFile(t, "unknown-kind-sourceref.yaml")
	assertNamedBucketContains(t, got, "flux_kustomizations", "external-artifact-consumer")
	assertBucketContainsFieldValue(t, got, "flux_kustomizations", "source_ref_kind", "ExternalArtifact")
}

// TestFluxComprehensiveFixtureOCISourceRefCapturedVerbatim proves a
// Kustomization referencing an OCIRepository source captures the OCIRepository
// kind, matching the kustomization.yaml (GitRepository) fixture's shape.
func TestFluxComprehensiveFixtureOCISourceRefCapturedVerbatim(t *testing.T) {
	t.Parallel()

	got := parseFluxFixtureFile(t, "oci-sourceref.yaml")
	assertNamedBucketContains(t, got, "flux_kustomizations", "apps-oci")
	assertBucketContainsFieldValue(t, got, "flux_kustomizations", "source_ref_kind", "OCIRepository")
	assertBucketContainsFieldValue(t, got, "flux_kustomizations", "source_ref_name", "app-manifests")
}

// TestFluxComprehensiveFixtureBucketSourceRefCapturedVerbatim proves a
// Kustomization referencing a Bucket source captures the Bucket kind.
func TestFluxComprehensiveFixtureBucketSourceRefCapturedVerbatim(t *testing.T) {
	t.Parallel()

	got := parseFluxFixtureFile(t, "bucket-sourceref.yaml")
	assertNamedBucketContains(t, got, "flux_kustomizations", "apps-bucket")
	assertBucketContainsFieldValue(t, got, "flux_kustomizations", "source_ref_kind", "Bucket")
	assertBucketContainsFieldValue(t, got, "flux_kustomizations", "source_ref_name", "flux-artifacts")
}

// TestFluxComprehensiveFixtureGenerateNameSourceHasEmptyName proves a
// GitRepository CR using metadata.generateName (never metadata.name) in this
// fixture repo has an empty "name" (never a fabricated "<nil>") and carries
// the generate_name evidence field -- the identity the edge builder's
// candidate-collection guard must never insert as a join target
// (TestCollectFluxSourceEntitiesExcludesEmptyNameCandidates).
func TestFluxComprehensiveFixtureGenerateNameSourceHasEmptyName(t *testing.T) {
	t.Parallel()

	got := parseFluxFixtureFile(t, "generatename-source.yaml")
	repos, ok := got["flux_git_repositories"].([]map[string]any)
	if !ok || len(repos) != 1 {
		t.Fatalf("flux_git_repositories = %#v, want exactly one row", got["flux_git_repositories"])
	}
	if name, present := repos[0]["name"]; !present || name != "" {
		t.Fatalf("name = %#v (present=%v), want empty string, never \"<nil>\"", repos[0]["name"], present)
	}
	if generateName, _ := repos[0]["generate_name"].(string); generateName != "ephemeral-preview-" {
		t.Fatalf("generate_name = %#v, want ephemeral-preview-", repos[0]["generate_name"])
	}
}
