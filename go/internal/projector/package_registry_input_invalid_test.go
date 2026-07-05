// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestExtractPackageRegistryRowsQuarantinesMissingPackageID is the flagship
// regression for the projector's package_registry typed-decode migration
// (Contract System v1 §3.2, Option B per-fact quarantine). It proves the
// accuracy guarantee AND the per-fact isolation contract: a
// package_registry.package fact missing its required package_id key is
// QUARANTINED as a visible input_invalid dead-letter — never silently
// producing a package row under a graph identity built from an empty-string
// segment — while every VALID fact in the same batch still projects, and the
// whole-repo canonical build never fails.
//
// Before the migration this behavior was impossible: packageRegistryPackageRow
// read package_id with payloadString, which returns "" for the absent key, and
// the row was dropped with no operator signal (a silent skip). A collector
// regression dropping package_id produced zero packages and no dead-letter.
func TestExtractPackageRegistryRowsQuarantinesMissingPackageID(t *testing.T) {
	t.Parallel()

	validFacts := packageRegistryFacts()

	// A package fact whose required package_id key is ABSENT (not merely
	// empty).
	malformed := facts.Envelope{
		FactID:        "package-registry-package-bad",
		ScopeID:       "package-registry-scope-1",
		GenerationID:  "package-registry-generation-1",
		FactKind:      facts.PackageRegistryPackageFactKind,
		SchemaVersion: facts.PackageRegistryPackageSchemaVersion,
		Payload: map[string]any{
			// "package_id" intentionally absent.
			"ecosystem": "npm",
		},
	}

	mat := &CanonicalMaterialization{ScopeID: "package-registry-scope-1"}
	quarantined := extractPackageRegistryRows(mat, append(validFacts, malformed))

	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1; the missing-package_id package fact must be quarantined", len(quarantined))
	}
	if got := quarantined[0].factKind; got != facts.PackageRegistryPackageFactKind {
		t.Fatalf("quarantined fact kind = %q, want %q", got, facts.PackageRegistryPackageFactKind)
	}
	if got := quarantined[0].field; got != "package_id" {
		t.Fatalf("quarantined field = %q, want %q", got, "package_id")
	}
	if got := quarantined[0].factID; got != "package-registry-package-bad" {
		t.Fatalf("quarantined fact id = %q, want %q", got, "package-registry-package-bad")
	}

	// The batch's VALID package (and its version) must still materialize:
	// isolation means a poisoned sibling never suppresses valid graph truth.
	if len(mat.PackageRegistryPackages) != 1 {
		t.Fatalf("len(PackageRegistryPackages) = %d, want 1; the valid package must still project despite the quarantined fact", len(mat.PackageRegistryPackages))
	}
	if got, want := mat.PackageRegistryPackages[0].UID, packageRegistryPackageID(); got != want {
		t.Fatalf("valid package UID = %q, want %q", got, want)
	}
	if len(mat.PackageRegistryVersions) != 1 {
		t.Fatalf("len(PackageRegistryVersions) = %d, want 1; the valid version must still project despite the quarantined sibling fact", len(mat.PackageRegistryVersions))
	}
}

// TestExtractPackageRegistryRowsPresentButEmptyPackageIDIsDroppedNotQuarantined
// proves the absent-vs-present-empty distinction: a package fact whose
// package_id key is PRESENT but empty is a valid decode (not a quarantine)
// that is still dropped as an incomplete, non-materializable row —
// byte-identical to the pre-typing behavior, where payloadString("") produced
// no row. Only an ABSENT (or null) required key dead-letters.
func TestExtractPackageRegistryRowsPresentButEmptyPackageIDIsDroppedNotQuarantined(t *testing.T) {
	t.Parallel()

	emptyPackageID := facts.Envelope{
		FactID:        "package-registry-package-empty",
		ScopeID:       "package-registry-scope-1",
		GenerationID:  "package-registry-generation-1",
		FactKind:      facts.PackageRegistryPackageFactKind,
		SchemaVersion: facts.PackageRegistryPackageSchemaVersion,
		Payload: map[string]any{
			"package_id": "", // present but empty
			"ecosystem":  "npm",
		},
	}

	mat := &CanonicalMaterialization{ScopeID: "package-registry-scope-1"}
	quarantined := extractPackageRegistryRows(mat, []facts.Envelope{emptyPackageID})

	if len(quarantined) != 0 {
		t.Fatalf("len(quarantined) = %d, want 0; a present-but-empty required field is a valid decode, not a quarantine", len(quarantined))
	}
	if len(mat.PackageRegistryPackages) != 0 {
		t.Fatalf("len(PackageRegistryPackages) = %d, want 0; a package with an empty package_id is incomplete and must be dropped", len(mat.PackageRegistryPackages))
	}
}

