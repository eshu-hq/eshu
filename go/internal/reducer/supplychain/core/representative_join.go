// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Outcome label values for
// eshu_dp_supply_chain_impact_os_representative_joins_total, the closed set of
// its outcome label (#9990). Each OS-shaped (cve, package_id) group an index
// evaluates gets exactly one.
const (
	// osRepresentativeJoined: the source-order representative joined an
	// installed row.
	osRepresentativeJoined = "joined"
	// osRepresentativeReselected: the source-order representative had no anchor,
	// so another fact of the group that joins an installed row named the finding.
	osRepresentativeReselected = "reselected"
	// osRepresentativeOtherAnchor: the representative joined no installed row but
	// an SBOM path or a consuming repository anchors the finding.
	osRepresentativeOtherAnchor = "other_anchor"
	// osRepresentativeNoJoinable: installed rows sit under the group's lookup
	// keys and no fact of the group passes the join's gates, so the group yields
	// no finding. Usually no fact comes from those rows' vendor; a vendor fact also
	// fails on an ecosystem or purl that does not meet the row, and no fact joins
	// a row from a non-vendor repository.
	osRepresentativeNoJoinable = "no_joinable_representative"
	// osRepresentativeNoInstalledRow: no installed row sits under the group's
	// lookup keys. The package is not on any scanned host this index holds,
	// but the identity could meet a row: it names a purl or a purl-formed
	// package id.
	osRepresentativeNoInstalledRow = "no_installed_row"
	// osRepresentativeNoJoinableIdentity: the group's identity can meet no
	// installed row at all (#10034 item 3). An os:// package id with no purl,
	// or a fact with neither, names no purl key, and rows key by purl, so the
	// row could exist and the identity still could not meet it. The group
	// yields no finding, as with no_installed_row, but the operator sees
	// which one it was.
	osRepresentativeNoJoinableIdentity = "no_joinable_identity"
)

// Result.SubSignals keys for the representative outcomes. Every key is written
// on every pass, 0 included, so a clean pass is visible on the success line.
const (
	osRepresentativesJoinedSubSignal             = "os_representatives_joined"
	osRepresentativesReselectedSubSignal         = "os_representatives_reselected"
	osRepresentativesOtherAnchorSubSignal        = "os_representatives_other_anchor"
	osRepresentativesNoJoinableSubSignal         = "os_representatives_no_joinable"
	osRepresentativesNoInstalledRowSubSignal     = "os_representatives_no_installed_row"
	osRepresentativesNoJoinableIdentitySubSignal = "os_representatives_no_joinable_identity"
)

// osRepresentativeOutcomeKey is one counter cell: the closed vendor label of the
// fact that named the group, and the outcome.
type osRepresentativeOutcomeKey struct {
	vendor  string
	outcome string
}

// withJoinableOSRepresentative returns only the facts of pkgs that join an installed
// row, with the fact that should name the finding marked, when the facts that join
// should decide the finding instead of the whole group (#9990). A fact that cannot
// join says nothing about this host, so it offers no fix and decides no verdict.
//
// Two groups are narrowed, and both are rows this change creates:
//
//   - reselected: the source-order representative has no anchor of its own and
//     another fact of the group joins an installed row.
//   - narrowed direct: the representative joins an installed row, its trimmed
//     ecosystem is blank, every row it joins carries an installed version, and at
//     least one fact of the group joins no row. The base drops a blank ecosystem
//     with an observed version as unsupported_ecosystem, so no row exists to move.
//     A spelled ecosystem that joins has a base row, a row with no version has a
//     base row (installed_version_missing is decided before the ecosystem is
//     read), and a group whose facts all join keeps its pick for the ranking rule
//     of #10034 item 1.
//
// It runs only for a group that has no SBOM path and no consuming repository: the
// caller has already found no SBOM path, and this checks the consumer. Those are
// the only sources of the owned anchor supplyChainImpactFindingHasOwnedAnchor
// requires, so a group with any anchor keeps its source-order representative and a
// finding that exists is not moved by a fact that could also have joined.
//
// reselected is true only for the first kind: the source-order representative did
// not join. The caller takes the narrowed slice when ok is true, whichever kind it
// is, and counts the narrowed direct group as joined because its representative
// did join.
//
// The input slice is never modified: the index shares it across installed rows
// and across scan targets, and a different target may join a different fact. The
// mark rides on the returned slice, so a later representativeAffectedPackage call on
// it, or on selectOSAdvisoryRelease's narrowed copy of it, agrees.
func withJoinableOSRepresentative(
	pkgs []supplychainmodel.AffectedPackage,
	representative supplychainmodel.AffectedPackage,
	index supplyChainImpactIndex,
) (joining []supplychainmodel.AffectedPackage, reselected, ok bool) {
	if len(pkgs) < 2 || len(index.osPackages) == 0 {
		return pkgs, false, false
	}
	if len(consumptionRepositoriesForPackage(representative.PackageID, index.consumption)) > 0 {
		return pkgs, false, false
	}
	_, direct := firstOSPackageImpactPath(representative, index)
	if direct && !blankEcosystemJoinsOnlyVersionedRows(representative, index) {
		return pkgs, false, false
	}
	best, bestRank := -1, 0
	for _, pkg := range pkgs {
		installed, joins := firstOSPackageImpactPath(pkg, index)
		if !joins {
			continue
		}
		rank := vendorClassPriorityRank(installed.VendorAdvisorySource, classifyAdvisorySource(pkg.Source, pkg.AdvisoryID))
		if best < 0 || joinableRepresentativeBefore(rank, pkg, bestRank, joining[best]) {
			best, bestRank = len(joining), rank
		}
		joining = append(joining, pkg)
	}
	if best < 0 || (direct && len(joining) == len(pkgs)) {
		return pkgs, false, false
	}
	joining[best].JoinRepresentative = true
	return joining, !direct, true
}

