// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageregistry

import (
	"slices"
	"testing"
)

func TestCorrelationAnchorsDeduplicateWhilePreservingOrder(t *testing.T) {
	t.Parallel()

	got := correlationAnchors(" package ", "purl", "package", "bom", "purl", "bom")
	want := []string{"package", "purl", "bom"}
	if !slices.Equal(got, want) {
		t.Fatalf("correlationAnchors() = %#v, want %#v", got, want)
	}
}

func TestPackageRegistryEnvelopesDeduplicateFallbackBOMRefAnchors(t *testing.T) {
	t.Parallel()

	pkgEnvelope, err := NewPackageEnvelope(PackageObservation{
		Identity: PackageIdentity{
			Ecosystem: EcosystemPyPI,
			Registry:  "https://pypi.org/simple/",
			RawName:   "Friendly_Bard",
		},
		ScopeID:             "pypi://pypi.org/simple/friendly-bard",
		GenerationID:        "etag:abc123",
		CollectorInstanceID: "public-pypi",
	})
	if err != nil {
		t.Fatalf("NewPackageEnvelope() error = %v", err)
	}
	assertUniqueCorrelationAnchors(t, pkgEnvelope.Payload["correlation_anchors"])

	versionEnvelope, err := NewPackageVersionEnvelope(PackageVersionObservation{
		Package: PackageIdentity{
			Ecosystem: EcosystemNPM,
			Registry:  "registry.npmjs.org",
			RawName:   "react",
		},
		Version:             "19.0.0",
		ScopeID:             "npm://registry.npmjs.org/react",
		GenerationID:        "etag:def456",
		CollectorInstanceID: "public-npm",
	})
	if err != nil {
		t.Fatalf("NewPackageVersionEnvelope() error = %v", err)
	}
	assertUniqueCorrelationAnchors(t, versionEnvelope.Payload["correlation_anchors"])

	dependencyEnvelope, err := NewPackageDependencyEnvelope(PackageDependencyObservation{
		Package: PackageIdentity{
			Ecosystem: EcosystemNPM,
			Registry:  "registry.npmjs.org",
			RawName:   "@example/web-app",
		},
		Version: "2.4.0",
		Dependency: PackageIdentity{
			Ecosystem: EcosystemNPM,
			Registry:  "registry.npmjs.org",
			RawName:   "left.pad",
		},
		Range:               "^1.3.0",
		DependencyType:      "runtime",
		ScopeID:             "npm://registry.npmjs.org/@example/web-app",
		GenerationID:        "etag:deps",
		CollectorInstanceID: "public-npm",
	})
	if err != nil {
		t.Fatalf("NewPackageDependencyEnvelope() error = %v", err)
	}
	assertUniqueCorrelationAnchors(t, dependencyEnvelope.Payload["correlation_anchors"])
}

func assertUniqueCorrelationAnchors(t *testing.T, value any) {
	t.Helper()

	anchors, ok := value.([]string)
	if !ok || len(anchors) == 0 {
		t.Fatalf("correlation_anchors = %#v, want non-empty []string", value)
	}
	seen := make(map[string]struct{}, len(anchors))
	for _, anchor := range anchors {
		if _, exists := seen[anchor]; exists {
			t.Fatalf("correlation_anchors contains duplicate %q: %#v", anchor, anchors)
		}
		seen[anchor] = struct{}{}
	}
}
