// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
)

// osvDirectoryWant lists the 11 top-level OSV bucket directories whose
// ecosystem the owned derivation can act on, each with the derivation
// ecosystem the planner and the owned-package reader use for it. Other
// directories (CRAN, Hackage, distros, ...) also publish a modified_id.csv and
// stay unmapped on purpose. The directory names were
// checked against the live osv-vulnerabilities bucket (one
// <directory>/modified_id.csv object each) and against the generation listing
// of the #9777 change-feed shims.
var osvDirectoryWant = map[string]string{
	"Go":        "go",
	"Hex":       "hex",
	"Maven":     "maven",
	"NuGet":     "nuget",
	"Packagist": "composer",
	"Pub":       "pub",
	"PyPI":      "pypi",
	"RubyGems":  "rubygems",
	"SwiftURL":  "swift",
	"crates.io": "cargo",
	"npm":       "npm",
}

// TestOSVDirectoryEcosystemMapsEveryDirectory pins all 11 directory names.
func TestOSVDirectoryEcosystemMapsEveryDirectory(t *testing.T) {
	t.Parallel()

	for directory, want := range osvDirectoryWant {
		got, ok := OSVDirectoryEcosystem(directory)
		if !ok || got != want {
			t.Errorf("OSVDirectoryEcosystem(%q) = (%q, %t), want (%q, true)", directory, got, ok, want)
		}
	}
}

// TestOSVDirectoryEcosystemFailsClosedOnUnknownDirectory pins that anything
// outside the closed list of 11 names is refused: a wrong case, padding, a
// path, an object name, a distro or snapshot directory, an alias that
// packageidentity accepts but OSV never publishes as a directory, and blank.
func TestOSVDirectoryEcosystemFailsClosedOnUnknownDirectory(t *testing.T) {
	t.Parallel()

	for _, directory := range []string{
		"",
		" ",
		"unknown",
		"pypi",
		"go",
		"NPM",
		"Npm",
		"CRATES.IO",
		"swifturl",
		" npm",
		"npm ",
		"npm/",
		"npm/modified_id.csv",
		"Alpine",
		"Alpine:v3.10",
		"Debian",
		"Debian:12",
		"Ubuntu",
		"GIT",
		"GitHub Actions",
		"Hackage",
		"golang",
		"gomod",
		"cargo",
		"composer",
		"python",
		"Gradle",
		"swift",
		"os",
		"generic",
	} {
		if got, ok := OSVDirectoryEcosystem(directory); ok || got != "" {
			t.Errorf("OSVDirectoryEcosystem(%q) = (%q, %t), want (\"\", false)", directory, got, ok)
		}
	}
}

// TestOSVEcosystemDirectoriesMatchTheConfigurableDerivationSet pins one source
// of truth: the directories map one-to-one onto the ecosystems a
// derive_from_owned_packages section may select. Adding a derivation ecosystem
// without its OSV directory, or a directory without a derivation ecosystem,
// fails here.
func TestOSVEcosystemDirectoriesMatchTheConfigurableDerivationSet(t *testing.T) {
	t.Parallel()

	directories := OSVEcosystemDirectories()
	if len(directories) != len(osvDirectoryWant) {
		t.Fatalf("OSVEcosystemDirectories() = %v, want the %d OSV directories", directories, len(osvDirectoryWant))
	}
	if !slices.IsSorted(directories) {
		t.Fatalf("OSVEcosystemDirectories() = %v, want sorted", directories)
	}
	var images []string
	for _, directory := range directories {
		if _, ok := osvDirectoryWant[directory]; !ok {
			t.Errorf("OSVEcosystemDirectories() lists %q, which the pinned table does not", directory)
		}
		canonical, ok := OSVDirectoryEcosystem(directory)
		if !ok {
			t.Errorf("listed directory %q does not map", directory)
			continue
		}
		images = append(images, canonical)
	}
	slices.Sort(images)
	if want := VulnerabilityOwnedDerivationEcosystems(); !slices.Equal(images, want) {
		t.Fatalf("directory ecosystems = %v, want the owned derivation set %v", images, want)
	}
}

// TestOSVDirectoryEcosystemAgreesWithPackageIdentity pins the relation to
// packageidentity.NormalizeEcosystem: the mapping goes through it, so the
// derivation ecosystem is its answer with the one documented exception, Go,
// which OSV and the derivation spell "go" where packageidentity says "gomod".
func TestOSVDirectoryEcosystemAgreesWithPackageIdentity(t *testing.T) {
	t.Parallel()

	for _, directory := range OSVEcosystemDirectories() {
		got, _ := OSVDirectoryEcosystem(directory)
		identity := packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(directory))
		want := string(identity)
		if identity == packageidentity.EcosystemGoModule {
			want = "go"
		}
		if got != want {
			t.Errorf("directory %q: derivation ecosystem %q, packageidentity %q (expected %q)", directory, got, identity, want)
		}
	}
}

// TestOSVEcosystemDirectoriesReturnsACopy pins that a caller cannot change the
// closed list by editing the returned slice.
func TestOSVEcosystemDirectoriesReturnsACopy(t *testing.T) {
	t.Parallel()

	directories := OSVEcosystemDirectories()
	if len(directories) == 0 {
		t.Fatal("OSVEcosystemDirectories() is empty")
	}
	directories[0] = "tampered"
	if _, ok := OSVDirectoryEcosystem("tampered"); ok {
		t.Fatal("OSVDirectoryEcosystem accepted a tampered directory")
	}
	if slices.Contains(OSVEcosystemDirectories(), "tampered") {
		t.Fatal("OSVEcosystemDirectories returned shared storage")
	}
}
