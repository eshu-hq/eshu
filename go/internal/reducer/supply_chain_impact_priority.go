// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// SupplyChainImpactPriorityContribution explains one additive or subtractive
// input to a vulnerability priority score. Contributions are triage metadata:
// they never change impact_status or missing-evidence truth.
type SupplyChainImpactPriorityContribution struct {
	ReasonCode   string
	Input        string
	Value        string
	Contribution int
}

func withSupplyChainImpactPriority(finding SupplyChainImpactFinding) SupplyChainImpactFinding {
	contributions := buildSupplyChainImpactPriorityContributions(finding)
	score := 0
	for _, contribution := range contributions {
		score += contribution.Contribution
	}
	if finding.Status == SupplyChainImpactNotAffectedKnownFixed && score > 20 {
		contributions = append(contributions, priorityContribution(
			"known_fixed_priority_cap",
			"impact_status",
			string(finding.Status),
			20-score,
		))
		score = 20
	}
	score = clampPriorityScore(score)
	finding.PriorityScore = score
	finding.PriorityBucket = supplyChainPriorityBucket(score)
	finding.PriorityContributions = contributions
	finding.PriorityReasonCodes = supplyChainPriorityReasonCodes(contributions)
	finding.PriorityReason = supplyChainPriorityReason(finding)
	return finding
}

func buildSupplyChainImpactPriorityContributions(
	finding SupplyChainImpactFinding,
) []SupplyChainImpactPriorityContribution {
	contributions := make([]SupplyChainImpactPriorityContribution, 0, 12)
	contributions = append(contributions, cvssPriorityContribution(finding)...)
	contributions = append(contributions, epssPriorityContributions(finding)...)
	if finding.KnownExploited {
		contributions = append(contributions, priorityContribution("cisa_kev", "kev", "true", 30))
	}
	contributions = append(contributions, advisoryAgePriorityContribution(finding)...)
	contributions = append(contributions, dependencyScopePriorityContribution(finding)...)
	contributions = append(contributions, dependencyRelationshipPriorityContribution(finding)...)
	contributions = append(contributions, versionEvidencePriorityContribution(finding)...)
	if finding.SubjectDigest != "" && finding.RuntimeReachability == "image_sbom" {
		contributions = append(contributions, priorityContribution("sbom_image_evidence", "sbom_image", finding.SubjectDigest, 15))
		contributions = append(contributions, priorityContribution("runtime_reachable", "runtime_reachability", finding.RuntimeReachability, 25))
	}
	if finding.Reachability != nil {
		switch finding.Reachability.State {
		case SupplyChainReachabilityReachable:
			if finding.Reachability.Source == "govulncheck" ||
				finding.Reachability.Source == "parser_js_ts" ||
				finding.Reachability.Source == "scip_js_ts" {
				contributions = append(contributions, priorityContribution(
					"reachable_code_evidence",
					"reachability",
					string(finding.Reachability.State),
					20,
				))
			}
		case SupplyChainReachabilityNotCalled:
			contributions = append(contributions, priorityContribution(
				"reachability_not_called",
				"reachability",
				string(finding.Reachability.State),
				-20,
			))
		case SupplyChainReachabilityMissingEvidence:
			if finding.Reachability.Source == "govulncheck" {
				contributions = append(contributions, priorityContribution(
					"reachability_missing_evidence",
					"reachability",
					string(finding.Reachability.State),
					-5,
				))
			}
		}
	}
	if hasDeploymentPriorityEvidence(finding) {
		contributions = append(contributions, priorityContribution("deployed_workload_evidence", "deployment", deploymentPriorityValue(finding), 20))
	}
	if finding.RepositoryID != "" {
		contributions = append(contributions, priorityContribution("owned_repository_evidence", "repository", finding.RepositoryID, 10))
	}
	if finding.FixedVersion != "" {
		contributions = append(contributions, priorityContribution("fixed_version_available", "fixed_version", finding.FixedVersion, 5))
	}
	if finding.Status == SupplyChainImpactNotAffectedKnownFixed {
		contributions = append(contributions, priorityContribution("observed_version_known_fixed", "fixed_version", finding.ObservedVersion, -75))
	}
	if len(finding.MissingEvidence) > 0 {
		contributions = append(contributions, priorityContribution("missing_evidence_present", "missing_evidence", strings.Join(finding.MissingEvidence, ","), -5))
	}
	return contributions
}

