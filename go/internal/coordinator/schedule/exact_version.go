// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schedule

import (
	"strings"

	"golang.org/x/mod/semver"
)

// ExactOwnedDependencyVersionForEcosystem reports the version the vulnerability
// derivation queries for a stored dependency in the OSV ecosystem named, and
// false when the stored text does not pin one release. It is the planner's
// rule; the owned-package resolver calls it too, so a stored row the planner
// would never query is not reported as affected by a changed name.
func ExactOwnedDependencyVersionForEcosystem(ecosystem string, raw string) (string, bool) {
	version := strings.TrimSpace(raw)
	if !exactOwnedDependencyVersionSyntaxAllowed(version) {
		return "", false
	}
	if unresolvedOwnedDependencyVersion(ecosystem, version) {
		return "", false
	}
	if ecosystem != "go" {
		return version, true
	}
	if !strings.HasPrefix(version, "v") || !semver.IsValid(version) {
		return "", false
	}
	return version, true
}

// unresolvedOwnedDependencyVersion reports whether version is manifest text
// that stands in for a release instead of naming one: an interpolation or
// property reference ($x, ${x}), a dynamic selector (latest.release), or a
// Gradle modifier (1.0!! strict marker, 1.0@jar artifact-only notation).
// Manifests keep these as raw text, so without this check each one becomes an
// OSV query for a version that does not exist.
//
// "!" is rejected everywhere except PyPI, where PEP 440 uses it as the epoch
// separator in real release versions such as 1!2.0.0. No other ecosystem
// the derivation accepts publishes a version containing "$", "{", "}", "!"
// or "@", and none publishes a version starting with "latest.".
func unresolvedOwnedDependencyVersion(ecosystem string, version string) bool {
	if strings.ContainsAny(version, "${}@") {
		return true
	}
	if ecosystem != "pypi" && strings.Contains(version, "!") {
		return true
	}
	return strings.HasPrefix(strings.ToLower(version), "latest.")
}

func exactOwnedDependencyVersionSyntaxAllowed(version string) bool {
	if version == "" {
		return false
	}
	lower := strings.ToLower(version)
	if lower == "latest" || NonVersionOwnedDependencyPrefix(lower) {
		return false
	}
	if lower == "main" || lower == "master" || lower == "develop" ||
		lower == "release" ||
		strings.HasPrefix(lower, "dev-") ||
		strings.HasPrefix(lower, "release-") ||
		strings.HasPrefix(lower, "feature-") ||
		strings.HasPrefix(lower, "bugfix-") ||
		strings.HasPrefix(lower, "hotfix-") ||
		strings.HasSuffix(lower, "-dev") ||
		strings.Contains(lower, "snapshot") {
		return false
	}
	if strings.ContainsAny(version, "<>^~*=|, []()/:\\") ||
		strings.Contains(version, ".+") ||
		strings.HasSuffix(version, "+") ||
		strings.Contains(lower, " - ") ||
		strings.Contains(lower, ".x") ||
		strings.Contains(lower, "x.") {
		return false
	}
	return true
}
