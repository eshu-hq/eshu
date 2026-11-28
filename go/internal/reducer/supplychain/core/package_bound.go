// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// normalizePackageAnchor folds a package_id or PURL the same way suppression
// and package matching compare them (packageAnchorEqual in scope.go: ASCII-only
// case folding over TrimSpace, keeping Unicode lookalikes distinct per #9584).
// The split keys provenance sets by this form while preserving the stored
// spelling on the filter lists so exact-match storage probes still bind the
// stored rows.
func normalizePackageAnchor(value string) string {
	return packageidentity.LowerASCII(strings.TrimSpace(value))
}

// SupplyChainImpactPackageExpansionBound is the #11097 provenance-bound
// switch. A non-nil pointer on the handler enables the direction split on
// package-to-CVE transitive expansion (production wiring sets it); nil keeps
// the prior unbounded semantics, which is also the one-line rollback. The
// struct carries no options: its presence is the switch, so the bound is never
// reinterpreted through a knob. The ruled design is
// docs/internal/design/11097-package-expansion-bound.md.
type SupplyChainImpactPackageExpansionBound struct{}

// supplyChainImpactPackageBoundOutcome is the per-pass telemetry of the #11097
// provenance bound: how many distinct transitive package identifiers and PURLs
// were suppressed (summed over rounds), how many loads ran, and the size of
// each package class remaining in the final follow-up filter (zero on a
// converged pass; non-zero on a round or budget cut). Zero when off.
type supplyChainImpactPackageBoundOutcome struct {
	SuppressedTransitivePackages int
	Loads                        int
	OwnedPackageIDs              int
	SeedPackageIDs               int
	OwnedPURLs                   int
	SeedPURLs                    int
	SeedCVECount                 int
}

// packageExpansionBoundState is the loop-local provenance state of one bounded
// pass: the seed vulnerability set (CVE and advisory ids closed over aliases
// and upstream keys), the owning scope, any scoped finding-package seeds, and
// the suppression counters. It lives in the until-stable loop, never on the
// handler, so concurrent passes cannot share seeds.
type packageExpansionBoundState struct {
	seeds map[string]struct{}
	// ownScopeID is the intent scope of the pass. Consumer/owner facts drive
	// both probes (affected_package_* and consumer/anchor probes) only when the
	// pass owns them (ownScopeID, or an SBOM component in an ownedDocument).
	// Own-scope OS-class affected_package facts also keep the 1-hop OS sibling
	// exemption so distro advisories split across scopes (DSA/DLA/USN/etc.)
	// consolidate identically (#11097 Decision 1).
	ownScopeID          string
	scoped              *scopedSupplyChainImpactScope
	suppressedTotal     int
	loads               int
	ownedPackageIDCount int
	seedPackageIDCount  int
	ownedPURLCount      int
	seedPURLCount       int
}

// newPackageExpansionBoundState seeds the bound from the pass's initial filter
// (CVEIDs and AdvisoryIDs) plus, for a scoped pass, the stamp CVEs and finding
// advisory seeds. Alias and upstream identity closure of seeded records grows
// the set as their records load (observe); transitive records loaded through
// shared packages never enter the seed set.
func newPackageExpansionBoundState(initial SupplyChainImpactFactFilter, scoped *scopedSupplyChainImpactScope, ownScopeID string) *packageExpansionBoundState {
	seeds := make(map[string]struct{}, len(initial.CVEIDs)+len(initial.AdvisoryIDs))
	for _, id := range initial.CVEIDs {
		if id != "" {
			seeds[id] = struct{}{}
		}
	}
	for _, id := range initial.AdvisoryIDs {
		if id != "" {
			seeds[id] = struct{}{}
		}
	}
	if scoped != nil {
		for _, id := range scoped.cves {
			if id != "" {
				seeds[id] = struct{}{}
			}
		}
		for id := range scoped.advisoryIDs {
			if id != "" {
				seeds[id] = struct{}{}
			}
		}
	}
	return &packageExpansionBoundState{
		seeds:      seeds,
		ownScopeID: ownScopeID,
		scoped:     scoped,
	}
}

