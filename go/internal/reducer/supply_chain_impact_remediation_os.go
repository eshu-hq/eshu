package reducer

import "strings"

func osRemediationMissingEvidence(finding SupplyChainImpactFinding) []string {
	var missing []string
	if !osRemediationMatchReasonSupported(finding.Ecosystem, finding.MatchReason) {
		missing = append(missing, SupplyChainRemediationMissingVersionOrdering)
	}
	if strings.TrimSpace(finding.FixedVersion) != "" &&
		!osRemediationSourceSupported(finding.Ecosystem, finding.FixedVersionSource) {
		missing = append(missing, SupplyChainRemediationMissingAdvisoryProvenance)
	}
	for _, branch := range finding.FixedVersionBranches {
		if strings.TrimSpace(branch.Version) == "" {
			continue
		}
		if !osRemediationSourceSupported(finding.Ecosystem, branch.Source) {
			missing = append(missing, SupplyChainRemediationMissingAdvisoryProvenance)
		}
	}
	return missing
}

func osRemediationMatchReasonSupported(ecosystem string, matchReason string) bool {
	switch osRemediationFamily(ecosystem) {
	case "rpm":
		return matchReason == supplyChainVersionReasonRPMExactAffected ||
			matchReason == supplyChainVersionReasonRPMKnownFixed
	case "dpkg":
		return matchReason == supplyChainVersionReasonDPKGExactAffected ||
			matchReason == supplyChainVersionReasonDPKGExactKnownFixed
	case "apk":
		return matchReason == supplyChainVersionReasonAPKExactAffected ||
			matchReason == supplyChainVersionReasonAPKExactKnownFixed
	default:
		return false
	}
}

func osRemediationSourceSupported(ecosystem string, source string) bool {
	source = strings.ToLower(strings.TrimSpace(source))
	switch osRemediationFamily(ecosystem) {
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
