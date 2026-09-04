// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychaincore"
)

// DetectionProfile aliases [supplychaincore.DetectionProfile]; the
// detection-tier vocabulary moved to the shared supply-chain leaf (#6061)
// while ValidDetectionProfile and the classifier below stay in this package.
type DetectionProfile = supplychaincore.DetectionProfile

const (
	// DetectionProfilePrecise aliases [supplychaincore.DetectionProfilePrecise].
	DetectionProfilePrecise = supplychaincore.DetectionProfilePrecise
	// DetectionProfileComprehensive aliases [supplychaincore.DetectionProfileComprehensive].
	DetectionProfileComprehensive = supplychaincore.DetectionProfileComprehensive
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
		supplyChainVersionReasonDPKGExactAffected,
		supplyChainVersionReasonDPKGExactKnownFixed,
		supplyChainVersionReasonAPKExactAffected,
		supplyChainVersionReasonAPKExactKnownFixed,
		supplyChainVersionReasonRubyGemsAffectedRange,
		supplyChainVersionReasonRubyGemsKnownFixed:
		return DetectionProfilePrecise
	default:
		return DetectionProfileComprehensive
	}
}