func (s *packageExpansionBoundState) isSeeded(cveID, advisoryID string) bool {
	if cveID != "" {
		if _, ok := s.seeds[cveID]; ok {
			return true
		}
	}
	if advisoryID != "" {
		if _, ok := s.seeds[advisoryID]; ok {
			return true
		}
	}
	return false
}

// observe grows the seed set by identity closure of seeded vulnerability.cve
// records only: a record whose CVE id or advisory id is already seeded adds its
// CVE id, advisory id, aliases list, and upstream group keys. It loops to a
// local fixed point over envelopes so slice order within one round cannot hide
// an alias or upstream link. Transitive records never seed.
func (s *packageExpansionBoundState) observe(envelopes []facts.Envelope) {
	add := func(id string) bool {
		if id == "" {
			return false
		}
		if _, exists := s.seeds[id]; exists {
			return false
		}
		s.seeds[id] = struct{}{}
		return true
	}
	for {
		grew := false
		for _, envelope := range envelopes {
			if envelope.FactKind != chain.VulnerabilityCVEFactKind {
				continue
			}
			cveID := supplyChainCVEID(envelope.Payload)
			advisoryID := payloadcore.PayloadStr(envelope.Payload, "advisory_id")
			if !s.isSeeded(cveID, advisoryID) {
				continue
			}
			if add(cveID) {
				grew = true
			}
			if add(advisoryID) {
				grew = true
			}
			for _, alias := range appendAliasIDs(nil, envelope.Payload) {
				if add(alias) {
					grew = true
				}
			}
			for _, key := range appendUpstreamCVEKeys(nil, envelope.Payload) {
				if add(key) {
					grew = true
				}
			}
		}
		if !grew {
			return
		}
	}
}

