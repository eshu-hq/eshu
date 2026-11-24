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
	// The join below used to append one row per matching (dependency,
	// affected package) pair with no bound: a batch-scale intent holds
	// hundreds of dependencies times hundreds of advisories naming the same
	// package, so one package retained tens of thousands of rows (observed
	// 2026-11-24: 42,000 rows for one package, 14GB engine-wide in the
	// heap profile) and OOMKilled the engine at its memory limit. Every
	// downstream reader keeps only
	// the representative row per (repository, observed version) —
	// firstConsumption — plus the repository set, so the join streams those
	// best rows instead of the pairs. Findings are unchanged by
	// construction: same pairs probed in the same order, same representative
	// rule per group (compareConsumptionRows), same versionless rule (first
	// preferred else first), same blank-repository drop.
	join := newManifestConsumptionJoin(affectedPackages)
	for pkg, rows := range index.consumption {
		join.seed(pkg, rows)
	}
	for _, dependency := range dependencies {
		join.addDependency(dependency)
	}
	index.consumption = join.materialize(index.consumption)
}

// manifestConsumptionJoin streams the manifest-dependency consumption join
// into one best row per (package, repository, observed version). It replaces
// the unbounded pair-per-match append it fronted: same probed pairs in the
// same dependency-major, affected-minor order, but only the row each
// downstream group would elect is retained.
//
// Group keys reuse consumptionObservedVersion, the same rule the finding's
// observed_version reads, so a split group and the finding it yields name
// the same version. Rows with no version keep the one-group rule only when
// no versioned row names the repository (splitRepositoryConsumptionByVersion);
// a versionless row beside versioned ones is dropped, never ranked. Blank
// repository rows are dropped outright, matching groupConsumptionRowsByRepository
// and firstConsumption, which both skip them. Packages with seeded rows but
// no affected package pass through verbatim: no downstream group ever reads
// them, and the tracker has no ecosystem to key their versions by.
type manifestConsumptionJoin struct {
	affected []manifestAffectedPackageMatch
	byKey    map[string][]int
	// ecosystem names the affected ecosystem per package, the same
	// representativeAffectedPackage(pkg).Ecosystem the split reads.
	ecosystem map[string]string
	best      map[manifestConsumptionGroup]supplychainmodel.PackageConsumption
	// versionless keeps the first preferred row in arrival order per
	// (package, repository), else the first row, mirroring what
	// firstConsumption reads from an unsorted versionless group.
	versionless map[manifestConsumptionRepo]supplychainmodel.PackageConsumption
	// versioned marks the (package, repository) pairs a versioned best
	// already covers, so a versionless row beside versioned ones is dropped.
	versioned map[manifestConsumptionRepo]bool
	// passthrough holds packages with seeded rows but no affected package:
	// their rows are preserved verbatim and never tracked.
	passthrough map[string]bool
}

// manifestConsumptionGroup names one downstream group: the package whose
// findings read it, the repository it classifies, and the observed version
// it classifies (empty for the versionless group).
type manifestConsumptionGroup struct {
	pkg        string
	repository string
	version    string
}

// manifestConsumptionRepo names the versionless scope: one group per
// (package, repository) when no versioned row names the repository.
type manifestConsumptionRepo struct {
	pkg        string
	repository string
}

// newManifestConsumptionJoin indexes the affected packages by consumption
// key once, so each dependency probes keys instead of scanning every
// affected package. Keyless affected packages never match and stay out of
// the index, exactly as the match loop skipped them.
func newManifestConsumptionJoin(affected []manifestAffectedPackageMatch) *manifestConsumptionJoin {
	join := &manifestConsumptionJoin{
		affected:    affected,
		byKey:       make(map[string][]int),
		ecosystem:   make(map[string]string),
		best:        make(map[manifestConsumptionGroup]supplychainmodel.PackageConsumption),
		versionless: make(map[manifestConsumptionRepo]supplychainmodel.PackageConsumption),
		versioned:   make(map[manifestConsumptionRepo]bool),
		passthrough: make(map[string]bool),
	}
	// byKey lists stay in affected order: indexes append in slice order, so
	// each key's list is already sorted and probes need no second sort.
	for i, match := range affected {
		if _, known := join.ecosystem[match.pkg.PackageID]; !known {
			join.ecosystem[match.pkg.PackageID] = match.pkg.Ecosystem
		}
		for _, key := range match.keys {
			join.byKey[key] = append(join.byKey[key], i)
		}
	}
	return join
}

// seed folds rows the index already holds (envelope-derived consumption the
// build appended before the join) into the best tracking, in order, so the
// versionless first-preferred-else-first rule reads the same arrival order
// the unbounded append produced. A seeded package with no affected package
// passes through verbatim instead: no group reads it, and there is no
// ecosystem to key its versions by.
func (join *manifestConsumptionJoin) seed(pkg string, rows []supplychainmodel.PackageConsumption) {
	if _, known := join.ecosystem[pkg]; !known {
		join.passthrough[pkg] = true
		return
	}
	for _, row := range rows {
		join.consider(pkg, row)
	}
}