// narrowShardToJoinableOSRepresentative ranks this scope's facts the way the
// naming ranks the whole group (#9990) and keeps the ones that join this
// scope's installed row. A live fact that cannot join must not reach release
// narrowing, provenance, holders, or the matcher, or its purl fixes the
// version family while the row's own fact is ignored in every fact order
// (#10034, the different-vendor group limit). The mark stays on this scope's
// slice, which the verdict ranks.
func narrowShardToJoinableOSRepresentative(
	pkgs []supplychainmodel.AffectedPackage,
	index supplyChainImpactIndex,
) []supplychainmodel.AffectedPackage {
	if joining, _, ok := withJoinableOSRepresentative(pkgs, representativeAffectedPackage(pkgs), index); ok {
		return joining
	}
	return pkgs
}

// liveShardPackages filters the group to the facts live on a fleet shard's
// scope, because an anchor is judged on the scope it anchors (#10034, the
// different-vendor group limit): a live fact joining only one shard no longer
// keeps a retracted record's facts on the shards it never joins. It then
// narrows to the facts that join the shard's row; an SBOM-scoped finding
// holds every fact of the group its digest names, narrowing or not, so the
// SBOM path never calls this.
func liveShardPackages(
	cves supplyChainCVEGroup,
	pkgs []supplychainmodel.AffectedPackage,
	index supplyChainImpactIndex,
) (supplyChainCVEGroup, []supplychainmodel.AffectedPackage) {
	cves, pkgs = liveAdvisoryPackages(cves, pkgs, index)
	return cves, narrowShardToJoinableOSRepresentative(pkgs, index)
}

