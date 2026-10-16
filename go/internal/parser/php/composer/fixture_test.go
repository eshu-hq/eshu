// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package composer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInFixtureDirMatchesWholeSegmentsCaseInsensitively(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		path string
		want bool
	}{
		{"composer.json", false},
		{"packages/core/composer.json", false},
		{"tests/composer.json", true},
		{"Tests/Fixtures/app/composer.json", true},
		{"packages/core/tests/fixtures/app/composer.json", true},
		{"FIXTURES/composer.json", true},
		{"testdata/pkg/composer.json", true},
		{"TestData/pkg/composer.json", true},
		{"docs/examples/demo/composer.json", true},
		{"Examples/composer.json", true},
		// A substring of a segment is not a segment.
		{"packages/testing/composer.json", false},
		{"packages/attestation/composer.json", false},
		{"packages/latests/composer.json", false},
		{"packages/contest/composer.json", false},
		{"packages/fixtures-lib/composer.json", false},
		{"packages/my-examples/composer.json", false},
		// The file name is never a directory segment.
		{"packages/tests", false},
	} {
		if got := InFixtureDir(tc.path); got != tc.want {
			t.Errorf("InFixtureDir(%q) = %t, want %t", tc.path, got, tc.want)
		}
	}
}

// TestProducerPackageSkipsFixtureManifests pins the #8223 rule: a composer.json
// under a fixture-style directory never publishes a package, and the walk does
// not fall through to an outer manifest for a file that sits under it.
func TestProducerPackageSkipsFixtureManifests(t *testing.T) {
	t.Parallel()

	const manifestJSON = `{"name":"acme/logging","autoload":{"psr-4":{"Acme\\Logging\\":"src/"}}}`
	const fqcn = `Acme\Logging\Logger`
	for _, tc := range []struct {
		name        string
		manifestDir string
		want        string
	}{
		{"repository root", "", "acme/logging"},
		{"nested monorepo package", "packages/logging", "acme/logging"},
		{"directory named testing", "packages/testing", "acme/logging"},
		{"directory containing the word tests", "packages/latests", "acme/logging"},
		{"tests fixtures app", "tests/fixtures/app", ""},
		{"capitalized Tests Fixtures", "Tests/Fixtures/app", ""},
		{"testdata", "testdata/pkg", ""},
		{"examples", "docs/examples/demo", ""},
		{"fixture under a real package", "packages/logging/tests/fixtures/app", ""},
		{"vendor stays excluded", "vendor/acme/logging", ""},
		{"vendor nested under a fixture-free path", "packages/logging/vendor/acme/logging", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repoRoot := t.TempDir()
			dir := filepath.Join(repoRoot, filepath.FromSlash(tc.manifestDir))
			if err := os.MkdirAll(dir, 0o750); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(manifestJSON), 0o600); err != nil {
				t.Fatalf("write manifest: %v", err)
			}
			path := filepath.Join(dir, "src", "Logger.php")
			if got := ProducerPackage(repoRoot, path, fqcn); got != tc.want {
				t.Fatalf("ProducerPackage() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestProducerPackageFixtureManifestDoesNotFallThroughToTheRootManifest pins
// the fail-closed walk: the nearest manifest decides, and a skipped fixture
// manifest must not let the root manifest claim the fixture's class file.
func TestProducerPackageFixtureManifestDoesNotFallThroughToTheRootManifest(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	rootManifest := `{"name":"acme/logging","autoload":{"psr-4":{"Acme\\Logging\\":"src/"}}}`
	fixtureManifest := `{"name":"acme/logging","autoload":{"psr-4":{"Acme\\Logging\\":"src/"}}}`
	for rel, content := range map[string]string{
		"composer.json":                    rootManifest,
		"tests/fixtures/app/composer.json": fixtureManifest,
	} {
		full := filepath.Join(repoRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	const fqcn = `Acme\Logging\Logger`
	if got := ProducerPackage(repoRoot, filepath.Join(repoRoot, "src", "Logger.php"), fqcn); got != "acme/logging" {
		t.Fatalf("real root class = %q, want acme/logging", got)
	}
	fixtureFile := filepath.Join(repoRoot, "tests", "fixtures", "app", "src", "Logger.php")
	if got := ProducerPackage(repoRoot, fixtureFile, fqcn); got != "" {
		t.Fatalf("fixture class = %q, want empty", got)
	}
}