// addDependency probes the affected index with one dependency's keys and
// folds every newly matched pair into the best tracking, in the original
// affected order. A pair reachable through two keys folds once, the way the
// match loop's first-key win appended it once.
func (join *manifestConsumptionJoin) addDependency(dependency correlation.PackageManifestDependency) {
	dependencyKeys := manifestDependencyKeySet(dependency)
	if len(dependencyKeys) == 0 {
		return
	}
	var matched []int
	for key := range dependencyKeys {
		matched = append(matched, join.byKey[key]...)
	}
	if len(matched) == 0 {
		return
	}
	// Matched indexes arrive grouped by probed key; restoring ascending
	// order replays the match loop's affected visit order, and adjacent
	// dedupe folds a multi-key pair once.
	slices.Sort(matched)
	for i, idx := range matched {
		if i > 0 && matched[i-1] == idx {
			continue
		}
		match := join.affected[idx]
		join.consider(match.pkg.PackageID, supplyChainConsumptionFromManifestDependency(dependency, match.pkg))
	}
}

// consider folds one candidate row into the best tracking of its package:
// the versioned argmin per (repository, version), or the versionless
// first-preferred-else-first per (repository). Blank-repository rows are
// dropped: every downstream reader skips them.
func (join *manifestConsumptionJoin) consider(pkg string, row supplychainmodel.PackageConsumption) {
	repository := strings.TrimSpace(row.RepositoryID)
	if repository == "" {
		return
	}
	version := consumptionObservedVersion(join.ecosystem[pkg], row)
	if version == "" {
		scope := manifestConsumptionRepo{pkg: pkg, repository: repository}
		if join.versioned[scope] {
			return
		}
		kept, ok := join.versionless[scope]
		if !ok || (!consumptionRowPreferred(kept) && consumptionRowPreferred(row)) {
			join.versionless[scope] = row
		}
		return
	}
	scope := manifestConsumptionRepo{pkg: pkg, repository: repository}
	join.versioned[scope] = true
	key := manifestConsumptionGroup{pkg: pkg, repository: repository, version: version}
	// The best row is the version group's minimum under the downstream
	// sort (compareConsumptionRows): a preferred row first, then by fact ID.
	if kept, ok := join.best[key]; !ok || compareConsumptionRows(row, kept) < 0 {
		join.best[key] = row
	}
}

// materialize renders the tracked best rows back into the per-package lists
// the downstream grouping reads, preserving the representative contract:
// one row per (repository, version), the versionless row only when no
// versioned row names the repository, no blank-repository rows. Passthrough
// packages keep their seeded rows verbatim. Rows read back sorted by
// repository then version for determinism; no reader depends on the order
// (groups re-sort versions, repositories sort explicitly, direct readers
// take representatives and sets).
func (join *manifestConsumptionJoin) materialize(into map[string][]supplychainmodel.PackageConsumption) map[string][]supplychainmodel.PackageConsumption {
	out := make(map[string][]supplychainmodel.PackageConsumption, len(into))
	ordered := make([]manifestConsumptionGroup, 0, len(join.best))
	for key := range join.best {
		ordered = append(ordered, key)
	}
	slices.SortFunc(ordered, func(a, b manifestConsumptionGroup) int {
		return cmp.Or(
			strings.Compare(a.pkg, b.pkg),
			strings.Compare(a.repository, b.repository),
			strings.Compare(a.version, b.version),
		)
	})
	byPackage := make(map[string][]supplychainmodel.PackageConsumption)
	for _, key := range ordered {
		byPackage[key.pkg] = append(byPackage[key.pkg], join.best[key])
	}
	versionlessScopes := make([]manifestConsumptionRepo, 0, len(join.versionless))
	for scope := range join.versionless {
		versionlessScopes = append(versionlessScopes, scope)
	}
	slices.SortFunc(versionlessScopes, func(a, b manifestConsumptionRepo) int {
		return cmp.Or(
			strings.Compare(a.pkg, b.pkg),
			strings.Compare(a.repository, b.repository),
		)
	})
	for _, scope := range versionlessScopes {
		if join.versioned[scope] {
			continue
		}
		byPackage[scope.pkg] = append(byPackage[scope.pkg], join.versionless[scope])
	}
	// Tracked packages render whether or not they were already keys: the
	// unbounded append created keys on match, and so does this. A package
	// the input holds but tracking dropped (only blank-repository rows, or
	// no rows at all) stays dropped: every reader skips blank rows and
	// treats a missing key like an empty list.
	for pkg, tracked := range byPackage {
		out[pkg] = tracked
	}
	for pkg, rows := range into {
		if join.passthrough[pkg] {
			out[pkg] = rows
		}
	}
	return out
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
