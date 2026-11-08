// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// ScopedFindingPackage is one (CVE, package) identity a scope's active
// finding rows carry for a stamped CVE (#9779). The scoped pass seeds its
// evidence filters from these rows instead of from everything the scope
// holds, so the manifest name list and the cross-scope evidence filter
// follow the stamped change. AdvisoryID is the finding's deciding advisory:
// the distro record id for an OS finding, the CVE itself for a native one.
// The scoped pass seeds its advisory arm from these rows because their
// advisories may live cross-scope, the way their packages may.
type ScopedFindingPackage struct {
	CVEID       string
	AdvisoryID  string
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

// seedScopedSupplyChainImpactScope collects the identities a scoped pass may
// load: the stamped CVEs, the packages those CVEs reach (the scope's own
// stamped advisories plus the finding rows that made this scope a holder),
// and the advisory and upstream keys of the stamped advisories. An advisory
// whose own id misses the stamp still seeds when its upstream list names a
// stamped CVE, finding rows seed their deciding advisory ids, and the
// stamp's own canonical keys seed the upstream set, so a distro-spelled
// record, an advisory-less holder, and a record arriving on a later round
// through the advisory arm all keep the arms the full pass would follow. It
// reports false when there is nothing to scope by — no finding rows and no
// local stamped advisory — so the pass runs full instead of loading an
// empty set.
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
	// A stamped CVE is always admissible on the upstream arm: the reopen
	// names the CVE whose suppression and signals the pass must re-read,
	// whether or not the scope's own advisories spell it — the deciding
	// record may live in another scope, arriving on a later round through
	// the seeded advisory arm.
	for _, id := range stamp {
		if key, ok := upstreamCVEKey(id); ok {
			scope.upstream[key] = struct{}{}
		}
	}
	// Finding rows seed first: their packages and deciding advisories may
	// both live cross-scope, and the envelope loop below admits advisories
	// those rows were decided by.
	for _, row := range findingPackages {
		if _, ok := stampSet[row.CVEID]; !ok {
			continue
		}
		if row.AdvisoryID != "" {
			scope.advisoryIDs[row.AdvisoryID] = struct{}{}
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
	for _, envelope := range scopeEnvelopes {
		switch envelope.FactKind {
		case chain.VulnerabilityCVEFactKind,
			chain.VulnerabilityAffectedPackageFactKind,
			chain.VulnerabilityAffectedProductFactKind:
			if !stampReachesAdvisory(envelope.Payload, stampSet) && !scopedAdvisoryLinked(scope, envelope.Payload) {
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
	if len(scope.packages) == 0 && len(scope.advisoryIDs) == 0 {
		return nil, false
	}
	return scope, true
}

// scopedAdvisoryLinked reports whether an advisory-bearing envelope names an
// already-seeded advisory: the holder's own finding rows were decided by it,
// so it joins the stamped subgraph even when neither its own id nor its
// upstream list names the stamp. The seed set it reads is stamp-bounded (row
// advisories for stamped CVEs only), so the link cannot widen the scope past
// the stamped change.
func scopedAdvisoryLinked(scope *scopedSupplyChainImpactScope, payload map[string]any) bool {
	id := payloadcore.PayloadStr(payload, "advisory_id")
	if id == "" {
		return false
	}
	_, ok := scope.advisoryIDs[id]
	return ok
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

// inStampSet reports whether an id is in the stamp. It keeps the seeding
// switch readable and gives the linter a named predicate instead of a blank
// map probe.
func inStampSet(set map[string]struct{}, id string) bool {
	_, ok := set[id]
	return ok
}

// stampReachesAdvisory reports whether an advisory-bearing envelope joins the
// stamped set: by its own CVE id, or through an upstream key naming a stamped
// CVE. A holder-side distro record spells its own id with the vendor prefix
// (DEBIAN-CVE-...) while the stamp carries the bare operator-side CVE; the
// record's upstream list is the designed bridge between the spellings (#9914),
// so gating only on the envelope's own spelling starves the seeded upstream
// set and drops the suppression branch a scoped holder pass must see (#9779).
func stampReachesAdvisory(payload map[string]any, stampSet map[string]struct{}) bool {
	if inStampSet(stampSet, supplyChainCVEID(payload)) {
		return true
	}
	for _, key := range appendUpstreamCVEKeys(nil, payload) {
		if inStampSet(stampSet, key) {
			return true
		}
	}
	return false
}