// TestExtractPackageRegistryRowsWhitespacePackageIDIsDroppedNotMaterialized
// proves the trim-before-gate accuracy contract (the terraform_state/oci
// family review lesson): the pre-typing payloadString path did NOT trim
// whitespace, so this test also demonstrates the typed decode's stricter (and
// intentional) behavior — a whitespace-only package_id ("   ") is treated as
// empty and the row is DROPPED, not materialized under a whitespace-only
// graph identity. This is NOT a dead-letter — the decode succeeded, the fact
// is a valid but non-materializable observation, exactly like
// present-but-empty.
func TestExtractPackageRegistryRowsWhitespacePackageIDIsDroppedNotMaterialized(t *testing.T) {
	t.Parallel()

	validFacts := packageRegistryFacts()

	whitespacePackageID := facts.Envelope{
		FactID:        "package-registry-package-ws",
		ScopeID:       "package-registry-scope-1",
		GenerationID:  "package-registry-generation-1",
		FactKind:      facts.PackageRegistryPackageFactKind,
		SchemaVersion: facts.PackageRegistryPackageSchemaVersion,
		Payload: map[string]any{
			"package_id": "   ", // present but whitespace-only
			"ecosystem":  "npm",
		},
	}

	mat := &CanonicalMaterialization{ScopeID: "package-registry-scope-1"}
	quarantined := extractPackageRegistryRows(mat, append(validFacts, whitespacePackageID))

	// A whitespace-only identity is a valid decode, so it must NOT dead-letter.
	if len(quarantined) != 0 {
		t.Fatalf("len(quarantined) = %d, want 0; a whitespace-only identity is a valid decode dropped as non-materializable, never a dead-letter", len(quarantined))
	}
	// Only the valid package from packageRegistryFacts() may materialize.
	if len(mat.PackageRegistryPackages) != 1 {
		t.Fatalf("len(PackageRegistryPackages) = %d, want 1; the whitespace-package_id package must be dropped, the valid sibling must still project", len(mat.PackageRegistryPackages))
	}
	if got, want := mat.PackageRegistryPackages[0].UID, packageRegistryPackageID(); got != want {
		t.Fatalf("materialized package UID = %q, want the valid sibling's %q (never a whitespace-only identity)", got, want)
	}
}

// TestExtractPackageRegistryRowsQuarantinesMissingDependencyJoinKey proves the
// dependency edge's join-key quarantine contract: a
// package_registry.package_dependency fact missing its required
// dependency_package_id join key is quarantined, and does not silently break
// the dependency edge extraction for the rest of the batch.
func TestExtractPackageRegistryRowsQuarantinesMissingDependencyJoinKey(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactID:           "package-registry-dependency-bad",
		ScopeID:          "package-registry-scope-1",
		GenerationID:     "package-registry-generation-1",
		FactKind:         facts.PackageRegistryPackageDependencyFactKind,
		StableFactKey:    "package-registry-dependency-bad",
		SchemaVersion:    facts.PackageRegistryPackageDependencySchemaVersion,
		CollectorKind:    "package_registry",
		SourceConfidence: facts.SourceConfidenceReported,
		Payload: map[string]any{
			"package_id": packageRegistryPackageID(),
			"version_id": packageRegistryVersionID(),
			"version":    "1.2.3",
			// "dependency_package_id" intentionally absent.
			"optional": true,
			"excluded": false,
		},
	}

	mat := &CanonicalMaterialization{ScopeID: "package-registry-scope-1"}
	quarantined := extractPackageRegistryRows(mat, append(packageRegistryFacts(), malformed))

	var found bool
	for _, q := range quarantined {
		if q.factID == "package-registry-dependency-bad" {
			found = true
			if q.factKind != facts.PackageRegistryPackageDependencyFactKind {
				t.Fatalf("quarantined fact kind = %q, want %q", q.factKind, facts.PackageRegistryPackageDependencyFactKind)
			}
			if q.field != "dependency_package_id" {
				t.Fatalf("quarantined field = %q, want %q", q.field, "dependency_package_id")
			}
		}
	}
	if !found {
		t.Fatal("package-registry-dependency-bad was not quarantined; a missing dependency_package_id join key must dead-letter as input_invalid")
	}

	// The valid package/version from packageRegistryFacts() must still project.
	if len(mat.PackageRegistryPackages) != 1 {
		t.Fatalf("len(PackageRegistryPackages) = %d, want 1", len(mat.PackageRegistryPackages))
	}
	if len(mat.PackageRegistryVersions) != 1 {
		t.Fatalf("len(PackageRegistryVersions) = %d, want 1", len(mat.PackageRegistryVersions))
	}
}

