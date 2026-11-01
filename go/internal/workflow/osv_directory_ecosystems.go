// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
)

// osvEcosystemDirectories is the closed list of top-level directories in the
// public OSV bucket (gs://osv-vulnerabilities) whose ecosystem the owned
// derivation can act on. Each carries a <directory>/modified_id.csv change
// feed. These are the names OSV uses, with OSV's own capitalization, because
// they are object-name prefixes and GCS object names are case-sensitive.
// Sorted.
//
// The list is closed on purpose and is not every directory that publishes a
// modified_id.csv. The bucket also holds other feeds (CRAN, Hackage, Julia,
// opam, the distros, GIT, GitHub Actions, ...) that stay unmapped until the
// reader, the derivation and the provider can act on them: an unlisted name
// must fail closed, never fall through to a lowercased guess. Distro
// ecosystems stay on the installed evidence rotation (#9777 ruling D5).
var osvEcosystemDirectories = []string{
	"Go", "Hex", "Maven", "NuGet", "Packagist", "Pub", "PyPI", "RubyGems", "SwiftURL", "crates.io", "npm",
}

// OSVEcosystemDirectories returns the sorted OSV bucket directory names this
// contract maps. The slice is a fresh copy.
func OSVEcosystemDirectories() []string {
	return slices.Clone(osvEcosystemDirectories)
}

// OSVDirectoryEcosystem maps one OSV bucket directory name to the derivation
// ecosystem the planner and the owned-package reader use for it, the same name
// [CanonicalOwnedPackageEcosystem] returns for a raw package_manager and that a
// derive_from_owned_packages section selects (see
// [VulnerabilityOwnedDerivationEcosystems]). It reports false for any name
// outside [OSVEcosystemDirectories]: the match is exact and case-sensitive, so
// padding, a path, an object name, a wrong case, or a distro directory is
// refused rather than normalized into a guess.
//
// There is no second hand-kept table. The name is resolved by
// [packageidentity.NormalizeEcosystem] and then [CanonicalOwnedPackageEcosystem],
// the two tables the rest of the platform already shares. The result differs
// from packageidentity's answer only where the derivation already differs from
// it: Go, which OSV and the derivation spell "go" where packageidentity says
// "gomod" (see ownedPackageManagerAliasTargets). A test pins that the 11
// directories map one-to-one onto the owned derivation set, so a directory
// added without its ecosystem, or the reverse, fails there.
func OSVDirectoryEcosystem(directory string) (string, bool) {
	if !slices.Contains(osvEcosystemDirectories, directory) {
		return "", false
	}
	identity := packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(directory))
	if identity == "" {
		return "", false
	}
	canonical := CanonicalOwnedPackageEcosystem(string(identity))
	if !slices.Contains(vulnerabilityOwnedDerivationEcosystems, canonical) {
		return "", false
	}
	return canonical, true
}
