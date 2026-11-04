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
//
// osPackages carries the scan target's installed rows so each finding can be
// matched to its host release line (the same row the primary path narrows
// release-tagged facts against). Findings with no matching row keep the
// existing containment behavior: without a host line no branch can be proven
// foreign.
func markAuthorizedFixedVersionConflicts(
	findings []SupplyChainImpactFinding,
	advisories []facts.Envelope,
	osPackages []supplychainmodel.OSPackage,
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
	hostLineByRow := make(map[string]string, len(osPackages))
	for _, row := range osPackages {
		if row.FactID == "" {
			continue
		}
		if _, ok := hostLineByRow[row.FactID]; !ok {
			hostLineByRow[row.FactID] = osHostReleaseLine(row.Distro, row.DistroVersion)
		}
	}
	for i := range findings {
		finding := &findings[i]
		hostLine := authorizedFindingHostLine(*finding, hostLineByRow)
		var related []supplychainmodel.AffectedPackage
		for _, pkg := range packages {
			if sameAuthorizedAdvisoryPackage(pkg, pkg, *finding) {
				related = append(related, pkg)
			}
		}
		// Only the host line's branches may contend. A tagged branch that
		// names no range for the installed row is another line's business:
		// the primary path already blanked its fix at the narrowing
		// boundary, so judging it here would present a foreign fix as the
		// host's own. When none qualifies the finding is left untouched and
		// keeps its primary verdict (not-in-advisory precision, #9806 P1).
		candidates := filterAuthorizedBranchesToHostLine(related, hostLine)
		if len(candidates) == 0 {
			continue
		}
		conflict := false
		for left := 0; left < len(candidates) && !conflict; left++ {
			for right := left + 1; right < len(candidates); right++ {
				first, second := candidates[left], candidates[right]
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
			selectApplicableAuthorizedBranch(finding, candidates, hostLine)
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

// authorizedFindingHostLine returns the installed row's release line for the
// finding, or "" when the finding names no known row. The finding carries its
// installed row's fact ID in EvidenceFactIDs (the primary path stamps
// osPackage.FactID there when it builds the finding), so the conflict path
// resolves the same host line the primary path narrowed facts against. An
// unknown line is not evidence of anything: without it no branch can be
// proven foreign, so callers keep the existing behavior.
func authorizedFindingHostLine(finding SupplyChainImpactFinding, hostLineByRow map[string]string) string {
	for _, factID := range finding.EvidenceFactIDs {
		if hostLine, ok := hostLineByRow[factID]; ok {
			return hostLine
		}
	}
	return ""
}

// filterAuthorizedBranchesToHostLine keeps the branches that may describe the
// finding's installed release line. Untagged facts always qualify: the primary
// path passes them through un-narrowed, so they contend exactly as before. A
// vendor-sourced fact qualifies unless its purl names another release than the
// host's (osVendorFactForOtherLine, #9945): its fix is that release's, so it
// must not be picked as the host's; a vendor fact with no readable line contends
// as before. A release-tagged fact qualifies only through a range that names the
// host line; a tagged fact naming only other lines is dropped.
//
// An empty hostLine disables the filter: without the installed row no branch
// can be proven foreign, so the existing containment behavior stands. When
// the line is known but nothing qualifies, the caller leaves the finding
// untouched, preserving the primary not-in-advisory precision.
func filterAuthorizedBranchesToHostLine(
	branches []supplychainmodel.AffectedPackage,
	hostLine string,
) []supplychainmodel.AffectedPackage {
	if hostLine == "" {
		return branches
	}
	var out []supplychainmodel.AffectedPackage
	for _, branch := range branches {
		if !osAdvisoryFactSelectable(branch) {
			if !osVendorFactForOtherLine(branch, hostLine) {
				out = append(out, branch)
			}
			continue
		}
		for _, affectedRange := range branch.AffectedRanges {
			if strings.EqualFold(affectedRange.Kind, "ECOSYSTEM") &&
				slices.Contains(affectedRange.Releases, hostLine) {
				out = append(out, branch)
				break
			}
		}
	}
	return out
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
// hostLine is the finding's installed release line ("ubuntu:22.04"): tagged
// branches naming only other lines are filtered before containment, so a
// cross-release fix can never present as the host's own (#9806 P1).
func selectApplicableAuthorizedBranch(
	finding *SupplyChainImpactFinding,
	packages []supplychainmodel.AffectedPackage,
	hostLine string,
) {
	branches := make([]supplychainmodel.AffectedPackage, 0)
	for _, pkg := range packages {
		if sameAuthorizedAdvisoryPackage(pkg, pkg, *finding) {
			branches = append(branches, pkg)
		}
	}
	branches = filterAuthorizedBranchesToHostLine(branches, hostLine)
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