// split partitions a derived filter's PackageIDs and PURLs by provenance
// (#11097 Decision 1):
//   - owned consumer/anchor/suppression sources (ownScopeID, plus SBOM
//     components in ownedDocuments) and own-scope OS-class affected_package
//     rows stay in PackageIDs/PURLs and drive all probes, including
//     affected_package_package_id, affected_package_purl, and foreign_key_guard;
//     owned PURLs also admit their version-stripped packageIDFromPURL form so
//     version-qualified SBOM components keep versionless advisory and
//     suppression PURLs on the owned arm;
//   - seeded non-exempt affected_package rows (and non-OS scoped finding seeds)
//     move to SeedPackageIDs/SeedPURLs, driving consumer/anchor/suppression
//     probes without pulling unrelated vulnerabilities via affected_package_*;
//   - every other package identifier or PURL (foreign consumer packages,
//     foreign suppressions, transitive non-seed advisories) is suppressed and
//     counted;
//   - foreign security_alert and vulnerability.suppression rows loaded solely
//     through a seed-only package (non-owned package, non-seeded CVE/advisory)
//     cannot widen next.CVEIDs or next.AdvisoryIDs into unrelated CVEs.
//
// Owned membership wins over seed membership. Each output list is diffed
// against requested so re-derived values never spin the loop.
func (s *packageExpansionBoundState) split(filter SupplyChainImpactFactFilter, envelopes []facts.Envelope, requested SupplyChainImpactFactFilter) (SupplyChainImpactFactFilter, int) {
	ownedPkgIDs := make(map[string]struct{})
	ownedPURLs := make(map[string]struct{})
	seedPkgIDs := make(map[string]struct{})
	seedPURLs := make(map[string]struct{})

	addAnchor := func(set map[string]struct{}, value string) {
		if norm := normalizePackageAnchor(value); norm != "" {
			set[norm] = struct{}{}
		}
	}
	addPURL := func(set map[string]struct{}, purl string) {
		addAnchor(set, purl)
		if unversioned := packageIDFromPURL(purl); unversioned != "" {
			addAnchor(set, unversioned)
		}
	}

	ownDocs := ownedDocuments(envelopes, s.ownScopeID)
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case chain.SBOMComponentFactKind:
			if _, ownDoc := ownDocs[payloadcore.PayloadStr(envelope.Payload, "document_id")]; envelope.ScopeID != s.ownScopeID && !ownDoc {
				continue
			}
			addAnchor(ownedPkgIDs, payloadcore.PayloadStr(envelope.Payload, "package_id"))
			addPURL(ownedPURLs, payloadcore.PayloadStr(envelope.Payload, "purl"))
		case correlation.PackageConsumptionFactKind,
			chain.VulnerabilityGoModuleEvidenceFactKind,
			facts.SecurityAlertRepositoryAlertFactKind,
			chain.PackageRegistryPackageFactKind:
			if envelope.ScopeID != s.ownScopeID {
				continue
			}
			addAnchor(ownedPkgIDs, payloadcore.PayloadStr(envelope.Payload, "package_id"))
		case chain.VulnerabilitySuppressionFactKind:
			if envelope.ScopeID != s.ownScopeID {
				continue
			}
			if scope := payloadcore.PayloadMap(envelope.Payload, "scope"); scope != nil {
				addAnchor(ownedPkgIDs, payloadcore.PayloadStr(scope, "package_id"))
				addPURL(ownedPURLs, payloadcore.PayloadStr(scope, "purl"))
			}
		case factload.FactKindContentEntity:
			if envelope.ScopeID != s.ownScopeID {
				continue
			}
			for _, dependency := range correlation.ExtractPackageManifestDependencies([]facts.Envelope{envelope}) {
				if pkgID, purl := canonicalGoManifestPackageIdentity(dependency); pkgID != "" {
					addAnchor(ownedPkgIDs, pkgID)
					addPURL(ownedPURLs, purl)
				}
			}
		case chain.VulnerabilityAffectedPackageFactKind:
			purl := payloadcore.PayloadStr(envelope.Payload, "purl")
			pkgID := canonicalSupplyChainAffectedPackageID(payloadcore.PayloadStr(envelope.Payload, "package_id"), purl)
			eco := payloadcore.PayloadStr(envelope.Payload, "ecosystem")
			if envelope.ScopeID == s.ownScopeID && (osPackageClassEcosystem(eco) || osPackageFamilyFromPURL(purl) != "") {
				addAnchor(ownedPkgIDs, pkgID)
				addPURL(ownedPURLs, purl)
				continue
			}
			if s.isSeeded(supplyChainCVEID(envelope.Payload), payloadcore.PayloadStr(envelope.Payload, "advisory_id")) {
				addAnchor(seedPkgIDs, pkgID)
				addPURL(seedPURLs, purl)
			}
		}
	}

	if s.scoped != nil {
		for id := range s.scoped.osPackages {
			addAnchor(ownedPkgIDs, id)
		}
		for purl := range s.scoped.osPURLs {
			addPURL(ownedPURLs, purl)
		}
		for id := range s.scoped.packages {
			addAnchor(seedPkgIDs, id)
		}
		for purl := range s.scoped.purls {
			addPURL(seedPURLs, purl)
		}
	}

	var ownedPkg, seedPkg []string
	suppressedPkg := make(map[string]struct{})
	for _, id := range filter.PackageIDs {
		switch norm := normalizePackageAnchor(id); {
		case norm == "":
		case containsCriteria(ownedPkgIDs, norm):
			ownedPkg = append(ownedPkg, id)
		case containsCriteria(seedPkgIDs, norm):
			seedPkg = append(seedPkg, id)
		default:
			suppressedPkg[id] = struct{}{}
		}
	}

	var ownedPURL, seedPURL []string
	suppressedPURL := make(map[string]struct{})
	for _, purl := range filter.PURLs {
		switch norm := normalizePackageAnchor(purl); {
		case norm == "":
		case containsCriteria(ownedPURLs, norm):
			ownedPURL = append(ownedPURL, purl)
		case containsCriteria(seedPURLs, norm):
			seedPURL = append(seedPURL, purl)
		default:
			suppressedPURL[purl] = struct{}{}
		}
	}

	requestedSeedPkg := unionSortedStrings(requested.PackageIDs, requested.SeedPackageIDs)
	requestedSeedPURL := unionSortedStrings(requested.PURLs, requested.SeedPURLs)
	filter.PackageIDs = missingStringValues(payloadcore.UniqueSortedStrings(ownedPkg), requested.PackageIDs)
	filter.SeedPackageIDs = missingStringValues(payloadcore.UniqueSortedStrings(seedPkg), requestedSeedPkg)
	filter.PURLs = missingStringValues(payloadcore.UniqueSortedStrings(ownedPURL), requested.PURLs)
	filter.SeedPURLs = missingStringValues(payloadcore.UniqueSortedStrings(seedPURL), requestedSeedPURL)
	filter = s.dropForeignSeedPackageAlertAndSuppressionCVEs(filter, envelopes, ownedPkgIDs, ownedPURLs)

	suppressed := len(suppressedPkg) + len(suppressedPURL)
	s.suppressedTotal += suppressed
	s.ownedPackageIDCount = len(filter.PackageIDs)
	s.seedPackageIDCount = len(filter.SeedPackageIDs)
	s.ownedPURLCount = len(filter.PURLs)
	s.seedPURLCount = len(filter.SeedPURLs)
	return filter, suppressed
}

