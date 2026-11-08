// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// ScopedFindingPackage is one (CVE, package) identity a scope's active
// finding rows carry for a stamped CVE (#9779). The scoped pass seeds its
// evidence filters from these rows instead of from everything the scope
// holds, so the manifest name list and the cross-scope evidence filter
// follow the stamped change.
type ScopedFindingPackage struct {
	CVEID       string
	PackageID   string
	PURL        string
	PackageName string
	Ecosystem   string
}

// scopedFindingPackageSource loads the finding-row package seeds of one
// (scope, generation) for stamped CVEs. The handler asserts it at runtime;
// a loader without it runs the pass full, so test doubles and legacy seams
// never scope by accident.
type scopedFindingPackageSource interface {
	ListScopedSupplyChainImpactFindingPackages(ctx context.Context, scopeID, generationID string, cveIDs []string) ([]ScopedFindingPackage, error)
}

// scopedSupplyChainImpactScope is what one re-derivation may load: the
// stamped CVEs plus the package, advisory, and upstream identities those
// CVEs reach through the scope's own advisories and finding rows.
type scopedSupplyChainImpactScope struct {
	cves        []string
	packages    map[string]struct{}
	purls       map[string]struct{}
	names       map[string]struct{}
	ecosystems  map[string]struct{}
	advisoryIDs map[string]struct{}
	upstream    map[string]struct{}
}

// scopedPassCVEIDs decides whether this pass re-derives a scoped set (#9779).
// Only a one-hop pass whose stamp names a bounded CVE set qualifies: the
// sentinel, a stamp without a set, and any genuine pass (including an owed
// retry, which may be establishing rows) run full.
func scopedPassCVEIDs(intent reducercontract.Intent) []string {
	if !fanOutOneHop(intent) {
		return nil
	}
	stamp := fanOutStampCVEIDs(intent)
	if len(stamp) == 0 || fanOutStampSentinel(stamp) {
		return nil
	}
	out := append([]string(nil), stamp...)
	sort.Strings(out)
	return out
}

