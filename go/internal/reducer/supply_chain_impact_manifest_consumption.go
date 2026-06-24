// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
)

func addManifestDependencySupplyChainConsumption(
	index *supplyChainImpactIndex,
	envelopes []facts.Envelope,
) {
	dependencies := extractPackageManifestDependencies(envelopes)
	if len(dependencies) == 0 {
		return
	}
	affectedPackages := manifestAffectedPackageMatches(index.affectedPackages)
	if len(affectedPackages) == 0 {
		return
	}
	for _, dependency := range dependencies {
		dependencyKeys := stringSet(packageConsumptionKeys(dependency.PackageManager, dependency.DependencyName))
		if len(dependencyKeys) == 0 {
			continue
		}
		for _, affected := range affectedPackages {
			if !manifestDependencyMatchesAffectedPackage(dependencyKeys, affected.keys) {
				continue
			}
			index.consumption[affected.pkg.packageID] = append(
				index.consumption[affected.pkg.packageID],
				supplyChainConsumptionFromManifestDependency(dependency, affected.pkg),
			)
		}
	}
}

type manifestAffectedPackageMatch struct {
	pkg  supplyChainAffectedPackage
	keys []string
}

func manifestAffectedPackageMatches(groups map[string][]supplyChainAffectedPackage) []manifestAffectedPackageMatch {
	out := make([]manifestAffectedPackageMatch, 0)
	for _, pkgs := range groups {
		for _, pkg := range pkgs {
			keys := affectedPackageConsumptionKeys(pkg)
			if len(keys) == 0 {
				continue
			}
			out = append(out, manifestAffectedPackageMatch{
				pkg:  pkg,
				keys: keys,
			})
		}
	}
	return out
}

func affectedPackageConsumptionKeys(pkg supplyChainAffectedPackage) []string {
	ecosystem := packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(pkg.ecosystem))
	if ecosystem == "" {
		return nil
	}
	keys := make([]string, 0)
	for _, name := range supplyChainAffectedPackageNameCandidates(pkg) {
		keys = append(keys, packageConsumptionKeys(string(ecosystem), name)...)
	}
	return keys
}

func manifestDependencyMatchesAffectedPackage(
	dependencyKeys map[string]struct{},
	affectedPackageKeys []string,
) bool {
	for _, key := range affectedPackageKeys {
		if _, ok := dependencyKeys[key]; ok {
			return true
		}
	}
	return false
}

func supplyChainConsumptionFromManifestDependency(
	dependency packageManifestDependency,
	pkg supplyChainAffectedPackage,
) supplyChainPackageConsumption {
	return supplyChainPackageConsumption{
		factID:                    dependency.FactID,
		evidenceKind:              factKindContentEntity,
		packageID:                 pkg.packageID,
		repositoryID:              strings.TrimSpace(dependency.RepositoryID),
		dependencyRange:           strings.TrimSpace(dependency.DependencyRange),
		observedVersion:           strings.TrimSpace(dependency.ObservedVersion),
		requestedRange:            strings.TrimSpace(dependency.RequestedRange),
		installedVersion:          strings.TrimSpace(dependency.InstalledVersion),
		dependencyPath:            append([]string(nil), dependency.DependencyPath...),
		dependencyDepth:           dependency.DependencyDepth,
		directDependency:          cloneBoolPointer(dependency.DirectDependency),
		dependencyScope:           strings.TrimSpace(dependency.DependencyScope),
		versionEvidence:           strings.TrimSpace(dependency.VersionEvidence),
		unresolvedMSBuildProperty: strings.TrimSpace(dependency.UnresolvedMSBuildProperty),
		ambiguousMSBuildProperty:  strings.TrimSpace(dependency.AmbiguousMSBuildProperty),
		packageAPIPackages:        uniqueSortedStrings(dependency.PackageAPIPackages),
		packageAPIIdentitySource:  strings.TrimSpace(dependency.PackageAPIIdentitySource),
		dependencyResolutionState: strings.TrimSpace(dependency.DependencyResolutionState),
		sourceSet:                 strings.TrimSpace(dependency.SourceSet),
		generatedCode:             cloneBoolPointer(dependency.GeneratedCode),
		partialEvidence:           dependency.PartialEvidence,
		lockfile:                  dependency.Lockfile,
	}
}

func stringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}

func cloneBoolPointer(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
