// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychaincore

// SupplyChainImpactStatus names the reducer decision for one vulnerability
// impact finding.
type SupplyChainImpactStatus string

const (
	// SupplyChainImpactAffectedExact means package identity and observed
	// version match the affected package evidence exactly.
	SupplyChainImpactAffectedExact SupplyChainImpactStatus = "affected_exact"
	// SupplyChainImpactAffectedDerived means impact follows from SBOM, image,
	// repository, or runtime joins after package identity is established.
	SupplyChainImpactAffectedDerived SupplyChainImpactStatus = "affected_derived"
	// SupplyChainImpactPossiblyAffected means advisory evidence exists but
	// package identity or version precision is incomplete.
	SupplyChainImpactPossiblyAffected SupplyChainImpactStatus = "possibly_affected"
	// SupplyChainImpactNotAffectedKnownFixed means the observed version is at
	// or beyond a source-reported fixed version under Eshu's conservative
	// numeric version comparison.
	SupplyChainImpactNotAffectedKnownFixed SupplyChainImpactStatus = "not_affected_known_fixed"
	// SupplyChainImpactUnknown means vulnerability source truth exists but Eshu
	// lacks enough package or runtime evidence to decide impact.
	SupplyChainImpactUnknown SupplyChainImpactStatus = "unknown_impact"
)