// seedScopedSupplyChainImpactScope collects the identities a scoped pass may
// load: the stamped CVEs, the packages those CVEs reach (the scope's own
// stamped advisories plus the finding rows that made this scope a holder),
// and the advisory and upstream keys of the stamped advisories. It reports
// false when there is nothing to scope by — no finding rows and no local
// stamped advisory — so the pass runs full instead of loading an empty set.
func seedScopedSupplyChainImpactScope(
	scopeEnvelopes []facts.Envelope,
	findingPackages []ScopedFindingPackage,
	stamp []string,
) (*scopedSupplyChainImpactScope, bool) {
	stampSet := make(map[string]struct{}, len(stamp))
	for _, id := range stamp {
		stampSet[id] = struct{}{}
	}
	scope := &scopedSupplyChainImpactScope{
		cves:        append([]string(nil), stamp...),
		packages:    map[string]struct{}{},
		purls:       map[string]struct{}{},
		names:       map[string]struct{}{},
		ecosystems:  map[string]struct{}{},
		advisoryIDs: map[string]struct{}{},
		upstream:    map[string]struct{}{},
	}
	for _, envelope := range scopeEnvelopes {
		switch envelope.FactKind {
		case chain.VulnerabilityCVEFactKind,
			chain.VulnerabilityAffectedPackageFactKind,
			chain.VulnerabilityAffectedProductFactKind:
			if !inStampSet(stampSet, supplyChainCVEID(envelope.Payload)) {
				continue
			}
			seedScopedAdvisoryPackage(scope, envelope)
			if id := payloadcore.PayloadStr(envelope.Payload, "advisory_id"); id != "" {
				scope.advisoryIDs[id] = struct{}{}
			}
			for _, key := range appendUpstreamCVEKeys(nil, envelope.Payload) {
				scope.upstream[key] = struct{}{}
			}
		case chain.VulnerabilityEPSSScoreFactKind,
			chain.VulnerabilityKnownExploitedFactKind:
			if !inStampSet(stampSet, supplyChainCVEID(envelope.Payload)) {
				continue
			}
		case chain.VulnerabilitySuppressionFactKind:
			scopeMap := payloadcore.PayloadMap(envelope.Payload, "scope")
			if scopeMap == nil {
				continue
			}
			if _, ok := stampSet[payloadcore.PayloadStr(scopeMap, "cve_id")]; !ok {
				continue
			}
			if id := payloadcore.PayloadStr(scopeMap, "package_id"); id != "" {
				scope.packages[id] = struct{}{}
			}
		case facts.SecurityAlertRepositoryAlertFactKind:
			hit := false
			for _, id := range payloadcore.PayloadStrings(envelope.Payload, "cve_id", "cve_ids") {
				if _, ok := stampSet[id]; ok {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
			seedScopedAdvisoryPackage(scope, envelope)
		}
	}
	for _, row := range findingPackages {
		if _, ok := stampSet[row.CVEID]; !ok {
			continue
		}
		if id := canonicalSupplyChainAffectedPackageID(row.PackageID, row.PURL); id != "" {
			scope.packages[id] = struct{}{}
		}
		if row.PURL != "" {
			scope.purls[row.PURL] = struct{}{}
		}
		eco := string(packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(row.Ecosystem)))
		if eco != "" {
			scope.ecosystems[eco] = struct{}{}
		}
		for _, name := range scopedFindingPackageNameCandidates(row, eco) {
			scope.names[name] = struct{}{}
		}
	}
	if len(scope.packages) == 0 && len(scope.advisoryIDs) == 0 {
		return nil, false
	}
	return scope, true
}

// seedScopedAdvisoryPackage folds one stamped advisory envelope's package
// identities into the scope, using the same canonicalization and name
// candidates the manifest filter derivation uses, so the seeded names match
// the names the full derivation would emit for the same envelopes.
func seedScopedAdvisoryPackage(scope *scopedSupplyChainImpactScope, envelope facts.Envelope) {
	switch envelope.FactKind {
	case chain.VulnerabilityAffectedPackageFactKind:
		pkg, ok := supplyChainImpactManifestDependencyPackage(envelope)
		if !ok {
			return
		}
		if id := canonicalSupplyChainAffectedPackageID(pkg.PackageID, pkg.PURL); id != "" {
			scope.packages[id] = struct{}{}
		}
		if pkg.PURL != "" {
			scope.purls[pkg.PURL] = struct{}{}
		}
		if id := payloadcore.PayloadStr(envelope.Payload, "advisory_id"); id != "" {
			scope.advisoryIDs[id] = struct{}{}
		}
		eco := string(packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(pkg.Ecosystem)))
		if eco != "" {
			scope.ecosystems[eco] = struct{}{}
		}
		for _, name := range supplyChainAffectedPackageNameCandidates(pkg) {
			for _, candidate := range correlation.PackageConsumptionNameCandidates(packageidentity.Ecosystem(eco), name) {
				scope.names[candidate] = struct{}{}
			}
		}
	case facts.SecurityAlertRepositoryAlertFactKind:
		if id := payloadcore.PayloadStr(envelope.Payload, "package_id"); id != "" {
			scope.packages[id] = struct{}{}
		}
	}
}

// scopedFindingPackageNameCandidates mirrors the manifest name pipeline for
// a finding row: the row's own name plus the purl and package-id spellings,
// each folded per ecosystem the way the filter derivation folds them.
func scopedFindingPackageNameCandidates(row ScopedFindingPackage, ecosystem string) []string {
	var out []string
	for _, name := range []string{row.PackageName, packageNameFromPURL(row.PURL), packageNameFromPackageID(row.PackageID)} {
		if name == "" {
			continue
		}
		out = append(out, correlation.PackageConsumptionNameCandidates(packageidentity.Ecosystem(ecosystem), name)...)
	}
	return payloadcore.UniqueSortedStrings(out)
}