// splitDirectBlankGroupByRowVersion splits a group by the version of the installed rows
// its representative joins, when the rows are mixed, so the rule for a row that exists
// at the base and the rule for a row this change creates each read only their own rows
// (ruling 9990-f6).
//
// Condition c of withJoinableOSRepresentative keeps the whole-group read when any row the
// representative joins has no installed version, because that row has a base row
// (installed_version_missing is decided before the ecosystem is read). Looked at over the
// whole index, one such row also kept the whole group for every versioned row of the same
// package, and a versioned row has no base row: the base drops a blank ecosystem with an
// observed version. A fact that cannot join then decided it, and a vendor fix above the
// host could read as already fixed. The row, not the shard or the index, is the unit
// that separates the two, because a digest shard can hold both kinds of row.
//
// A group splits when all of these hold: it has two or more facts and the index has OS
// rows; the representative's trimmed ecosystem is blank; no repository consumes its
// package; its joined rows include one with and one without an installed version; and at
// least one fact joins no row with a version. versioned holds the rows with a trimmed
// installed version and versionless holds the rest, each only under the group's lookup
// keys. versionless has no outcome map, so a group is counted once, by the versioned
// part. The caller runs the same group over each part, and each part is uniform, so
// neither splits again.
//
// The input index and its maps are not modified. The cheap tests run first, so a spelled
// ecosystem, which no producer leaves blank, costs one string trim.
func splitDirectBlankGroupByRowVersion(
	pkgs []supplychainmodel.AffectedPackage,
	representative supplychainmodel.AffectedPackage,
	index supplyChainImpactIndex,
) (versioned, versionless supplyChainImpactIndex, split bool) {
	if len(pkgs) < 2 || len(index.osPackages) == 0 || strings.TrimSpace(representative.Ecosystem) != "" {
		return index, index, false
	}
	if len(consumptionRepositoriesForPackage(representative.PackageID, index.consumption)) > 0 {
		return index, index, false
	}
	anyVersioned, anyVersionless := false, false
	for _, row := range matchingOSPackageImpactPaths(representative, index) {
		if strings.TrimSpace(row.InstalledVersion) == "" {
			anyVersionless = true
		} else {
			anyVersioned = true
		}
	}
	if !anyVersioned || !anyVersionless {
		return index, index, false
	}
	withVersion := map[string][]supplychainmodel.OSPackage{}
	withoutVersion := map[string][]supplychainmodel.OSPackage{}
	seen := map[string]bool{}
	for _, pkg := range pkgs {
		for _, key := range affectedOSPackageLookupKeys(pkg) {
			if seen[key] {
				continue
			}
			seen[key] = true
			for _, row := range index.osPackages[key] {
				if strings.TrimSpace(row.InstalledVersion) == "" {
					withoutVersion[key] = append(withoutVersion[key], row)
				} else {
					withVersion[key] = append(withVersion[key], row)
				}
			}
		}
	}
	versioned, versionless = index, index
	versioned.osPackages, versionless.osPackages = withVersion, withoutVersion
	versionless.osRepresentativeOutcomes = nil
	if !slices.ContainsFunc(pkgs, func(pkg supplychainmodel.AffectedPackage) bool {
		return !hasMatchingOSPackage(pkg, versioned)
	}) {
		return index, index, false
	}
	return versioned, versionless, true
}

// blankEcosystemJoinsOnlyVersionedRows reports whether a representative that joins an
// installed row is of the kind the narrowed direct group covers: its trimmed
// ecosystem is blank and every row it joins carries an installed version. The cheap
// ecosystem test runs before any row is collected, so a spelled ecosystem, which no
// producer leaves blank, costs one string trim.
func blankEcosystemJoinsOnlyVersionedRows(
	representative supplychainmodel.AffectedPackage,
	index supplyChainImpactIndex,
) bool {
	if strings.TrimSpace(representative.Ecosystem) != "" {
		return false
	}
	for _, row := range matchingOSPackageImpactPaths(representative, index) {
		if strings.TrimSpace(row.InstalledVersion) == "" {
			return false
		}
	}
	return true
}

// joinableRepresentativeBefore orders two facts that each join an installed row:
// source rank under the vendor of the row the fact joins, then source, then fact id.
// Each key comes from its own fact, so the order is total and the group's order
// cannot change the pick. The rank uses the row's vendor directly, not an ecosystem
// name: alma and rocky have no ecosystem table of their own, and the vendor's fact
// must outrank OSV on those rows as it does on the others.
func joinableRepresentativeBefore(
	rankA int,
	a supplychainmodel.AffectedPackage,
	rankB int,
	b supplychainmodel.AffectedPackage,
) bool {
	if rankA != rankB {
		return rankA < rankB
	}
	if a.Source != b.Source {
		return a.Source < b.Source
	}
	return a.FactID < b.FactID
}

// affectedFactHasOSShape reports whether a fact describes an OS package: an OS
// ecosystem or package manager name, a deb/apk/rpm purl or package id, or an
// os:// package id. It reads no source, so an OSV fact counts too.
func affectedFactHasOSShape(pkg supplychainmodel.AffectedPackage) bool {
	return normalizedSupplyChainVersionEcosystem(pkg.Ecosystem) == string(packageidentity.EcosystemOS) ||
		osPackageFamilyFromPackageManager(pkg.Ecosystem) != "" ||
		osPackageFamilyFromPURL(pkg.PURL) != "" ||
		osPackageFamilyFromPURL(pkg.PackageID) != "" ||
		strings.HasPrefix(packageidentity.LowerASCII(strings.TrimSpace(pkg.PackageID)), "os://")
}

