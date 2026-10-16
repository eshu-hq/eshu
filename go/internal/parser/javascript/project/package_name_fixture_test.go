// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package project

import (
	"path/filepath"
	"testing"
)

// TestNearestPackageNameSkipsFixtureManifests pins the #8244 rule: a
// package.json under a test, tests, fixtures, testdata, or examples directory
// is scaffolding, not a producer, so it publishes no package name. node_modules
// is not part of this rule and keeps its prior handling.
func TestNearestPackageNameSkipsFixtureManifests(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		manifestDir string
		want        string
	}{
		{"repository root", "", "acme-lib"},
		{"nested workspace package", "packages/lib", "acme-lib"},
		{"directory named testing", "packages/testing", "acme-lib"},
		{"directory containing the word tests", "packages/latests", "acme-lib"},
		{"tests fixtures app", "tests/fixtures/app", ""},
		{"singular test", "test/app", ""},
		{"capitalized Tests Fixtures", "Tests/Fixtures/app", ""},
		{"testdata", "testdata/pkg", ""},
		{"examples", "docs/examples/demo", ""},
		{"fixture under a real package", "packages/lib/tests/fixtures/app", ""},
		{"long s is not a fold of s", "a/teſts/app", "acme-lib"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repoRoot := t.TempDir()
			dir := filepath.Join(repoRoot, filepath.FromSlash(tc.manifestDir))
			writePackageJSONTestFile(t, filepath.Join(dir, "package.json"), `{"name":"acme-lib"}`)
			path := filepath.Join(dir, "src", "index.js")
			if got := NearestPackageName(repoRoot, path); got != tc.want {
				t.Fatalf("NearestPackageName() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestNearestPackageNameFixtureManifestDoesNotFallThroughToTheRootManifest pins
// the fail-closed walk: the nearest manifest decides, and a fixture manifest
// must not let the root manifest claim a file that sits under the fixture.
func TestNearestPackageNameFixtureManifestDoesNotFallThroughToTheRootManifest(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writePackageJSONTestFile(t, filepath.Join(repoRoot, "package.json"), `{"name":"acme-lib"}`)
	writePackageJSONTestFile(t, filepath.Join(repoRoot, "tests", "fixtures", "app", "package.json"), `{"name":"acme-lib"}`)

	if got := NearestPackageName(repoRoot, filepath.Join(repoRoot, "src", "index.js")); got != "acme-lib" {
		t.Fatalf("real root file = %q, want acme-lib", got)
	}
	fixtureFile := filepath.Join(repoRoot, "tests", "fixtures", "app", "src", "index.js")
	if got := NearestPackageName(repoRoot, fixtureFile); got != "" {
		t.Fatalf("fixture file = %q, want empty (no fall-through to the root manifest)", got)
	}
}

// TestNearestPackageNameRuleIsOnTheManifestLocationNotTheFile pins the limit: a
// root manifest still keys a source file under a tests directory when no
// nearer manifest exists.
func TestNearestPackageNameRuleIsOnTheManifestLocationNotTheFile(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writePackageJSONTestFile(t, filepath.Join(repoRoot, "package.json"), `{"name":"acme-lib"}`)
	if got := NearestPackageName(repoRoot, filepath.Join(repoRoot, "tests", "helper.js")); got != "acme-lib" {
		t.Fatalf("file under tests with only a root manifest = %q, want acme-lib", got)
	}
}