// scopedSupplyChainImpactFilter derives the cross-scope evidence filter for
// one pass. A scoped pass derives it from the stamped envelopes and
// restricts its CVE and package arms to the stamp and its seeds; the
// until-stable loop then expands only along what the stamp reaches.
// Finding-row seeds join the package arms because their advisories may live
// cross-scope, so envelope derivation alone would miss them on the first
// round; the follow-up path never re-adds them (it only intersects), so the
// loop still terminates.
func (h SupplyChainImpactHandler) scopedSupplyChainImpactFilter(envelopes []facts.Envelope, scoped *scopedSupplyChainImpactScope) SupplyChainImpactFactFilter {
	if scoped == nil {
		return supplyChainImpactFilter(envelopes)
	}
	filter := restrictSupplyChainImpactFilter(
		supplyChainImpactFilter(scopeSupplyChainImpactEnvelopes(envelopes, scoped)), scoped)
	filter.PackageIDs = unionSortedStrings(filter.PackageIDs, setKeys(scoped.packages))
	filter.PURLs = unionSortedStrings(filter.PURLs, setKeys(scoped.purls))
	return filter
}

// scopedSupplyChainImpactFilterEnvelopes resolves one pass's re-derivation
// scope and the envelope subset its cross-scope filters derive from. A
// one-hop pass whose stamp names a bounded CVE set re-derives that set only
// (#9779). The scope carries the stamp and the package seeds; the subset is
// the stamped envelopes. The protected and static-only paths never scope:
// their authorization witnesses cover the whole static input set.
func (h SupplyChainImpactHandler) scopedSupplyChainImpactFilterEnvelopes(
	ctx context.Context,
	intent reducercontract.Intent,
	envelopes []facts.Envelope,
) (*scopedSupplyChainImpactScope, []facts.Envelope) {
	if cves := scopedPassCVEIDs(intent); len(cves) > 0 && h.AuthorizedEvidenceLoader == nil && !h.authorizedStaticOnly {
		if scoped := h.scopeSupplyChainImpactPass(ctx, intent, envelopes, cves); scoped != nil {
			return scoped, scopeSupplyChainImpactEnvelopes(envelopes, scoped)
		}
	}
	return nil, envelopes
}

// scopeSupplyChainImpactPass resolves one pass's re-derivation scope: the
// stamp plus the finding-row seeds. A loader without the seed seam, a failed
// seed read, or an empty seed set all run the pass full: scoping is an
// optimization, and it must never fail a pass or starve it of evidence.
func (h SupplyChainImpactHandler) scopeSupplyChainImpactPass(
	ctx context.Context,
	intent reducercontract.Intent,
	scopeEnvelopes []facts.Envelope,
	stamp []string,
) *scopedSupplyChainImpactScope {
	var seeds []ScopedFindingPackage
	if source, ok := h.FactLoader.(scopedFindingPackageSource); ok && source != nil {
		rows, err := source.ListScopedSupplyChainImpactFindingPackages(ctx, intent.ScopeID, intent.GenerationID, stamp)
		if err != nil {
			return nil
		}
		seeds = rows
	}
	scope, ok := seedScopedSupplyChainImpactScope(scopeEnvelopes, seeds, stamp)
	if !ok {
		return nil
	}
	return scope
}

// inStampSet reports whether an id is in the stamp. It keeps the seeding
// switch readable and gives the linter a named predicate instead of a blank
// map probe.
func inStampSet(set map[string]struct{}, id string) bool {
	_, ok := set[id]
	return ok
}

// scopeSupplyChainImpactEnvelopes selects the envelopes a scoped pass derives
// its evidence filters from: stamped advisories and signals, envelopes naming
// a seeded package, manifest content for seeded names, and envelopes with no
// CVE or package linkage, which cannot be attributed to a change and stay.
// Everything else belongs to findings this pass does not re-derive.
func scopeSupplyChainImpactEnvelopes(envelopes []facts.Envelope, scope *scopedSupplyChainImpactScope) []facts.Envelope {
	stampSet := make(map[string]struct{}, len(scope.cves))
	for _, id := range scope.cves {
		stampSet[id] = struct{}{}
	}
	out := make([]facts.Envelope, 0, len(envelopes))
	for _, envelope := range envelopes {
		if scopedEnvelopeIncluded(envelope, scope, stampSet) {
			out = append(out, envelope)
		}
	}
	return out
}

