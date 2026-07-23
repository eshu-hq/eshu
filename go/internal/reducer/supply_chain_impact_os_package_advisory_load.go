// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// osPackageAdvisoryFactLoader loads active installed OS package advisory
// evidence cross-scope by ecosystem, already reconstructed as
// vulnerability.os_package fact envelopes, so
// loadSupplyChainImpactOSPackageAdvisoryFacts can feed them through the
// same decode/index/match path a natively-loaded os_package fact takes.
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
// already satisfies this signature: it delegates to the SAME
// advisory-matched ListOSPackageAdvisoryTargets reader and SQL matching
// logic the vulnerability-intelligence coordinator planner uses
// (internal/coordinator/service_installed_advisory_targets.go), then
// reconstructs the envelope there, where both internal/workflow and
// internal/facts are importable.
type osPackageAdvisoryFactLoader interface {
	ListOSPackageAdvisoryFactEnvelopes(ctx context.Context, ecosystems []string, limit int) ([]facts.Envelope, error)
}

// maxSupplyChainImpactOSPackageAdvisoryTargets bounds how many active
// installed OS package advisory targets loadSupplyChainImpactOSPackageAdvisoryFacts
// will request per intent, so a pathological ecosystem with an unbounded
// number of installed-package observations cannot turn one intent's evidence
// load into an unbounded read. Matches the postgres advisory-target reader's
// own max clamp (maxOwnedPackageDependencyTargetLimit,
// internal/storage/postgres/owned_package_targets.go).
const maxSupplyChainImpactOSPackageAdvisoryTargets = 500

// loadSupplyChainImpactOSPackageAdvisoryFacts loads vulnerability.os_package
// evidence for one supply-chain-impact intent through the cross-scope
// advisory-target reader, keyed by the vendor advisory source(s) the
// intent's already-loaded vulnerability.affected_package facts name
// (supplyChainImpactOSPackageAdvisoryEcosystems). Before this stage existed,
// loadSupplyChainImpactEvidence never loaded vulnerability.os_package facts
// at all — supplyChainImpactFactKinds intentionally omits that kind (it lives
// cross-scope, not in the intent's own vulnerability-intelligence scope), and
// no other load stage populated it either — so every os_package
// supply-chain-impact finding was inert end-to-end (issue #5463/#5705). This
// stage MUST run before loadSupplyChainImpactScannerAnalysisScopeFacts, which
// keys its own sibling scanner_worker.analysis load off the os_package
// envelopes this stage adds (supplyChainImpactOSPackageScopeGenerationPairs).
func (h SupplyChainImpactHandler) loadSupplyChainImpactOSPackageAdvisoryFacts(
	ctx context.Context,
	envelopes []facts.Envelope,
) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(osPackageAdvisoryFactLoader)
	if !ok {
		return nil, nil
	}
	ecosystems := supplyChainImpactOSPackageAdvisoryEcosystems(envelopes)
	if len(ecosystems) == 0 {
		return nil, nil
	}
	loaded, err := loader.ListOSPackageAdvisoryFactEnvelopes(ctx, ecosystems, maxSupplyChainImpactOSPackageAdvisoryTargets)
	if err != nil {
		return nil, classifyFactLoadError(err)
	}
	return loaded, nil
}

// supplyChainImpactOSPackageAdvisoryEcosystems returns the distinct,
// non-empty vendor-advisory-source strings (for example "debian", "alpine")
// classified from every loaded vulnerability.affected_package fact, in sorted
// order. These are the SAME values classifyAffectedPackageAdvisorySource
// derives for firstOSPackageImpactPath's own matching
// (supply_chain_impact_match.go) — which is also exactly what the SQL
// advisory-target reader's ecosystem column computes
// (LOWER(COALESCE(vendor_advisory_source, distro)),
// listOSPackageAdvisoryTargetsQuery). A raw affected_package "ecosystem"
// field value (for example "deb", "npm" — a purl-type-shaped string) would
// never match that column, so this derivation intentionally goes through the
// same classifier the matcher uses rather than reading pkg.ecosystem
// directly. Only affected_package is consulted: an os_package match always
// requires a co-present affected_package (classifySupplyChainImpactPackage
// only calls firstOSPackageImpactPath when index.affectedPackages[cveID] is
// non-empty), so a CVE fact with no affected_package sibling could never
// produce an os_package finding regardless of what ecosystem it implies.
func supplyChainImpactOSPackageAdvisoryEcosystems(envelopes []facts.Envelope) []string {
	seen := make(map[string]struct{})
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.VulnerabilityAffectedPackageFactKind {
			continue
		}
		pkg, err := supplyChainAffectedPackageFromEnvelope(envelope)
		if err != nil {
			// A malformed affected_package fact is quarantined by the real
			// index build later; this derivation simply cannot use it as an
			// ecosystem hint.
			continue
		}
		vendorSource := classifyAffectedPackageAdvisorySource(pkg)
		if vendorSource == "" {
			continue
		}
		seen[vendorSource] = struct{}{}
	}
	if len(seen) == 0 {
		return nil
	}
	ecosystems := make([]string, 0, len(seen))
	for ecosystem := range seen {
		ecosystems = append(ecosystems, ecosystem)
	}
	sort.Strings(ecosystems)
	return ecosystems
}
