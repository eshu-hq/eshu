// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

// osPackageAdvisoryFactLoader loads active installed OS package advisory
// evidence cross-scope, already reconstructed as vulnerability.os_package fact
// envelopes, so loadSupplyChainImpactOSPackageAdvisoryFacts can feed them
// through the same decode/index/match path a natively-loaded os_package fact
// takes.
//
// This interface is declared with only leaf types (context.Context,
// []string, int, []facts.Envelope) rather than
// internal/workflow's OSPackageAdvisoryTarget/OSPackageAdvisoryTargetFilter,
// because internal/reducer cannot import internal/workflow: internal/workflow
// itself imports internal/reducer (for GraphProjectionKeyspace/Phase
// constants — see internal/workflow/collector_contract.go, progress.go,
// store.go), so the reverse import would be a compile-time cycle. The
// production postgres.FactStore.ListOSPackageAdvisoryFactEnvelopes method
// (internal/storage/postgres/installed_advisory_targets_os_package_envelope.go)
// satisfies this signature, and a compile-time assertion in the core_test
// package pins that (os_package_advisory_loader_binding_test.go): the handler
// reaches the reader through a runtime type assertion, and a drifted signature
// would skip the stage silently and let the pass retract every OS-package
// finding.
//
// The reader is narrowed to packageIDs (the intent's affected-package lookup
// keys) and ecosystems (their vendor sources), and drains every matching
// installed package inside one snapshot. It stops once the envelopes it holds
// exceed limit, keeps the page that crossed it, and reports truncated. The int
// return counts targets skipped for missing required fields.
type osPackageAdvisoryFactLoader interface {
	ListOSPackageAdvisoryFactEnvelopes(
		ctx context.Context,
		ecosystems []string,
		packageIDs []string,
		limit int,
	) (envelopes []facts.Envelope, skipped int, truncated bool, err error)
}

// loadSupplyChainImpactOSPackageAdvisoryFacts loads vulnerability.os_package
// evidence for one supply-chain-impact intent through the cross-scope
// advisory-target reader, narrowed to the installed packages the intent's
// already-loaded vulnerability.affected_package facts could match
// (supplyChainImpactOSPackageAdvisoryTargets). Before this stage existed,
// loadSupplyChainImpactEvidence never loaded vulnerability.os_package facts
// at all — supplyChainImpactFactKinds intentionally omits that kind (it lives
// cross-scope, not in the intent's own vulnerability-intelligence scope), and
// no other load stage populated it either — so every os_package
// supply-chain-impact finding was inert end-to-end (issue #5463/#5705). This
// stage MUST run before loadSupplyChainImpactScannerAnalysisScopeFacts, which
// keys its own sibling scanner_worker.analysis load off the os_package
// envelopes this stage adds (supplyChainImpactOSPackageScopeGenerationPairs).
//
// An intent with no OS affected package, or none with a usable lookup key,
// reads nothing and is not partial: the finding set depends only on installed
// rows matching one of those keys. The reader drains to completion inside one
// snapshot unless the per-intent evidence budget is spent first; the caller
// reads that through budget.exhausted(). The returned int counts rows the
// reader skipped for missing fields.
func (h SupplyChainImpactHandler) loadSupplyChainImpactOSPackageAdvisoryFacts(
	ctx context.Context,
	envelopes []facts.Envelope,
	budget *supplyChainImpactEvidenceBudget,
) ([]facts.Envelope, int, error) {
	loader, ok := h.FactLoader.(osPackageAdvisoryFactLoader)
	if !ok {
		return nil, 0, nil
	}
	ecosystems, packageIDs := supplyChainImpactOSPackageAdvisoryTargets(envelopes)
	if len(ecosystems) == 0 || len(packageIDs) == 0 {
		return nil, 0, nil
	}
	loaded, skipped, _, err := loader.ListOSPackageAdvisoryFactEnvelopes(
		ctx, ecosystems, packageIDs, budget.remaining(),
	)
	if err != nil {
		return nil, 0, factload.ClassifyFactLoadError(err)
	}
	budget.charge(len(loaded))
	// A truncated drain always spent the budget: the limit above was
	// budget.remaining(), and the reader reports truncated only when it loaded
	// more than that limit, so the charge has already overflowed.
	return loaded, skipped, nil
}

// supplyChainImpactOSPackageAdvisoryTargets derives the OS-package read from
// every loaded vulnerability.affected_package fact: the distinct vendor
// advisory sources (for example "debian", "alpine") and the distinct lookup
// keys the matcher compares installed package ids against.
//
// The sources are the SAME values classifyAffectedPackageAdvisorySource derives
// for firstOSPackageImpactPath's own matching (match.go), which is also what the
// SQL reader's ecosystem column computes. The keys are affectedOSPackageLookupKeys
// (os_package_identity.go), the exact strings osPackageMatchesAffectedPackage
// compares to an installed package's id, so a read over these keys is complete
// for the finding set. A raw affected_package "ecosystem" field value (for
// example "deb", "npm") would never match that column, so the derivation goes
// through the same classifier the matcher uses. Only affected packages with a
// vendor source contribute: the matcher returns no OS match without one. Only
// affected_package is consulted: an os_package match always requires a
// co-present affected_package (classifySupplyChainImpactPackage only calls
// firstOSPackageImpactPath when index.affectedPackages[cveID] is non-empty), so
// a CVE fact with no affected_package sibling could never produce an
// os_package finding regardless of what ecosystem it implies.
func supplyChainImpactOSPackageAdvisoryTargets(envelopes []facts.Envelope) (ecosystems []string, packageIDs []string) {
	sources := make(map[string]struct{})
	keys := make(map[string]struct{})
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.VulnerabilityAffectedPackageFactKind {
			continue
		}
		pkg, err := supplyChainAffectedPackageFromEnvelope(envelope)
		if err != nil {
			// A malformed affected_package fact is quarantined by the real
			// index build later; this derivation simply cannot use it.
			continue
		}
		vendorSource := classifyAffectedPackageAdvisorySource(pkg)
		if vendorSource == "" {
			continue
		}
		sources[vendorSource] = struct{}{}
		for _, key := range affectedOSPackageLookupKeys(pkg) {
			keys[key] = struct{}{}
		}
	}
	return sortedKeys(sources), sortedKeys(keys)
}

// sortedKeys returns the map's keys in sorted order, nil for an empty map.
func sortedKeys(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
