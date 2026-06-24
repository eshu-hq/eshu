// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "strings"

func osRemediationMissingEvidence(finding SupplyChainImpactFinding) []string {
	var missing []string
	family := osRemediationFamilyForFinding(finding)
	if !osRemediationMatchReasonSupported(family, finding.MatchReason) {
		missing = append(missing, SupplyChainRemediationMissingVersionOrdering)
	}
	if strings.TrimSpace(finding.FixedVersion) != "" &&
		!osRemediationSourceSupported(family, finding.FixedVersionSource) {
		missing = append(missing, SupplyChainRemediationMissingAdvisoryProvenance)
	}
	for _, branch := range finding.FixedVersionBranches {
		if strings.TrimSpace(branch.Version) == "" {
			continue
		}
		if !osRemediationSourceSupported(family, branch.Source) {
			missing = append(missing, SupplyChainRemediationMissingAdvisoryProvenance)
		}
	}
	return missing
}

func osRemediationMatchReasonSupported(family string, matchReason string) bool {
	switch family {
	case "rpm":
		return matchReason == supplyChainVersionReasonRPMExactAffected ||
			matchReason == supplyChainVersionReasonRPMKnownFixed
	case "dpkg":
		return matchReason == supplyChainVersionReasonDPKGExactAffected ||
			matchReason == supplyChainVersionReasonDPKGExactKnownFixed ||
			matchReason == supplyChainVersionReasonDPKGAffectedRange ||
			matchReason == supplyChainVersionReasonDPKGKnownFixed
	case "apk":
		return matchReason == supplyChainVersionReasonAPKExactAffected ||
			matchReason == supplyChainVersionReasonAPKExactKnownFixed ||
			matchReason == supplyChainVersionReasonAPKAffectedRange ||
			matchReason == supplyChainVersionReasonAPKKnownFixed
	default:
		return false
	}
}

func osRemediationSourceSupported(family string, source string) bool {
	source = strings.ToLower(strings.TrimSpace(source))
	switch family {
	case "rpm":
		switch source {
		case "redhat", "fedora", "centos", "rocky", "alma", "amazonlinux":
			return true
		default:
			return false
		}
	case "dpkg":
		switch source {
		case "debian", "ubuntu":
			return true
		default:
			return false
		}
	case "apk":
		return source == "alpine"
	default:
		return false
	}
}

func osRemediationFamilyForFinding(finding SupplyChainImpactFinding) string {
	if family := osRemediationFamily(finding.Ecosystem); family != "" {
		return family
	}
	switch strings.TrimSpace(finding.MatchReason) {
	case supplyChainVersionReasonRPMExactAffected, supplyChainVersionReasonRPMKnownFixed:
		return "rpm"
	case supplyChainVersionReasonDPKGExactAffected, supplyChainVersionReasonDPKGExactKnownFixed,
		supplyChainVersionReasonDPKGAffectedRange, supplyChainVersionReasonDPKGKnownFixed:
		return "dpkg"
	case supplyChainVersionReasonAPKExactAffected, supplyChainVersionReasonAPKExactKnownFixed,
		supplyChainVersionReasonAPKAffectedRange, supplyChainVersionReasonAPKKnownFixed:
		return "apk"
	default:
		return osRemediationFamilyFromSource(finding.FixedVersionSource)
	}
}

func osRemediationFamilyFromSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "redhat", "fedora", "centos", "rocky", "alma", "amazonlinux":
		return "rpm"
	case "debian", "ubuntu":
		return "dpkg"
	case "alpine":
		return "apk"
	default:
		return ""
	}
}

func osRemediationFamily(ecosystem string) string {
	switch strings.ToLower(strings.TrimSpace(ecosystem)) {
	case "redhat", "fedora", "centos", "rocky", "alma", "amazonlinux", "rpm":
		return "rpm"
	case "debian", "ubuntu", "deb", "dpkg":
		return "dpkg"
	case "alpine", "apk":
		return "apk"
	default:
		return ""
	}
}
