// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	if reachability, ok := longTailSupplyChainReachability(finding, ecosystem, maturity, detail); ok {
		return reachability
	}
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
	case pythonReachabilityParserCall:
		return SupplyChainReachability{
			State:            SupplyChainReachabilityReachable,
			Confidence:       "partial",
			Source:           "python_parser",
			Evidence:         detail,
			Reason:           "parser evidence proves code calls an imported PyPI package API",
			LanguageMaturity: maturity,
		}
	case pythonReachabilitySCIPCall:
		return SupplyChainReachability{
			State:            SupplyChainReachabilityReachable,
			Confidence:       "partial",
			Source:           "python_scip",
			Evidence:         detail,
			Reason:           "SCIP evidence proves code calls a PyPI package API",
			LanguageMaturity: maturity,
		}
	case pythonReachabilityParserDecorator:
		return SupplyChainReachability{
			State:            SupplyChainReachabilityReachable,
			Confidence:       "partial",
			Source:           "python_parser",
			Evidence:         detail,
			Reason:           "parser evidence proves code uses a PyPI package decorator API",
			LanguageMaturity: maturity,
		}
	case pythonReachabilityParserImport:
		return SupplyChainReachability{
			State:            SupplyChainReachabilityReachable,
			Confidence:       "partial",
			Source:           "python_parser",
			Evidence:         detail,
			Reason:           "parser evidence proves code imports the PyPI package API",
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
	case jsTSPackageAPICallEvidence, jsTSPackageAPIImportEvidence, jsTSPackageAPIReExportEvidence:
		return SupplyChainReachability{
			State:            SupplyChainReachabilityReachable,
			Confidence:       "partial",
			Source:           "parser_js_ts",
			Evidence:         detail,
			Reason:           "JavaScript/TypeScript parser evidence proves the package API identity is imported, called, or re-exported",
			LanguageMaturity: maturity,
		}
	case jsTSPackageAPISCIPCallEvidence:
		return SupplyChainReachability{
			State:            SupplyChainReachabilityReachable,
			Confidence:       "partial",
			Source:           "scip_js_ts",
			Evidence:         detail,
			Reason:           "SCIP-backed JavaScript/TypeScript evidence proves the package API identity is referenced",
			LanguageMaturity: maturity,
		}
	case jsTSPackageAPIUnknownEvidence:
		return SupplyChainReachability{
			State:            SupplyChainReachabilityUnknown,
			Confidence:       "unknown",
			Source:           "parser_js_ts",
			Evidence:         detail,
			Reason:           "JavaScript/TypeScript parser or SCIP evidence exists, but no matching package API identity is proven",
			LanguageMaturity: maturity,
			MissingEvidence:  supplyChainReachabilityMissingEvidence(finding, ecosystem),
		}
	case jsTSPackageAPIAmbiguousEvidence:
		return SupplyChainReachability{
			State:            SupplyChainReachabilityUnknown,
			Confidence:       "unknown",
			Source:           "parser_js_ts",
			Evidence:         detail,
			Reason:           "JavaScript/TypeScript parser or SCIP evidence is similar but package API identity is ambiguous",
			LanguageMaturity: maturity,
			MissingEvidence:  supplyChainReachabilityMissingEvidence(finding, ecosystem),
		}
	case jsTSPackageAPIMissingEvidence:
		return SupplyChainReachability{
			State:            SupplyChainReachabilityMissingEvidence,
			Confidence:       "unknown",
			Source:           "parser_js_ts",
			Evidence:         detail,
			Reason:           "JavaScript/TypeScript package reachability is supported, but parser or SCIP package API evidence is missing",
			LanguageMaturity: maturity,
			MissingEvidence:  supplyChainReachabilityMissingEvidence(finding, ecosystem),
		}
	case jvmRuntimeReachabilityPackageAPIReachable:
		return SupplyChainReachability{
			State:            SupplyChainReachabilityReachable,
			Confidence:       "partial",
			Source:           "jvm_parser_resolver",
			Evidence:         detail,
			Reason:           "Maven or Gradle resolver evidence proved package API identity and parser or SCIP evidence observed that API in source",
			LanguageMaturity: maturity,
			MissingEvidence:  supplyChainReachabilityMissingEvidence(finding, ecosystem),
		}
	case "package_manifest", string(GoVulnReachabilityModuleOnly), string(GoVulnReachabilityUnknown), "known_fixed":
		if ecosystem == "pypi" {
			return pythonUnknownSupplyChainReachability(finding, detail, maturity)
		}
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

type longTailReachabilitySpec struct {
	source         string
	evidence       string
	reason         string
	missingReasons []string
}

func longTailSupplyChainReachability(
	finding SupplyChainImpactFinding,
	ecosystem string,
	maturity string,
	detail string,
) (SupplyChainReachability, bool) {
	if detail != "package_manifest" {
		return SupplyChainReachability{}, false
	}
	spec, ok := longTailReachabilitySpecForEcosystem(ecosystem)
	if !ok {
		return SupplyChainReachability{}, false
	}
	missing := uniqueSortedStrings(append(
		append([]string(nil), finding.MissingEvidence...),
		spec.missingReasons...,
	))
	if longTailReachabilityHasMissingEvidence(finding.MissingEvidence) {
		return SupplyChainReachability{
			State:            SupplyChainReachabilityMissingEvidence,
			Confidence:       "unknown",
			Source:           spec.source,
			Evidence:         spec.evidence,
			Reason:           "ecosystem package evidence exists, but unresolved project or resolver evidence prevents reachability classification",
			LanguageMaturity: maturity,
			MissingEvidence:  missing,
		}, true
	}
	if !longTailReachabilityHasPackageEvidence(finding) {
		return SupplyChainReachability{
			State:            SupplyChainReachabilityUnknown,
			Confidence:       "unknown",
			Source:           spec.source,
			Evidence:         "package_manifest",
			Reason:           "dependency evidence exists, but ecosystem package usage evidence is not strong enough for reachability",
			LanguageMaturity: maturity,
			MissingEvidence:  missing,
		}, true
	}
	return SupplyChainReachability{
		State:            SupplyChainReachabilityReachable,
		Confidence:       "partial",
		Source:           spec.source,
		Evidence:         spec.evidence,
		Reason:           spec.reason,
		LanguageMaturity: maturity,
		MissingEvidence:  missing,
	}, true
}

func longTailReachabilitySpecForEcosystem(ecosystem string) (longTailReachabilitySpec, bool) {
	switch ecosystem {
	case "composer":
		return longTailReachabilitySpec{
			source:   "composer",
			evidence: "composer_dependency_path",
			reason:   "Composer dependency evidence proves the vulnerable package is installed or declared for the repository; PHP API calls are not proven",
			missingReasons: []string{
				"php autoload and dynamic dispatch evidence missing",
				"php parser/scip package API call evidence missing",
			},
		}, true
	case "rubygems":
		return longTailReachabilitySpec{
			source:   "bundler",
			evidence: "bundler_dependency_path",
			reason:   "Bundler dependency evidence proves the vulnerable gem is installed or declared for the repository; Ruby API calls are not proven",
			missingReasons: []string{
				"ruby metaprogramming and autoload evidence missing",
				"ruby parser/scip package API call evidence missing",
			},
		}, true
	case "cargo":
		return longTailReachabilitySpec{
			source:   "cargo",
			evidence: "cargo_dependency_path",
			reason:   "Cargo dependency evidence proves the vulnerable crate is installed or declared for the repository; Rust API calls are not proven",
			missingReasons: []string{
				"rust macro and cfg reachability evidence missing",
				"rust parser/scip package API call evidence missing",
			},
		}, true
	case "nuget":
		return longTailReachabilitySpec{
			source:   "nuget",
			evidence: "nuget_dependency_path",
			reason:   "NuGet dependency evidence proves the vulnerable package is installed or declared for the repository; .NET API calls are not proven",
			missingReasons: []string{
				".net reflection dependency-injection and generated-code evidence missing",
				".net parser/scip package API call evidence missing",
			},
		}, true
	default:
		return longTailReachabilitySpec{}, false
	}
}

func longTailReachabilityHasPackageEvidence(finding SupplyChainImpactFinding) bool {
	return len(finding.DependencyPath) > 0 || finding.DirectDependency != nil ||
		strings.TrimSpace(finding.ObservedVersion) != "" || strings.TrimSpace(finding.RequestedRange) != ""
}

func longTailReachabilityHasMissingEvidence(values []string) bool {
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if strings.Contains(normalized, "unresolved") ||
			strings.Contains(normalized, "ambiguous") ||
			strings.Contains(normalized, "project-reference") ||
			strings.Contains(normalized, "msbuild") {
			return true
		}
	}
	return false
}

