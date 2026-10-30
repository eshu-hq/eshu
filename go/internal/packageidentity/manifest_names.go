// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageidentity

import "strings"

// ManifestEntityNameCandidates returns the names a reducer asks a store for
// when it looks up manifest dependency rows by their stored entity_name. It is
// [ConsumptionNameCandidates] plus the one form a manifest parser actually
// writes for a Maven-family package: the case-preserved "group:artifact"
// coordinate. Maven and Gradle manifests store that coordinate, while an
// advisory can name the same package as a bare artifact, a slash Package URL
// path, or a package ID, so the coordinate is derived from whichever
// group-qualified form is present. A name with no group (a bare artifact id)
// and a name that already carries a registry path segment derive no
// coordinate. Other ecosystems return exactly what [ConsumptionNameCandidates]
// returns.
//
// This is a read-prefilter name set only. It never feeds [ConsumptionKeys], so
// the persisted join keys do not change; the key join stays the accuracy gate
// and rejects anything the prefilter over-selects.
func ManifestEntityNameCandidates(ecosystem Ecosystem, packageName string) []string {
	candidates := ConsumptionNameCandidates(ecosystem, packageName)
	if ecosystem != EcosystemMaven {
		return candidates
	}
	coordinate, ok := mavenColonCoordinate(packageName)
	if !ok {
		return candidates
	}
	return uniqueSortedStrings(append(candidates, coordinate, LowerASCII(coordinate)))
}

// mavenColonCoordinate converts a group-qualified Maven name written as
// "group:artifact" or "group/artifact" to the "group:artifact" form manifests
// store, preserving case. It reports false for a name without exactly one
// separator or with an empty side, which includes any name carrying a registry
// path segment alongside the coordinate.
func mavenColonCoordinate(name string) (string, bool) {
	name = strings.TrimSpace(name)
	colons := strings.Count(name, ":")
	slashes := strings.Count(name, "/")
	var group, artifact string
	switch {
	case colons == 1 && slashes == 0:
		group, artifact, _ = strings.Cut(name, ":")
	case colons == 0 && slashes == 1:
		group, artifact, _ = strings.Cut(name, "/")
	default:
		return "", false
	}
	group = strings.TrimSpace(group)
	artifact = strings.TrimSpace(artifact)
	if group == "" || artifact == "" {
		return "", false
	}
	return group + ":" + artifact, true
}
