// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "strings"

// DetectionProfile names the evidence tier a supply-chain impact finding
// meets. Reducers emit every finding with a tier so downstream readers can
// choose between low-noise precision and broader recall without losing
// truth labels.
type DetectionProfile string

const (
	// DetectionProfilePrecise marks findings backed by an exact installed
	// version anchor (lockfile, manifest with pinned version, SBOM
	// component version) that resolved with an ecosystem-aware matcher.
	// Range-only manifest, malformed, derived product/CPE, and
	// missing-version evidence do not qualify. Unsupported matcher
	// ecosystems are withheld from impact findings and surfaced through
	// readiness coverage gaps instead.
	DetectionProfilePrecise DetectionProfile = "precise"
	// DetectionProfileComprehensive marks findings that do not meet the
	// precise bar but still carry owned anchor evidence (SBOM component,
	// CPE-derived image path, range-only manifest, malformed range, or
	// missing observed version). They keep their truth labels (status,
	// confidence, runtime_reachability) and explicit missing-evidence reasons
	// so callers can interpret recall correctly.
	DetectionProfileComprehensive DetectionProfile = "comprehensive"
)

// ValidDetectionProfile reports whether value is a known detection profile
// string. The empty profile is allowed because old, on-disk findings written
// before profile tagging do not have a profile in their payload.
func ValidDetectionProfile(value string) bool {
	switch DetectionProfile(value) {
	case "", DetectionProfilePrecise, DetectionProfileComprehensive:
		return true
	default:
		return false
	}
}

// classifySupplyChainImpactDetectionProfile returns the profile tier the
// finding qualifies for. The reducer always emits the finding; the profile
// is request-time evidence selection, not data suppression. A finding only
// qualifies as precise when its installed-version evidence is non-empty,
// status is exact or known-fixed, and the match reason is a supported
// ecosystem-aware exact match.
func classifySupplyChainImpactDetectionProfile(finding SupplyChainImpactFinding) DetectionProfile {
	switch finding.Status {
	case SupplyChainImpactAffectedExact, SupplyChainImpactNotAffectedKnownFixed:
	default:
		return DetectionProfileComprehensive
	}
	if strings.TrimSpace(finding.ObservedVersion) == "" {
		return DetectionProfileComprehensive
	}
	switch finding.MatchReason {
	case supplyChainVersionReasonNPMSemverAffectedRange,
		supplyChainVersionReasonNPMSemverKnownFixed,
		supplyChainVersionReasonNuGetSemverAffectedRange,
		supplyChainVersionReasonNuGetSemverKnownFixed,
		supplyChainVersionReasonCargoSemverAffectedRange,
		supplyChainVersionReasonCargoSemverKnownFixed,
		supplyChainVersionReasonHexSemverAffectedRange,
		supplyChainVersionReasonHexSemverKnownFixed,
		supplyChainVersionReasonPyPIPep440AffectedRange,
		supplyChainVersionReasonPyPIPep440KnownFixed,
		supplyChainVersionReasonSwiftSemverAffectedRange,
		supplyChainVersionReasonSwiftSemverKnownFixed,
		supplyChainVersionReasonPubSemverAffectedRange,
		supplyChainVersionReasonPubSemverKnownFixed,
		supplyChainVersionReasonComposerSemverAffectedRange,
		supplyChainVersionReasonComposerSemverKnownFixed,
		supplyChainVersionReasonMavenRangeMatch,
		supplyChainVersionReasonMavenKnownFixed,
		supplyChainVersionReasonRPMExactAffected,
		supplyChainVersionReasonRPMKnownFixed,
		supplyChainVersionReasonRubyGemsAffectedRange,
		supplyChainVersionReasonRubyGemsKnownFixed:
		return DetectionProfilePrecise
	default:
		return DetectionProfileComprehensive
	}
}
