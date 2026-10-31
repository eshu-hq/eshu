// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

const (
	supplyChainVersionReasonAdvisoryFixedConflict = "advisory_fixed_version_conflict"
	supplyChainMissingAdvisoryFixedConflict       = "authorized advisory fixed versions conflict"
	supplyChainVersionReasonBranchUnknown         = "advisory_branch_applicability_unknown"
	supplyChainMissingBranchUnknown               = "advisory branch applicability unknown"
)

// markAuthorizedFixedVersionConflicts withholds a disputed fix when two
// current authorized observations of one advisory and applicable branch
// disagree. Fact order, payload source labels, and disclosure class
// never choose a winner.
func markAuthorizedFixedVersionConflicts(
	findings []SupplyChainImpactFinding,
	advisories []facts.Envelope,
) {
	affectedCount := 0
	for _, envelope := range advisories {
		if envelope.FactKind == chain.VulnerabilityAffectedPackageFactKind {
			affectedCount++
		}
	}
	if affectedCount < 2 {
		return
	}
	packages := make([]supplychainmodel.AffectedPackage, 0, affectedCount)
	for _, envelope := range advisories {
		if envelope.FactKind != chain.VulnerabilityAffectedPackageFactKind {
			continue
		}
		pkg, err := supplyChainAffectedPackageFromEnvelope(envelope)
		if err != nil || pkg.PackageID == "" {
			continue // The classifier already quarantines malformed advisory facts.
		}
		packages = append(packages, pkg)
	}
	for i := range findings {
		finding := &findings[i]
		conflict := false
		for left := 0; left < len(packages) && !conflict; left++ {
			for right := left + 1; right < len(packages); right++ {
				first, second := packages[left], packages[right]
				if !sameAuthorizedAdvisoryPackage(first, second, *finding) ||
					fixedVersionsEquivalent(first.FixedVersions, second.FixedVersions) ||
					provenDisjointAffectedRanges(first, second) {
					continue
				}
				conflict = true
				break
			}
		}
		if !conflict {
			selectApplicableAuthorizedBranch(finding, packages)
			continue
		}
		finding.Status = SupplyChainImpactPossiblyAffected
		finding.Confidence = "partial"
		finding.MatchReason = supplyChainVersionReasonAdvisoryFixedConflict
		finding.FixedVersion = ""
		finding.FixedVersionSource = ""
		finding.VulnerableRange = ""
		finding.RangeSource = ""
		finding.MissingEvidence = payloadcore.UniqueSortedStrings(append(finding.MissingEvidence, supplyChainMissingAdvisoryFixedConflict))
		finding.Remediation = BuildSupplyChainImpactRemediation(*finding)
		finding.Remediation.FirstPatchedVersion = ""
		finding.Remediation.Confidence = SupplyChainRemediationConfidencePartial
		finding.Remediation.Reason = SupplyChainRemediationReasonMultiplePatchedBranches
		finding.Remediation.ManifestAllowsFix = SupplyChainRemediationManifestUnknown
		finding.Remediation.MissingEvidence = payloadcore.UniqueSortedStrings(append(finding.Remediation.MissingEvidence, SupplyChainRemediationMissingFixedBranchAmbiguous))
		*finding = withSupplyChainImpactPriority(*finding)
	}
}

func sameAuthorizedAdvisoryPackage(left, right supplychainmodel.AffectedPackage, finding SupplyChainImpactFinding) bool {
	return left.CVEID == finding.CVEID && right.CVEID == finding.CVEID &&
		left.AdvisoryID == finding.AdvisoryID && right.AdvisoryID == finding.AdvisoryID &&
		left.PackageID == finding.PackageID && right.PackageID == finding.PackageID
}

func fixedVersionsEquivalent(first, second []string) bool {
	left := payloadcore.UniqueSortedStrings(slices.Clone(first))
	right := payloadcore.UniqueSortedStrings(slices.Clone(second))
	return slices.Equal(left, right)
}

// provenDisjointAffectedRanges accepts only a simple, comparable pair of
// explicit half-open ecosystem intervals. Any richer or malformed range is
// uncertain, so the conflict remains visible.
func provenDisjointAffectedRanges(first, second supplychainmodel.AffectedPackage) bool {
	if first.Ecosystem != second.Ecosystem || affectedVersionSetsOverlap(first.AffectedVersions, second.AffectedVersions) {
		return false
	}
	compare := rangeComparator(first.Ecosystem)
	if compare == nil {
		return false
	}
	leftStart, leftEnd, leftOK := simpleAffectedInterval(first)
	rightStart, rightEnd, rightOK := simpleAffectedInterval(second)
	if !leftOK || !rightOK || len(first.FixedVersions) != 1 || len(second.FixedVersions) != 1 ||
		first.FixedVersions[0] != leftEnd || second.FixedVersions[0] != rightEnd {
		return false
	}
	if cmp, ok := compare(leftStart, leftEnd); !ok || cmp >= 0 {
		return false
	}
	if cmp, ok := compare(rightStart, rightEnd); !ok || cmp >= 0 {
		return false
	}
	if !affectedVersionsWithinInterval(first.AffectedVersions, leftStart, leftEnd, compare) ||
		!affectedVersionsWithinInterval(second.AffectedVersions, rightStart, rightEnd, compare) {
		return false
	}
	if cmp, ok := compare(leftEnd, rightStart); ok && cmp <= 0 {
		return true
	}
	if cmp, ok := compare(rightEnd, leftStart); ok && cmp <= 0 {
		return true
	}
	return false
}

