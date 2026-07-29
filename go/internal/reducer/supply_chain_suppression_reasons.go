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
			"suppression %s scope mismatch: empty scope; an applied scope MUST specify at least one of cve_id, advisory_id, package_id, purl, repository_id, subject_digest, evidence_path, environment, workload_id, or service_id so a malformed fact cannot hide every finding",
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
	if s.Scope.Environment != "" && !scopeListAnchorMatches(s.Scope.Environment, finding.Environments) {
		diffs = append(diffs, fmt.Sprintf("environment=%q vs finding %v", s.Scope.Environment, finding.Environments))
	}
	if s.Scope.WorkloadID != "" && !scopeListAnchorMatches(s.Scope.WorkloadID, finding.WorkloadIDs) {
		diffs = append(diffs, fmt.Sprintf("workload_id=%q vs finding %v", s.Scope.WorkloadID, finding.WorkloadIDs))
	}
	if s.Scope.ServiceID != "" && !scopeListAnchorMatches(s.Scope.ServiceID, finding.ServiceIDs) {
		diffs = append(diffs, fmt.Sprintf("service_id=%q vs finding %v", s.Scope.ServiceID, finding.ServiceIDs))
	}
	// #5466 round-7 review P1-A follow-up: distinguish "the anchors
	// genuinely did not match" from "the anchors could not be verified
	// TOGETHER" -- without this, a multi-anchor scope that fails ONLY the
	// suppressionDeploymentContextUnambiguous guard (every individual
	// scopeListAnchorMatches call above passes, since each value really is
	// somewhere in its own list) would add nothing to diffs and fall
	// through to the generic "scope anchors did not match the finding"
	// below, which is actively misleading here: the operator's values were
	// each individually correct, the combination just could not be
	// verified against a single deployment context. This check is cheap
	// (reuses the same pure function the matcher already calls) so there is
	// no reason not to make it precise.
	if ambiguous := suppressionAmbiguousCombinationDiff(finding, s.Scope); ambiguous != "" {
		diffs = append(diffs, ambiguous)
	}
	if len(diffs) == 0 {
		diffs = append(diffs, "scope anchors did not match the finding")
	}
	return fmt.Sprintf("suppression %s scope mismatch: %s", s.SuppressionID, strings.Join(diffs, "; "))
}

// suppressionAmbiguousCombinationDiff returns a non-empty diagnostic when
// suppressionDeploymentContextUnambiguous would reject scope for finding,
// naming exactly which dimension(s) have more than one distinct value on
// the finding so an operator can tell this apart from a real value
// mismatch. Returns "" when the scope is unambiguous (including every
// single-anchor scope, since suppressionDeploymentContextUnambiguous is a
// no-op below two referenced dimensions).
func suppressionAmbiguousCombinationDiff(finding SupplyChainImpactFinding, scope vulnerabilitySuppressionScope) string {
	if suppressionDeploymentContextUnambiguous(finding, scope) {
		return ""
	}
	var ambiguous []string
	if strings.TrimSpace(scope.Environment) != "" && distinctNonEmptyValueCount(finding.Environments) > 1 {
		ambiguous = append(ambiguous, fmt.Sprintf("environment (finding evidence: %v)", finding.Environments))
	}
	if strings.TrimSpace(scope.WorkloadID) != "" && distinctNonEmptyValueCount(finding.WorkloadIDs) > 1 {
		ambiguous = append(ambiguous, fmt.Sprintf("workload_id (finding evidence: %v)", finding.WorkloadIDs))
	}
	if strings.TrimSpace(scope.ServiceID) != "" && distinctNonEmptyValueCount(finding.ServiceIDs) > 1 {
		ambiguous = append(ambiguous, fmt.Sprintf("service_id (finding evidence: %v)", finding.ServiceIDs))
	}
	return fmt.Sprintf(
		"multi-anchor combination unverifiable, not necessarily a value mismatch: the finding aggregates multiple deployments with more than one distinct value in %s, so this scope cannot be checked against a single deployment context and fails closed (#5466 round-7 review P1-A)",
		strings.Join(ambiguous, ", "),
	)
}
