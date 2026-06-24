// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildPackageConsumptionDecisionsMatchesSwiftPackageResolved(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	decisions := BuildPackageConsumptionDecisions([]facts.Envelope{
		packageRegistryPackageFact(
			"swift://github.com/apple/swift-argument-parser",
			"swift",
			"swift-argument-parser",
			"github.com/apple",
			observedAt,
		),
		packageSourceRepositoryFact(
			"repo-swift",
			"swift-api",
			"https://github.com/acme/swift-api",
			false,
			observedAt,
		),
		packageManifestDependencyFactWithMetadata(
			"repo-swift",
			"swift-api",
			"Package.resolved",
			"github.com/apple/swift-argument-parser",
			"swift",
			"1.2.3",
			observedAt,
			map[string]any{
				"section":         "Package.resolved",
				"lockfile":        true,
				"lockfile_format": "swift-package-resolved",
			},
		),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if got, want := decision.PackageID, "swift://github.com/apple/swift-argument-parser"; got != want {
		t.Fatalf("PackageID = %q, want %q", got, want)
	}
	if got, want := decision.PackageName, "github.com/apple/swift-argument-parser"; got != want {
		t.Fatalf("PackageName = %q, want source-backed Swift dependency name %q", got, want)
	}
	if got, want := decision.DependencyRange, "1.2.3"; got != want {
		t.Fatalf("DependencyRange = %q, want exact Package.resolved version %q", got, want)
	}
	if !decision.Lockfile {
		t.Fatal("Lockfile = false, want true for Package.resolved evidence")
	}
	if decision.DirectDependency != nil {
		t.Fatalf("DirectDependency = %#v, want nil until Package.resolved proves a dependency chain", decision.DirectDependency)
	}
}
