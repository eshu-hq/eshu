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
	for _, dependency := range dependencies {
		for _, pkg := range allAffectedPackages(index.affectedPackages) {
			if !manifestDependencyMatchesAffectedPackage(dependency, pkg) {
				continue
			}
			index.consumption[pkg.packageID] = append(
				index.consumption[pkg.packageID],
				supplyChainConsumptionFromManifestDependency(dependency, pkg),
			)
		}
	}
}

func allAffectedPackages(groups map[string][]supplyChainAffectedPackage) []supplyChainAffectedPackage {
	out := make([]supplyChainAffectedPackage, 0)
	for _, pkgs := range groups {
		out = append(out, pkgs...)
	}
	return out
}

func manifestDependencyMatchesAffectedPackage(
	dependency packageManifestDependency,
	pkg supplyChainAffectedPackage,
) bool {
	ecosystem := packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(pkg.ecosystem))
	if ecosystem == "" {
		return false
	}
	dependencyKeys := stringSet(packageConsumptionKeys(dependency.PackageManager, dependency.DependencyName))
	for _, name := range supplyChainAffectedPackageNameCandidates(pkg) {
		for _, key := range packageConsumptionKeys(string(ecosystem), name) {
			if _, ok := dependencyKeys[key]; ok {
				return true
			}
		}
	}
	return false
}

func supplyChainConsumptionFromManifestDependency(
	dependency packageManifestDependency,
	pkg supplyChainAffectedPackage,
) supplyChainPackageConsumption {
	return supplyChainPackageConsumption{
		factID:           dependency.FactID,
		evidenceKind:     factKindContentEntity,
		packageID:        pkg.packageID,
		repositoryID:     strings.TrimSpace(dependency.RepositoryID),
		dependencyRange:  strings.TrimSpace(dependency.DependencyRange),
		dependencyPath:   append([]string(nil), dependency.DependencyPath...),
		dependencyDepth:  dependency.DependencyDepth,
		directDependency: cloneBoolPointer(dependency.DirectDependency),
		dependencyScope:  strings.TrimSpace(dependency.DependencyScope),
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