func cvssPriorityContribution(finding SupplyChainImpactFinding) []SupplyChainImpactPriorityContribution {
	if finding.CVSSScore <= 0 {
		return nil
	}
	version := cvssVersionReasonSegment(finding.SeverityVector)
	value := fmt.Sprintf("%.1f", finding.CVSSScore)
	switch {
	case finding.CVSSScore >= 9:
		return []SupplyChainImpactPriorityContribution{
			priorityContribution("cvss_"+version+"_critical", "cvss", value, 60),
		}
	case finding.CVSSScore >= 7:
		return []SupplyChainImpactPriorityContribution{
			priorityContribution("cvss_"+version+"_high", "cvss", value, 35),
		}
	case finding.CVSSScore >= 4:
		return []SupplyChainImpactPriorityContribution{
			priorityContribution("cvss_"+version+"_medium", "cvss", value, 10),
		}
	default:
		return []SupplyChainImpactPriorityContribution{
			priorityContribution("cvss_"+version+"_low", "cvss", value, 3),
		}
	}
}

func epssPriorityContributions(finding SupplyChainImpactFinding) []SupplyChainImpactPriorityContribution {
	var contributions []SupplyChainImpactPriorityContribution
	if value, ok := parsePriorityFloat(finding.EPSSProbability); ok {
		switch {
		case value >= 0.5:
			contributions = append(contributions, priorityContribution("epss_high", "epss_probability", finding.EPSSProbability, 20))
		case value >= 0.1:
			contributions = append(contributions, priorityContribution("epss_elevated", "epss_probability", finding.EPSSProbability, 12))
		case value > 0:
			contributions = append(contributions, priorityContribution("epss_observed", "epss_probability", finding.EPSSProbability, 4))
		}
	}
	if value, ok := parsePriorityFloat(finding.EPSSPercentile); ok {
		switch {
		case value >= 0.95:
			contributions = append(contributions, priorityContribution("epss_percentile_high", "epss_percentile", finding.EPSSPercentile, 20))
		case value >= 0.8:
			contributions = append(contributions, priorityContribution("epss_percentile_elevated", "epss_percentile", finding.EPSSPercentile, 10))
		}
	}
	return contributions
}

func advisoryAgePriorityContribution(finding SupplyChainImpactFinding) []SupplyChainImpactPriorityContribution {
	published, ok := parsePriorityTime(finding.AdvisoryPublishedAt)
	if !ok {
		return nil
	}
	updated, ok := parsePriorityTime(finding.AdvisoryUpdatedAt)
	if !ok || updated.Before(published) {
		return nil
	}
	days := int(math.Round(updated.Sub(published).Hours() / 24))
	value := strconv.Itoa(days)
	switch {
	case days <= 30:
		return []SupplyChainImpactPriorityContribution{
			priorityContribution("advisory_age_recent", "advisory_age_days", value, 5),
		}
	case days <= 180:
		return []SupplyChainImpactPriorityContribution{
			priorityContribution("advisory_age_maturing", "advisory_age_days", value, 3),
		}
	default:
		return []SupplyChainImpactPriorityContribution{
			priorityContribution("advisory_age_mature", "advisory_age_days", value, 1),
		}
	}
}

func dependencyScopePriorityContribution(finding SupplyChainImpactFinding) []SupplyChainImpactPriorityContribution {
	scope := normalizePriorityToken(finding.DependencyScope)
	switch scope {
	case "dev", "development", "devdependencies", "test", "tests", "optional", "peer":
		return []SupplyChainImpactPriorityContribution{
			priorityContribution("dependency_scope_dev", "dependency_scope", finding.DependencyScope, -35),
		}
	case "runtime", "production", "prod", "dependencies", "dependency":
		return []SupplyChainImpactPriorityContribution{
			priorityContribution("dependency_scope_runtime", "dependency_scope", finding.DependencyScope, 8),
		}
	default:
		return nil
	}
}

