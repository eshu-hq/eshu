// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"log/slog"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

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

// outOfStampActiveRowsSource reports whether one (scope, generation) holds
// active finding rows outside a stamped CVE set (#10415). The handler
// asserts it at runtime; a loader without it keeps the pre-fallback scoping
// behavior, so test doubles and legacy seams never fall back by accident.
type outOfStampActiveRowsSource interface {
	HasOutOfStampActiveSupplyChainImpactRows(ctx context.Context, scopeID, generationID string, cveIDs []string) (bool, error)
}

// scopedSupplyChainImpactFilterEnvelopes resolves one pass's re-derivation
// scope and the envelope subset its cross-scope filters derive from. A
// one-hop pass whose stamp names a bounded CVE set re-derives that set only
// (#9779). The scope carries the stamp and the package seeds; the subset is
// the stamped envelopes. The protected and static-only paths never scope:
// their authorization witnesses cover the whole static input set.
//
// The boolean reports the #10415 bounded full-pass fallback: the pass
// qualified for a scoped re-derivation but the holder carries active finding
// rows outside the stamp, so the pass runs full and reaps them instead of
// leaving them to linger.
func (h SupplyChainImpactHandler) scopedSupplyChainImpactFilterEnvelopes(
	ctx context.Context,
	intent reducercontract.Intent,
	envelopes []facts.Envelope,
) (*scopedSupplyChainImpactScope, []facts.Envelope, bool) {
	if cves := scopedPassCVEIDs(intent); len(cves) > 0 && h.AuthorizedEvidenceLoader == nil && !h.authorizedStaticOnly {
		if scoped := h.scopeSupplyChainImpactPass(ctx, intent, envelopes, cves); scoped != nil {
			if h.hasOutOfStampActiveSupplyChainImpactRows(ctx, intent, cves) {
				return nil, envelopes, true
			}
			return scoped, scopeSupplyChainImpactEnvelopes(envelopes, scoped), false
		}
	}
	return nil, envelopes, false
}

// hasOutOfStampActiveSupplyChainImpactRows reports whether the bounded
// full-pass fallback must fire: the store holds an active finding row the
// stamp does not name. A loader without the seam keeps scoping (the
// production fact store implements it). A check error fails closed to full:
// an unreadable store must not pin a scoped pass that leaves out-of-stamp
// rows behind.
func (h SupplyChainImpactHandler) hasOutOfStampActiveSupplyChainImpactRows(ctx context.Context, intent reducercontract.Intent, cves []string) bool {
	source, ok := h.FactLoader.(outOfStampActiveRowsSource)
	if !ok || source == nil {
		return false
	}
	has, err := source.HasOutOfStampActiveSupplyChainImpactRows(ctx, intent.ScopeID, intent.GenerationID, cves)
	if err != nil {
		// Log the probe failure: the mode counter cannot tell an
		// error-fallback from a drift-fallback, so without this line a
		// fallback spike during a store incident reads as drift reap.
		if h.Logger != nil {
			h.Logger.WarnContext(ctx, "supply chain impact out-of-stamp probe failed; falling back to full",
				slog.String("scope_id", intent.ScopeID),
				slog.String("generation_id", intent.GenerationID),
				slog.String("error", err.Error()),
			)
		}
		return true
	}
	return has
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

// scopeSupplyChainImpactEnvelopes selects the envelopes a scoped pass derives
// its evidence filters from: stamped advisories and signals, advisories an
// upstream key joins to the stamp, envelopes naming a seeded package,
// manifest content for seeded names, and envelopes with no CVE or package
// linkage, which cannot be attributed to a change and stay. Everything else
// belongs to findings this pass does not re-derive.
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
		return stampReachesAdvisory(envelope.Payload, stampSet) ||
			scopedAdvisoryLinked(scope, envelope.Payload)
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
