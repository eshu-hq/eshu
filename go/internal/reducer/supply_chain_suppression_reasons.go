// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func suppressionStateForJustification(justification string) SupplyChainSuppressionState {
	switch strings.TrimSpace(justification) {
	case facts.VulnerabilitySuppressionJustificationNotAffected:
		return SupplyChainSuppressionStateNotAffected
	case facts.VulnerabilitySuppressionJustificationAcceptedRisk:
		return SupplyChainSuppressionStateAcceptedRisk
	case facts.VulnerabilitySuppressionJustificationFalsePositive:
		return SupplyChainSuppressionStateFalsePositive
	case facts.VulnerabilitySuppressionJustificationIgnored:
		return SupplyChainSuppressionStateIgnored
	case facts.VulnerabilitySuppressionJustificationProviderDismissed:
		return SupplyChainSuppressionStateProviderDismissed
	default:
		return SupplyChainSuppressionStateActive
	}
}

func suppressionReasonOrDefault(s vulnerabilitySuppression, state SupplyChainSuppressionState) string {
	if r := strings.TrimSpace(s.Reason); r != "" {
		return r
	}
	return fmt.Sprintf("suppression %s asserted %s by %s", s.SuppressionID, state, defaultIfBlank(s.Author, s.Source))
}

func suppressionScopeMismatchReason(finding SupplyChainImpactFinding, s vulnerabilitySuppression) string {
	if suppressionScopeIsEmpty(s.Scope) {
		return fmt.Sprintf(
			"suppression %s scope mismatch: empty scope; an applied scope MUST specify at least one of cve_id, advisory_id, package_id, purl, repository_id, subject_digest, or evidence_path so a malformed fact cannot hide every finding",
			s.SuppressionID,
		)
	}
	var diffs []string
	if s.Scope.CVEID != "" && !strings.EqualFold(s.Scope.CVEID, finding.CVEID) {
		diffs = append(diffs, fmt.Sprintf("cve_id=%q vs finding %q", s.Scope.CVEID, finding.CVEID))
	}
	if s.Scope.AdvisoryID != "" && !strings.EqualFold(s.Scope.AdvisoryID, finding.AdvisoryID) {
		diffs = append(diffs, fmt.Sprintf("advisory_id=%q vs finding %q", s.Scope.AdvisoryID, finding.AdvisoryID))
	}
	if s.Scope.PackageID != "" && !strings.EqualFold(s.Scope.PackageID, finding.PackageID) {
		diffs = append(diffs, fmt.Sprintf("package_id=%q vs finding %q", s.Scope.PackageID, finding.PackageID))
	}
	if s.Scope.PURL != "" && !strings.EqualFold(s.Scope.PURL, finding.PURL) {
		diffs = append(diffs, fmt.Sprintf("purl=%q vs finding %q", s.Scope.PURL, finding.PURL))
	}
	if s.Scope.RepositoryID != "" && !strings.EqualFold(s.Scope.RepositoryID, finding.RepositoryID) {
		diffs = append(diffs, fmt.Sprintf("repository_id=%q vs finding %q", s.Scope.RepositoryID, finding.RepositoryID))
	}
	if s.Scope.SubjectDigest != "" && !strings.EqualFold(s.Scope.SubjectDigest, finding.SubjectDigest) {
		diffs = append(diffs, fmt.Sprintf("subject_digest=%q vs finding %q", s.Scope.SubjectDigest, finding.SubjectDigest))
	}
	if !evidencePathContainsAll(finding.EvidencePath, s.Scope.EvidencePath) {
		diffs = append(diffs, fmt.Sprintf("evidence_path %v not satisfied by finding %v", s.Scope.EvidencePath, finding.EvidencePath))
	}
	if len(diffs) == 0 {
		diffs = append(diffs, "scope anchors did not match the finding")
	}
	return fmt.Sprintf("suppression %s scope mismatch: %s", s.SuppressionID, strings.Join(diffs, "; "))
}