func dependencyRelationshipPriorityContribution(finding SupplyChainImpactFinding) []SupplyChainImpactPriorityContribution {
	if finding.DirectDependency == nil {
		return nil
	}
	if *finding.DirectDependency {
		return []SupplyChainImpactPriorityContribution{
			priorityContribution("direct_dependency", "dependency_relationship", "direct", 8),
		}
	}
	return []SupplyChainImpactPriorityContribution{
		priorityContribution("transitive_dependency", "dependency_relationship", "transitive", 3),
	}
}

func versionEvidencePriorityContribution(finding SupplyChainImpactFinding) []SupplyChainImpactPriorityContribution {
	if finding.ObservedVersion != "" {
		return []SupplyChainImpactPriorityContribution{
			priorityContribution("exact_version_evidence", "version_evidence", finding.ObservedVersion, 15),
		}
	}
	if finding.RequestedRange != "" {
		return []SupplyChainImpactPriorityContribution{
			priorityContribution("range_only_version_evidence", "version_evidence", finding.RequestedRange, -5),
		}
	}
	return []SupplyChainImpactPriorityContribution{
		priorityContribution("missing_version_evidence", "version_evidence", "missing", -10),
	}
}

func hasDeploymentPriorityEvidence(finding SupplyChainImpactFinding) bool {
	return len(finding.WorkloadIDs) > 0 || len(finding.ServiceIDs) > 0 ||
		len(finding.Environments) > 0
}

func deploymentPriorityValue(finding SupplyChainImpactFinding) string {
	var values []string
	values = append(values, finding.WorkloadIDs...)
	values = append(values, finding.ServiceIDs...)
	values = append(values, finding.Environments...)
	return strings.Join(uniqueSortedStrings(values), ",")
}

func priorityContribution(
	code string,
	input string,
	value string,
	contribution int,
) SupplyChainImpactPriorityContribution {
	return SupplyChainImpactPriorityContribution{
		ReasonCode:   strings.TrimSpace(code),
		Input:        strings.TrimSpace(input),
		Value:        strings.TrimSpace(value),
		Contribution: contribution,
	}
}

func supplyChainPriorityReasonCodes(contributions []SupplyChainImpactPriorityContribution) []string {
	codes := make([]string, 0, len(contributions))
	for _, contribution := range contributions {
		codes = append(codes, contribution.ReasonCode)
	}
	return uniqueSortedStrings(codes)
}

func supplyChainPriorityReason(finding SupplyChainImpactFinding) string {
	if finding.PriorityBucket == "" {
		return ""
	}
	return fmt.Sprintf(
		"%s triage priority score %d; impact_status remains %s and priority does not prove affected truth",
		finding.PriorityBucket,
		finding.PriorityScore,
		finding.Status,
	)
}

func supplyChainPriorityBucket(score int) string {
	switch {
	case score >= 95:
		return "critical"
	case score >= 60:
		return "high"
	case score >= 30:
		return "medium"
	case score > 0:
		return "low"
	default:
		return "informational"
	}
}

func clampPriorityScore(score int) int {
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

func cvssVersionReasonSegment(vector string) string {
	vector = strings.ToUpper(strings.TrimSpace(vector))
	switch {
	case strings.HasPrefix(vector, "CVSS:4."):
		return "v4"
	case strings.HasPrefix(vector, "CVSS:3."):
		return "v3"
	default:
		return "cvss"
	}
}

func parsePriorityFloat(raw string) (float64, bool) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return value, err == nil
}

func parsePriorityTime(raw string) (time.Time, bool) {
	value, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	return value, err == nil
}

func normalizePriorityToken(raw string) string {
	return strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(raw)))
}
