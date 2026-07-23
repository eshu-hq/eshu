// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import "testing"

// noDiscriminatorDependencyMetadata builds entity_metadata for the four
// #5507 formats whose parser already guarantees (section, name) uniqueness
// without an added discriminator:
//
//   - "go" (gomod/parser.go's goRequireRow): modulePath is unique per
//     require/require-indirect section — a go.mod cannot legitimately
//     require the same module path twice in the same section; `go mod tidy`
//     keeps this deduplicated, and the sibling replace/exclude/retract rows
//     use a different config_kind ("dependency_replace" etc.), so they never
//     reach this gate at all.
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
// proves each of the four #5507 no-discriminator formats routes to the
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
		{name: "gomod require", packageManager: "go", path: "go.mod", section: "require", depName: "github.com/pkg/errors"},
		{name: "gomod require-indirect", packageManager: "go", path: "go.mod", section: "require-indirect", depName: "golang.org/x/sys"},
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

// TestCanonicalEntityIDWithMetadataGomodReplaceStaysLineKeyed proves that
// go.mod `replace` rows (config_kind=="dependency_replace", not
// "dependency") are correctly excluded from the section-keyed scheme by
// condition 2 of the gate — they were never in scope for #5507 in the first
// place, since a replace directive's target module/version, not just its old
// path, is what makes two replace declarations distinct, and the row's
// config_kind already keeps it off this migration's surface entirely.
func TestCanonicalEntityIDWithMetadataGomodReplaceStaysLineKeyed(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "go.mod"
		name   = "github.com/pkg/errors"
		line   = 12
	)
	metadata := map[string]any{
		"config_kind":     "dependency_replace",
		"package_manager": "go",
		"section":         "replace",
	}

	got := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line, metadata)
	want := CanonicalEntityID(repoID, path, "Variable", name, line)
	if got != want {
		t.Fatalf("CanonicalEntityIDWithMetadata() = %q, want legacy CanonicalEntityID() = %q for a dependency_replace row", got, want)
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
