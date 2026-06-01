package reducer

import "strings"

// SupplyChainReachabilityState is the stable cross-language reachability
// enrichment state attached to a vulnerability finding. The state is
// prioritization metadata; it never changes impact truth.
type SupplyChainReachabilityState string

const (
	// SupplyChainReachabilityReachable means evidence proves the vulnerable
	// package, image component, package import, or symbol is reachable for the
	// scoped target.
	SupplyChainReachabilityReachable SupplyChainReachabilityState = "reachable"
	// SupplyChainReachabilityNotCalled means an ecosystem-specific analyzer
	// proved the vulnerable symbol or package is not called from an entrypoint.
	SupplyChainReachabilityNotCalled SupplyChainReachabilityState = "not_called"
	// SupplyChainReachabilityUnknown means Eshu has some target evidence but
	// cannot classify call/runtime reachability.
	SupplyChainReachabilityUnknown SupplyChainReachabilityState = "unknown"
	// SupplyChainReachabilityUnavailable means no implemented reachability
	// analyzer exists for the ecosystem/target shape.
	SupplyChainReachabilityUnavailable SupplyChainReachabilityState = "unavailable"
	// SupplyChainReachabilityMissingEvidence means a supported analyzer could
	// answer, but its evidence is missing from the current finding.
	SupplyChainReachabilityMissingEvidence SupplyChainReachabilityState = "missing_evidence"
)

// SupplyChainReachability describes the evidence and confidence behind one
// finding's reachability enrichment. Impact confidence remains on the parent
// finding so callers cannot mistake reachability for affected/not-affected
// truth.
type SupplyChainReachability struct {
	State            SupplyChainReachabilityState
	Confidence       string
	Source           string
	Evidence         string
	Reason           string
	LanguageMaturity string
	MissingEvidence  []string
}

func withSupplyChainReachability(finding SupplyChainImpactFinding) SupplyChainImpactFinding {
	reachability := supplyChainReachabilityForFinding(finding)
	finding.Reachability = &reachability
	return finding
}

func supplyChainReachabilityForFinding(finding SupplyChainImpactFinding) SupplyChainReachability {
	detail := strings.TrimSpace(finding.RuntimeReachability)
	ecosystem := normalizedSupplyChainVersionEcosystem(finding.Ecosystem)
	maturity := supplyChainReachabilityLanguageMaturity(ecosystem)
	switch detail {
	case string(GoVulnReachabilitySymbolReachable):
		return SupplyChainReachability{
			State:            SupplyChainReachabilityReachable,
			Confidence:       "strong",
			Source:           "govulncheck",
			Evidence:         detail,
			Reason:           "govulncheck recorded a call trace into a vulnerable symbol",
			LanguageMaturity: maturity,
		}
	case string(GoVulnReachabilityPackageImportReachable):
		return SupplyChainReachability{
			State:            SupplyChainReachabilityReachable,
			Confidence:       "strong",
			Source:           "govulncheck",
			Evidence:         detail,
			Reason:           "govulncheck observed a vulnerable package in the import graph",
			LanguageMaturity: maturity,
			MissingEvidence:  []string{"symbol-level call trace missing"},
		}
	case string(GoVulnReachabilityNotCalled):
		return SupplyChainReachability{
			State:            SupplyChainReachabilityNotCalled,
			Confidence:       "strong",
			Source:           "govulncheck",
			Evidence:         detail,
			Reason:           "govulncheck proved the vulnerable surface is not called from any entry point",
			LanguageMaturity: maturity,
		}
	case "image_sbom", "image_os_package", "deployed_image":
		return SupplyChainReachability{
			State:            SupplyChainReachabilityReachable,
			Confidence:       "partial",
			Source:           "runtime_or_sbom",
			Evidence:         detail,
			Reason:           "runtime, image, or SBOM evidence places the vulnerable component on an owned target",
			LanguageMaturity: maturity,
		}
	case "package_manifest", string(GoVulnReachabilityModuleOnly), string(GoVulnReachabilityUnknown), "known_fixed":
		return SupplyChainReachability{
			State:            SupplyChainReachabilityUnknown,
			Confidence:       "unknown",
			Source:           supplyChainReachabilitySourceForUnknown(ecosystem, detail),
			Evidence:         detail,
			Reason:           "dependency evidence exists, but call or runtime reachability is not proven",
			LanguageMaturity: maturity,
			MissingEvidence:  supplyChainReachabilityMissingEvidence(finding, ecosystem),
		}
	default:
		if ecosystem == "gomod" {
			return SupplyChainReachability{
				State:            SupplyChainReachabilityMissingEvidence,
				Confidence:       "unknown",
				Source:           "govulncheck",
				Reason:           "Go vulnerability reachability is implemented, but govulncheck evidence is missing",
				LanguageMaturity: maturity,
				MissingEvidence:  supplyChainReachabilityMissingEvidence(finding, ecosystem),
			}
		}
		return SupplyChainReachability{
			State:            SupplyChainReachabilityUnavailable,
			Confidence:       "none",
			Source:           "not_available",
			Reason:           "no implemented vulnerability reachability analyzer is available for this ecosystem",
			LanguageMaturity: maturity,
		}
	}
}

func supplyChainReachabilitySourceForUnknown(ecosystem string, detail string) string {
	if ecosystem == "gomod" {
		return "govulncheck"
	}
	if detail == "package_manifest" {
		return "parser"
	}
	return "evidence_gap"
}

func supplyChainReachabilityMissingEvidence(
	finding SupplyChainImpactFinding,
	ecosystem string,
) []string {
	missing := uniqueSortedStrings(finding.MissingEvidence)
	if ecosystem == "gomod" && !hasGovulncheckMissingEvidence(missing) {
		missing = append(missing, "govulncheck call-graph evidence missing")
	}
	return uniqueSortedStrings(missing)
}

func hasGovulncheckMissingEvidence(values []string) bool {
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), "govulncheck") {
			return true
		}
	}
	return false
}

func supplyChainReachabilityLanguageMaturity(ecosystem string) string {
	switch ecosystem {
	case "gomod":
		return "implemented"
	case "npm", "pypi", "maven", "gradle", "composer", "rubygems", "cargo", "nuget":
		return "partial"
	case "pub", "hex", "swift":
		return "unsupported"
	default:
		return "unavailable"
	}
}