func affectedVersionsWithinInterval(versions []string, start, end string, compare versionCompareFunc) bool {
	for _, version := range versions {
		fromStart, startOK := compare(version, start)
		beforeEnd, endOK := compare(version, end)
		if !startOK || !endOK || fromStart < 0 || beforeEnd >= 0 {
			return false
		}
	}
	return true
}

func simpleAffectedInterval(pkg supplychainmodel.AffectedPackage) (string, string, bool) {
	if len(pkg.AffectedRanges) != 1 || len(pkg.AffectedRanges[0].Events) != 2 ||
		!strings.EqualFold(pkg.AffectedRanges[0].Kind, "ECOSYSTEM") {
		return "", "", false
	}
	events := pkg.AffectedRanges[0].Events
	if events[0].Introduced == "" || events[1].Fixed == "" ||
		events[0].Fixed != "" || events[0].Limit != "" || events[0].LastAffected != "" ||
		events[1].Introduced != "" || events[1].Limit != "" || events[1].LastAffected != "" {
		return "", "", false
	}
	return events[0].Introduced, events[1].Fixed, true
}

func rangeComparator(ecosystem string) versionCompareFunc {
	switch strings.ToLower(ecosystem) {
	case "deb", "debian", "ubuntu", "dpkg":
		return compareDPKGVersion
	case "apk", "alpine":
		return compareAPKVersion
	default:
		return nil
	}
}

func affectedVersionSetsOverlap(first, second []string) bool {
	for _, left := range first {
		for _, right := range second {
			if strings.TrimSpace(left) != "" && left == right {
				return true
			}
		}
	}
	return false
}

// selectApplicableAuthorizedBranch chooses a fixed version only when explicit
// disjoint ranges prove one branch applies to this observed installation.
func selectApplicableAuthorizedBranch(
	finding *SupplyChainImpactFinding,
	packages []supplychainmodel.AffectedPackage,
) {
	branches := make([]supplychainmodel.AffectedPackage, 0)
	for _, pkg := range packages {
		if sameAuthorizedAdvisoryPackage(pkg, pkg, *finding) {
			branches = append(branches, pkg)
		}
	}
	if len(branches) < 2 {
		return
	}
	firstFixed := branches[0].FixedVersions
	differentFixes := false
	for _, branch := range branches[1:] {
		if !fixedVersionsEquivalent(firstFixed, branch.FixedVersions) {
			differentFixes = true
			break
		}
	}
	if !differentFixes {
		return
	}
	compare := rangeComparator(finding.Ecosystem)
	if compare == nil {
		markUnknownAuthorizedBranch(finding)
		return
	}
	applicable := make([]supplychainmodel.AffectedPackage, 0, 1)
	for _, branch := range branches {
		start, end, ok := simpleAffectedInterval(branch)
		if !ok || len(branch.FixedVersions) != 1 || branch.FixedVersions[0] != end {
			markUnknownAuthorizedBranch(finding)
			return
		}
		afterStart, startOK := compare(finding.ObservedVersion, start)
		beforeEnd, endOK := compare(finding.ObservedVersion, end)
		if !startOK || !endOK {
			markUnknownAuthorizedBranch(finding)
			return
		}
		if afterStart >= 0 && beforeEnd < 0 {
			applicable = append(applicable, branch)
		}
	}
	if len(applicable) != 1 {
		markUnknownAuthorizedBranch(finding)
		return
	}
	selected := applicable[0]
	finding.FixedVersion = selected.FixedVersions[0]
	finding.FixedVersionSource = classifyAffectedPackageAdvisorySource(selected)
	finding.VulnerableRange = supplyChainAffectedRangeSummary(selected)
	finding.RangeSource = finding.FixedVersionSource
	decision := evaluateSupplyChainVersionMatch(finding.Ecosystem, finding.ObservedVersion, finding.RequestedRange, finding.FixedVersion, []supplychainmodel.AffectedPackage{selected})
	finding.Status = decision.Status
	finding.Confidence = decision.Confidence
	finding.MatchReason = decision.Reason
	finding.MissingEvidence = payloadcore.UniqueSortedStrings(append(finding.MissingEvidence, decision.MissingEvidence...))
	if finding.Status == SupplyChainImpactAffectedExact {
		finding.RuntimeReachability = "image_os_package"
	}
	finding.DetectionProfile = classifySupplyChainImpactDetectionProfile(*finding)
	*finding = withSupplyChainReachability(*finding)
	*finding = withSupplyChainImpactPriority(*finding)
	finding.Remediation = BuildSupplyChainImpactRemediation(*finding)
}

func markUnknownAuthorizedBranch(finding *SupplyChainImpactFinding) {
	finding.Status = SupplyChainImpactPossiblyAffected
	finding.Confidence = "partial"
	finding.MatchReason = supplyChainVersionReasonBranchUnknown
	finding.FixedVersion = ""
	finding.FixedVersionSource = ""
	finding.VulnerableRange = ""
	finding.RangeSource = ""
	finding.MissingEvidence = payloadcore.UniqueSortedStrings(append(finding.MissingEvidence, supplyChainMissingBranchUnknown))
	finding.Remediation = BuildSupplyChainImpactRemediation(*finding)
	finding.Remediation.FirstPatchedVersion = ""
	finding.Remediation.Confidence = SupplyChainRemediationConfidencePartial
	finding.Remediation.Reason = SupplyChainRemediationReasonMultiplePatchedBranches
	finding.Remediation.ManifestAllowsFix = SupplyChainRemediationManifestUnknown
	finding.Remediation.MissingEvidence = payloadcore.UniqueSortedStrings(append(finding.Remediation.MissingEvidence, SupplyChainRemediationMissingFixedBranchAmbiguous))
	*finding = withSupplyChainImpactPriority(*finding)
}