// TestExtractPackageRegistryRowsProjectsWithOmittedStatusBools proves the
// descriptive status flags are OPTIONAL, not required identity keys: a
// package_version fact carrying every identity key (package_id, version_id,
// version) but OMITTING is_yanked/is_unlisted/is_deprecated/is_retracted, and a
// package_dependency fact carrying every join key but OMITTING optional/excluded,
// must decode VALID and PROJECT their nodes with the flags defaulting to false
// (nil pointer -> false in the row builder). They must NEVER quarantine.
//
// This is the accuracy contract the projector re-decode path depends on:
// runtime.go re-decodes STORED facts on every re-projection, so a persisted,
// older, or out-of-tree fact that predates a status flag must still materialize
// its version/dependency node rather than dead-letter the whole node on a
// missing descriptive bool. Before the *bool change these fields were typed as
// required non-pointer bools, which would have quarantined the entire node here.
func TestExtractPackageRegistryRowsProjectsWithOmittedStatusBools(t *testing.T) {
	t.Parallel()

	versionNoBools := facts.Envelope{
		FactID:           "package-registry-version-nobools",
		ScopeID:          "package-registry-scope-1",
		GenerationID:     "package-registry-generation-1",
		FactKind:         facts.PackageRegistryPackageVersionFactKind,
		StableFactKey:    packageRegistryVersionID(),
		SchemaVersion:    facts.PackageRegistryPackageVersionSchemaVersion,
		CollectorKind:    "package_registry",
		SourceConfidence: facts.SourceConfidenceReported,
		Payload: map[string]any{
			"package_id": packageRegistryPackageID(),
			"version_id": packageRegistryVersionID(),
			"version":    "1.2.3",
			// is_yanked / is_unlisted / is_deprecated / is_retracted omitted.
		},
	}
	dependencyNoBools := facts.Envelope{
		FactID:           "package-registry-dependency-nobools",
		ScopeID:          "package-registry-scope-1",
		GenerationID:     "package-registry-generation-1",
		FactKind:         facts.PackageRegistryPackageDependencyFactKind,
		StableFactKey:    "package-registry-dependency-nobools",
		SchemaVersion:    facts.PackageRegistryPackageDependencySchemaVersion,
		CollectorKind:    "package_registry",
		SourceConfidence: facts.SourceConfidenceReported,
		Payload: map[string]any{
			"package_id":            packageRegistryPackageID(),
			"version_id":            packageRegistryVersionID(),
			"version":               "1.2.3",
			"dependency_package_id": "package://npm/registry.npmjs.org/left-pad",
			// optional / excluded omitted.
		},
	}

	mat := &CanonicalMaterialization{ScopeID: "package-registry-scope-1"}
	quarantined := extractPackageRegistryRows(mat, []facts.Envelope{versionNoBools, dependencyNoBools})

	if len(quarantined) != 0 {
		t.Fatalf("len(quarantined) = %d, want 0; omitting the OPTIONAL status bools must not quarantine — they are descriptive flags, not identity keys", len(quarantined))
	}
	if len(mat.PackageRegistryVersions) != 1 {
		t.Fatalf("len(PackageRegistryVersions) = %d, want 1; a version with every identity key but no status bools must still project", len(mat.PackageRegistryVersions))
	}
	version := mat.PackageRegistryVersions[0]
	if version.IsYanked || version.IsUnlisted || version.IsDeprecated || version.IsRetracted {
		t.Fatalf("version status flags = yanked:%t unlisted:%t deprecated:%t retracted:%t, want all false (nil pointer -> false)", version.IsYanked, version.IsUnlisted, version.IsDeprecated, version.IsRetracted)
	}
	if len(mat.PackageRegistryDependencies) != 1 {
		t.Fatalf("len(PackageRegistryDependencies) = %d, want 1; a dependency with every join key but no optional/excluded bools must still project", len(mat.PackageRegistryDependencies))
	}
	dependency := mat.PackageRegistryDependencies[0]
	if dependency.Optional || dependency.Excluded {
		t.Fatalf("dependency flags = optional:%t excluded:%t, want both false (nil pointer -> false)", dependency.Optional, dependency.Excluded)
	}
}
