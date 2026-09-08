// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/reducer/packagecorrelation"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

func addManifestDependencySupplyChainConsumption(
	index *supplyChainImpactIndex,
	envelopes []facts.Envelope,
) {
	dependencies := packagecorrelation.ExtractPackageManifestDependencies(envelopes)
	if len(dependencies) == 0 {
		return
	}
	affectedPackages := manifestAffectedPackageMatches(index.affectedPackages)
	if len(affectedPackages) == 0 {
		return
	}
	for _, dependency := range dependencies {
		dependencyKeys := stringSet(packagecorrelation.PackageConsumptionKeys(dependency.PackageManager, dependency.DependencyName))
		if len(dependencyKeys) == 0 {
			continue
		}
		for _, affected := range affectedPackages {
			if !manifestDependencyMatchesAffectedPackage(dependencyKeys, affected.keys) {
				continue
			}
			index.consumption[affected.pkg.PackageID] = append(
				index.consumption[affected.pkg.PackageID],
				supplyChainConsumptionFromManifestDependency(dependency, affected.pkg),
			)
		}
	}
}

type manifestAffectedPackageMatch struct {
	pkg  supplychainmodel.AffectedPackage
	keys []string
}

func manifestAffectedPackageMatches(groups map[string][]supplychainmodel.AffectedPackage) []manifestAffectedPackageMatch {
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

func affectedPackageConsumptionKeys(pkg supplychainmodel.AffectedPackage) []string {
	ecosystem := packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(pkg.Ecosystem))
	if ecosystem == "" {
		return nil
	}
	keys := make([]string, 0)
	for _, name := range supplyChainAffectedPackageNameCandidates(pkg) {
		keys = append(keys, packagecorrelation.PackageConsumptionKeys(string(ecosystem), name)...)
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
	dependency packagecorrelation.PackageManifestDependency,
	pkg supplychainmodel.AffectedPackage,
) supplychainmodel.PackageConsumption {
	return supplychainmodel.PackageConsumption{
		FactID:                    dependency.FactID,
		EvidenceKind:              factKindContentEntity,
		PackageID:                 pkg.PackageID,
		RepositoryID:              strings.TrimSpace(dependency.RepositoryID),
		DependencyRange:           strings.TrimSpace(dependency.DependencyRange),
		ObservedVersion:           strings.TrimSpace(dependency.ObservedVersion),
		RequestedRange:            strings.TrimSpace(dependency.RequestedRange),
		InstalledVersion:          strings.TrimSpace(dependency.InstalledVersion),
		DependencyPath:            append([]string(nil), dependency.DependencyPath...),
		DependencyDepth:           dependency.DependencyDepth,
		DirectDependency:          cloneBoolPointer(dependency.DirectDependency),
		DependencyScope:           strings.TrimSpace(dependency.DependencyScope),
		VersionEvidence:           strings.TrimSpace(dependency.VersionEvidence),
		UnresolvedMSBuildProperty: strings.TrimSpace(dependency.UnresolvedMSBuildProperty),
		AmbiguousMSBuildProperty:  strings.TrimSpace(dependency.AmbiguousMSBuildProperty),
		PackageAPIPackages:        uniqueSortedStrings(dependency.PackageAPIPackages),
		PackageAPIIdentitySource:  strings.TrimSpace(dependency.PackageAPIIdentitySource),
		DependencyResolutionState: strings.TrimSpace(dependency.DependencyResolutionState),
		SourceSet:                 strings.TrimSpace(dependency.SourceSet),
		GeneratedCode:             cloneBoolPointer(dependency.GeneratedCode),
		PartialEvidence:           dependency.PartialEvidence,
		Lockfile:                  dependency.Lockfile,
	}
}

// stringSet forwards to [payloadcore.StringSet].
func stringSet(values []string) map[string]struct{} {
	return payloadcore.StringSet(values)
}

// cloneBoolPointer forwards to [payloadcore.CloneBoolPointer].
func cloneBoolPointer(value *bool) *bool {
	return payloadcore.CloneBoolPointer(value)
}
