// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "strings"

func packageManifestDependencyNameCandidates(dependency packageManifestDependency) []string {
	names := []string{dependency.DependencyName}
	namespace := strings.TrimSpace(dependency.PackageNamespace)
	name := strings.TrimSpace(dependency.DependencyName)
	if namespace != "" && name != "" {
		names = append(names, namespace+"/"+name)
	}
	return uniqueSortedStrings(names)
}

func securityAlertPackageNameMatchesDependency(
	alert providerSecurityAlert,
	dependency packageManifestDependency,
) bool {
	for _, dependencyName := range packageManifestDependencyNameCandidates(dependency) {
		if securityAlertPackageNameMatches(alert, dependencyName) {
			return true
		}
	}
	return false
}