// scopedEnvelopeIncluded is the per-envelope rule behind
// scopeSupplyChainImpactEnvelopes.
func scopedEnvelopeIncluded(envelope facts.Envelope, scope *scopedSupplyChainImpactScope, stampSet map[string]struct{}) bool {
	switch envelope.FactKind {
	case chain.VulnerabilityCVEFactKind,
		chain.VulnerabilityAffectedPackageFactKind,
		chain.VulnerabilityAffectedProductFactKind:
		_, ok := stampSet[supplyChainCVEID(envelope.Payload)]
		return ok
	case chain.VulnerabilityGoCallReachabilityFactKind:
		_, ok := stampSet[payloadcore.PayloadStr(envelope.Payload, "osv_id")]
		return ok
	case facts.SecurityAlertRepositoryAlertFactKind:
		for _, id := range payloadcore.PayloadStrings(envelope.Payload, "cve_id", "cve_ids") {
			if _, ok := stampSet[id]; ok {
				return true
			}
		}
		return scopedEnvelopePackageLinked(envelope, scope)
	case chain.VulnerabilityEPSSScoreFactKind,
		chain.VulnerabilityKnownExploitedFactKind:
		_, ok := stampSet[supplyChainCVEID(envelope.Payload)]
		return ok
	case chain.VulnerabilitySuppressionFactKind:
		if scopeMap := payloadcore.PayloadMap(envelope.Payload, "scope"); scopeMap != nil {
			if _, ok := stampSet[payloadcore.PayloadStr(scopeMap, "cve_id")]; ok {
				return true
			}
		}
		return scopedEnvelopePackageLinked(envelope, scope)
	case factload.FactKindContentEntity:
		// The scope's own manifest evidence stays: the manifest-load filter
		// derivation ignores this kind (it reads affected-package and
		// security-alert envelopes only), so keeping these rows cannot widen
		// the name list, and the consumption joins need them.
		return true
	default:
		if len(scopedEnvelopePackageKeys(envelope)) == 0 && len(scopedEnvelopePURLs(envelope)) == 0 {
			return true
		}
		return scopedEnvelopePackageLinked(envelope, scope)
	}
}

// scopedEnvelopePackageLinked reports whether an envelope names a seeded package.
func scopedEnvelopePackageLinked(envelope facts.Envelope, scope *scopedSupplyChainImpactScope) bool {
	for _, key := range scopedEnvelopePackageKeys(envelope) {
		if _, ok := scope.packages[key]; ok {
			return true
		}
	}
	for _, purl := range scopedEnvelopePURLs(envelope) {
		if _, ok := scope.purls[purl]; ok {
			return true
		}
	}
	return false
}

// scopedEnvelopePackageKeys mirrors the active filter's package arms, so the
// subset keeps exactly the envelopes the full filter would attribute to the
// seeded packages.
func scopedEnvelopePackageKeys(envelope facts.Envelope) []string {
	var out []string
	switch envelope.FactKind {
	case chain.VulnerabilityAffectedPackageFactKind:
		out = append(out, canonicalSupplyChainAffectedPackageID(
			payloadcore.PayloadStr(envelope.Payload, "package_id"),
			payloadcore.PayloadStr(envelope.Payload, "purl")))
	case chain.PackageRegistryPackageFactKind,
		correlation.PackageConsumptionFactKind,
		chain.VulnerabilityGoModuleEvidenceFactKind:
		out = append(out, payloadcore.PayloadStr(envelope.Payload, "package_id"))
	case chain.SBOMComponentFactKind:
		out = append(out, canonicalSupplyChainAffectedPackageID(
			payloadcore.PayloadStr(envelope.Payload, "package_id"),
			payloadcore.PayloadStr(envelope.Payload, "purl")))
	case chain.VulnerabilitySuppressionFactKind:
		if scopeMap := payloadcore.PayloadMap(envelope.Payload, "scope"); scopeMap != nil {
			out = append(out, payloadcore.PayloadStr(scopeMap, "package_id"))
		}
	case facts.SecurityAlertRepositoryAlertFactKind:
		out = append(out, payloadcore.PayloadStr(envelope.Payload, "package_id"))
	}
	kept := out[:0]
	for _, id := range out {
		if id != "" {
			kept = append(kept, id)
		}
	}
	return kept
}

