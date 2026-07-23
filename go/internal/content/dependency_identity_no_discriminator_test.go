// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import "testing"

// noDiscriminatorDependencyMetadata builds entity_metadata for the three
// #5507 formats whose parser already guarantees (section, name) uniqueness
// without an added discriminator. "go" (gomod) is deliberately NOT one of
// these — see dependency_identity_gomod_test.go and
// dependencyIdentityDiscriminator's "go" case for why a discriminator is
// required there too (golang.org/x/mod's modfile.Parse does not de-duplicate
// require directives).
//
//   - "rubygems" (ruby/bundler.go's appendBundlerDependency, Gemfile only):
//     section is the sorted, joined dependency-group list; Bundler raises
//     Bundler::GemfileError on a duplicate gem declaration in the same
//     evaluation context, so a valid Gemfile cannot repeat a name within one
//     section.
//   - "pub" (yaml/pubspec.go's pubspecManifestSectionRows, pubspec.yaml/yml
//     only): name is a YAML mapping key under dependencies/dev_dependencies,
//     unique by construction.
//   - "hex" (elixir/hex_dependencies.go's appendMixManifestDependencyRows,
//     mix.exs only): section is the fixed literal "deps"; Mix raises
//     Mix.Error on a duplicate app name in one deps list.
func noDiscriminatorDependencyMetadata(packageManager, section string) map[string]any {
	return map[string]any{
		"config_kind":     "dependency",
		"package_manager": packageManager,
		"section":         section,
	}
}

// TestCanonicalEntityIDWithMetadataNoDiscriminatorFormatsAdmitInScopeRows
// proves each of the three #5507 no-discriminator formats routes to the
// section-keyed scheme, and that reordering (a line-number-only change)
// never changes the minted id.
func TestCanonicalEntityIDWithMetadataNoDiscriminatorFormatsAdmitInScopeRows(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		packageManager string
		path           string
		section        string
		depName        string
	}{
		{name: "rubygems Gemfile default group", packageManager: "rubygems", path: "Gemfile", section: "default", depName: "rails"},
		{name: "rubygems Gemfile test group", packageManager: "rubygems", path: "Gemfile", section: "test", depName: "rspec"},
		{name: "pub pubspec.yaml dependencies", packageManager: "pub", path: "pubspec.yaml", section: "dependencies", depName: "http"},
		{name: "hex mix.exs deps", packageManager: "hex", path: "mix.exs", section: "deps", depName: "phoenix"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repoID := "repository:r_12345678"
			metadata := noDiscriminatorDependencyMetadata(tc.packageManager, tc.section)

			got := CanonicalEntityIDWithMetadata(repoID, tc.path, "Variable", tc.depName, 3, metadata)
			if legacy := CanonicalEntityID(repoID, tc.path, "Variable", tc.depName, 3); got == legacy {
				t.Fatalf("%s: CanonicalEntityIDWithMetadata() = %q unexpectedly matched legacy CanonicalEntityID()", tc.name, got)
			}

			want := CanonicalDependencyEntityID(repoID, tc.path, tc.section, tc.depName)
			if got != want {
				t.Fatalf("%s: CanonicalEntityIDWithMetadata() = %q, want CanonicalDependencyEntityID() = %q (no discriminator expected)", tc.name, got, want)
			}

			reordered := CanonicalEntityIDWithMetadata(repoID, tc.path, "Variable", tc.depName, 99, metadata)
			if reordered != got {
				t.Fatalf("%s: reordering changed the id: line 3 = %q, line 99 = %q", tc.name, got, reordered)
			}
		})
	}
}

// TestCanonicalEntityIDWithMetadataNoDiscriminatorCrossSectionDistinctness
// proves the same declared name in two different sections (e.g. Gemfile
// "default" vs "test" groups) stays distinct — reusing the same discriminator
// mechanics is not needed for these formats, but section keying itself must
// still hold.
func TestCanonicalEntityIDWithMetadataNoDiscriminatorCrossSectionDistinctness(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "Gemfile"
		name   = "rspec"
		line   = 5
	)

	defaultGroup := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line,
		noDiscriminatorDependencyMetadata("rubygems", "default"))
	testGroup := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line,
		noDiscriminatorDependencyMetadata("rubygems", "test"))

	if defaultGroup == testGroup {
		t.Fatalf("default and test Gemfile groups collapsed into one id: %q", defaultGroup)
	}
}