// dropForeignSeedPackageAlertAndSuppressionCVEs stops foreign security_alert
// and vulnerability.suppression rows loaded solely via SeedPackageIDs/SeedPURLs
// from pulling unrelated CVEs into a seed-only pass. Any CVE or advisory ID
// emitted by a seeded record, an own-scope row, an owned-package alert or
// suppression, or any non-alert/non-suppression envelope stays untouched.
func (s *packageExpansionBoundState) dropForeignSeedPackageAlertAndSuppressionCVEs(
	filter SupplyChainImpactFactFilter,
	envelopes []facts.Envelope,
	ownedPkgIDs map[string]struct{},
	ownedPURLs map[string]struct{},
) SupplyChainImpactFactFilter {
	if len(filter.CVEIDs) == 0 && len(filter.AdvisoryIDs) == 0 {
		return filter
	}
	allowedCVEs := make(map[string]struct{}, len(s.seeds))
	allowedAdvs := make(map[string]struct{}, len(s.seeds))
	for id := range s.seeds {
		allowedCVEs[id] = struct{}{}
		allowedAdvs[id] = struct{}{}
	}
	allowCVE := func(id string) {
		if id != "" {
			allowedCVEs[id] = struct{}{}
		}
	}
	allowAdv := func(id string) {
		if id != "" {
			allowedAdvs[id] = struct{}{}
		}
	}
	hasOwnedAnchor := func(pkgID, purl string) bool {
		if norm := normalizePackageAnchor(pkgID); norm != "" && containsCriteria(ownedPkgIDs, norm) {
			return true
		}
		if norm := normalizePackageAnchor(purl); norm != "" && containsCriteria(ownedPURLs, norm) {
			return true
		}
		if unv := normalizePackageAnchor(packageIDFromPURL(purl)); unv != "" && containsCriteria(ownedPURLs, unv) {
			return true
		}
		return false
	}
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.SecurityAlertRepositoryAlertFactKind:
			if env.ScopeID != s.ownScopeID && !hasOwnedAnchor(payloadcore.PayloadStr(env.Payload, "package_id"), "") {
				continue
			}
			for _, id := range payloadcore.PayloadStrings(env.Payload, "cve_id", "cve_ids") {
				allowCVE(id)
			}
		case chain.VulnerabilitySuppressionFactKind:
			scope := payloadcore.PayloadMap(env.Payload, "scope")
			if scope == nil {
				continue
			}
			if env.ScopeID != s.ownScopeID && !hasOwnedAnchor(payloadcore.PayloadStr(scope, "package_id"), payloadcore.PayloadStr(scope, "purl")) {
				continue
			}
			allowCVE(payloadcore.PayloadStr(scope, "cve_id"))
			allowAdv(payloadcore.PayloadStr(scope, "advisory_id"))
		case chain.VulnerabilityCVEFactKind, chain.VulnerabilityAffectedPackageFactKind, chain.VulnerabilityAffectedProductFactKind:
			allowCVE(supplyChainCVEID(env.Payload))
			allowAdv(payloadcore.PayloadStr(env.Payload, "advisory_id"))
		case chain.VulnerabilityEPSSScoreFactKind, chain.VulnerabilityKnownExploitedFactKind:
			allowCVE(supplyChainCVEID(env.Payload))
		case chain.VulnerabilityGoCallReachabilityFactKind:
			allowCVE(payloadcore.PayloadStr(env.Payload, "osv_id"))
		}
	}
	keep := func(values []string, allowed map[string]struct{}) []string {
		if len(values) == 0 {
			return values
		}
		out := make([]string, 0, len(values))
		for _, v := range values {
			if _, ok := allowed[v]; ok {
				out = append(out, v)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	filter.CVEIDs = keep(filter.CVEIDs, allowedCVEs)
	filter.AdvisoryIDs = keep(filter.AdvisoryIDs, allowedAdvs)
	return filter
}

// outcome snapshots the loop-local #11097 bound state for telemetry. A nil
// state (bound off) reports the zero outcome.
func (s *packageExpansionBoundState) outcome() supplyChainImpactPackageBoundOutcome {
	if s == nil {
		return supplyChainImpactPackageBoundOutcome{}
	}
	return supplyChainImpactPackageBoundOutcome{
		SuppressedTransitivePackages: s.suppressedTotal,
		Loads:                        s.loads,
		OwnedPackageIDs:              s.ownedPackageIDCount,
		SeedPackageIDs:               s.seedPackageIDCount,
		OwnedPURLs:                   s.ownedPURLCount,
		SeedPURLs:                    s.seedPURLCount,
		SeedCVECount:                 len(s.seeds),
	}
}

// apply runs one derivation step of the package provenance bound: identity
// closure over the newly loaded records, then the package/PURL split. A nil
// state (bound off) returns the filter untouched, keeping the until-stable
// loop byte-for-byte on the prior semantics.
func (s *packageExpansionBoundState) apply(filter SupplyChainImpactFactFilter, envelopes, fresh []facts.Envelope, requested SupplyChainImpactFactFilter) (SupplyChainImpactFactFilter, int) {
	if s == nil {
		return filter, 0
	}
	s.observe(fresh)
	return s.split(filter, envelopes, requested)
}

// mergeExpansionBoundSubSignals adds both provenance-bound sub-signal sets
// (#11087 criteria and #11097 package) on every pass, zeros included when off.
func mergeExpansionBoundSubSignals(subSignals map[string]float64, loaded supplyChainImpactLoadedEvidence) {
	mergeCriteriaBoundSubSignals(subSignals, loaded)
	mergePackageBoundSubSignals(subSignals, loaded)
}

// mergePackageBoundSubSignals adds the #11097 provenance-bound signals to a
// result's sub-signals on every pass, zeros included when the bound is off, so
// bound-on and bound-off passes stay comparable.
func mergePackageBoundSubSignals(subSignals map[string]float64, loaded supplyChainImpactLoadedEvidence) {
	bound := loaded.expansionBounds.pkg
	subSignals["package_bound_suppressed_transitive"] = float64(bound.SuppressedTransitivePackages)
	subSignals["package_bound_loads"] = float64(bound.Loads)
	subSignals["package_bound_owned_package_ids"] = float64(bound.OwnedPackageIDs)
	subSignals["package_bound_seed_package_ids"] = float64(bound.SeedPackageIDs)
	subSignals["package_bound_owned_purls"] = float64(bound.OwnedPURLs)
	subSignals["package_bound_seed_purls"] = float64(bound.SeedPURLs)
	subSignals["package_bound_seed_cves"] = float64(bound.SeedCVECount)
}
