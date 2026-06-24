// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildPackageConsumptionDecisionsAdmitsRubyGemsLockfileEvidence(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	decisions := BuildPackageConsumptionDecisions([]facts.Envelope{
		packageRegistryPackageFact(
			"pkg:gem/rails",
			"rubygems",
			"rails",
			"",
			observedAt,
		),
		packageSourceRepositoryFact("repo-ruby-api", "ruby-api", "https://example.com/ruby-api.git", false, observedAt),
		packageManifestDependencyFactWithMetadata(
			"repo-ruby-api",
			"ruby-api",
			"Gemfile.lock",
			"rails",
			"rubygems",
			"7.1.3",
			observedAt,
			map[string]any{
				"section":           "gemfile.lock",
				"lockfile":          true,
				"dependency_path":   []any{"rails"},
				"dependency_depth":  1,
				"direct_dependency": true,
			},
		),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if decision.PackageID != "pkg:gem/rails" {
		t.Fatalf("PackageID = %q, want pkg:gem/rails", decision.PackageID)
	}
	if decision.DependencyRange != "7.1.3" {
		t.Fatalf("DependencyRange = %q, want exact lockfile version 7.1.3", decision.DependencyRange)
	}
	if decision.InstalledVersion != "7.1.3" {
		t.Fatalf("InstalledVersion = %q, want exact lockfile version 7.1.3", decision.InstalledVersion)
	}
	if !reflect.DeepEqual(decision.DependencyPath, []string{"rails"}) {
		t.Fatalf("DependencyPath = %#v, want rails", decision.DependencyPath)
	}
	if decision.DependencyDepth != 1 {
		t.Fatalf("DependencyDepth = %d, want 1", decision.DependencyDepth)
	}
	if decision.DirectDependency == nil || !*decision.DirectDependency {
		t.Fatalf("DirectDependency = %#v, want true", decision.DirectDependency)
	}
}

func TestBuildPackageConsumptionDecisionsJoinsRubyGemsManifestRangeToLockfileVersion(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	decisions := BuildPackageConsumptionDecisions([]facts.Envelope{
		packageRegistryPackageFact("pkg:gem/rails", "rubygems", "rails", "", observedAt),
		packageSourceRepositoryFact("repo-ruby-api", "ruby-api", "https://example.com/ruby-api.git", false, observedAt),
		packageManifestDependencyFactWithMetadata(
			"repo-ruby-api",
			"ruby-api",
			"Gemfile",
			"rails",
			"rubygems",
			"~> 7.1",
			observedAt,
			map[string]any{
				"section":          "default",
				"dependency_scope": "runtime",
			},
		),
		packageManifestDependencyFactWithMetadata(
			"repo-ruby-api",
			"ruby-api",
			"Gemfile.lock",
			"rails",
			"rubygems",
			"7.1.3",
			observedAt,
			map[string]any{
				"section":           "gemfile.lock",
				"lockfile":          true,
				"dependency_path":   []any{"rails"},
				"dependency_depth":  1,
				"direct_dependency": true,
			},
		),
	})

	lockDecision, ok := packageConsumptionDecisionByPath(decisions, "Gemfile.lock")
	if !ok {
		t.Fatalf("Gemfile.lock decision missing: %#v", decisions)
	}
	if got, want := lockDecision.InstalledVersion, "7.1.3"; got != want {
		t.Fatalf("InstalledVersion = %q, want exact lockfile version %q", got, want)
	}
	if got, want := lockDecision.DependencyRange, "~> 7.1"; got != want {
		t.Fatalf("DependencyRange = %q, want requested manifest range %q", got, want)
	}
	if got, want := lockDecision.DependencyScope, "runtime"; got != want {
		t.Fatalf("DependencyScope = %q, want joined Gemfile scope %q", got, want)
	}
}

func TestBuildPackageConsumptionDecisionsDoesNotInventRubyGemsDependencyChains(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	decisions := BuildPackageConsumptionDecisions([]facts.Envelope{
		packageRegistryPackageFact("pkg:gem/rails", "rubygems", "rails", "", observedAt),
		packageManifestDependencyFactWithMetadata(
			"repo-ruby-api",
			"ruby-api",
			"Gemfile",
			"rails",
			"rubygems",
			"~> 7.1",
			observedAt,
			map[string]any{
				"section":          "default",
				"dependency_scope": "runtime",
			},
		),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if len(decision.DependencyPath) != 0 {
		t.Fatalf("DependencyPath = %#v, want empty until Bundler lockfile proves direct/transitive chain", decision.DependencyPath)
	}
	if decision.DependencyDepth != 0 {
		t.Fatalf("DependencyDepth = %d, want 0 until Bundler lockfile proves direct/transitive chain", decision.DependencyDepth)
	}
	if decision.DirectDependency != nil {
		t.Fatalf("DirectDependency = %#v, want nil until Bundler lockfile proves direct/transitive chain", decision.DirectDependency)
	}
}

func TestBuildPackageConsumptionDecisionsRejectsAmbiguousRubyGemsSources(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	decisions := BuildPackageConsumptionDecisions([]facts.Envelope{
		packageRegistryPackageFact("pkg:gem/internal_admin", "rubygems", "internal_admin", "", observedAt),
		packageManifestDependencyFactWithMetadata(
			"repo-ruby-api",
			"ruby-api",
			"Gemfile.lock",
			"internal_admin",
			"rubygems",
			"0.1.0",
			observedAt,
			map[string]any{
				"section":           "gemfile.lock",
				"lockfile":          true,
				"dependency_path":   []any{"internal_admin"},
				"dependency_depth":  1,
				"direct_dependency": true,
				"source_type":       "git",
				"source_path":       "https://example.com/acme/internal_admin.git",
				"source_ambiguous":  true,
			},
		),
	})

	if len(decisions) != 0 {
		t.Fatalf("ambiguous git/path Bundler source produced package consumption decisions: %#v", decisions)
	}
}

func packageConsumptionDecisionByPath(
	decisions []PackageConsumptionDecision,
	relativePath string,
) (PackageConsumptionDecision, bool) {
	for _, decision := range decisions {
		if decision.RelativePath == relativePath {
			return decision, true
		}
	}
	return PackageConsumptionDecision{}, false
}
