// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "strings"

const (
	supplyChainVersionReasonDPKGExactAffected   = "dpkg_exact_affected_version"
	supplyChainVersionReasonDPKGExactKnownFixed = "dpkg_exact_known_fixed"
	supplyChainVersionReasonDPKGAffectedRange   = "dpkg_affected_range"
	supplyChainVersionReasonDPKGKnownFixed      = "dpkg_known_fixed"
	supplyChainVersionReasonAPKExactAffected    = "apk_exact_affected_version"
	supplyChainVersionReasonAPKExactKnownFixed  = "apk_exact_known_fixed"
	supplyChainVersionReasonAPKAffectedRange    = "apk_affected_range"
	supplyChainVersionReasonAPKKnownFixed       = "apk_known_fixed"
)

func evaluateOSPackageVersionMatch(
	observed string,
	fixedVersion string,
	pkgs []supplyChainAffectedPackage,
	affectedReason string,
	knownFixedReason string,
	rangeAffectedReason string,
	rangeKnownFixedReason string,
	compare versionCompareFunc,
) supplyChainVersionMatchDecision {
	if _, ok := compare(observed, observed); !ok {
		return malformedInstalledVersionDecision()
	}
	if affected, malformed := osPackageAffectedByAnyPackage(observed, pkgs, compare); affected {
		return affectedVersionDecision(rangeAffectedReason)
	} else if malformed {
		return malformedVersionDecision()
	}
	if fixedVersion != "" {
		cmp, valid := compare(observed, fixedVersion)
		if !valid {
			return malformedVersionDecision()
		}
		if cmp >= 0 {
			return knownFixedDecision(rangeKnownFixedReason)
		}
	}
	for _, pkg := range pkgs {
		for _, candidate := range pkg.affectedVersions {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				continue
			}
			cmp, ok := compare(observed, candidate)
			if ok && cmp == 0 {
				return affectedVersionDecision(affectedReason)
			} else if !ok {
				return malformedVersionDecision()
			}
		}
	}
	return possiblyAffectedDecision(supplyChainVersionReasonNoAffectedMatch, nil)
}

func osPackageAffectedByAnyPackage(
	observed string,
	pkgs []supplyChainAffectedPackage,
	compare versionCompareFunc,
) (bool, bool) {
	malformed := false
	for _, pkg := range pkgs {
		if affected, valid := osPackageAffectedByPackage(observed, pkg, compare); affected {
			return true, false
		} else if !valid {
			malformed = true
		}
	}
	return false, malformed
}

func osPackageAffectedByPackage(
	observed string,
	pkg supplyChainAffectedPackage,
	compare versionCompareFunc,
) (bool, bool) {
	valid := true
	for _, affectedRange := range pkg.affectedRanges {
		if !strings.EqualFold(affectedRange.kind, "ECOSYSTEM") {
			continue
		}
		if affected, ok := versionRangeContainsDecision(affectedRange, observed, compare); affected {
			return true, true
		} else if !ok {
			valid = false
		}
	}
	return false, valid
}

func affectedVersionDecision(reason string) supplyChainVersionMatchDecision {
	return supplyChainVersionMatchDecision{
		Status:              SupplyChainImpactAffectedExact,
		Confidence:          "exact",
		RuntimeReachability: "package_manifest",
		Reason:              reason,
	}
}

func knownFixedDecision(reason string) supplyChainVersionMatchDecision {
	return supplyChainVersionMatchDecision{
		Status:              SupplyChainImpactNotAffectedKnownFixed,
		Confidence:          "exact",
		RuntimeReachability: "known_fixed",
		Reason:              reason,
	}
}

func malformedVersionDecision() supplyChainVersionMatchDecision {
	return possiblyAffectedDecision(supplyChainVersionReasonMalformedRange, []string{supplyChainMissingMalformedRange})
}

func malformedInstalledVersionDecision() supplyChainVersionMatchDecision {
	return possiblyAffectedDecision(
		supplyChainVersionReasonMalformedInstalled,
		[]string{supplyChainMissingMalformedInstalled},
	)
}

func possiblyAffectedDecision(reason string, missing []string) supplyChainVersionMatchDecision {
	return supplyChainVersionMatchDecision{
		Status:          SupplyChainImpactPossiblyAffected,
		Confidence:      "partial",
		Reason:          reason,
		MissingEvidence: missing,
		FailClosed: reason == supplyChainVersionReasonMalformedRange ||
			reason == supplyChainVersionReasonMalformedInstalled,
	}
}