func pythonUnknownSupplyChainReachability(
	finding SupplyChainImpactFinding,
	detail string,
	maturity string,
) SupplyChainReachability {
	missing := supplyChainReachabilityMissingEvidence(finding, "pypi")
	state := SupplyChainReachabilityMissingEvidence
	reason := "Python parser or SCIP reachability evidence is missing for the PyPI package"
	if hasPythonAmbiguousReachabilityEvidence(missing) {
		state = SupplyChainReachabilityUnknown
		reason = "Python parser evidence is ambiguous because dynamic imports, plugin loading, or package API identity gaps remain"
	}
	return SupplyChainReachability{
		State:            state,
		Confidence:       "unknown",
		Source:           "python_parser",
		Evidence:         detail,
		Reason:           reason,
		LanguageMaturity: maturity,
		MissingEvidence:  missing,
	}
}

func hasPythonAmbiguousReachabilityEvidence(values []string) bool {
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if strings.Contains(value, "dynamic import") ||
			strings.Contains(value, "plugin loading") ||
			strings.Contains(value, "api identity") {
			return true
		}
	}
	return false
}

func supplyChainReachabilitySourceForUnknown(ecosystem string, detail string) string {
	if ecosystem == "gomod" {
		return "govulncheck"
	}
	if ecosystem == "pypi" {
		return "python_parser"
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
