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
	// lookup keys. Usually the package is not on any scanned host this index holds;
	// an os:// package id with no purl also lands here, because rows key by purl.
	osRepresentativeNoInstalledRow = "no_installed_row"
)

// Result.SubSignals keys for the representative outcomes. Every key is written
// on every pass, 0 included, so a clean pass is visible on the success line.
const (
	osRepresentativesJoinedSubSignal         = "os_representatives_joined"
	osRepresentativesReselectedSubSignal     = "os_representatives_reselected"
	osRepresentativesOtherAnchorSubSignal    = "os_representatives_other_anchor"
	osRepresentativesNoJoinableSubSignal     = "os_representatives_no_joinable"
	osRepresentativesNoInstalledRowSubSignal = "os_representatives_no_installed_row"
)

// osRepresentativeOutcomeKey is one counter cell: the closed vendor label of the
// fact that named the group, and the outcome.
type osRepresentativeOutcomeKey struct {
	vendor  string
	outcome string
}

// withJoinableOSRepresentative returns a copy of pkgs with the fact that should
// name the finding marked, when the source-order representative has no anchor of
// its own and another fact of the group joins an installed row (#9990).
//
// It runs only for a group that produces no finding without it: the caller has
// already found no SBOM path, this checks no consuming repository and no OS join,
// and those are the only sources of the owned anchor
// supplyChainImpactFindingHasOwnedAnchor requires. A group with any anchor keeps
// its source-order representative, so a finding that exists is not moved by a
// fact that could also have joined.
//
// The input slice is never modified: the index shares it across installed rows
// and across scan targets, and a different target may join a different fact. The
// mark rides on the copy, so a later representativeAffectedPackage call on the
// slice, or on selectOSAdvisoryRelease's narrowed copy of it, agrees.
func withJoinableOSRepresentative(
	pkgs []supplychainmodel.AffectedPackage,
	representative supplychainmodel.AffectedPackage,
	index supplyChainImpactIndex,
) ([]supplychainmodel.AffectedPackage, bool) {
	if len(pkgs) < 2 || len(index.osPackages) == 0 {
		return pkgs, false
	}
	if len(consumptionRepositoriesForPackage(representative.PackageID, index.consumption)) > 0 ||
		hasMatchingOSPackage(representative, index) {
		return pkgs, false
	}
	best, bestRank := -1, 0
	for i, pkg := range pkgs {
		installed, joins := firstOSPackageImpactPath(pkg, index)
		if !joins {
			continue
		}
		rank := vendorClassPriorityRank(installed.VendorAdvisorySource, classifyAdvisorySource(pkg.Source, pkg.AdvisoryID))
		if best < 0 || joinableRepresentativeBefore(rank, pkg, bestRank, pkgs[best]) {
			best, bestRank = i, rank
		}
	}
	if best < 0 {
		return pkgs, false
	}
	marked := slices.Clone(pkgs)
	marked[best].JoinRepresentative = true
	return marked, true
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
	default:
		key.outcome = osRepresentativeNoInstalledRow
	}
	idx.osRepresentativeOutcomes[key]++
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
