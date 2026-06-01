package reducer

import "strings"

const (
	supplyChainVersionReasonDPKGExactAffected   = "dpkg_exact_affected_version"
	supplyChainVersionReasonDPKGExactKnownFixed = "dpkg_exact_known_fixed"
	supplyChainVersionReasonAPKExactAffected    = "apk_exact_affected_version"
	supplyChainVersionReasonAPKExactKnownFixed  = "apk_exact_known_fixed"
)

func evaluateExactOSPackageVersionMatch(
	observed string,
	fixedVersion string,
	pkgs []supplyChainAffectedPackage,
	affectedReason string,
	knownFixedReason string,
) supplyChainVersionMatchDecision {
	for _, pkg := range pkgs {
		for _, candidate := range pkg.affectedVersions {
			if strings.TrimSpace(candidate) == observed {
				return affectedVersionDecision(affectedReason)
			}
		}
	}
	if fixedVersion != "" && observed == fixedVersion {
		return knownFixedDecision(knownFixedReason)
	}
	return possiblyAffectedDecision(supplyChainVersionReasonNoAffectedMatch, nil)
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