// scopedEnvelopePURLs mirrors the active filter's purl arm.
func scopedEnvelopePURLs(envelope facts.Envelope) []string {
	var out []string
	switch envelope.FactKind {
	case chain.VulnerabilityAffectedPackageFactKind,
		chain.SBOMComponentFactKind:
		out = append(out, payloadcore.PayloadStr(envelope.Payload, "purl"))
	case chain.VulnerabilitySuppressionFactKind:
		if scopeMap := payloadcore.PayloadMap(envelope.Payload, "scope"); scopeMap != nil {
			out = append(out, payloadcore.PayloadStr(scopeMap, "purl"))
		}
	}
	kept := out[:0]
	for _, purl := range out {
		if purl != "" {
			kept = append(kept, purl)
		}
	}
	return kept
}

// restrictSupplyChainImpactFilter narrows a derived evidence filter to a
// scoped pass: CVE-bearing arms intersect the stamp, package-bearing arms
// intersect the seeds, and the upstream arm keeps only seeded group keys.
// It intersects in place: every caller hands it a freshly derived filter,
// so no retained backing array is ever shared.
// Arms without CVE or package linkage stay, because they cannot be
// attributed to a change; the differential proves the attribution complete.
// It never adds values, so a follow-up it empties ends the until-stable loop
// instead of spinning on re-requested seeds: callers union the finding-row
// seeds into the initial filter only.
func restrictSupplyChainImpactFilter(filter SupplyChainImpactFactFilter, scope *scopedSupplyChainImpactScope) SupplyChainImpactFactFilter {
	stampSet := make(map[string]struct{}, len(scope.cves))
	for _, id := range scope.cves {
		stampSet[id] = struct{}{}
	}
	intersect := func(values []string, set map[string]struct{}) []string {
		kept := values[:0]
		for _, value := range values {
			if _, ok := set[value]; ok {
				kept = append(kept, value)
			}
		}
		return kept
	}
	filter.CVEIDs = intersect(filter.CVEIDs, stampSet)
	filter.UpstreamCVEIDs = intersect(filter.UpstreamCVEIDs, scope.upstream)
	filter.PackageIDs = intersect(filter.PackageIDs, scope.packages)
	filter.PURLs = intersect(filter.PURLs, scope.purls)
	filter.AdvisoryIDs = intersect(filter.AdvisoryIDs, scope.advisoryIDs)
	return filter
}

// restrictManifestFilterToScope unions the finding-row seeds into a manifest
// filter derived from scope envelopes: finding rows name packages whose
// advisories may live cross-scope, so envelope derivation alone would miss
// their manifest names.
func restrictManifestFilterToScope(filter correlation.PackageManifestDependencyFactFilter, scope *scopedSupplyChainImpactScope) correlation.PackageManifestDependencyFactFilter {
	filter.Ecosystems = unionSortedStrings(filter.Ecosystems, setKeys(scope.ecosystems))
	filter.PackageNames = unionSortedStrings(filter.PackageNames, setKeys(scope.names))
	return filter
}

// setKeys returns the sorted keys of a set.
func setKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// emitScopedPassSignals records how one findings pass ran (#9779): one
// pass-total count by mode, once per pass that completes its evidence load,
// whatever the downstream stages do, so scoped and full stay comparable.
func (h SupplyChainImpactHandler) emitScopedPassSignals(ctx context.Context, intent reducercontract.Intent, loaded supplyChainImpactLoadedEvidence) {
	h.countSupplyChainImpactPass(ctx, supplyChainImpactPassMode(intent, loaded))
}

// mergeScopedPassSubSignals adds the #9779 re-derivation signals to a
// result's sub-signals: the stamped set size and the evidence envelopes a
// scoped pass loaded (zero on a full pass, so the series stays comparable).
func mergeScopedPassSubSignals(subSignals map[string]float64, loaded supplyChainImpactLoadedEvidence) {
	subSignals["scoped_cve_count"] = float64(len(scopedWriteCVEIDs(loaded)))
	if loaded.scoped != nil {
		subSignals["scoped_evidence_envelopes"] = float64(loaded.evidenceEnvelopeCount())
	} else {
		subSignals["scoped_evidence_envelopes"] = 0
	}
}
