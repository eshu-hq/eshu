// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

func addManifestDependencySupplyChainConsumption(
	index *supplyChainImpactIndex,
	envelopes []facts.Envelope,
) {
	dependencies := correlation.ExtractPackageManifestDependencies(envelopes)
	if len(dependencies) == 0 {
		return
	}
	affectedPackages := manifestAffectedPackageMatches(index.affectedPackages)
	if len(affectedPackages) == 0 {
		return
	}
	for _, dependency := range dependencies {
		dependencyKeys := manifestDependencyKeySet(dependency)
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

// manifestDependencyKeySet is the one place a manifest dependency becomes the
// registry-identity key set both supply-chain paths compare against affected
// packages: the consumption join here and the parser-file repository scope in
// active_filter.go. An sbt row yields the keys of its Maven artifact and
// nothing else (#8961), so the two paths cannot disagree about a row.
func manifestDependencyKeySet(dependency correlation.PackageManifestDependency) map[string]struct{} {
	return stringSet(dependency.ConsumptionKeys(dependency.DependencyName))
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
		keys = append(keys, correlation.PackageConsumptionKeys(string(ecosystem), name)...)
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
	dependency correlation.PackageManifestDependency,
	pkg supplychainmodel.AffectedPackage,
) supplychainmodel.PackageConsumption {
	return supplychainmodel.PackageConsumption{
		FactID:                    dependency.FactID,
		EvidenceKind:              factload.FactKindContentEntity,
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
		PackageAPIPackages:        payloadcore.UniqueSortedStrings(dependency.PackageAPIPackages),
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

// consumptionObservedVersion is the observed version a consumption row gives
// its finding: the row's own observed version, else the exact version its
// lockfile or installed text proves. It is the one rule both the finding's
// observed_version (applySupplyChainConsumptionEvidence) and the per-version
// split below read, so a split group and the finding it yields cannot name
// different versions.
func consumptionObservedVersion(ecosystem string, consumption supplychainmodel.PackageConsumption) string {
	if version := strings.TrimSpace(consumption.ObservedVersion); version != "" {
		return version
	}
	if version, ok := exactConsumptionDependencyVersion(ecosystem, consumption); ok {
		return version
	}
	return ""
}

// splitRepositoryConsumptionByVersion partitions one repository's rows for a
// package into one group per distinct observed version (#11113), so each
// installed version classifies once, as #10057 does per (release line,
// installed version) for OS rows. One firstConsumption pick per repository
// let a fixed row hide a vulnerable one by load order.
//
// Groups come back ordered by version. Within a group, rows sort preferred
// first (installed version or lockfile, the firstConsumption preference),
// then by fact ID, so the row that represents the group does not depend on
// load order either. When no row carries a version, the rows come back as
// one group in index order: the pre-#11113 rule. Rows with no version beside
// versioned rows form no group: a repository that proves an installed version
// is judged on its versions, never on a row that names none. The input is
// left as it was.
func splitRepositoryConsumptionByVersion(
	ecosystem string,
	rows []supplychainmodel.PackageConsumption,
) [][]supplychainmodel.PackageConsumption {
	if len(rows) < 2 {
		return [][]supplychainmodel.PackageConsumption{rows}
	}
	byVersion := make(map[string][]supplychainmodel.PackageConsumption)
	for _, row := range rows {
		if version := consumptionObservedVersion(ecosystem, row); version != "" {
			byVersion[version] = append(byVersion[version], row)
		}
	}
	if len(byVersion) == 0 {
		return [][]supplychainmodel.PackageConsumption{rows}
	}
	versions := slices.Sorted(maps.Keys(byVersion))
	out := make([][]supplychainmodel.PackageConsumption, 0, len(versions))
	for _, version := range versions {
		group := byVersion[version]
		slices.SortStableFunc(group, compareConsumptionRows)
		out = append(out, group)
	}
	return out
}

// compareConsumptionRows orders rows of one version: a row firstConsumption
// prefers (installed version or lockfile) first, then by fact ID.
func compareConsumptionRows(a, b supplychainmodel.PackageConsumption) int {
	return cmp.Or(
		-cmpBool(consumptionRowPreferred(a), consumptionRowPreferred(b)),
		cmp.Compare(a.FactID, b.FactID),
	)
}

func consumptionRowPreferred(row supplychainmodel.PackageConsumption) bool {
	return strings.TrimSpace(row.InstalledVersion) != "" || row.Lockfile
}

func cmpBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}

// multiVersionRepositoryGroupsSubSignal is the Result.SubSignals key counting
// the (repository, CVE, advisory, package) groups whose findings classify two
// or more observed versions (#11113); 0 when none, so a clean pass is
// visible.
const multiVersionRepositoryGroupsSubSignal = "multi_version_repository_groups"

// countMultiVersionRepositoryGroups counts the (repository, CVE, advisory,
// package) groups of repository-anchored findings that hold two or more
// observed versions: the groups the per-version split classified more than
// once. Image-subject and scan-target findings are left out; they split per
// digest (#9858, #10057).
func countMultiVersionRepositoryGroups(findings []SupplyChainImpactFinding) int {
	type groupKey struct{ repositoryID, cveID, advisoryID, packageID string }
	first := make(map[groupKey]string)
	multi := make(map[groupKey]struct{})
	for _, finding := range findings {
		if finding.RepositoryID == "" || finding.SubjectDigest != "" || finding.ScanTargetID != "" {
			continue
		}
		key := groupKey{finding.RepositoryID, finding.CVEID, finding.AdvisoryID, finding.PackageID}
		version, seen := first[key]
		switch {
		case !seen:
			first[key] = finding.ObservedVersion
		case version != finding.ObservedVersion:
			multi[key] = struct{}{}
		}
	}
	return len(multi)
}