// recordOSRepresentativeOutcome counts what happened to one OS-shaped group on
// this index. pkg is the fact that names the group after any reselection. An
// index built with no outcome map records nothing, and a language group never
// counts: it cannot reach an installed OS row.
func (idx supplyChainImpactIndex) recordOSRepresentativeOutcome(
	pkgs []supplychainmodel.AffectedPackage,
	pkg supplychainmodel.AffectedPackage,
	reselected bool,
	hasSBOMPath bool,
) {
	if idx.osRepresentativeOutcomes == nil || !slices.ContainsFunc(pkgs, affectedFactHasOSShape) {
		return
	}
	key := osRepresentativeOutcomeKey{vendor: osAdvisoryJoinVendorLabel(classifyAffectedPackageAdvisorySource(pkg))}
	switch {
	case reselected:
		key.outcome = osRepresentativeReselected
	case hasMatchingOSPackage(pkg, idx):
		key.outcome = osRepresentativeJoined
	case hasSBOMPath || len(consumptionRepositoriesForPackage(pkg.PackageID, idx.consumption)) > 0:
		key.outcome = osRepresentativeOtherAnchor
	case slices.ContainsFunc(pkgs, idx.hasInstalledRowUnderLookupKeys):
		key.outcome = osRepresentativeNoJoinable
	case slices.ContainsFunc(pkgs, affectedPackageIdentityCanMeetInstalledRow):
		key.outcome = osRepresentativeNoInstalledRow
	default:
		key.outcome = osRepresentativeNoJoinableIdentity
	}
	idx.osRepresentativeOutcomes[key]++
}

// affectedPackageIdentityCanMeetInstalledRow reports whether the fact's
// identity can meet an installed row at all (#10034 item 3): a purl names the
// advisory-side lookup key directly, and a purl-formed package id meets a row
// through the PackageID equality arm. An os:// package id with no purl, or a
// fact with neither, names neither, and rows key by purl, so such an identity
// can meet no row even when the package is installed.
func affectedPackageIdentityCanMeetInstalledRow(pkg supplychainmodel.AffectedPackage) bool {
	if strings.TrimSpace(pkg.PURL) != "" {
		return true
	}
	return strings.HasPrefix(packageidentity.LowerASCII(strings.TrimSpace(pkg.PackageID)), "pkg:")
}

// hasInstalledRowUnderLookupKeys reports whether any installed row is indexed
// under one of pkg's lookup keys, before the join's vendor and shape gates.
func (idx supplyChainImpactIndex) hasInstalledRowUnderLookupKeys(pkg supplychainmodel.AffectedPackage) bool {
	for _, key := range affectedOSPackageLookupKeys(pkg) {
		if len(idx.osPackages[key]) > 0 {
			return true
		}
	}
	return false
}

// osRepresentativesWithOutcome sums one outcome over vendors.
func (s supplyChainImpactBuildStats) osRepresentativesWithOutcome(outcome string) int {
	total := 0
	for key, count := range s.osRepresentativeOutcomes {
		if key.outcome == outcome {
			total += count
		}
	}
	return total
}

// recordOSRepresentativeOutcomes emits one counter point per non-zero cell and
// writes the outcome totals into the pass's result sub-signals.
func (h SupplyChainImpactHandler) recordOSRepresentativeOutcomes(
	ctx context.Context,
	stats supplyChainImpactBuildStats,
	subSignals map[string]float64,
) {
	subSignals[osRepresentativesJoinedSubSignal] = float64(stats.osRepresentativesWithOutcome(osRepresentativeJoined))
	subSignals[osRepresentativesReselectedSubSignal] = float64(stats.osRepresentativesWithOutcome(osRepresentativeReselected))
	subSignals[osRepresentativesOtherAnchorSubSignal] = float64(stats.osRepresentativesWithOutcome(osRepresentativeOtherAnchor))
	subSignals[osRepresentativesNoJoinableSubSignal] = float64(stats.osRepresentativesWithOutcome(osRepresentativeNoJoinable))
	subSignals[osRepresentativesNoInstalledRowSubSignal] = float64(stats.osRepresentativesWithOutcome(osRepresentativeNoInstalledRow))
	subSignals[osRepresentativesNoJoinableIdentitySubSignal] = float64(stats.osRepresentativesWithOutcome(osRepresentativeNoJoinableIdentity))
	if h.Instruments == nil || h.Instruments.SupplyChainImpactOSRepresentativeJoins == nil {
		return
	}
	for key, count := range stats.osRepresentativeOutcomes {
		if count == 0 {
			continue
		}
		h.Instruments.SupplyChainImpactOSRepresentativeJoins.Add(ctx, int64(count), metric.WithAttributes(
			telemetry.AttrSource(key.vendor),
			telemetry.AttrOutcome(key.outcome),
		))
	}
}
