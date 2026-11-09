// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
)

// BuildSupplyChainImpactFindings classifies vulnerability source facts against
// explicit package, SBOM, image, and repository evidence.
//
// Multi-source CVE and affected_package observations for the same advisory
// identity are consolidated into one finding so callers see a single row per
// (cve_id, package_id) anchor with full per-source provenance, instead of one
// row per advisory source overwriting the others at the writer.
//
// A vulnerability.* fact whose payload is missing a required identity field is
// excluded from the index (mirroring the pre-typing behavior of dropping a
// fact with a blank required string), matching this function's fixed,
// error-free signature that the existing table tests already assert against.
// SupplyChainImpactHandler.Handle calls the quarantine-aware
// buildSupplyChainImpactFindingsWithQuarantine instead, so the reducer intent
// path still reports a visible input_invalid dead-letter (counter + structured
// log) for the malformed fact while this function stays a pure,
// table-test-friendly classifier with no telemetry side effects.
func BuildSupplyChainImpactFindings(envelopes []facts.Envelope, configs ...supplyChainAliasGroupConfig) []SupplyChainImpactFinding {
	findings, _, _ := buildSupplyChainImpactFindingsWithQuarantine(envelopes, configs...)
	return findings
}

// negatedProductsExcludedSubSignal is the Result.SubSignals key counting NVD CPE
// matches a pass excluded as negated; 0 when none, so a clean pass is visible (#9696).
const negatedProductsExcludedSubSignal = "negated_affected_products_excluded"

// buildSupplyChainImpactFindingsWithQuarantine is the quarantine-aware
// counterpart BuildSupplyChainImpactFindings delegates to. It wraps
// buildSupplyChainImpactFindingsAndExclusions, which Handle calls so the intent
// path can report each malformed vulnerability.* fact as a visible input_invalid
// dead-letter via factdecode.RecordQuarantinedFacts. A non-decode error is
// returned so the caller fails the whole intent for durable triage.
func buildSupplyChainImpactFindingsWithQuarantine(envelopes []facts.Envelope, configs ...supplyChainAliasGroupConfig) ([]SupplyChainImpactFinding, []factdecode.QuarantinedFact, error) {
	findings, quarantined, _, err := buildSupplyChainImpactFindingsAndExclusions(envelopes, configs...)
	return findings, quarantined, err
}

// buildSupplyChainImpactFindingsAndExclusions is the quarantine-aware builder plus
// the pass counts in supplyChainImpactBuildStats (negated CPE matches, OS joins).
func buildSupplyChainImpactFindingsAndExclusions(envelopes []facts.Envelope, configs ...supplyChainAliasGroupConfig) ([]SupplyChainImpactFinding, []factdecode.QuarantinedFact, supplyChainImpactBuildStats, error) {
	index, quarantined, err := buildSupplyChainImpactIndexWithQuarantine(envelopes, configs...)
	if err != nil {
		return nil, nil, supplyChainImpactBuildStats{}, err
	}
	index.osRepresentativeOutcomes = make(map[osRepresentativeOutcomeKey]int)
	findings := appendSupplyChainImpactIndexFindings(nil, index)
	findings, securityAlertQuarantined, err := appendSecurityAlertImpactFindings(findings, envelopes, index)
	if err != nil {
		return nil, nil, supplyChainImpactBuildStats{}, err
	}
	// Merge the security-alert decode quarantines with the vulnerability
	// quarantines so SupplyChainImpactHandler.Handle records every malformed
	// fact of either family as a per-fact input_invalid dead-letter through the
	// same factdecode.RecordQuarantinedFacts path, and a poisoned security_alert fact never
	// aborts the whole supply_chain_impact generation.
	quarantined = append(quarantined, securityAlertQuarantined...)
	findings = coalesceAndSortSupplyChainImpactFindings(findings)
	return findings, quarantined, supplyChainImpactBuildStats{
		negatedProductsExcluded:     index.negatedProductsExcluded,
		osAdvisoryJoins:             countOSAdvisoryJoins(index),
		osRepresentativeOutcomes:    index.osRepresentativeOutcomes,
		osAdvisoriesJoinedViaSource: countOSAdvisoriesJoinedViaSource(findings),
		osReleaseNotInAdvisory:      countOSReleaseNotInAdvisory(findings),
		osUpstreamGroups:            index.osUpstream.cells,
		aliasGroups:                 index.aliasStats.cells,
		aliasGroupCount:             index.aliasStats.groups,
		aliasGroupSizes:             index.aliasStats.sizes,
		aliasGroupCompute:           index.aliasStats.compute,
	}, nil
}

func appendSupplyChainImpactFinding(
	findings []SupplyChainImpactFinding,
	finding SupplyChainImpactFinding,
) []SupplyChainImpactFinding {
	if !supplyChainImpactFindingHasOwnedAnchor(finding) {
		return findings
	}
	if supplyChainImpactFindingHasUnsupportedMatcher(finding) {
		return findings
	}
	finding.DetectionProfile = classifySupplyChainImpactDetectionProfile(finding)
	finding = withSupplyChainReachability(finding)
	finding = withSupplyChainImpactPriority(finding)
	finding.Remediation = BuildSupplyChainImpactRemediation(finding)
	return append(findings, finding)
}

// supplyChainImpactFindingHasOwnedAnchor reports whether finding is backed by
// evidence this reducer actually observed, so appendSupplyChainImpactFinding
// keeps it rather than dropping an unanchored guess. RepositoryID and
// SubjectDigest are the two general anchors every ecosystem can supply. An
// os_package match is its own anchor even when neither of those is set: it
// already required repositoryClass=="vendor" plus a matching
// VendorAdvisorySource (osPackageMatchesAffectedPackage) before
// classifySupplyChainImpactPackage ever reached this finding, so the finding
// is anchored to a real scanned installation, not a guess — its accuracy does
// not depend on whether a sibling scanner_worker.analysis fact happened to
// resolve a real image digest for it (issue #5463 deleted the scope_id
// fallback that used to make SubjectDigest non-blank here as a side effect;
// this keeps the owned-anchor gate independently correct without resurrecting
// scope_id as a fake digest).
func supplyChainImpactFindingHasOwnedAnchor(finding SupplyChainImpactFinding) bool {
	if strings.TrimSpace(finding.RepositoryID) != "" || strings.TrimSpace(finding.SubjectDigest) != "" {
		return true
	}
	return slices.Contains(finding.EvidencePath, chain.VulnerabilityOSPackageFactKind)
}

// supplyChainImpactFindingHasUnsupportedMatcher reports a finding with no version
// matcher and nothing else to stand on (#9990): a joined installed row keeps it
// visible as possibly_affected with the matcher reported missing.
func supplyChainImpactFindingHasUnsupportedMatcher(finding SupplyChainImpactFinding) bool {
	if strings.TrimSpace(finding.MatchReason) != supplyChainVersionReasonUnsupportedEcosystem {
		return false
	}
	return normalizedSupplyChainVersionEcosystem(finding.Ecosystem) != "os" &&
		!slices.Contains(finding.EvidencePath, chain.VulnerabilityOSPackageFactKind)
}
